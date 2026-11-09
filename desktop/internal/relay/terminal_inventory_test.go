package relay

import "testing"

func TestTerminalOnlyInventoryKeepsInputButHasNoConversation(t *testing.T) {
	rows := ParseAgents([]any{map[string]any{"pane_id": "shell", "terminal_id": "terminal", "generation": 2, "terminal_only": true, "tab_label": "writer"}})
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	shell := rows[0]
	if !shell.TerminalOnly || shell.Provider() != "终端" || shell.Title() != "writer" || shell.CanChat() {
		t.Fatalf("invalid terminal presentation: %+v", shell)
	}
	for _, action := range []string{"agent_rename", "agent_stop"} {
		if !shell.OperationWritable(action) {
			t.Fatalf("terminal blocked %s", action)
		}
	}
	if shell.OperationWritable("submit_prompt") {
		t.Fatal("terminal accepted conversation prompt")
	}
	if !(Agent{}).CanChat() {
		t.Fatal("unknown agent kind was incorrectly treated as a shell")
	}
}
