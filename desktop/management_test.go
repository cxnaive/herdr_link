package main

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/cxnaive/herdr-desktop/internal/vault"
	"github.com/egoist/mygo/ui"
)

func managementFixture(t *testing.T) (*application, *machine, *conversation) {
	t.Helper()
	a := newApplication(true)
	t.Cleanup(a.cancel)
	a.loadDemo()
	a.machines = a.machines[:1]
	m, chat := a.selected()
	m.session = &relay.Session{}
	m.protocol = 3
	m.inventoryReady = true
	m.config.Auth.Role = "controller"
	m.workspaces = []relay.Workspace{{ID: "robotics", ServerSessionID: "primary", Cwd: "/work/robotics", Label: "机器人研究"}}
	m.profiles = []relay.Profile{{ID: "codex", Label: "Codex"}, {ID: "claude", Label: "Claude"}}
	m.launchDefaults = map[string][]string{"codex": {"-c", `tui.terminal_title=["thread-id","thread-name"]`}}
	v, err := vault.Open(filepath.Join(t.TempDir(), "state.enc"), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	a.store = &storage{vault: v, state: savedState{Computers: []relay.Computer{m.config}, Drafts: map[string]string{}, Uncertain: map[string]bool{}}}
	return a, m, chat
}
func TestAgentWriteGuardsAndPersistentUncertainty(t *testing.T) {
	a, m, chat := managementFixture(t)
	agent := chat.agent
	if !a.agentWriteAllowed(m, agent, "agent_rename") {
		t.Fatal("valid rename blocked")
	}
	key := operationKey(m, agent)
	if err := a.setOperation(key, "agent_restart"); err != nil {
		t.Fatal(err)
	}
	var saved savedState
	if err := a.store.vault.Load(&saved); err != nil || saved.Operations[key] != "agent_restart" {
		t.Fatal("unknown result not durable", err)
	}
	if a.agentWriteAllowed(m, agent, "agent_restart") {
		t.Fatal("duplicate write allowed")
	}
	a.setOperation(key, "")
	m.config.Auth.Role = "reader"
	if a.agentWriteAllowed(m, agent, "agent_rename") {
		t.Fatal("reader could mutate")
	}
	m.config.Auth.Role = "controller"
	m.inventoryReady = false
	if a.agentWriteAllowed(m, agent, "agent_restart") {
		t.Fatal("stale inventory allowed")
	}
	m.inventoryReady = true
	changed := agent
	changed.BindingEpoch++
	if a.agentWriteAllowed(m, changed, "agent_rename") {
		t.Fatal("binding epoch ignored")
	}
	changed = agent
	changed.Cwd = "/other"
	if a.agentWriteAllowed(m, changed, "agent_rename") {
		t.Fatal("workspace identity ignored")
	}
	chat.uncertain = true
	if a.agentWriteAllowed(m, agent, "agent_stop") {
		t.Fatal("unconfirmed prompt allowed competing write")
	}
	chat.uncertain = false
	m.agents[0].BindingState = "conflict"
	if !a.agentWriteAllowed(m, agent, "agent_restart") || a.agentWriteAllowed(m, agent, "answer_question") {
		t.Fatal("terminal and chat permissions mixed")
	}
}
func TestDesktopManagementMenusAndNewAgentForm(t *testing.T) {
	a, m, _ := managementFixture(t)
	tt := ui.NewTester(a.view, 1380, 900)
	if err := tt.Click("在 机器人研究 新建 Agent"); err != nil {
		t.Fatal(err)
	}
	if a.management == nil || !tt.HasText("默认启动参数") || !tt.HasText("自动在终端标题显示会话 ID 和名称。") {
		t.Fatal("launch UI missing", tt.Texts())
	}
	if a.management.arguments != "" {
		t.Fatal("server defaults copied into user args")
	}
	if path := os.Getenv("HERDR_MANAGEMENT_SCREENSHOT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err = png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if err := tt.RightClick("评测报告"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重命名…"); err != nil {
		t.Fatal(err)
	}
	if a.management == nil || a.management.kind != "agent_rename" || a.management.agent.Target != m.agents[0].Target {
		t.Fatal("rename targeted wrong agent")
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("管理"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("重启…"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("结束当前进程并使用原启动配置重新启动。") {
		t.Fatal("restart confirmation missing")
	}
	// A stale launch form must reject before touching the session.
	a.openLaunch(m, m.workspaces[0])
	d := a.management
	d.name = "review"
	m.launchDefaults["codex"] = []string{"changed"}
	a.submitManagement(d)
	if !strings.Contains(d.message, "配置已变化") || d.busy {
		t.Fatal("changed defaults were dispatched", d.message)
	}
}
func TestNewAgentFailureIsVisibleAndPreservesForm(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			a, m, _ := managementFixture(t)
			a.appearance.Application = theme
			tt := ui.NewTester(a.view, 1000, 768)
			if err := tt.Click("在 机器人研究 新建 Agent"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("创建失败") {
				t.Fatal("fresh form shows an error")
			}
			field, ok := tt.AccessByRole(ui.RoleTextField, "名称")
			if !ok {
				t.Fatal("name input missing")
			}
			tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
			tt.Type("EMHI_data")
			d := a.management
			d.prompt = "保留这段任务"
			d.arguments = "--model demo"
			tt.Frame()
			if err := tt.Click("创建 Agent"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("创建失败") || !tt.HasText("名称须以小写字母开头，最多 32 位，可含数字、_ 和 -") {
				t.Fatal("validation failure not clearly displayed", tt.Texts())
			}
			if a.management != d || d.busy || d.name != "EMHI_data" || d.prompt != "保留这段任务" || d.arguments != "--model demo" {
				t.Fatalf("failure closed or reset the form: same=%v busy=%v name=%q prompt=%q args=%q", a.management == d, d.busy, d.name, d.prompt, d.arguments)
			}
			if len(a.operations) != 0 || len(a.busyOperations) != 0 {
				t.Fatal("invalid form dispatched a write")
			}
			if dir := os.Getenv("HERDR_MANAGEMENT_FAILURE_SCREENSHOTS"); dir != "" {
				f, err := os.Create(filepath.Join(dir, "launch-failure-"+theme+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, tt.Image()); err != nil {
					t.Fatal(err)
				}
			}
			// A corrected name reaches the storage guard, without a live write.
			d.name = "emhi_data"
			m.capabilities = map[string]bool{"agent_launch_args_v1": true}
			a.store = nil
			tt.Frame()
			if err := tt.Click("创建 Agent"); err != nil {
				t.Fatal(err)
			}
			if !tt.HasText("创建失败") || !tt.HasText("加密存储尚未就绪") || tt.HasText("名称须以小写字母开头，最多 32 位，可含数字、_ 和 -") {
				t.Fatal("retry did not replace the previous error", tt.Texts())
			}
			if err := tt.Click("关闭"); err != nil {
				t.Fatal(err)
			}
			if err := tt.Click("在 机器人研究 新建 Agent"); err != nil {
				t.Fatal(err)
			}
			if tt.HasText("创建失败") {
				t.Fatal("new dialog retained the previous failure")
			}
		})
	}
}

func TestNewAgentUnknownOutcomeIsNotShownAsFailure(t *testing.T) {
	a, m, _ := managementFixture(t)
	a.openLaunch(m, m.workspaces[0])
	d := a.management
	d.name = "review"
	d.message = "连接中断，请先核对电脑上的实际创建结果"
	key := launchKey(m, d.workspace)
	if err := a.setOperation(key, "agent_start"); err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.view, 1000, 768)
	if !tt.HasText("创建结果待确认") || tt.HasText("创建失败") || !tt.HasText(d.message) {
		t.Fatal("unknown outcome was presented as a definite failure", tt.Texts())
	}
	if err := tt.Click("创建 Agent"); err != nil {
		t.Fatal(err)
	}
	if a.operations[key] != "agent_start" || a.busyOperations[key] || d.busy {
		t.Fatal("unknown outcome allowed a duplicate write")
	}
}

func TestReaderCannotUseManagementAndQuestionsStayScoped(t *testing.T) {
	a, m, chat := managementFixture(t)
	m.config.Auth.Role = "reader"
	m.agents[0].Interaction = &relay.Interaction{ID: "q", Kind: "single_select", Question: "选择运行范围", Options: []relay.QuestionOption{{Index: 1, Label: "当前项目"}, {Index: 2, Label: "全部项目"}}, SubmitLabel: "提交答案", QuestionIndex: 1, QuestionTotal: 2}
	tt := ui.NewTester(a.view, 1380, 900)
	if !tt.HasText("选择运行范围") {
		t.Fatal("question missing")
	}
	if !tt.HasText("1 / 2") {
		t.Fatal("question number is not server's one-based index")
	}
	_ = tt.Click("当前项目")
	if chat.question.selected[1] {
		t.Fatal("reader could edit answer")
	}
	m.config.Auth.Role = "controller"
	tt.Frame()
	if err := tt.Click("当前项目"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("全部项目"); err != nil {
		t.Fatal(err)
	}
	if chat.question.selected[1] || !chat.question.selected[2] {
		t.Fatal("single select allowed multiple choices")
	}
	m.agents[0].Interaction = &relay.Interaction{ID: "q2", Kind: "single_select", Question: "新问题", Options: []relay.QuestionOption{{Index: 1, Label: "新答案"}}}
	tt.Frame()
	if chat.question.selected[2] || chat.question.id != "q2" {
		t.Fatal("answers carried into new question")
	}
}
func TestConversationDeltaRecoveryAndCacheBounds(t *testing.T) {
	a, m, c := managementFixture(t)
	c.entries = []entry{{ID: "a", Text: "old"}, {ID: "b", Text: "remove"}}
	c.subscription = "sub"
	c.revision = "rev"
	c.sequence = 4
	good := relay.Message{"kind": "batch", "subscription_id": "sub", "source_revision": "rev", "sequence": 5, "remove": []string{"b"}, "entries": []entry{{ID: "a", Text: "updated"}, {ID: "c", Text: "new"}}}
	if !c.applyBatch(good) || len(c.entries) != 2 || c.entries[0].Text != "updated" || c.entries[1].ID != "c" {
		t.Fatal(c.entries)
	}
	if c.applyBatch(good) {
		t.Fatal("duplicate batch accepted")
	}
	good["sequence"] = 7
	if c.applyBatch(good) {
		t.Fatal("missing sequence accepted")
	}
	good["sequence"] = 6
	good["subscription_id"] = "other"
	if c.applyBatch(good) {
		t.Fatal("cross subscription accepted")
	}
	got := mergeHistory([]entry{{ID: "1"}, {ID: "3"}, {ID: "4"}}, []entry{{ID: "1", Text: "updated"}, {ID: "2"}, {ID: "3"}}, false)
	if len(got) != 4 || got[1].ID != "2" || got[2].ID != "3" || got[0].Text != "updated" {
		t.Fatal("middle page appended out of order", got)
	}
	entries := []entry{}
	for i := 0; i < 350; i++ {
		entries = append(entries, entry{ID: strings.Repeat("a", i+1), Text: "cache"})
	}
	if err := a.store.saveHistory(m.config.ID+":"+c.agent.Identity(), entries, "rev"); err != nil {
		t.Fatal(err)
	}
	var state savedState
	if err := a.store.vault.Load(&state); err != nil {
		t.Fatal(err)
	}
	for _, cache := range state.History {
		if len(cache.Entries) != 300 {
			t.Fatal("unbounded cache")
		}
	}
}
func TestForgetComputerCleansOnlyItsLocalState(t *testing.T) {
	a, m, c := managementFixture(t)
	key := m.config.ID + ":" + c.agent.Identity()
	other := relay.Computer{ID: "other", Name: "保留电脑"}
	a.store.saveComputer(other)
	a.store.saveDraft(key, "private", true)
	a.store.saveDraft("other:chat", "keep", true)
	a.setOperation(operationKey(m, c.agent), "agent_restart")
	a.store.saveAttachments(key, []relay.Attachment{{Ref: "private"}})
	a.store.saveHistory(key, []entry{{ID: "one", Text: "private"}}, "rev")
	if err := a.store.removeComputer(m.config.ID); err != nil {
		t.Fatal(err)
	}
	var state savedState
	if err := a.store.vault.Load(&state); err != nil {
		t.Fatal(err)
	}
	if len(state.Computers) != 1 || state.Computers[0].ID != "other" || state.Drafts["other:chat"] != "keep" || state.Drafts[key] != "" || len(state.Operations) > 0 || len(state.History) > 0 || len(state.Attachments) > 0 {
		t.Fatal("forget crossed computer scope")
	}
}
func TestNotificationStateTransitions(t *testing.T) {
	before := relay.Agent{Target: relay.Target{PaneID: "p", TerminalID: "t", AgentSessionID: "s"}, Status: "working"}
	after := before
	after.Status = "done"
	if notificationTransition(before, after) == "" {
		t.Fatal("completion missing")
	}
	after.Generation++
	if notificationTransition(before, after) != "" {
		t.Fatal("replacement reported as completion")
	}
	after = before
	after.Status = "blocked"
	if notificationTransition(before, after) == "" {
		t.Fatal("attention missing")
	}
	if notificationTransition(after, after) != "" {
		t.Fatal("duplicate notification")
	}
}

func TestCodexCompletionRequiresExactObservedTurn(t *testing.T) {
	before := relay.Agent{Agent: "codex", BindingState: "verified", Target: relay.Target{PaneID: "p", TerminalID: "t", AgentSessionID: "s", BindingEpoch: 1}, Status: "working", RunState: &relay.RunEvidence{Source: "codex_runtime", Evidence: "rollout_lifecycle", Status: "working", TurnID: "a", ObservedAt: 1000}}
	after := before
	after.Status = "done"
	r := *before.RunState
	r.Status = "done"
	r.CompletionConfirmed = true
	r.ObservedAt = 5000
	after.RunState = &r
	if notificationTransition(before, after) == "" {
		t.Fatal("proven completion missing")
	}
	for _, kind := range []string{"idle", "different turn", "no proof", "unknown", "replaced", "stale", "no runtime"} {
		t.Run(kind, func(t *testing.T) {
			b, n := before, after
			br, nr := *b.RunState, *n.RunState
			b.RunState = &br
			n.RunState = &nr
			switch kind {
			case "idle":
				nr.Status = "idle"
			case "different turn":
				nr.TurnID = "b"
			case "no proof":
				nr.CompletionConfirmed = false
			case "unknown":
				br.Status = "unknown"
			case "replaced":
				n.BindingEpoch++
			case "stale":
				nr.ObservedAt = 100000
			case "no runtime":
				n.RunState = nil
			}
			if notificationTransition(b, n) != "" {
				t.Fatal("unproven completion")
			}
		})
	}
}
