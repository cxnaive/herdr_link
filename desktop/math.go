package main

import (
	"fmt"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/mathtex"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

type nativeMath struct {
	cache  *mathtex.Cache
	images map[*mathtex.Formula]*ui.SVG
}

func (a *application) configureMath(o *markdown.Options) {
	o.Extensions = parser.Default | parser.ExtDollarMath
	previous := o.Block
	o.Block = func(c *ui.Context, d *parser.Document, i int) bool {
		b := d.Block(i)
		if (b.Kind == parser.MathBlock && b.Open) || unfinishedInlineMath(d, i) {
			ui.Text(c, d.Source()[b.Start:b.End]).Selectable()
			return true
		}
		return previous != nil && previous(c, d, i)
	}
	o.InlineMath = func(tex string, size float32) (markdown.InlineObject, bool) {
		f, image := a.mathImage(tex, false)
		if image == nil || f.Width > 32 {
			return markdown.InlineObject{}, false
		}
		return markdown.InlineObject{Width: f.Width * size, Height: f.Height * size, Depth: f.Depth * size,
			Draw: func(p *ui.Painter, r ui.Rect) { p.Icon(image, r, p.Theme().Text) }}, true
	}
	o.Math = func(c *ui.Context, tex string) {
		f, image := a.mathImage(tex, true)
		if image == nil {
			ui.Text(c, "\\[\n"+tex+"\n\\]").Selectable()
			return
		}
		size := c.Theme().FontSize
		ui.ScrollHorizontal(c).FillWidth().Label("数学公式").ContextMenu(func(m *ui.Menu) {
			if m.Item("复制 TeX 公式").Chosen() {
				c.WriteClipboard("\\[\n" + tex + "\n\\]")
			}
		}).Children(func() {
			ui.Icon(c, image).Size(f.Width*size, (f.Height+f.Depth)*size).Shrink(0).Label(tex)
		})
	}
}
func (a *application) mathImage(tex string, display bool) (*mathtex.Formula, *ui.SVG) {
	if a.math == nil {
		a.math = &nativeMath{cache: mathtex.NewCache(a.ctx), images: make(map[*mathtex.Formula]*ui.SVG)}
	}
	f, _ := a.math.cache.Get(tex, display)
	if f == nil {
		return nil, nil
	}
	if image, ok := a.math.images[f]; ok {
		return f, image
	}
	image, _ := ui.ParseSVG(f.SVG)
	if len(a.math.images) >= 64 {
		clear(a.math.images)
	}
	a.math.images[f] = image
	return f, image
}
func (a *application) markdownView(c *ui.Context, src string, o *markdown.Options) {
	var revision uint64
	if a.math != nil {
		revision, _ = a.math.cache.State()
	}
	// MyGo caches paragraph objects by source. A finished background render needs
	// fresh objects, but the outer timeline/list keeps its scroll and history state.
	markdown.View(c.Key(fmt.Sprintf("math-%d", revision)), src, o)
	if a.math != nil {
		current, pending := a.math.cache.State()
		if pending || current != revision {
			c.After(30 * time.Millisecond)
		}
	}
}

// Unclosed backslash math would otherwise go through CommonMark escaping and
// emphasis. Keep that paragraph literal until its closing delimiter arrives.
func unfinishedInlineMath(d *parser.Document, block int) bool {
	if d.Block(block).Kind != parser.Paragraph {
		return false
	}
	in := d.Inlines(block)
	for i := 0; i < len(in); i++ {
		node := in[i]
		if node.Kind == parser.Link || node.Kind == parser.Image {
			i = node.Next - 1
			continue
		}
		if node.Kind != parser.Text {
			continue
		}
		raw := d.Source()[node.Start:node.End]
		for j := 0; j+1 < len(raw); j++ {
			if raw[j] != '\\' {
				continue
			}
			if raw[j+1] == '(' || raw[j+1] == '[' {
				return true
			}
			j++
		}
	}
	return false
}
