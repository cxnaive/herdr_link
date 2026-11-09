package main

import (
	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestRetainedTerminalOpensWithoutReusingAgentHistory(t *testing.T) {
	app := newApplication(true)
	defer app.cancel()
	app.loadDemo()
	machine, old := app.selected()
	old.draft = "old conversation draft"
	shell := old.agent
	shell.Agent = ""
	shell.TerminalOnly = true
	shell.Generation++
	shell.AgentSessionID = ""
	machine.agents = []relay.Agent{shell}
	app.selectAgent(machine, shell)
	if app.mode != 1 {
		t.Fatal("retained terminal did not open terminal view")
	}
	_, selected := app.selected()
	if selected == old || selected.draft != "" || len(selected.entries) != 0 {
		t.Fatal("shell inherited conversation state")
	}
	tt := ui.NewTester(app.view, 1180, 760)
	if !tt.HasText("打开本机终端") {
		t.Fatal("terminal control not available")
	}
}
