package main

import (
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
)

type agentGroup struct {
	key, title, cwd string
	order           int
	agents          []relay.Agent
	workspace       relay.Workspace
}

func groupKey(parts ...string) string { b, _ := json.Marshal(parts); return string(b) }
func machineGroups(m *machine) []agentGroup {
	groups := []agentGroup{}
	positions := map[string]int{}
	for _, w := range m.workspaces {
		key := groupKey(m.config.ID, w.ServerSessionID, w.ID)
		title := w.Label
		if title == "" {
			title = path.Base(w.Cwd)
		}
		if title == "." || title == "/" || title == "" {
			title = "未命名项目"
		}
		positions[key] = len(groups)
		groups = append(groups, agentGroup{key: key, title: title, cwd: w.Cwd, order: w.Number, workspace: w})
	}
	for _, agent := range m.agents {
		id := agent.WorkspaceID
		if id == "" {
			id = "cwd:" + agent.Cwd
		}
		key := groupKey(m.config.ID, agent.ServerSessionID, id)
		index, ok := positions[key]
		if !ok {
			title := agent.WorkspaceLabel
			if title == "" {
				title = agent.Project
			}
			if title == "" {
				title = path.Base(agent.Cwd)
			}
			if title == "." || title == "/" || title == "" {
				title = "未命名项目"
			}
			index = len(groups)
			positions[key] = index
			groups = append(groups, agentGroup{key: key, title: title, cwd: agent.Cwd, order: 100000})
		}
		groups[index].agents = append(groups[index].agents, agent)
	}
	for i := range groups {
		slices.SortStableFunc(groups[i].agents, func(x, y relay.Agent) int {
			xo, yo := x.TabOrder, y.TabOrder
			if xo == 0 {
				xo = x.TabNumber
			}
			if yo == 0 {
				yo = y.TabNumber
			}
			if xo < yo {
				return -1
			}
			if xo > yo {
				return 1
			}
			return strings.Compare(x.TabID, y.TabID)
		})
	}
	slices.SortStableFunc(groups, func(x, y agentGroup) int {
		if x.order < y.order {
			return -1
		}
		if x.order > y.order {
			return 1
		}
		return strings.Compare(x.title, y.title)
	})
	return groups
}
func (a *application) toggleGroup(key string) {
	a.collapsed[key] = !a.collapsed[key]
	if a.store != nil {
		if err := a.store.saveSidebarGroups(a.collapsed); err != nil {
			a.status = "未能保存菜单状态"
		}
	}
}
func disclosure(c *ui.Context, label string, collapsed bool, count int) bool {
	mark := "▾"
	if collapsed {
		mark = "▸"
	}
	b := ui.Button(c, "").Label(label).FillWidth().BorderWidth(0).Background(c.Theme().Surface).Padding(7, 4).Justify(ui.Start)
	b.Children(func() {
		ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, mark).FontSize(12).TextColor(c.Theme().TextMuted)
			ui.Text(c, label).Bold().FontSize(12).Grow(1)
			if count >= 0 {
				ui.Text(c, fmtCount(count)).FontSize(10).TextColor(c.Theme().TextMuted)
			}
		})
	})
	return b.Clicked()
}
func fmtCount(n int) string { b, _ := json.Marshal(n); return string(b) }
func (a *application) sidebarView(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Padding(14, 12).Gap(12).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Image(c, brandIcon).Size(24, 24).Margin(0, 7, 0, 0)
			ui.Text(c, "Herdr Desktop").Bold().Grow(1)
		})
		ui.TextInput(c, &a.query).Placeholder("搜索电脑、项目、Agent").Label("搜索 Agent")
		query := strings.ToLower(strings.TrimSpace(a.query))
		ui.Scroll(c).Grow(1).Gap(12).Children(func() {
			for _, m := range a.machines {
				activity := relay.CountAgentActivity(m.agents)
				groups := machineGroups(m)
				visible := []agentGroup{}
				for _, g := range groups {
					parentMatch := strings.Contains(strings.ToLower(m.config.Name+" "+g.title+" "+g.cwd), query)
					filtered := g
					filtered.agents = nil
					for _, agent := range g.agents {
						if parentMatch || strings.Contains(strings.ToLower(agent.Title()+" "+agent.Provider()+" "+agent.Cwd), query) {
							filtered.agents = append(filtered.agents, agent)
						}
					}
					if parentMatch || len(filtered.agents) > 0 {
						visible = append(visible, filtered)
					}
				}
				if query != "" && len(visible) == 0 && !strings.Contains(strings.ToLower(m.config.Name), query) {
					continue
				}
				mc := c.Key("machine:" + m.config.ID)
				key := groupKey("computer", m.config.ID)
				collapsed := a.collapsed[key] && query == ""
				ui.Column(mc).Gap(3).Children(func() {
					if disclosure(mc, m.config.Name, collapsed, activity.Agents) && query == "" {
						a.toggleGroup(key)
						collapsed = a.collapsed[key]
					}
					ui.Row(mc).Padding(0, 4, 0, 19).AlignItems(ui.Center).Children(func() {
						if strings.Contains(m.status, "正在") {
							ui.Spinner(mc).Size(12, 12)
						}
						ui.Text(mc, m.status).FontSize(10).TextColor(t.TextMuted).Grow(1)
						ui.MenuButton(mc, "⋯", func(menu *ui.Menu) { a.computerMenu(menu, m) }).Label("管理电脑 "+m.config.Name).Padding(2, 6)
						if !a.demo && m.session == nil && ui.Button(mc, "重连").FontSize(10).Clicked() {
							a.connect(m)
						}
					})
					summary := activity.Summary(m.session != nil || a.demo, m.inventoryReady || a.demo)
					if activity.Terminals > 0 {
						summary += " · " + fmtCount(activity.Terminals) + " 个终端"
					}
					ui.Text(mc, summary).FontSize(11).TextColor(t.TextMuted).Wrap().Padding(0, 4, 0, 19)
					if collapsed {
						return
					}
					for _, g := range visible {
						gc := mc.Key(g.key)
						closed := a.collapsed[g.key] && query == ""
						ui.Column(gc).Padding(2, 0, 0, 12).Gap(3).ContextMenu(func(menu *ui.Menu) {
							if g.workspace.ID != "" {
								a.projectMenu(menu, m, g.workspace)
							}
						}).Children(func() {
							ui.Row(gc).AlignItems(ui.Center).Children(func() {
								ui.Column(gc).Grow(1).Children(func() {
									if disclosure(gc, g.title, closed, len(g.agents)) && query == "" {
										a.toggleGroup(g.key)
										closed = a.collapsed[g.key]
									}
								})
								if g.workspace.ID != "" && ui.Button(gc, "＋").Label("在 "+g.title+" 新建 Agent").Disabled(!managementReady(m) || g.workspace.ServerSessionID != "primary" || len(m.profiles) == 0).Clicked() {
									a.openLaunch(m, g.workspace)
								}
							})
							if closed {
								return
							}
							for _, agent := range g.agents {
								ac := gc.Key(agent.PaneID)
								selected := a.selectedMachine == m.config.ID && a.selectedAgent == agent.PaneID
								button := ui.Button(ac, "").Label(agent.Title()).Selected(selected).FillWidth().BorderWidth(0).Background(t.Surface).Padding(9, 12).Justify(ui.Start)
								button.ContextMenu(func(menu *ui.Menu) { a.agentMenu(menu, m, agent) })
								if selected {
									button.Background(t.Background).TextColor(t.Accent)
								}
								button.Children(func() {
									ui.Column(ac).FillWidth().Gap(3).Children(func() {
										ui.Text(ac, agent.Title()).FontSize(13)
										ui.Text(ac, agent.Provider()+" · "+statusLabel(agent.Status)).FontSize(10).TextColor(t.TextMuted)
									})
								})
								if button.Clicked() {
									a.selectAgent(m, agent)
								}
							}
							if len(g.agents) == 0 {
								ui.Text(gc, "暂无 Agent").FontSize(11).TextColor(t.TextMuted).Padding(8, 16)
							}
						})
					}
					if len(groups) == 0 {
						ui.Text(mc, "暂无项目").FontSize(11).TextColor(t.TextMuted).Padding(8, 19)
					}
				})
			}
		})
		ui.Row(c).Gap(6).Children(func() {
			if ui.Button(c, "＋ 添加电脑").Grow(1).Disabled(a.store == nil && !a.demo).Clicked() {
				a.saveCurrentDraft()
				a.pairing = true
			}
			if ui.Button(c, "外观").Clicked() {
				a.appearanceOpen = true
			}
		})
	})
}
