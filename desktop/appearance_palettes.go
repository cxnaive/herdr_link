package main

import "github.com/egoist/mygo/ui"

// Fixed local palettes match the Android client. Existing system/light/dark
// preferences retain their original soft colors and terminal settings.
type interfacePalette struct {
	id, label                                                                                  string
	dark                                                                                       bool
	background, panel, raised, ink, muted, accent, onAccent, border, selected, warning, danger string
}

var interfacePalettes = []interfacePalette{
	{"mint", "薄荷深色", true, "#111417", "#1b2025", "#252c32", "#edf0f3", "#a5afb9", "#a6dbc9", "#112b23", "#354049", "#263b34", "#ebc18c", "#ffb0ac"},
	{"midnight", "午夜蓝 · 深色", true, "#101826", "#172235", "#21304a", "#e7eef9", "#acbdd5", "#9cc7ff", "#102542", "#405674", "#243b58", "#f1c68e", "#ffb4ab"},
	{"plum", "石墨紫 · 深色", true, "#191620", "#231f2d", "#302938", "#f1eaf7", "#bcb0c9", "#d4b7f5", "#2f194a", "#50445c", "#3b2e49", "#efc58e", "#ffb2bb"},
	{"cloud", "云白蓝 · 浅色", false, "#f4f7fb", "#ffffff", "#e9eef5", "#1c293a", "#4d6075", "#19569f", "#ffffff", "#a4b3c5", "#dbe9fa", "#80520d", "#ae3038"},
	{"paper", "暖纸棕 · 浅色", false, "#f8f4ed", "#fffdf7", "#eee7dc", "#30291f", "#655848", "#775026", "#ffffff", "#b9a891", "#e8ddcc", "#78500b", "#a83432"},
}

func findInterfacePalette(id string) (interfacePalette, bool) {
	for _, p := range interfacePalettes {
		if p.id == id {
			return p, true
		}
	}
	return interfacePalette{}, false
}

func (p interfacePalette) apply(t *ui.Theme) {
	t.Dark = p.dark
	t.Background = ui.Hex(p.background)
	t.Surface = ui.Hex(p.panel)
	t.SurfaceHover = ui.Hex(p.raised)
	t.SurfacePressed = ui.Hex(p.selected)
	t.Text = ui.Hex(p.ink)
	t.TextMuted = ui.Hex(p.muted)
	t.Border = ui.Hex(p.border)
	t.Accent = ui.Hex(p.accent)
	t.AccentText = ui.Hex(p.onAccent)
	t.AccentHover = t.Accent.Mix(t.AccentText, .06)
	t.AccentPressed = t.Accent.Mix(t.AccentText, .12)
	t.Selection = ui.Hex(p.selected)
	t.Focus = t.Accent
	t.Danger = ui.Hex(p.danger)
	t.Warning = ui.Hex(p.warning)
	t.Success = t.Accent
	t.Scrollbar = t.Border
	t.Inverse, t.InverseText = t.Text, t.Background
}
