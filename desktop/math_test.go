package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

func TestNativeMathBayesAndThemeScreenshots(t *testing.T) {
	src, err := os.ReadFile("../testdata/client-math/bayes.md")
	if err != nil {
		t.Fatal(err)
	}
	a := newApplication(true)
	defer a.cancel()
	opts := &markdown.Options{}
	a.configureMath(opts)
	theme := ui.LightTheme()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		ui.Scroll(c).Fill().Padding(24).Children(func() { a.markdownView(c, string(src), opts) })
	}, 1000, 1500)
	end := time.Now().Add(15 * time.Second)
	for {
		tt.Frame()
		if a.math != nil {
			_, pending := a.math.cache.State()
			if !pending {
				tt.Frame()
				break
			}
		}
		if time.Now().After(end) {
			t.Fatal("math rendering timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(a.math.images) != 11 {
		t.Fatalf("only %d/11 formulas rendered", len(a.math.images))
	}
	for formula, image := range a.math.images {
		if image == nil || formula.Width <= 0 {
			t.Fatal("formula failed")
		}
	}
	for _, palette := range interfacePalettes {
		a.appearance.Application = palette.id
		theme = a.interfaceTheme(ui.LightTheme())
		tt.Frame()
		// Native image rendering, not only testing whether a parser recognized TeX.
		im := tt.Image()
		path := os.Getenv("HERDR_MATH_SCREENSHOTS")
		if path != "" {
			if err := os.MkdirAll(path, 0755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(path, palette.id+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, im)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	inline, ok := opts.InlineMath(`p(A\mid \mathbf x)`, 16)
	if !ok || inline.Width <= 0 || inline.Height <= 0 || inline.Depth < 0 {
		t.Fatal("inline baseline missing")
	}
}

func TestMathHooksInConversationAndDocument(t *testing.T) {
	a := newApplication(true)
	defer a.cancel()
	a.loadDemo()
	m, chat := a.selected()
	a.configureChatMarkdown(m, chat)
	f := filesFor(chat)
	a.configureFileMarkdown(m, chat, f)
	for _, o := range []*markdown.Options{chat.md, f.md} {
		if o.Math == nil || o.InlineMath == nil || o.Extensions&parser.ExtDollarMath == 0 || o.OnLink == nil {
			t.Fatal("missing math or file-link hook")
		}
	}
}

func TestNativeMathLeavesCodeCurrencyAndIncompleteStreamsAlone(t *testing.T) {
	for _, src := range []string{"`$x$`", "```tex\n\\[x\\]\n```", `价格 $5 和 $10。`, `\(x_i`, `前 \(x_a_b`, `\[` + "\nx_i", `\$x\$`} {
		a := newApplication(true)
		opts := &markdown.Options{}
		a.configureMath(opts)
		tt := ui.NewTester(func(c *ui.Context) { a.markdownView(c, src, opts) }, 640, 500)
		tt.Frame()
		if a.math != nil {
			t.Fatalf("premature math: %q", src)
		}
		if (src == `前 \(x_a_b` || src == `\(x_i`) && !tt.HasText(src) {
			t.Fatalf("lost unfinished TeX: %q", tt.Texts())
		}
		a.cancel()
	}
}

func TestMathCompletionKeepsHistoryPositionAndDraft(t *testing.T) {
	rows := historyEntries(0, 100)
	for i := range rows {
		rows[i].Role = "assistant"
		rows[i].Text += ` 公式 \(x_i^2\)。`
	}
	a, _, chat, tt := historyPositionFixture(t, rows)
	chat.draft = "保留这份草稿"
	chat.list.ScrollTo(25, ui.Start)
	tt.Frame()
	before := visibleHistoryKey(chat)
	end := time.Now().Add(10 * time.Second)
	for {
		tt.Frame()
		if a.math != nil {
			_, pending := a.math.cache.State()
			if !pending {
				tt.Frame()
				break
			}
		}
		if time.Now().After(end) {
			t.Fatal("math render timed out")
		}
		time.Sleep(time.Millisecond)
	}
	if visibleHistoryKey(chat) != before || chat.list.AtEnd() || chat.draft != "保留这份草稿" {
		t.Fatal("formula displaced history or draft")
	}
}
