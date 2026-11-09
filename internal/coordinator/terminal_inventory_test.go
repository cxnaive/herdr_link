package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

func TestTerminalInventoryDoesNotReviveAgentMetadata(t *testing.T) {
	active := herdr.Pane{ID: "active", TerminalID: "t1", Agent: "pi", Session: "live"}
	stale := herdr.Pane{ID: "shell", TerminalID: "t2", Agent: "codex", Session: "old", SessionRaw: herdr.AgentSession{Agent: "codex", Source: "hook", Kind: "id", Value: "old"}, Status: "blocked", StateChangeSeq: 19}
	got := terminalInventory([]herdr.Pane{active}, []herdr.Pane{active, stale, stale, {ID: "non-terminal"}, {TerminalID: "no-pane"}})
	if len(got) != 2 || got[0] != active {
		t.Fatalf("inventory = %+v", got)
	}
	shell := got[1]
	if !shell.TerminalOnly || shell.ID != stale.ID || shell.TerminalID != stale.TerminalID || shell.Agent != "" || shell.Session != "" || shell.SessionRaw != (herdr.AgentSession{}) || shell.Status != "idle" || shell.StateChangeSeq != 0 {
		t.Fatalf("stale agent data leaked into shell: %+v", shell)
	}
	if stale.SessionRaw.Value != "old" {
		t.Fatal("modified caller's topology")
	}
}

func TestTerminalSurvivesAgentExitAndRelayReconnect(t *testing.T) {
	for _, events := range []bool{false, true} {
		name := "poll"
		if events {
			name = "events"
		}
		t.Run(name, func(t *testing.T) {
			pane := herdr.Pane{ID: "pane", TerminalID: "terminal", WorkspaceID: "ws", TabID: "tab", Agent: "codex", Status: "working"}
			// No reported session: an agent exit must still fence admitted commands.
			var phase atomic.Int32
			socket := startPollerTestSocket(t, func(method string) (any, string) {
				switch method {
				case "agent.list":
					agents := []herdr.Pane{}
					if phase.Load() == 0 || phase.Load() == 2 {
						agents = append(agents, pane)
					}
					return map[string]any{"type": "agent_list", "agents": agents}, ""
				case "pane.list":
					panes := []herdr.Pane{}
					if phase.Load() != 3 {
						panes = append(panes, pane)
					}
					return map[string]any{"type": "pane_list", "panes": panes}, ""
				}
				return pollerSuccessResult(method), ""
			})
			client := herdr.NewClient("unused", socket)
			t.Cleanup(func() { _ = client.Close() })
			state := testState()
			poller := NewPoller(client, state, time.Second, testLogger())
			refresh := func() {
				if events {
					panes := []herdr.Pane{}
					if phase.Load() != 3 {
						panes = append(panes, pane)
					}
					poller.commitEventTopology(t.Context(), herdr.TopologySnapshot{Panes: panes}, state.RevisionCounter())
				} else {
					poller.poll(t.Context())
				}
			}
			refresh()
			active, ok := state.Agent(pane.ID)
			if !ok {
				t.Fatal("missing active agent")
			}
			phase.Store(1)
			refresh()
			shell, ok := state.Agent(pane.ID)
			if !ok || !shell.TerminalOnly || shell.Agent != "" || shell.TerminalID != pane.TerminalID || shell.Generation <= active.Generation {
				t.Fatalf("lost terminal or failed to revoke old commands: %+v", shell)
			}
			if state.PaneSessionCurrent(pane.ID, uint64(active.Generation)) {
				t.Fatal("old command can reach the shell")
			}
			refresh()
			repeated, _ := state.Agent(pane.ID)
			if repeated.Generation != shell.Generation {
				t.Fatal("shell identity churned on refresh")
			}
			reconnected := testState()
			cold := NewPoller(client, reconnected, time.Second, testLogger())
			cold.poll(t.Context())
			if shell, ok := reconnected.Agent(pane.ID); !ok || !shell.TerminalOnly || shell.Agent != "" {
				t.Fatalf("cold start lost retained shell: %+v", shell)
			}
			phase.Store(2)
			refresh()
			restarted, ok := state.Agent(pane.ID)
			if !ok || restarted.Agent != "codex" || restarted.Generation <= shell.Generation {
				t.Fatalf("restarted agent was not fenced: %+v", restarted)
			}
			phase.Store(3)
			refresh()
			if _, ok := state.Agent(pane.ID); ok {
				t.Fatal("closed terminal remained in inventory")
			}
		})
	}
}

func TestRetainedShellCanReceiveTerminalTextAndEnter(t *testing.T) {
	d, fake := newControlledFilterDispatcher(t)
	old, _ := d.state.Agent("pane-1")
	shell := *old
	shell.Agent = ""
	shell.TerminalOnly = true
	shell.Status = "idle"
	d.state.CommitInventory([]*AgentState{&shell}, d.state.RevisionCounter())
	if result := d.Handle(t.Context(), map[string]any{"action": "submit_prompt", "pane_id": "pane-1", "text": "do not execute as shell"}); result.OK || len(fake.recordedWrites()) != 0 {
		t.Fatal("conversation prompt reached a shell")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "keys")
	bin := writeScript(t, dir, "herdr", "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+record+"\"\n")
	keyClient := herdr.NewClient(bin, "")
	keyDispatcher := NewDispatcher(keyClient, d.state, nil, testLogger())
	t.Cleanup(func() { _ = keyDispatcher.Close(context.Background()); _ = keyClient.Close() })
	for _, message := range []map[string]any{
		{"action": "send_terminal_text", "request_id": "shell-text", "pane_id": "pane-1", "text": "printf retained-terminal"},
		{"action": "send_keys", "request_id": "shell-enter", "pane_id": "pane-1", "keys": []any{"Enter"}},
	} {
		dispatcher := d
		if message["action"] == "send_keys" {
			dispatcher = keyDispatcher
		}
		if result := dispatcher.Handle(context.Background(), message); !result.OK {
			t.Fatalf("terminal operation failed: %+v", result)
		}
	}
	data, err := os.ReadFile(record)
	if err != nil || !strings.Contains(string(data), "pane send-keys pane-1 Enter") {
		t.Fatalf("Enter not sent: %s %v", data, err)
	}
	want := []filterSocketWrite{{"pane-1", "pane.send_input", "printf retained-terminal"}}
	if got := fake.recordedWrites(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes = %+v", got)
	}
}

func TestPaneListFailureKeepsTerminalsButMarksInventoryStale(t *testing.T) {
	var failed atomic.Bool
	socket := startPollerTestSocket(t, func(method string) (any, string) {
		if method == "pane.list" {
			if failed.Load() {
				return nil, "internal_error"
			}
			return map[string]any{"type": "pane_list", "panes": []herdr.Pane{{ID: "shell", TerminalID: "terminal"}}}, ""
		}
		return pollerSuccessResult(method), ""
	})
	client := herdr.NewClient("unused", socket)
	t.Cleanup(func() { _ = client.Close() })
	state := testState()
	poller := NewPoller(client, state, time.Second, testLogger())
	poller.poll(t.Context())
	failed.Store(true)
	poller.poll(t.Context())
	if shell, ok := state.Agent("shell"); !ok || !shell.TerminalOnly {
		t.Fatal("temporary pane.list failure deleted terminal")
	}
	if state.InventoryStatus()["state"] != "error" || state.InventoryStatus()["stale"] != true || poller.ConsecutiveFailures() != 1 {
		t.Fatal("failed terminal fetch did not mark inventory stale")
	}
	failed.Store(false)
	poller.poll(t.Context())
	if state.InventoryStatus()["state"] != "ready" || poller.ConsecutiveFailures() != 0 {
		t.Fatal("terminal inventory did not recover")
	}
}

func TestRetainedTerminalIgnoresLateAgentEvents(t *testing.T) {
	state := testState()
	state.CommitEventForSession("shell", "tab", "ws", "blocked", time.Now().UnixMilli())
	shell := &AgentState{PaneID: "shell", TerminalID: "terminal", TabID: "tab", WorkspaceID: "ws", TerminalOnly: true, Status: "idle"}
	state.CommitInventory([]*AgentState{shell}, state.RevisionCounter())
	for _, status := range []string{"working", "blocked", "done"} {
		if state.CommitEventForSession("shell", "tab", "ws", status, time.Now().UnixMilli()) {
			t.Fatal("late agent event changed terminal status")
		}
	}
	current, _ := state.Agent("shell")
	if current.Status != "idle" || current.AttentionKind != "" || current.BlockedEventID != "" {
		t.Fatalf("shell inherited pending agent event: %+v", current)
	}
}
