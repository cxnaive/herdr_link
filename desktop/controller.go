package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/herdr-terminal"
)

func (a *application) selected() (*machine, *conversation) {
	for _, m := range a.machines {
		if m.config.ID == a.selectedMachine {
			for _, agent := range m.agents {
				if agent.PaneID == a.selectedAgent {
					key := m.config.ID + ":" + agent.Identity()
					chat := a.chats[key]
					if chat == nil {
						chat = &conversation{agent: agent, folds: map[string]*bool{}}
						if a.store != nil {
							chat.draft, chat.uncertain = a.store.draft(key)
							chat.attachments = a.store.attachments(key)
							cached := a.store.cachedHistory(key)
							chat.entries = cached.Entries
							chat.revision = cached.Revision
							chat.historyBookmark = cached.Bookmark
							if len(chat.entries) > 0 {
								chat.message = "正在同步，当前显示最近保存的对话"
							}
						}
						a.chats[key] = chat
					}
					chat.agent = agent
					return m, chat
				}
			}
		}
	}
	return nil, nil
}
func (a *application) saveCurrentDraft() {
	m, c := a.selected()
	if m == nil || c == nil || a.store == nil {
		return
	}
	if err := a.store.saveDraft(m.config.ID+":"+c.agent.Identity(), c.draft, c.uncertain); err != nil {
		a.status = err.Error()
	}
}
func (a *application) selectAgent(m *machine, agent relay.Agent) {
	a.saveCurrentDraft()
	if previous, old := a.selected(); old != nil {
		a.cancelHistoryLoad(old, false)
		a.stopConversationUpdates(previous, old)
	}
	if _, old := a.selected(); old != nil && old.uploadCancel != nil {
		old.uploadCancel()
	}
	if _, old := a.selected(); old != nil && old.files != nil {
		old.files.closePDF()
	}
	a.imagePreview = nil
	a.selectedMachine = m.config.ID
	a.selectedAgent = agent.PaneID
	a.pairing = false
	if _, chat := a.selected(); chat != nil && !chat.historyBookmark.Paused && a.mode == 0 {
		a.loadHistory(false)
	}
	a.refreshRun()
	if a.files {
		_, chat := a.selected()
		if chat != nil {
			a.ensureFiles(m, chat)
		}
	}
}

func (a *application) setMode(mode int) {
	if a.mode == mode {
		return
	}
	a.mode = mode
	m, chat := a.selected()
	if chat == nil {
		return
	}
	if mode == 1 {
		a.cancelHistoryLoad(chat, false)
		chat.historyWork = nil
		a.stopConversationUpdates(m, chat)
	} else if !chat.historyBookmark.Paused {
		a.loadHistory(false)
	}
}
func (a *application) pair() {
	if a.pairBusy || a.store == nil {
		return
	}
	c, err := relay.ParseInvitation(a.pairLink, a.pairName, time.Now())
	if err != nil {
		a.status = err.Error()
		return
	}
	a.pairBusy = true
	go func() {
		err := a.store.saveComputer(c)
		a.win.Update(func() {
			a.pairBusy = false
			if err != nil {
				a.status = err.Error()
				return
			}
			m := &machine{config: c, status: "未连接"}
			a.machines = append(a.machines, m)
			a.pairing = false
			a.pairLink = ""
			a.pairName = ""
			a.status = ""
			a.connect(m)
		})
	}()
}
func (a *application) connect(m *machine) {
	a.applyFileAccess(m, false, true)
	for key, chat := range a.chats {
		if strings.HasPrefix(key, m.config.ID+":") {
			chat.historySynchronized = false
			chat.resumeOlder = chat.resumeOlder || chat.loading && chat.historyOlder
			a.cancelHistoryLoad(chat, false)
			chat.historyWork = nil
			chat.subscription = ""
			if chat.updateCancel != nil {
				chat.updateCancel()
			}
			if chat.uploadCancel != nil {
				chat.uploadCancel()
			}
			if f := chat.files; f != nil {
				f.closePDF()
				if f.cancel != nil {
					f.cancel()
				}
				if f.cancelDownload != nil {
					f.cancelDownload()
				}
			}
			chat.files = nil
			chat.images = nil
			chat.md = nil
			chat.run = runPresentation{}
		}
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.session != nil {
		m.session.Close()
		m.session = nil
	}
	if m.term != nil {
		m.term.Close()
		m.term = nil
	}
	if m.remote != nil {
		m.remote.Close()
		m.remote = nil
	}
	m.opening = false
	m.inventoryReady = false
	for i := range m.agents {
		m.agents[i].RunState = nil
	}
	m.protocol = 0
	m.capabilities = nil
	m.profiles = nil
	m.launchDefaults = nil
	m.epoch++
	epoch := m.epoch
	ctx, cancel := context.WithCancel(a.ctx)
	m.cancel = cancel
	m.status = "正在连接…"
	config := m.config
	go func() {
		var auth = config.Auth
		s, err := relay.Connect(ctx, config, func(next relay.Auth) error {
			if err := a.store.auth(config.ID, next); err != nil {
				return err
			}
			auth = next
			return nil
		})
		a.win.Update(func() {
			if epoch != m.epoch || ctx.Err() != nil {
				if s != nil {
					s.Close()
				}
				return
			}
			m.config.Auth = auth
			if err != nil {
				m.status = "连接失败"
				a.status = err.Error()
				return
			}
			m.session = s
			m.status = "已加密连接"
			if m.config.LocalSocket != "" {
				m.status = "本机直连 · 已加密"
			}
			a.status = ""
		})
		if err != nil {
			return
		}
		for event := range s.Events {
			a.win.Update(func() {
				if epoch == m.epoch && ctx.Err() == nil {
					a.event(m, event)
				}
			})
		}
		a.win.Update(func() {
			if epoch == m.epoch {
				a.applyFileAccess(m, false, true)
				m.status = "连接已断开"
				m.session = nil
				if m.remote != nil {
					m.remote.Close()
					m.remote = nil
				}
				m.termState = "连接中断，请重新连接"
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(4 * time.Second):
			a.win.Update(func() {
				if m.epoch == epoch && ctx.Err() == nil {
					a.connect(m)
				}
			})
		}
	}()
}
func (a *application) event(m *machine, event relay.Message) {
	switch relay.Text(event, "type") {
	case "activity_history":
		if p := a.toolsPanel; p != nil && p.kind == "activity" && p.machine == m && p.busy {
			p.busy = false
			p.data = event
		}
	case "inventory_status":
		m.inventoryReady = relay.Text(event, "state") == "ready"
		if !m.inventoryReady {
			for i := range m.agents {
				m.agents[i].RunState = nil
			}
		}
	case "workspaces":
		m.workspaces = relay.ParseWorkspaces(event["workspaces"])
	case "push_config":
		m.protocol = relay.Number(event, "protocol")
		m.inventoryReady = relay.Text(relay.Object(event["inventory"]), "state") == "ready"
		m.profiles = relay.Decode[[]relay.Profile](event["agent_profiles"])
		m.launchDefaults = relay.Decode[map[string][]string](event["agent_launch_defaults"])
		m.capabilities = map[string]bool{}
		if names, ok := event["capabilities"].([]any); ok {
			for _, n := range names {
				if s, ok := n.(string); ok {
					m.capabilities[s] = true
				}
			}
		}
		a.applyFileAccess(m, event["filesystem_read"] == true, false)
	case "agents":
		next := relay.ParseAgents(event["agents"])
		a.notifyAgentChanges(m, next)
		m.agents = next
		slices.SortStableFunc(m.agents, func(x, y relay.Agent) int { return strings.Compare(x.WorkspaceID, y.WorkspaceID) })
		if m.term != nil && !slices.ContainsFunc(m.agents, func(agent relay.Agent) bool { return agent.Identity() == m.termIdentity }) {
			m.term.Close()
			m.term = nil
			if m.remote != nil {
				m.remote.Close()
				m.remote = nil
			}
			m.termState = "终端身份已变化，请重新连接"
		}
		a.applyTerminalBackgrounds(m)
		if a.selectedMachine == "" && len(m.agents) > 0 {
			a.selectAgent(m, m.agents[0])
		}
	case "agent_update":
		// Read a complete inventory after incremental changes; never infer identity.
		if m.session != nil {
			go m.session.Send(relay.Message{"type": "refresh_agents", "protocol": 3})
		}
	}
}
func (a *application) send() {
	m, c := a.selected()
	if m == nil || c == nil || a.store == nil || !a.agentWriteAllowed(m, c.agent, "submit_prompt") || c.agent.Status == "blocked" || c.uploading {
		return
	}
	text := strings.TrimSpace(c.draft)
	if text == "" && len(c.attachments) == 0 {
		return
	}
	var attachmentErr error
	text, attachmentErr = relay.PromptWithAttachments(text, c.attachments, time.Now())
	if attachmentErr != nil {
		c.message = attachmentErr.Error()
		return
	}
	key := m.config.ID + ":" + c.agent.Identity()
	if err := a.store.saveDraft(key, c.draft, true); err != nil {
		a.status = err.Error()
		return
	}
	c.pending = true
	c.uncertain = true
	s, agent := m.session, c.agent
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		request := agent.Request("submit_prompt")
		request["text"] = text
		_, err := s.Request(ctx, request)
		a.win.Update(func() {
			c.pending = false
			if !slices.Contains(a.machines, m) {
				return
			}
			if err != nil {
				if !relay.UnknownOutcome(err) {
					if saveErr := a.store.saveDraft(key, c.draft, false); saveErr == nil {
						c.uncertain = false
					}
				}
				c.message = err.Error()
				return
			}
			c.uncertain = false
			c.draft = ""
			c.message = ""
			if err = a.store.saveAttachments(key, nil); err != nil {
				c.uncertain = true
				c.message = err.Error()
				return
			}
			c.attachments = nil
			if err = a.store.saveDraft(key, "", false); err != nil {
				c.uncertain = true
				c.message = err.Error()
			}
			a.loadHistory(false)
		})
	}()
}
func (a *application) openTerminal() {
	m, c := a.selected()
	if m == nil || c == nil || m.opening {
		return
	}
	if m.term != nil && m.termIdentity == c.agent.Identity() {
		return
	}
	if m.term != nil {
		m.term.Close()
		m.term = nil
	}
	if m.remote != nil {
		m.remote.Close()
		m.remote = nil
	}
	if a.demo {
		term, err := terminal.New(a.terminalAppearanceOptions(""))
		if err != nil {
			m.termState = err.Error()
			return
		}
		m.term = term
		m.termIdentity = c.agent.Identity()
		m.termState = "本机终端 · 演示"
		return
	}
	if m.session == nil {
		m.termState = "请先连接电脑"
		return
	}
	if !m.capabilities["desktop_terminal_v1"] {
		m.termState = "请更新这台电脑上的 Relay 后再连接终端"
		return
	}
	if m.config.Auth.Role != "controller" {
		m.termState = "当前设备为只读权限"
		return
	}
	m.opening = true
	m.termState = "正在连接终端…"
	remote := relay.NewTerminalConn(m.session, c.agent)
	m.remote = remote
	identity, epoch := c.agent.Identity(), m.epoch
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		err := remote.Open(ctx)
		a.win.Update(func() {
			m.opening = false
			if m.epoch != epoch || m.remote != remote {
				remote.Close()
				return
			}
			if err != nil {
				m.termState = err.Error()
				m.remote = nil
				return
			}
			opts := a.terminalAppearanceOptions(c.agent.Agent)
			opts.Conn = remote
			opts.ResizeByConn = true
			opts.OnScroll = func(lines int) bool { go remote.Scroll(lines); return true }
			opts.OnExit = func(_ int) {
				a.win.Update(func() {
					if m.epoch == epoch && m.remote == remote {
						if m.term != nil {
							m.term.Close()
						}
						m.term = nil
						m.remote = nil
						m.termState = "终端已断开，可重新连接"
					}
				})
			}
			term, termErr := terminal.New(opts)
			if termErr != nil {
				remote.Close()
				m.termState = termErr.Error()
				m.remote = nil
				return
			}
			remote.Start(term.Resize)
			m.term = term
			m.termIdentity = identity
			m.termState = "终端已连接"
			a.applyTerminalBackgrounds(m)
		})
	}()
}
func (a *application) loadDemo() {
	a.status = ""
	for i, name := range []string{"研究工作站", "训练服务器"} {
		m := &machine{config: relay.Computer{ID: fmt.Sprint(i), Name: name}, status: "演示"}
		for j, title := range []string{"评测报告", "结果复核"} {
			agent := relay.Agent{Target: relay.Target{ServerSessionID: "primary", PaneID: fmt.Sprintf("pane-%d", j), TerminalID: fmt.Sprintf("term-%d", j), AgentSessionID: "demo"}, Name: title, Agent: "codex", Cwd: "/work/robotics", Project: "机器人研究", WorkspaceID: "robotics", Status: []string{"working", "blocked"}[j], BindingState: "verified"}
			m.agents = append(m.agents, agent)
			a.chats[m.config.ID+":"+agent.Identity()] = &conversation{agent: agent, folds: map[string]*bool{}, entries: []entry{
				{ID: "user", Role: "user", Text: "检查评测结果，整理一份简短报告。"},
				{ID: "assistant", Role: "assistant", Text: "已完成首轮核对。\n\n### 评测摘要\n\n成功率 **96.0%**，较基线提高 4 个百分点。\n\n验证集划分一致，没有发现数据泄漏。接下来复查失败样例。", Tools: []map[string]any{{"name": "read_file", "input": "eval-summary.json", "output": "校验完成"}}},
			}}
		}
		a.machines = append(a.machines, m)
	}
	a.selectedMachine = "0"
	a.selectedAgent = "pane-0"
}
