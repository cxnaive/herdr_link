package relay

import "testing"

func TestAgentActivitySeparatesWorkingWaitingAndUnknown(t *testing.T) {
	counts := CountAgentActivity([]Agent{
		{Status: "working"}, {Status: " BUSY "}, {Status: "blocked"}, {Status: "needs_input"},
		{Status: "idle"}, {Status: "done"}, {Status: "unknown"},
		{Agent: "codex", BindingState: "ambiguous", Status: "working"},
		{TerminalOnly: true, Status: "working"},
	})
	if want := (AgentActivity{Agents: 8, Terminals: 1, Running: 2, Waiting: 2, Unknown: 2}); counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
	if got := counts.Summary(true, true); got != "运行中 2 · 待处理 2 · 待确认 2" {
		t.Fatal(got)
	}
	for _, flags := range [][2]bool{{false, true}, {true, false}, {false, false}} {
		if got := counts.Summary(flags[0], flags[1]); got != "运行数待确认" {
			t.Fatalf("stale counts: %s", got)
		}
	}
	if got := CountAgentActivity(nil).Summary(true, true); got != "运行中 0" {
		t.Fatal(got)
	}
}

func TestInactiveAgentStatesDoNotCountAsRunning(t *testing.T) {
	for _, status := range []string{"idle", "ready", "done", "completed", "finished", "interrupted", "failed", "error", "stopped", "exited", "ended"} {
		if got := CountAgentActivity([]Agent{{Status: status}}); got != (AgentActivity{Agents: 1}) {
			t.Fatalf("%s: %+v", status, got)
		}
	}
	for _, status := range []string{"working", "running", "busy"} {
		if got := CountAgentActivity([]Agent{{Agent: "claude", Status: status, BindingState: "ambiguous"}}); got.Running != 1 {
			t.Fatal(got)
		}
	}
	for _, binding := range []string{"verifying", "stale", "ambiguous", "conflict"} {
		if got := CountAgentActivity([]Agent{{Agent: "codex", Status: "working", BindingState: binding}}); got.Unknown != 1 || got.Running != 0 {
			t.Fatal(got)
		}
	}
}
