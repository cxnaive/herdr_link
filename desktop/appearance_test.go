package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/herdr-terminal"
	"github.com/egoist/mygo/ui"
)

func TestAppearancePersistsAndSurvivesComputerRemoval(t *testing.T) {
	a, m, _ := managementFixture(t)
	p := appearanceSettings{Application: "dark", Terminal: "light", OriginalColors: true, OriginalBackgrounds: true}
	a.changeAppearance(p)
	var saved savedState
	if err := a.store.vault.Load(&saved); err != nil {
		t.Fatal(err)
	}
	if saved.Appearance != p || a.appearance != p {
		t.Fatal("appearance was not persisted", saved.Appearance)
	}
	if err := a.store.removeComputer(m.config.ID); err != nil {
		t.Fatal(err)
	}
	if a.store.appearance() != p {
		t.Fatal("removing a computer changed local appearance")
	}
	if value := (appearanceSettings{}).normalized(); value.Application != "system" || value.Terminal != "dark" || value.contrast() != 4.5 {
		t.Fatal("unsafe defaults", value)
	}
}
func TestAppearanceKeepsApplicationAndTerminalIndependent(t *testing.T) {
	a := newApplication(true)
	defer a.cancel()
	for _, row := range []struct {
		app, term                 string
		system, appDark, termDark bool
	}{
		{"light", "dark", false, false, true}, {"dark", "light", true, true, false},
		{"system", "application", true, true, true}, {"system", "application", false, false, false},
		{"system", "dark", false, false, true},
	} {
		a.appearance = appearanceSettings{Application: row.app, Terminal: row.term}
		a.systemDark = row.system
		if a.interfaceTheme(ui.LightTheme()).Dark != row.appDark || a.appearance.terminalDark(a.systemDark) != row.termDark {
			t.Fatal("wrong mode", row)
		}
		opts := a.terminalAppearanceOptions("codex")
		if opts.Theme != opts.DarkTheme || !opts.NoClipboardWrite || opts.MinimumContrast != 4.5 {
			t.Fatal("terminal follows system palette or lost protections")
		}
	}
}
func TestAppearanceEntryAndNativeControls(t *testing.T) {
	a, _, _ := managementFixture(t)
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("外观"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"应用主题", "终端配色", "增强终端文字对比度", "柔化浅色终端背景（仅 Codex）", "终端预览"} {
		if !tt.HasText(label) {
			t.Fatal("missing appearance control", label)
		}
	}
	if err := tt.Click("选择应用主题"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if a.appearance.Application != "dark" || a.store.appearance().Application != "dark" {
		t.Fatal("app control did not persist")
	}
	if err := tt.Click("选择终端配色"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if a.appearance.Terminal != "light" {
		t.Fatal("terminal control changed wrong setting")
	}
	if err := tt.Click("增强终端文字对比度"); err != nil {
		t.Fatal(err)
	}
	if !a.appearance.OriginalColors {
		t.Fatal("contrast toggle did not apply")
	}
	if err := tt.Click("柔化浅色终端背景（仅 Codex）"); err != nil {
		t.Fatal(err)
	}
	if !a.appearance.OriginalBackgrounds || !a.store.appearance().OriginalBackgrounds {
		t.Fatal("background toggle did not persist")
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	if a.appearanceOpen {
		t.Fatal("dialog did not close")
	}
}
func appearanceTerminal(t *testing.T, a *application, agentKind string) *terminal.Terminal {
	t.Helper()
	prepareNativeTerminal(t)
	r, w := io.Pipe()
	conn := &terminalFixture{PipeReader: r, writer: w}
	opts := a.terminalAppearanceOptions(agentKind)
	opts.Conn = conn
	term, err := terminal.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { term.Close() })
	return term
}
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(n uint32) float64 {
		v := float64(n) / 65535
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return .2126*linear(r) + .7152*linear(g) + .0722*linear(b)
}
func readablePixels(im image.Image, bg color.Color) int {
	l := luminance(bg)
	count := 0
	for y := 0; y < min(60, im.Bounds().Dy()); y++ {
		for x := 0; x < min(280, im.Bounds().Dx()); x++ {
			v := luminance(im.At(x, y))
			if (max(v, l)+.05)/(min(v, l)+.05) >= 4 {
				count++
			}
		}
	}
	return count
}
func TestTerminalContrastCorrectsRemoteTrueColorAndDarkStrip(t *testing.T) {
	a := newApplication(true)
	defer a.cancel()
	a.appearance.Terminal = "light"
	a.appearance.OriginalColors = true
	a.appearance.OriginalBackgrounds = true
	term := appearanceTerminal(t, a, "codex")
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill() }, 500, 150)
	term.Feed([]byte("\x1b[?25l\x1b[38;2;255;223;159mMMMM readable text\x1b[0m"))
	tt.Frame()
	bg := color.NRGBA{R: 238, G: 237, B: 233, A: 255}
	before := readablePixels(tt.Image(), bg)
	term.SetMinimumContrast(4.5)
	tt.Frame()
	after := readablePixels(tt.Image(), bg)
	if after <= before+30 {
		t.Fatal("explicit pale ANSI text not corrected", before, after)
	}
	term.SetMinimumContrast(1)
	term.Feed([]byte("\x1b[2J\x1b[H\x1b[48;2;38;38;38m\x1b[38;2;0;0;0mMMMM dark input strip\x1b[0m"))
	tt.Frame()
	beforeImage := tt.Image()
	term.SetMinimumContrast(4.5)
	tt.Frame()
	afterImage := tt.Image()
	// Count changed pixels rather than mistaking the surrounding light canvas
	// for text with adequate contrast inside the explicit dark input strip.
	changed := 0
	for y := 4; y < 24; y++ {
		for x := 6; x < 150; x++ {
			if luminance(afterImage.At(x, y))-luminance(beforeImage.At(x, y)) > .2 {
				changed++
			}
		}
	}
	if changed < 30 {
		t.Fatal("black text on remote dark strip remained unreadable", changed)
	}
}
func TestTerminalThemeSwitchKeepsSessionAndPreview(t *testing.T) {
	a, m, _ := managementFixture(t)
	a.demo = false
	a.mode = 1
	term := appearanceTerminal(t, a, "codex")
	m.term = term
	m.termIdentity = m.agents[0].Identity()
	m.termState = "终端已连接"
	tt := ui.NewTester(a.view, 1280, 840)
	term.Feed([]byte("\x1b[?25l 训练评估\r\n\r\n\x1b[38;2;255;223;159m### COTR：稳定推进\x1b[0m\r\n\r\n 训练进度  \x1b[38;2;255;128;170m100%\x1b[0m\r\n 文件位置  \x1b[38;2;160;220;160m/mnt/workspace/report.md\x1b[0m\r\n\r\n\x1b[48;2;38;38;38m\x1b[38;2;0;0;0m ❯ Ask Codex to do anything                         \x1b[0m\r\n"))
	tt.Frame()
	original := term.Text()
	for _, mode := range []string{"dark", "light"} {
		a.changeAppearance(appearanceSettings{Application: mode, Terminal: mode})
		tt.Frame()
		if m.term != term || term.Text() != original {
			t.Fatal("theme switch replaced terminal session or content")
		}
		if dir := os.Getenv("HERDR_APPEARANCE_SCREENSHOTS"); dir != "" {
			f, err := os.Create(filepath.Join(dir, "appearance-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func pixelsOf(im image.Image, want ui.Color) int {
	count := 0
	for y := im.Bounds().Min.Y; y < im.Bounds().Max.Y; y++ {
		for x := im.Bounds().Min.X; x < im.Bounds().Max.X; x++ {
			r, g, b, a := im.At(x, y).RGBA()
			if uint8(r>>8) == want.R && uint8(g>>8) == want.G && uint8(b>>8) == want.B && uint8(a>>8) == want.A {
				count++
			}
		}
	}
	return count
}
func TestSoftBackgroundRecolorsExistingCellsWithoutChangingTerminalState(t *testing.T) {
	a, m, _ := managementFixture(t)
	a.demo = false
	a.appearance = appearanceSettings{Application: "light", Terminal: "light"}
	term := appearanceTerminal(t, a, "codex")
	m.term = term
	m.termIdentity = m.agents[0].Identity()
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill() }, 600, 240)
	// Include a long erased area: Ghostty stores blank-cell backgrounds outside
	// the style table, so testing only letters misses most of the black rectangle.
	term.Feed([]byte("\x1b[?25l\x1b[48;2;38;38;38m\x1b[38;2;210;210;210m 问题与输入栏        \x1b[K\r\n\x1b[48;5;236m 另一条深灰背景       \x1b[K\x1b[0m\r\n\x1b[48;2;100;20;20m 错误背景保留          \x1b[0m"))
	tt.Frame()
	original := append([]byte{}, term.Snapshot()...)
	plain := term.Text()
	if count := pixelsOf(tt.Image(), ui.RGB(38, 38, 38)); count != 0 {
		t.Fatal("literal black strip remains", count)
	}
	if count := pixelsOf(tt.Image(), ui.RGB(214, 216, 214)); count < 1000 {
		t.Fatal("neutral strip was not softened", count)
	}
	if count := pixelsOf(tt.Image(), ui.RGB(100, 20, 20)); count < 100 {
		t.Fatal("colored error background was altered", count)
	}
	a.changeAppearance(appearanceSettings{Application: "light", Terminal: "light", OriginalBackgrounds: true})
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) < 1000 {
		t.Fatal("original backgrounds did not return immediately")
	}
	if !bytes.Equal(original, term.Snapshot()) || plain != term.Text() {
		t.Fatal("appearance changed stored ANSI state or text")
	}
	a.changeAppearance(appearanceSettings{Application: "dark", Terminal: "dark"})
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) < 1000 {
		t.Fatal("dark mode changed original RGB background")
	}
	a.changeAppearance(appearanceSettings{Application: "light", Terminal: "light"})
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) != 0 {
		t.Fatal("switching back to light retained black background")
	}
	if m.term != term || term.Text() != plain {
		t.Fatal("appearance switch replaced session")
	}
}
func TestSoftBackgroundPreservesInverseAndSelection(t *testing.T) {
	a := newApplication(true)
	defer a.cancel()
	a.appearance.Terminal = "light"
	term := appearanceTerminal(t, a, "codex")
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill() }, 600, 240)
	term.Feed([]byte("\x1b[?25l\x1b[38;2;38;38;38;48;2;238;237;233;7m inverse selection      \x1b[0m\r\n\x1b[48;2;38;38;38m ordinary block        \x1b[0m"))
	tt.Frame()
	if count := pixelsOf(tt.Image(), ui.RGB(38, 38, 38)); count < 100 {
		t.Fatal("inverse highlight was softened", count)
	}
	if count := pixelsOf(tt.Image(), ui.RGB(214, 216, 214)); count < 100 {
		t.Fatal("ordinary background not softened", count)
	}
	if err := tt.RightClick("Terminal"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Select All"); err != nil {
		t.Fatal(err)
	}
	if count := pixelsOf(tt.Image(), softLightTerminal.Selection); count < 100 {
		t.Fatal("text selection highlight lost", count)
	}
}

func TestSoftBackgroundLeavesOtherAgentsUnchanged(t *testing.T) {
	for _, kind := range []string{"claude", "pi", "gemini", "shell", "", "custom-codex"} {
		t.Run(kind, func(t *testing.T) {
			a, m, _ := managementFixture(t)
			a.demo = false
			a.appearance = appearanceSettings{Application: "light", Terminal: "light"}
			m.agents[0].Agent = kind
			// Keep a different Codex selected while this terminal stays attached.
			a.selectedAgent = m.agents[1].PaneID
			term := appearanceTerminal(t, a, kind)
			m.term, m.termIdentity = term, m.agents[0].Identity()
			tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill() }, 600, 240)
			term.Feed([]byte("\x1b[?25l\x1b[48;2;38;38;38m background \x1b[K\x1b[0m"))
			tt.Frame()
			before := tt.Image()
			if pixelsOf(before, ui.RGB(38, 38, 38)) < 1000 {
				t.Fatal("non-Codex background altered on creation")
			}
			for _, original := range []bool{true, false} {
				a.changeAppearance(appearanceSettings{Application: "light", Terminal: "light", OriginalBackgrounds: original})
				tt.Frame()
				if !bytes.Equal(before.Pix, tt.Image().Pix) {
					t.Fatal("Codex-only setting changed another Agent's rendering")
				}
			}
		})
	}
}

func TestSoftBackgroundTracksAttachedAgentKind(t *testing.T) {
	a, m, _ := managementFixture(t)
	a.demo = false
	a.appearance = appearanceSettings{Application: "light", Terminal: "light"}
	term := appearanceTerminal(t, a, "codex")
	m.term, m.termIdentity = term, m.agents[0].Identity()
	tt := ui.NewTester(func(c *ui.Context) { terminal.View(c, term).Fill() }, 600, 240)
	term.Feed([]byte("\x1b[?25l\x1b[48;2;38;38;38m background \x1b[K\x1b[0m"))
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) != 0 {
		t.Fatal("Codex background not softened")
	}
	before := append([]byte(nil), term.Snapshot()...)
	m.agents[0].Agent = "claude"
	a.event(m, relay.Message{"type": "agents", "agents": m.agents})
	tt.Frame()
	if m.term != term || pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) < 1000 {
		t.Fatal("provider update did not restore original background")
	}
	m.agents[0].Agent = "codex"
	a.event(m, relay.Message{"type": "agents", "agents": m.agents})
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) != 0 {
		t.Fatal("provider update did not enable Codex softening")
	}
	// Missing inventory and local demo shells must never opt into the transform.
	m.termIdentity = "unknown"
	a.applyTerminalAppearance()
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) < 1000 {
		t.Fatal("unknown terminal identity was softened")
	}
	m.termIdentity = m.agents[0].Identity()
	a.demo = true
	a.applyTerminalAppearance()
	tt.Frame()
	if pixelsOf(tt.Image(), ui.RGB(38, 38, 38)) < 1000 {
		t.Fatal("local demo shell was softened")
	}
	if !bytes.Equal(before, term.Snapshot()) {
		t.Fatal("provider update changed terminal state")
	}
}

func TestSharedInterfacePalettesKeepModesAndReadableColors(t *testing.T) {
	a := newApplication(true)
	defer a.cancel()
	for _, palette := range interfacePalettes {
		t.Run(palette.id, func(t *testing.T) {
			for _, system := range []bool{false, true} {
				a.systemDark = system
				a.appearance = appearanceSettings{Application: palette.id, Terminal: "dark"}.normalized()
				if a.appearance.Application != palette.id || a.appearance.appDark(system) != palette.dark || !a.appearance.terminalDark(system) {
					t.Fatal("palette did not preserve explicit modes")
				}
				want := mygo.ThemeLight
				if palette.dark {
					want = mygo.ThemeDark
				}
				if a.appearance.themeSource() != want {
					t.Fatal("native window controls use the wrong mode")
				}
				p := a.appearance
				p.Terminal = "application"
				if p.terminalDark(system) != palette.dark {
					t.Fatal("terminal follow-application ignored palette")
				}
			}
			theme := a.interfaceTheme(ui.LightTheme())
			if theme.Background != ui.Hex(palette.background) || theme.Dark != palette.dark {
				t.Fatal("interface did not adopt palette")
			}
			contrast := func(fg, bg ui.Color) float64 {
				l1 := luminance(color.NRGBA{R: fg.R, G: fg.G, B: fg.B, A: 255})
				l2 := luminance(color.NRGBA{R: bg.R, G: bg.G, B: bg.B, A: 255})
				return (max(l1, l2) + .05) / (min(l1, l2) + .05)
			}
			for _, bg := range []ui.Color{theme.Background, theme.Surface, theme.SurfaceHover, theme.SurfacePressed} {
				for _, fg := range []ui.Color{theme.Text, theme.TextMuted, theme.Accent, theme.Warning, theme.Danger} {
					if ratio := contrast(fg, bg); ratio < 4.5 {
						t.Fatalf("unreadable text/status contrast: %.2f", ratio)
					}
				}
			}
			for _, bg := range []ui.Color{theme.Accent, theme.AccentHover, theme.AccentPressed} {
				if ratio := contrast(theme.AccentText, bg); ratio < 4.5 {
					t.Fatalf("unreadable primary control: %.2f", ratio)
				}
			}
		})
	}
	if (appearanceSettings{Application: "missing"}).normalized().Application != "system" {
		t.Fatal("invalid preference did not retain system fallback")
	}
}

func TestSharedPaletteControlsPersistWithoutReplacingSessionOrDraft(t *testing.T) {
	a, m, chat := managementFixture(t)
	a.demo = false
	term := appearanceTerminal(t, a, "codex")
	m.term, m.termIdentity = term, m.agents[0].Identity()
	term.Feed([]byte("\x1b[?25l\x1b[31mred ANSI\x1b[0m retained terminal"))
	original := term.Text()
	session, target := m.session, chat.agent.Target
	chat.draft, chat.uncertain = "配色切换不能清除草稿", true
	key := operationKey(m, chat.agent)
	if err := a.store.saveDraft(key, chat.draft, true); err != nil {
		t.Fatal(err)
	}
	if err := a.setOperation(key, "agent_restart"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1280, 840)
	for index, palette := range interfacePalettes {
		if err := tt.Click("外观"); err != nil {
			t.Fatal(err)
		}
		if err := tt.Click("选择应用主题"); err != nil {
			t.Fatal(err)
		}
		steps := 1
		if index == 0 {
			steps = 3 // system, light, dark, mint
		}
		for range steps {
			tt.Key(0, ui.KeyDown)
		}
		tt.Key(0, ui.KeyEnter)
		if a.appearance.Application != palette.id || a.store.appearance().Application != palette.id {
			t.Fatal("selection was not persisted", palette.id, a.appearance)
		}
		var saved savedState
		if err := a.store.vault.Load(&saved); err != nil || saved.Appearance != a.appearance {
			t.Fatal("palette was not saved to the encrypted local store", err)
		}
		if m.term != term || term.Text() != original || a.terminalTheme() != softDarkTerminal || m.session != session || chat.agent.Target != target {
			t.Fatal("palette switch altered terminal, session or target")
		}
		if draft, uncertain := a.store.draft(key); draft != chat.draft || !uncertain || !chat.uncertain || a.store.operations()[key] != "agent_restart" {
			t.Fatal("palette switch lost draft or delivery/operation state")
		}
		if dir := os.Getenv("HERDR_APPEARANCE_SCREENSHOTS"); dir != "" {
			save := func(name string) {
				t.Helper()
				f, err := os.Create(filepath.Join(dir, name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
			}
			save(palette.id + "-picker")
			if err := tt.Click("完成"); err != nil {
				t.Fatal(err)
			}
			save(palette.id + "-conversation")
		} else if err := tt.Click("完成"); err != nil {
			t.Fatal(err)
		}
	}
}
