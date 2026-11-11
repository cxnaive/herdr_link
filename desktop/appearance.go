package main

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/herdr-terminal"
	"github.com/egoist/mygo/ui"
)

type appearanceSettings struct {
	Application         string
	Terminal            string
	OriginalColors      bool
	OriginalBackgrounds bool
}

func (p appearanceSettings) normalized() appearanceSettings {
	switch p.Application {
	case "system", "light", "dark":
	default:
		if _, ok := findInterfacePalette(p.Application); !ok {
			p.Application = "system"
		}
	}
	switch p.Terminal {
	case "application", "light", "dark":
	default:
		p.Terminal = "dark"
	}
	return p
}
func (p appearanceSettings) appDark(systemDark bool) bool {
	if palette, ok := findInterfacePalette(p.Application); ok {
		return palette.dark
	}
	switch p.normalized().Application {
	case "light":
		return false
	case "dark":
		return true
	default:
		return systemDark
	}
}
func (p appearanceSettings) terminalDark(systemDark bool) bool {
	switch p.normalized().Terminal {
	case "light":
		return false
	case "dark":
		return true
	default:
		return p.appDark(systemDark)
	}
}
func (p appearanceSettings) contrast() float32 {
	if p.OriginalColors {
		return 1
	}
	return 4.5
}

// Both terminal theme slots receive the chosen palette, keeping the terminal
// independent of the UI's light/dark mode. Cell color adjustments happen only
// in the renderer, preserving the original ANSI state across appearance changes.
var softDarkTerminal = &terminal.Theme{
	Background: ui.Hex("#202225"), Foreground: ui.Hex("#d7d9dc"),
	Cursor: ui.Hex("#c2d5ce"), CursorText: ui.Hex("#202225"),
	Selection: ui.Hex("#414b50"), SelectionText: ui.Hex("#f0f1f2"),
	Search: ui.Hex("#6a5937"), SearchText: ui.Hex("#ffffff"), SearchSelected: ui.Hex("#a67c40"), SearchSelectedText: ui.Hex("#ffffff"),
	Palette: [16]ui.Color{
		ui.Hex("#25272b"), ui.Hex("#cf8686"), ui.Hex("#93b69a"), ui.Hex("#c6b68b"),
		ui.Hex("#8eacca"), ui.Hex("#b69fc4"), ui.Hex("#8fbabc"), ui.Hex("#c6c9cd"),
		ui.Hex("#8a8e95"), ui.Hex("#dda0a0"), ui.Hex("#aec9b3"), ui.Hex("#d6c9a5"),
		ui.Hex("#a9c1da"), ui.Hex("#c7b4d2"), ui.Hex("#acd0d1"), ui.Hex("#e3e5e8"),
	},
}
var softLightTerminal = &terminal.Theme{
	Background: ui.Hex("#eeede9"), Foreground: ui.Hex("#30353b"),
	Cursor: ui.Hex("#526b61"), CursorText: ui.Hex("#eeede9"),
	Selection: ui.Hex("#cbd6d2"), SelectionText: ui.Hex("#27342f"),
	Search: ui.Hex("#e0cda4"), SearchText: ui.Hex("#322a1c"), SearchSelected: ui.Hex("#c5a773"), SearchSelectedText: ui.Hex("#241d12"),
	Palette: [16]ui.Color{
		ui.Hex("#30353b"), ui.Hex("#9c454b"), ui.Hex("#426b4a"), ui.Hex("#7e642f"),
		ui.Hex("#38628b"), ui.Hex("#795383"), ui.Hex("#326a70"), ui.Hex("#596069"),
		ui.Hex("#626970"), ui.Hex("#a34e56"), ui.Hex("#4b7053"), ui.Hex("#816735"),
		ui.Hex("#456a92"), ui.Hex("#805d8a"), ui.Hex("#3c7177"), ui.Hex("#767d85"),
	},
}

func (a *application) terminalTheme() *terminal.Theme {
	if a.appearance.terminalDark(a.systemDark) {
		return softDarkTerminal
	}
	return softLightTerminal
}
func (p appearanceSettings) softenBackgrounds(agentKind string) bool {
	return agentKind == "codex" && !p.OriginalBackgrounds
}
func (a *application) terminalAppearanceOptions(agentKind string) terminal.Options {
	th := a.terminalTheme()
	return terminal.Options{Theme: th, DarkTheme: th, MinimumContrast: a.appearance.contrast(), SoftenDarkBackgrounds: a.appearance.softenBackgrounds(agentKind), NoClipboardWrite: true}
}
func (a *application) applyTerminalBackgrounds(m *machine) {
	if m.term == nil {
		return
	}
	agentKind := ""
	// Demo terminals run a local shell. Remote terminals use their own identity,
	// which may differ from the Agent currently selected in the sidebar.
	if !a.demo {
		for _, agent := range m.agents {
			if agent.Identity() == m.termIdentity {
				agentKind = agent.Agent
				break
			}
		}
	}
	m.term.SetSoftenDarkBackgrounds(a.appearance.softenBackgrounds(agentKind))
}
func (a *application) applyTerminalAppearance() {
	th := a.terminalTheme()
	for _, m := range a.machines {
		if m.term != nil {
			m.term.SetTheme(th, th)
			m.term.SetMinimumContrast(a.appearance.contrast())
			a.applyTerminalBackgrounds(m)
		}
	}
}
func (s *storage) appearance() appearanceSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Appearance.normalized()
}
func (s *storage) saveAppearance(p appearanceSettings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state.Appearance
	s.state.Appearance = p.normalized()
	if err := s.vault.Save(s.state); err != nil {
		s.state.Appearance = old
		return err
	}
	return nil
}
func (a *application) changeAppearance(p appearanceSettings) {
	p = p.normalized()
	if p == a.appearance {
		return
	}
	if a.store != nil {
		if err := a.store.saveAppearance(p); err != nil {
			a.appearanceMessage = "外观设置未保存：" + err.Error()
			return
		}
	}
	a.appearance = p
	a.appearanceMessage = ""
	a.applyAppearance()
}
func (p appearanceSettings) themeSource() mygo.ThemeSource {
	if palette, ok := findInterfacePalette(p.Application); ok {
		if palette.dark {
			return mygo.ThemeDark
		}
		return mygo.ThemeLight
	}
	return mygo.ThemeSource(p.normalized().Application)
}
func (a *application) applyAppearance() {
	if a.win != nil {
		mygo.Theme.SetSource(a.appearance.themeSource())
		if a.appearance.normalized().Application == "system" {
			a.systemDark = mygo.Theme.IsDark()
		}
	}
	a.applyTerminalAppearance()
}
func (a *application) interfaceTheme(base *ui.Theme) *ui.Theme {
	dark := a.appearance.appDark(a.systemDark)
	theme := *base
	if theme.Dark != dark {
		if dark {
			theme = *ui.DarkTheme()
		} else {
			theme = *ui.LightTheme()
		}
	}
	if dark {
		theme.Background = ui.Hex("#222427")
		theme.Surface = ui.Hex("#292c30")
		theme.SurfaceHover = ui.Hex("#33373b")
		theme.SurfacePressed = ui.Hex("#3c4146")
		theme.Text = ui.Hex("#dedfe1")
		theme.TextMuted = ui.Hex("#a6abb2")
		theme.Border = ui.Hex("#40454b")
		theme.Accent = ui.Hex("#a1c3b6")
		theme.AccentText = ui.Hex("#21332b")
		theme.AccentHover = ui.Hex("#b0cdc2")
		theme.AccentPressed = ui.Hex("#91b4a6")
		theme.Selection = ui.Hex("#41534b")
		theme.Focus = ui.Hex("#a1c3b6")
	} else {
		theme.Background = ui.Hex("#f1f2ef")
		theme.Surface = ui.Hex("#e7ebe7")
		theme.SurfaceHover = ui.Hex("#dde3de")
		theme.SurfacePressed = ui.Hex("#d4ddd6")
		theme.Text = ui.Hex("#303630")
		theme.TextMuted = ui.Hex("#646e66")
		theme.Border = ui.Hex("#c5cdc6")
		theme.Accent = ui.Hex("#386c56")
		theme.AccentText = ui.Hex("#ffffff")
		theme.AccentHover = ui.Hex("#2f5e4a")
		theme.AccentPressed = ui.Hex("#284f3e")
		theme.Selection = ui.Hex("#cfdfd4")
		theme.Focus = ui.Hex("#386c56")
	}
	if palette, ok := findInterfacePalette(a.appearance.Application); ok {
		palette.apply(&theme)
	}
	theme.FontSize = 14
	theme.Radius = 8
	return &theme
}
func appearanceChoice(c *ui.Context, label string, value *string, ids, labels []string) {
	choice := labels[0]
	for i, id := range ids {
		if id == *value {
			choice = labels[i]
		}
	}
	ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Select(c, &choice, labels).Label("选择" + label).FillWidth()
	for i, text := range labels {
		if text == choice {
			*value = ids[i]
		}
	}
}
func (a *application) appearanceView(c *ui.Context) {
	if !a.appearanceOpen {
		return
	}
	ui.Modal(c, &a.appearanceOpen, func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "外观").Bold().FontSize(19).Grow(1)
			if ui.Button(c, "完成").Clicked() {
				a.appearanceOpen = false
			}
		})
		p := a.appearance
		ids, labels := []string{"system", "light", "dark"}, []string{"跟随系统", "柔和浅色", "柔和深色"}
		for _, palette := range interfacePalettes {
			ids = append(ids, palette.id)
			labels = append(labels, palette.label)
		}
		appearanceChoice(c, "应用主题", &p.Application, ids, labels)
		ui.Text(c, "本机保存，立即生效；不影响连接、草稿或远端设置。").FontSize(12).TextColor(c.Theme().TextMuted).Wrap()
		appearanceChoice(c, "终端配色", &p.Terminal, []string{"dark", "light", "application"}, []string{"柔和深色", "柔和浅色", "跟随应用"})
		enhance := !p.OriginalColors
		ui.Checkbox(c, &enhance, "增强终端文字对比度")
		p.OriginalColors = !enhance
		soften := !p.OriginalBackgrounds
		ui.Checkbox(c, &soften, "柔化浅色终端背景（仅 Codex）")
		p.OriginalBackgrounds = !soften
		if p != a.appearance {
			a.changeAppearance(p)
		}
		th := a.terminalTheme()
		ui.Column(c).FillWidth().Background(th.Background).Padding(18).Radius(8).Gap(10).Children(func() {
			ui.Text(c, "终端预览").TextColor(th.Foreground).FontSize(12)
			ui.Text(c, "❯ 检查训练结果").TextColor(th.Foreground).Font("monospace")
			ui.Text(c, "已完成  ·  ~/project/report.md").TextColor(th.Palette[2]).Font("monospace")
			ui.Text(c, "警告  2   错误  0").TextColor(th.Palette[3]).Font("monospace")
		})
		if a.appearanceMessage != "" {
			ui.Text(c, a.appearanceMessage).FontSize(12).TextColor(c.Theme().Warning)
		}
	}).Width(480)
}
