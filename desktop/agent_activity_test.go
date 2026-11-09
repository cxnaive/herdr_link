package main

import (
	"github.com/cxnaive/herdr-desktop/internal/relay"
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestComputerActivityCountsIgnoreSearchAndRemainVisibleWhenCollapsed(t *testing.T) {
	app := newApplication(true)
	defer app.cancel()
	app.loadDemo()
	m := app.machines[0]
	m.agents = append(m.agents, relay.Agent{Target: relay.Target{PaneID: "shell"}, TerminalOnly: true, Status: "working"})
	app.machines[1].agents[0].Status = "idle"
	app.machines[1].agents[1].Status = "idle"
	tt := ui.NewTester(app.view, 1180, 760)
	if !tt.HasText("运行中 1 · 待处理 1 · 1 个终端") || !tt.HasText("运行中 0") {
		t.Fatal("missing per-computer counts")
	}
	if err := tt.Click(m.config.Name); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("运行中 1 · 待处理 1 · 1 个终端") {
		t.Fatal("fold hid running counts")
	}
	app.query = m.agents[1].Title()
	tt.Frame()
	if !tt.HasText("运行中 1 · 待处理 1 · 1 个终端") {
		t.Fatal("search changed whole-computer count")
	}
	app.query = ""
	m.agents[0].Status = "done"
	tt.Frame()
	if !tt.HasText("运行中 0 · 待处理 1 · 1 个终端") {
		t.Fatal("status update did not refresh counts")
	}
	if !app.collapsed[groupKey("computer", m.config.ID)] {
		t.Fatal("refresh expanded computer")
	}
	app.demo = false
	m.inventoryReady = true
	tt.Frame()
	if !tt.HasText("运行数待确认 · 1 个终端") {
		t.Fatal("offline computer shows stale running count")
	}
}
