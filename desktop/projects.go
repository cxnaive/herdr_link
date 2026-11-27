package main

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
)

type projectDialog struct {
	kind, title, name, path, branch, base, message string
	machine                                        *machine
	workspace                                      relay.Workspace
	epoch                                          uint64
	busy                                           bool
	trees                                          []relay.Message
}

func (a *application) openProject(m *machine, w relay.Workspace, kind string) {
	titles := map[string]string{"computer_rename": "电脑名称", "computer_remove": "移除电脑", "workspace_create": "添加项目", "workspace_rename": "重命名项目", "workspace_close": "关闭项目", "workspace_reorder": "项目排序", "worktree_list": "Git Worktree", "worktree_create": "创建 Worktree", "worktree_remove": "移除 Worktree", "worktree_open": "打开 Worktree"}
	d := &projectDialog{kind: kind, title: titles[kind], machine: m, workspace: w, epoch: m.epoch, name: w.Label, path: w.Cwd}
	if kind == "computer_rename" {
		d.name = m.config.Name
	}
	if kind == "workspace_reorder" {
		d.name = strconv.Itoa(w.Number + 1)
	}
	a.projectDialog = d
	if kind == "worktree_list" {
		d.busy = true
		s := m.session
		if s == nil {
			d.busy = false
			d.message = "电脑已断开"
			return
		}
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
			defer cancel()
			data, err := s.Request(ctx, relay.Message{"type": "worktree_list", "workspace_id": w.ID, "server_session_id": w.ServerSessionID})
			a.update(func() {
				d.busy = false
				if m.epoch != d.epoch {
					d.message = "连接已变化，请重新打开"
					return
				}
				if err != nil {
					d.message = err.Error()
				} else {
					d.trees = relay.Decode[[]relay.Message](data["worktrees"])
				}
			})
		}()
	}
}
func (a *application) computerMenu(menu *ui.Menu, m *machine) {
	if menu.Item("文件访问范围…").Disabled(m.session == nil || !m.capabilities["filesystem_read_opt_in_v1"]).Chosen() {
		a.openFileAccess(m)
	}
	if menu.Item("配对设备…").Disabled(m.session == nil).Chosen() {
		a.openDevices(m)
	}
	if menu.Item("重命名电脑…").Disabled(a.store == nil).Chosen() {
		a.openProject(m, relay.Workspace{}, "computer_rename")
	}
	if menu.Item("添加项目…").Disabled(!managementReady(m)).Chosen() {
		a.openProject(m, relay.Workspace{}, "workspace_create")
	}
	if menu.Item("刷新列表").Disabled(m.session == nil).Chosen() {
		go m.session.Send(relay.Message{"type": "refresh_agents", "protocol": 3})
	}
	if menu.Item("移除电脑…").Disabled(a.store == nil).Chosen() {
		a.openProject(m, relay.Workspace{}, "computer_remove")
	}
}
func (a *application) projectMenu(menu *ui.Menu, m *machine, w relay.Workspace) {
	if menu.Item("新建 Agent…").Disabled(!managementReady(m) || w.ServerSessionID != "primary" || len(m.profiles) == 0).Chosen() {
		a.openLaunch(m, w)
	}
	menu.Separator()
	for _, item := range []struct{ kind, label string }{{"workspace_rename", "重命名项目…"}, {"workspace_reorder", "调整项目顺序…"}, {"worktree_list", "查看 Worktree…"}, {"worktree_create", "创建 Worktree…"}, {"workspace_close", "关闭项目…"}, {"worktree_remove", "移除 Worktree…"}} {
		disabled := !managementReady(m) || w.ServerSessionID != "primary"
		if item.kind == "worktree_list" {
			disabled = m.session == nil || w.ServerSessionID != "primary"
		}
		if menu.Item(item.label).Disabled(disabled).Chosen() {
			a.openProject(m, w, item.kind)
		}
	}
}
func (a *application) submitProject(d *projectDialog) {
	if d.busy {
		return
	}
	m := d.machine
	if d.kind == "computer_rename" {
		value, err := relay.RenamePayload(d.name)
		if err != nil {
			d.message = err.Error()
			return
		}
		next := m.config
		next.Name = relay.Text(value, "name")
		if err = a.store.saveComputer(next); err != nil {
			d.message = err.Error()
			return
		}
		m.config = next
		a.projectDialog = nil
		return
	}
	if d.kind == "computer_remove" {
		if err := a.store.removeComputer(m.config.ID); err != nil {
			d.message = err.Error()
			return
		}
		a.collapsed = a.store.sidebarGroups()
		a.operations = a.store.operations()
		m.epoch++
		if m.fileCancel != nil {
			m.fileCancel()
		}
		if m.cancel != nil {
			m.cancel()
		}
		if m.session != nil {
			m.session.Close()
		}
		if m.remote != nil {
			m.remote.Close()
		}
		if m.term != nil {
			m.term.Close()
		}
		for key, chat := range a.chats {
			if strings.HasPrefix(key, m.config.ID+":") {
				if chat.files != nil {
					chat.files.closePDF()
					if chat.files.cancel != nil {
						chat.files.cancel()
					}
					if chat.files.cancelDownload != nil {
						chat.files.cancelDownload()
					}
				}
				if chat.uploadCancel != nil {
					chat.uploadCancel()
				}
				delete(a.chats, key)
			}
		}
		a.machines = slices.DeleteFunc(a.machines, func(v *machine) bool { return v == m })
		if a.selectedMachine == m.config.ID {
			a.selectedMachine = ""
			a.selectedAgent = ""
		}
		a.projectDialog = nil
		return
	}
	if m.epoch != d.epoch || !managementReady(m) {
		d.message = "连接或权限已变化，请重新打开"
		return
	}
	if d.workspace.ID != "" && !slices.ContainsFunc(m.workspaces, func(w relay.Workspace) bool {
		return w.ID == d.workspace.ID && w.Cwd == d.workspace.Cwd && w.ServerSessionID == d.workspace.ServerSessionID
	}) {
		d.message = "项目已变化，请刷新列表"
		return
	}
	req := relay.Message{"type": d.kind, "server_session_id": "primary"}
	if d.workspace.ID != "" {
		req["workspace_id"] = d.workspace.ID
	}
	switch d.kind {
	case "workspace_create", "workspace_rename":
		value, err := relay.RenamePayload(d.name)
		if err != nil {
			d.message = err.Error()
			return
		}
		req["label"] = value["name"]
		if d.kind == "workspace_create" {
			p := strings.TrimSpace(d.path)
			if !relay.RemoteDirectoryPath(p) {
				d.message = "请输入远端电脑上的绝对目录路径"
				return
			}
			req["cwd"] = p
		}
	case "workspace_reorder":
		i, err := strconv.Atoi(strings.TrimSpace(d.name))
		if err != nil || i < 1 || i > len(m.workspaces) {
			d.message = "请输入有效的项目序号（从 1 开始）"
			return
		}
		req["insert_index"] = i - 1
	case "worktree_create":
		if strings.TrimSpace(d.branch) == "" {
			d.message = "请输入分支名称"
			return
		}
		req["branch"] = d.branch
		req["base"] = d.base
		req["label"] = d.name
	case "worktree_open":
		req["path"] = d.path
	case "worktree_remove":
		req["force"] = false
	}
	d.busy = true
	d.message = ""
	a.writeOperation(m, req, groupKey("project", m.config.ID, d.workspace.ID), 80*time.Second, func(_ relay.Message, err error) {
		d.busy = false
		if err != nil {
			d.message = err.Error()
		} else {
			a.status = d.title + "已完成"
			if a.projectDialog == d {
				a.projectDialog = nil
			}
		}
	})
}
func (s *storage) removeComputer(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state
	next := savedState{Computers: []relay.Computer{}, Drafts: map[string]string{}, Uncertain: map[string]bool{}, Collapsed: map[string]bool{}, Operations: map[string]string{}, Attachments: map[string][]relay.Attachment{}, History: map[string]cachedConversation{}}
	next.Appearance = old.Appearance
	for _, c := range old.Computers {
		if c.ID != id {
			next.Computers = append(next.Computers, c)
		}
	}
	for k, v := range old.Drafts {
		if !strings.HasPrefix(k, id+":") {
			next.Drafts[k] = v
		}
	}
	for k, v := range old.Uncertain {
		if !strings.HasPrefix(k, id+":") {
			next.Uncertain[k] = v
		}
	}
	for k, v := range old.Attachments {
		if !strings.HasPrefix(k, id+":") {
			next.Attachments[k] = v
		}
	}
	for k, v := range old.History {
		if !strings.HasPrefix(k, id+":") {
			next.History[k] = v
		}
	}
	keep := func(key string) bool {
		var parts []string
		if json.Unmarshal([]byte(key), &parts) != nil {
			return true
		}
		return !slices.Contains(parts, id)
	}
	for k, v := range old.Collapsed {
		if keep(k) {
			next.Collapsed[k] = v
		}
	}
	for k, v := range old.Operations {
		if keep(k) {
			next.Operations[k] = v
		}
	}
	s.state = next
	if err := s.vault.Save(next); err != nil {
		s.state = old
		return err
	}
	return nil
}
func (a *application) projectView(c *ui.Context) {
	d := a.projectDialog
	if d == nil {
		return
	}
	open := true
	m := d.machine
	local := strings.HasPrefix(d.kind, "computer_")
	ui.Modal(c, &open, func() {
		ui.Row(c).Children(func() {
			ui.Text(c, d.title).Bold().FontSize(19).Grow(1)
			if ui.Button(c, "关闭窗口").Clicked() {
				open = false
			}
		})
		ui.Text(c, m.config.Name).FontSize(12).TextColor(c.Theme().TextMuted)
		if !local {
			a.operationNotice(c, groupKey("project", m.config.ID, d.workspace.ID))
		}
		switch d.kind {
		case "computer_rename", "workspace_rename":
			labeledInput(c, "名称", &d.name, "")
		case "workspace_create":
			labeledInput(c, "项目名称", &d.name, "")
			labeledInput(c, "远端目录", &d.path, "/home/user/project")
		case "workspace_reorder":
			labeledInput(c, "目标序号", &d.name, "从 1 开始")
		case "computer_remove":
			ui.Text(c, "移除本机保存的连接凭据、草稿和缓存。远端 Agent 将继续运行。").FontSize(13)
		case "workspace_close":
			ui.Text(c, "关闭项目及其中的会话："+d.workspace.Label).FontSize(13)
		case "worktree_create":
			labeledInput(c, "分支", &d.branch, "")
			labeledInput(c, "基础分支（可选）", &d.base, "")
			labeledInput(c, "名称（可选）", &d.name, "")
		case "worktree_remove":
			ui.Text(c, "删除此 Worktree 的检出目录："+d.workspace.Cwd+"。存在未提交更改时将停止操作。").FontSize(13)
		case "worktree_open":
			ui.Text(c, "打开："+d.path).FontSize(13)
		case "worktree_list":
			ui.Scroll(c).Height(320).Gap(8).Children(func() {
				for _, tree := range d.trees {
					p := relay.Text(tree, "path")
					if ui.Button(c, p).FillWidth().Disabled(!managementReady(m)).Clicked() {
						a.openProject(m, d.workspace, "worktree_open")
						a.projectDialog.path = p
					}
				}
			})
		}
		if d.busy {
			ui.Spinner(c).Size(18, 18)
		}
		if d.message != "" {
			ui.Text(c, d.message).TextColor(c.Theme().Warning).FontSize(12)
		}
		if d.kind != "worktree_list" && ui.PrimaryButton(c, "确认操作").Disabled(d.busy || !local && (!managementReady(m) || a.operations[groupKey("project", m.config.ID, d.workspace.ID)] != "")).Clicked() {
			a.submitProject(d)
		}
	}).Width(580)
	if !open {
		a.projectDialog = nil
	}
}
