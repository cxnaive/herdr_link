package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/codexruntime"
	"github.com/0cv/herdr-mobile-relay/internal/conversation"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
)

func TestWindowsCodexLiveProcessProof(t *testing.T) {
	pane := os.Getenv("HERDR_CODEX_LIVE_TEST_PANE")
	if pane == "" {
		t.Skip("requires explicit isolated live Codex pane")
	}
	client := herdr.NewClient(os.Getenv("HERDR_BIN"), os.Getenv("HERDR_SOCKET_PATH"))
	defer client.Close()
	ctx, c := context.WithTimeout(t.Context(), 15*time.Second)
	defer c()
	info, err := client.PaneProcessInfoFresh(ctx, pane)
	if err != nil {
		t.Fatal(err)
	}
	p, err := codexRuntimeForegroundProcess(info, pane)
	if err != nil {
		t.Fatal("process proof", err)
	}
	t.Log("native PID", p.PID, "cwd", p.Cwd)
	listeners, err := codexProcessListeners(p.PID)
	if err != nil || len(listeners) == 0 {
		t.Fatal("listener proof", listeners, err)
	}
	t.Log("listeners", listeners)
	home, _ := os.UserHomeDir()
	s := &Server{herdrC: client, home: home, conversationM: conversation.NewReader(home)}
	current, err := client.PaneGet(ctx, pane)
	if err != nil {
		t.Fatal(err)
	}
	a := &coordinator.AgentState{PaneID: pane, TerminalID: current.TerminalID, Agent: "codex", Cwd: current.Cwd, ServerSessionID: "primary"}
	obs := s.observeCodexRuntime(ctx, []*coordinator.AgentState{a})[pane]
	if obs.reason != "" || obs.thread.ID == "" {
		t.Fatal("runtime proof", obs.reason)
	}
	t.Log("verified", obs.probe.mode, "thread", obs.thread.ID)
	reader := conversation.NewReader(home)
	loc, reason := reader.ValidateCodexReport(a.Cwd, obs.thread.ID)
	if reason != "" {
		t.Fatal("history metadata", reason)
	}
	t.Log("history metadata verified", filepath.Base(loc.Path))
	if obs.probe.mode == "standalone" {
		if obs.run == nil {
			t.Fatal("missing embedded lifecycle state")
		}
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				for range 10 {
					p, r := s.inspectCodexProcess(ctx, a)
					if r != "" || p.mode != "standalone" {
						t.Errorf("concurrent writer proof: %s %s", p.mode, r)
						return
					}
				}
			})
		}
		wg.Wait()
		return
	}
	socket := filepath.Join(filepath.Dir(obs.probe.root), "app-server-control", "app-server-control.sock")
	settings, err := codexruntime.Settings(ctx, socket, obs.thread.ID, a.Cwd, nil, func() error { return nil })
	if err != nil || !settings.Available {
		t.Fatal("runtime settings", err)
	}
	t.Log("settings", settings.Model, settings.Effort, "catalog", len(settings.Models))
}
