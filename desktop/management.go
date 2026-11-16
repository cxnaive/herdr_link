package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type managementDialog struct {
	kind, title                      string
	machine                          *machine
	agent                            relay.Agent
	workspace                        relay.Workspace
	epoch                            uint64
	name, profile, prompt, arguments string
	defaults                         map[string][]string
	model, effort                    string
	settings                         relay.AgentSettings
	busy                             bool
	message                          string
}

func (a *application) update(fn func()) {
	if a.win == nil {
		fn()
	} else {
		a.win.Update(fn)
	}
}
func managementReady(m *machine) bool {
	return m != nil && m.session != nil && m.protocol == 3 && m.inventoryReady && m.config.Auth.Role == "controller"
}
func exactAgent(m *machine, captured relay.Agent) (relay.Agent, bool) {
	for _, live := range m.agents {
		if live.Target == captured.Target && live.Cwd == captured.Cwd && live.WorkspaceID == captured.WorkspaceID {
			return live, true
		}
	}
	return relay.Agent{}, false
}
func operationKey(m *machine, agent relay.Agent) string {
	return groupKey("operation", m.config.ID, agent.Identity(), agent.Cwd)
}
func launchKey(m *machine, w relay.Workspace) string {
	return groupKey("launch", m.config.ID, w.ServerSessionID, w.ID)
}
func (a *application) setOperation(key, action string) error {
	if a.store == nil {
		return errors.New("加密存储尚未就绪")
	}
	if err := a.store.saveOperation(key, action); err != nil {
		return fmt.Errorf("无法保存操作状态：%w", err)
	}
	if action == "" {
		delete(a.operations, key)
	} else {
		a.operations[key] = action
	}
	return nil
}
func (a *application) agentWriteAllowed(m *machine, agent relay.Agent, action string) bool {
	if m == nil {
		return false
	}
	live, ok := exactAgent(m, agent)
	key := operationKey(m, agent)
	chat := a.chats[m.config.ID+":"+agent.Identity()]
	return ok && managementReady(m) && live.OperationWritable(action) && !a.busyOperations[key] && a.operations[key] == "" && (chat == nil || (!chat.pending && !chat.uncertain && !chat.uploading))
}
func (a *application) writeAgent(m *machine, agent relay.Agent, action string, payload relay.Message, done func(relay.Message, error)) {
	if !a.agentWriteAllowed(m, agent, action) {
		done(nil, &relay.RequestError{Message: "请检查连接、控制权限和待核对的操作；Agent 也可能已变化"})
		return
	}
	request := agent.Request(action)
	for k, v := range payload {
		request[k] = v
	}
	a.writeOperation(m, request, operationKey(m, agent), 30*time.Second, done)
}
func (a *application) writeOperation(m *machine, request relay.Message, key string, timeout time.Duration, done func(relay.Message, error)) {
	if !managementReady(m) || a.busyOperations[key] || a.operations[key] != "" {
		done(nil, &relay.RequestError{Message: "请先核对上次操作结果"})
		return
	}
	action := relay.Text(request, "type")
	if err := a.setOperation(key, action); err != nil {
		done(nil, &relay.RequestError{Message: err.Error()})
		return
	}
	a.busyOperations[key] = true
	s := m.session
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, timeout)
		defer cancel()
		result, err := s.Request(ctx, request)
		a.update(func() {
			delete(a.busyOperations, key)
			if !slices.Contains(a.machines, m) {
				return
			}
			if !relay.UnknownOutcome(err) {
				if saveErr := a.setOperation(key, ""); saveErr != nil {
					err = &relay.RequestError{Unknown: true, Message: saveErr.Error()}
				}
			}
			done(result, err)
			if m.session == s {
				go s.Send(relay.Message{"type": "refresh_agents", "protocol": 3})
			}
		})
	}()
}
func (a *application) readAgent(m *machine, agent relay.Agent, action string, payload relay.Message, done func(relay.Message, error)) {
	if _, ok := exactAgent(m, agent); !ok || m.session == nil {
		done(nil, errors.New("Agent 已断开或会话已变化"))
		return
	}
	req := agent.Request(action)
	for k, v := range payload {
		req[k] = v
	}
	s, epoch := m.session, m.epoch
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		data, err := s.Request(ctx, req)
		a.update(func() {
			if _, ok := exactAgent(m, agent); !ok || m.epoch != epoch {
				done(nil, errors.New("Agent 已变化，请重新打开"))
				return
			}
			done(data, err)
		})
	}()
}
func (a *application) openManagement(m *machine, agent relay.Agent, kind string) {
	titles := map[string]string{"agent_rename": "重命名 Agent", "agent_stop": "停止 Agent", "agent_restart": "重启 Agent", "agent_clear": "清空会话", "settings": "模型与思考强度", "tab_reorder": "调整 Agent 顺序"}
	d := &managementDialog{kind: kind, title: titles[kind], machine: m, agent: agent, epoch: m.epoch, name: agent.Title()}
	if kind == "tab_reorder" {
		d.name = strconv.Itoa(agent.TabOrder + 1)
	}
	a.management = d
	if kind == "settings" {
		a.readSettings(d)
	}
}
func (a *application) openLaunch(m *machine, w relay.Workspace) {
	d := &managementDialog{kind: "agent_start", title: "新建 Agent", machine: m, workspace: w, epoch: m.epoch, defaults: relay.Decode[map[string][]string](m.launchDefaults)}
	if len(m.profiles) > 0 {
		d.profile = m.profiles[0].ID
	}
	a.management = d
}
func (a *application) readSettings(d *managementDialog) {
	d.busy = true
	d.message = ""
	d.settings = relay.AgentSettings{}
	a.readAgent(d.machine, d.agent, "get_agent_settings", nil, func(data relay.Message, err error) {
		d.busy = false
		if err != nil {
			d.message = err.Error()
			return
		}
		d.settings = relay.Decode[relay.AgentSettings](data)
		d.model = d.settings.Model
		d.effort = d.settings.Effort
		if !d.settings.Available {
			d.message = d.settings.Reason
			if d.message == "" {
				d.message = "此 Agent 暂不支持修改模型，请在终端设置"
			}
		}
		key := operationKey(d.machine, d.agent)
		if d.settings.Available && a.operations[key] == "set_agent_settings" && !a.busyOperations[key] {
			if err := a.setOperation(key, ""); err != nil {
				d.message = err.Error()
			}
		}
	})
}
func (a *application) submitManagement(d *managementDialog) {
	if d.busy {
		return
	}
	m := d.machine
	if m.epoch != d.epoch {
		d.message = "连接已变化，请关闭后重新打开"
		return
	}
	var payload relay.Message
	var err error
	action := d.kind
	switch action {
	case "agent_start":
		if !slices.ContainsFunc(m.workspaces, func(w relay.Workspace) bool {
			return w.ID == d.workspace.ID && w.ServerSessionID == d.workspace.ServerSessionID && w.Cwd == d.workspace.Cwd
		}) {
			d.message = "项目已变化，请刷新列表"
			return
		}
		if !slices.ContainsFunc(m.profiles, func(p relay.Profile) bool { return p.ID == d.profile }) || !reflect.DeepEqual(d.defaults[d.profile], m.launchDefaults[d.profile]) {
			d.message = "Agent 配置已变化，请重新打开"
			return
		}
		payload, err = relay.LaunchPayload(d.workspace, d.profile, strings.TrimSpace(d.name), d.prompt, d.arguments, m.capabilities["agent_launch_args_v1"])
	case "agent_rename":
		payload, err = relay.RenamePayload(d.name)
	case "tab_reorder":
		var index int
		index, err = strconv.Atoi(strings.TrimSpace(d.name))
		if err != nil || index < 1 || index > len(m.agents) {
			err = errors.New("请输入有效的目标序号（从 1 开始）")
		} else {
			payload = relay.Message{"insert_index": index - 1}
		}
	case "settings":
		action = "set_agent_settings"
		payload, err = d.settings.Payload(d.model, d.effort)
	}
	if err != nil {
		d.message = err.Error()
		return
	}
	d.busy = true
	d.message = ""
	expectedModel, expectedEffort := d.model, d.effort
	done := func(data relay.Message, err error) {
		d.busy = false
		if err != nil {
			d.message = err.Error()
			if d.kind == "settings" {
				d.settings.Available = false
			}
			return
		}
		if d.kind == "settings" {
			if _, ok := exactAgent(m, d.agent); !ok || m.epoch != d.epoch {
				d.settings.Available = false
				d.message = "会话已变化，请重新读取当前设置"
				return
			}
			actual := relay.Decode[relay.AgentSettings](data)
			d.settings = actual
			d.model = actual.Model
			d.effort = actual.Effort
			if !actual.Available || actual.Model != expectedModel || actual.Effort != expectedEffort {
				d.settings.Available = false
				d.message = "返回的设置与选择不一致，请重新读取确认"
				return
			}
			d.message = "设置已保存"
			if actual.NextTurn {
				d.message = "设置已保存，将用于下一轮对话"
			}
			return
		}
		a.status = d.title + "已完成"
		if a.management == d {
			a.management = nil
		}
	}
	if action == "agent_start" {
		a.writeOperation(m, payload, launchKey(m, d.workspace), 80*time.Second, done)
	} else {
		a.writeAgent(m, d.agent, action, payload, done)
	}
}
func (a *application) agentMenu(menu *ui.Menu, m *machine, agent relay.Agent) {
	for _, item := range []struct{ kind, label string }{{"git", "Git 更改…"}, {"diagnostics", "会话诊断…"}, {"activity", "活动记录…"}} {
		disabled := m.session == nil
		if item.kind == "diagnostics" {
			disabled = disabled || !m.capabilities["session_binding_diagnostics_v1"]
		}
		if menu.Item(item.label).Disabled(disabled).Chosen() {
			a.openTools(m, agent, item.kind)
		}
	}
	muted := a.collapsed[groupKey("mute", m.config.ID, agent.Identity())]
	label := "静音此 Agent"
	if muted {
		label = "恢复此 Agent 通知"
	}
	if menu.Item(label).Chosen() {
		a.toggleGroup(groupKey("mute", m.config.ID, agent.Identity()))
	}
	menu.Separator()
	for _, item := range []struct{ action, label string }{{"agent_rename", "重命名…"}, {"settings", "模型与思考强度…"}, {"tab_reorder", "调整顺序…"}} {
		disabled := !a.agentWriteAllowed(m, agent, item.action)
		if item.action == "settings" {
			disabled = m.session == nil || agent.Agent != "codex" || !m.capabilities["agent_model_settings_v1"]
		}
		if menu.Item(item.label).Disabled(disabled).Chosen() {
			a.openManagement(m, agent, item.action)
		}
	}
	menu.Separator()
	if menu.Item("复制 Agent ID").Disabled(agent.AgentSessionID == "").Chosen() {
		mygo.Clipboard.WriteText(agent.AgentSessionID)
	}
	if menu.Item("复制最新回复").Disabled(m.session == nil || !agent.CanChat()).Chosen() {
		a.readAgent(m, agent, "copy_agent_response", nil, func(data relay.Message, err error) {
			if err != nil {
				a.status = err.Error()
			} else {
				mygo.Clipboard.WriteText(relay.Text(data, "text"))
			}
		})
	}
	menu.Separator()
	for _, item := range []struct{ action, label string }{{"agent_stop", "停止…"}, {"agent_restart", "重启…"}, {"agent_clear", "清空会话…"}} {
		if menu.Item(item.label).Disabled(!a.agentWriteAllowed(m, agent, item.action)).Chosen() {
			a.openManagement(m, agent, item.action)
		}
	}
}
func (a *application) operationNotice(c *ui.Context, key string) {
	if a.operations[key] == "" {
		return
	}
	ui.Column(c).Gap(7).Children(func() {
		if a.busyOperations[key] {
			ui.Row(c).Gap(8).Children(func() { ui.Spinner(c).Size(16, 16); ui.Text(c, "正在处理…").FontSize(12) })
		} else {
			ui.Text(c, "上次操作结果未确认，请先检查 Agent 列表或终端。").TextColor(c.Theme().Warning).FontSize(12)
			if ui.Button(c, "已核对结果，解除锁定").Clicked() {
				if err := a.setOperation(key, ""); err != nil {
					a.status = err.Error()
				} else {
					for _, m := range a.machines {
						for _, agent := range m.agents {
							if operationKey(m, agent) == key {
								if chat := a.chats[m.config.ID+":"+agent.Identity()]; chat != nil {
									chat.question.awaiting = ""
								}
							}
						}
					}
				}
			}
		}
	})
}
func labeledInput(c *ui.Context, label string, value *string, placeholder string) {
	ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.TextInput(c, value).Label(label).Placeholder(placeholder).FillWidth()
}
func (a *application) managementView(c *ui.Context) {
	d := a.management
	if d == nil {
		return
	}
	open := true
	m := d.machine
	ui.Modal(c, &open, func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			ui.Text(c, d.title).Bold().FontSize(19).Grow(1)
			if ui.Button(c, "关闭").Clicked() {
				open = false
			}
		})
		subtitle := m.config.Name
		if d.kind != "agent_start" {
			subtitle += " / " + d.agent.Title()
		}
		ui.Text(c, subtitle).TextColor(c.Theme().TextMuted).FontSize(12)
		key := operationKey(m, d.agent)
		if d.kind == "agent_start" {
			key = launchKey(m, d.workspace)
		}
		a.operationNotice(c, key)
		ui.Scroll(c).MaxHeight(470).Gap(12).Children(func() {
			switch d.kind {
			case "agent_start":
				ui.Text(c, d.workspace.Label+" · "+d.workspace.Cwd).Selectable().FontSize(13)
				labels, ids := []string{}, []string{}
				for _, p := range m.profiles {
					label := p.Label
					if label == "" {
						label = p.ID
					}
					labels = append(labels, label+" ("+p.ID+")")
					ids = append(ids, p.ID)
				}
				choice := ""
				if i := slices.Index(ids, d.profile); i >= 0 {
					choice = labels[i]
				}
				ui.Text(c, "Agent 类型").FontSize(12)
				ui.Select(c, &choice, labels).Label("Agent 类型").Disabled(d.busy)
				if i := slices.Index(labels, choice); i >= 0 {
					d.profile = ids[i]
				}
				labeledInput(c, "名称", &d.name, "例如：code-review")
				ui.Text(c, "初始任务（可选）").FontSize(12)
				ui.TextArea(c, &d.prompt).Lines(3, 6).Label("初始任务")
				if defaults := d.defaults[d.profile]; len(defaults) > 0 {
					ui.Text(c, "默认启动参数").FontSize(12)
					ui.Text(c, strings.Join(defaults, " ")).FontSize(12).Selectable()
					if d.profile == "codex" {
						ui.Text(c, "自动在终端标题显示会话 ID 和名称。").FontSize(12).TextColor(c.Theme().TextMuted)
					}
				}
				labeledInput(c, "额外启动参数（可选）", &d.arguments, "例如：--model gpt-5")
			case "agent_rename":
				labeledInput(c, "Agent 名称", &d.name, "")
				ui.Text(c, "同一标签页中的分屏将使用相同名称。").FontSize(12).TextColor(c.Theme().TextMuted)
			case "tab_reorder":
				labeledInput(c, "目标序号", &d.name, "从 1 开始")
			case "settings":
				if ui.Button(c, "重新读取设置").Disabled(d.busy).Clicked() {
					a.readSettings(d)
				}
				if d.settings.Available {
					labels, ids := []string{}, []string{}
					for _, model := range d.settings.Models {
						label := model.DisplayName
						if label == "" {
							label = model.Model
						}
						labels = append(labels, label+" ("+model.Model+")")
						ids = append(ids, model.Model)
					}
					choice := ""
					if i := slices.Index(ids, d.model); i >= 0 {
						choice = labels[i]
					}
					ui.Text(c, "模型").FontSize(12)
					ui.Select(c, &choice, labels).Label("模型").Disabled(d.busy)
					if i := slices.Index(labels, choice); i >= 0 && ids[i] != d.model {
						d.model = ids[i]
						d.effort = d.settings.Models[i].DefaultEffort
					}
					efforts := []string{}
					for _, model := range d.settings.Models {
						if model.Model == d.model {
							for _, e := range model.Efforts {
								efforts = append(efforts, e.Value)
							}
						}
					}
					ui.Text(c, "思考强度").FontSize(12)
					ui.Select(c, &d.effort, efforts).Label("思考强度").Disabled(d.busy)
				}
			case "agent_stop":
				ui.Text(c, "停止当前 Agent 进程。").FontSize(14)
			case "agent_restart":
				ui.Text(c, "结束当前进程并使用原启动配置重新启动。").FontSize(14)
			case "agent_clear":
				ui.Text(c, "清空当前 Agent 的对话上下文，此操作无法撤销。").FontSize(14)
			}
		})
		if d.busy {
			ui.Spinner(c).Size(18, 18)
		}
		if d.message != "" {
			if d.kind == "agent_start" {
				title, color := "创建失败", c.Theme().Danger
				if a.operations[key] != "" {
					title, color = "创建结果待确认", c.Theme().Warning
				}
				ui.Column(c).FillWidth().Padding(12).Gap(6).Border(1, color).Radius(8).Role(ui.RoleAlert).Children(func() {
					ui.Text(c, title).Bold().FontSize(14).TextColor(color)
					ui.Text(c, d.message).FontSize(13).TextColor(color).Wrap().Selectable()
				})
			} else {
				ui.Text(c, d.message).FontSize(12).Selectable()
			}
		}
		action := d.kind
		if action == "settings" {
			action = "set_agent_settings"
		}
		enabled := !d.busy && managementReady(m) && m.epoch == d.epoch && a.operations[key] == ""
		if action != "agent_start" {
			enabled = enabled && a.agentWriteAllowed(m, d.agent, action)
		}
		if d.kind == "settings" {
			enabled = enabled && d.settings.Available
		}
		label := "确认"
		if d.kind == "agent_start" {
			label = "创建 Agent"
		} else if d.kind == "settings" || d.kind == "agent_rename" {
			label = "保存"
		}
		if ui.PrimaryButton(c, label).Disabled(!enabled).Clicked() {
			a.submitManagement(d)
		}
	}).Width(600).MaxWidth(720)
	if !open {
		a.management = nil
	}
}
