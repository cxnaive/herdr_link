package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/codexruntime"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/protocol"
)

const titleConfig = `tui.terminal_title=["thread-id","thread-name"]`

func shortTitle(id string) string { return id[:29] + "... | Synthetic title" }

func enableTitleProbe(s *Server) {
	probe := s.codexProcessProbe
	s.codexProcessProbe = func(ctx context.Context, a *coordinator.AgentState) (codexRuntimeProbe, string) {
		p, r := probe(ctx, a)
		p.titleID = codexTitleIDConfigured([]string{"codex", "-c", titleConfig})
		return p, r
	}
}

func TestCodexTitleLayoutRequiresExplicitNativeArgs(t *testing.T) {
	for _, args := range [][]string{
		{"codex", "-c", titleConfig},
		{"/bin/codex", "-c", titleConfig, "--no-daemon", "resume", "--last"},
		{"codex", "--config=" + titleConfig},
		{"codex", "-c" + titleConfig},
		{"codex", "-c=" + titleConfig},
		{"codex", "-c", titleConfig, "-c", "model=custom"},
		{"codex", "--config", `tui.terminal_title = ["thread-id", "thread-name"]`},
		{"codex", "--config", `tui.terminal_title=["thread-id"]`},
		append([]string{"codex"}, coordinator.AgentLaunchDefaults()["codex"]...),
		{"codex", "-c", `tui.terminal_title=['thread-id','thread-name']`},
		{"codex", "-c", `tui.terminal_title = [ 'thread-id' ]`},
		{"codex", "-c", `tui.terminal_title=[]`, "-c", titleConfig},
		{"codex", "-c", titleConfig, "--", "--config=tui.terminal_title=[]"},
	} {
		if !codexTitleIDConfigured(args) {
			t.Errorf("missed layout: %q", args)
		}
	}
	for _, args := range [][]string{
		nil, {"codex"}, {"codex", "-c", titleConfig, "exec"}, {"codex", "app-server", "-c", titleConfig}, {"node", "codex.js", "-c", titleConfig}, {"claude", "-c", titleConfig},
		{"codex", "--", "-c", titleConfig}, {"codex", "-m", "-c", titleConfig},
		{"codex", "--unknown", "-c", titleConfig}, {"codex", "--remote", "url", "-c", titleConfig},
		{"codex", "-c", titleConfig, "-c", `tui.terminal_title=[]`},
		{"codex", "-c", titleConfig, "--config=tui.terminal_title=[\"thread-name\"]"},
		{"codex", "-c", titleConfig, "-c", `tui={terminal_title=[]}`},
		{"codex", "-c", titleConfig, "-c", `tui."terminal_title"=[]`},
		{"codex", "-c", titleConfig, "-c", `tui.terminal_title=["thread-name","thread-id"]`},
		{"codex", "-c", `tui.terminal_title=['thread-name','thread-id']`},
		{"codex", "-c", `tui.terminal_title=[thread-id,thread-name]`},
		{"codex", "-c", `tui.terminal_title=['thread-id','thread-name']`, "--config=tui.terminal_title=[]"},
		{"codex", "-c", titleConfig, "-c"}, {"codex", "-c", titleConfig, "-c", "bad"},
		{"codex", "-c", titleConfig, "--image"}, {"codex", "-c", titleConfig, "--no-daemon=false"},
	} {
		if codexTitleIDConfigured(args) {
			t.Errorf("unproven layout: %q", args)
		}
	}
}

func TestCodexTitleIDOnlyMatchesCompleteOrKnownTruncatedUUID(t *testing.T) {
	first, second := rootReportThread(reportID), rootReportThread(otherReportID)
	first.Connected = true
	second.Connected = true
	roots := []codexruntime.Thread{first, second}
	for _, title := range []string{reportID, reportID + " | Name", shortTitle(reportID), strings.ToUpper(shortTitle(reportID)), reportID + " | " + otherReportID} {
		got, reason := selectCodexTitle(title, roots)
		if reason != "" || got.ID != reportID {
			t.Fatalf("%q: %s %s", title, got.ID, reason)
		}
	}
	for title, reason := range map[string]string{
		"": "terminal_title_missing", "Name": "terminal_title_invalid", "writer | " + reportID: "terminal_title_invalid",
		reportID[:28] + "...": "terminal_title_invalid", reportID[:30] + "...": "terminal_title_invalid",
		reportID[:29] + "…": "terminal_title_invalid", " " + reportID: "terminal_title_invalid",
		reportID + "\n": "terminal_title_invalid", reportID + "\x1b": "terminal_title_invalid",
		reportID + " | " + strings.Repeat("x", 1024): "terminal_title_invalid",
		childReportID: "terminal_title_conflict", shortTitle(childReportID): "terminal_title_conflict",
	} {
		if got, r := selectCodexTitle(title, roots); got.ID != "" || r != reason {
			t.Fatalf("%q: %s %s (want %s)", title, got.ID, r, reason)
		}
	}
	collision := first
	collision.ID = reportID[:29] + "9999999"
	if _, r := selectCodexTitle(shortTitle(reportID), []codexruntime.Thread{first, collision}); r != "terminal_title_ambiguous" {
		t.Fatal(r)
	}
	first.Connected = false
	if _, r := selectCodexTitle(shortTitle(reportID), []codexruntime.Thread{first, second}); r != "runtime_disconnected" {
		t.Fatal(r)
	}
}

func TestCodexSharedTitleSelectsScopedRootInsteadOfRetainedHook(t *testing.T) {
	for _, hook := range []herdr.AgentSession{{}, {Agent: "codex", Kind: "id", Value: reportID}} {
		s, f, a, _ := reportServer(t)
		enableTitleProbe(s)
		f.threads[otherReportID] = rootReportThread(otherReportID)
		f.pane.TerminalTitle = shortTitle(otherReportID)
		f.pane.SessionRaw = hook
		reportRollout(t, s.home, otherReportID, a.Cwd, "synthetic selected history")
		got := commitReport(s, a)
		if got.BindingState != "verified" || got.SessionID != otherReportID || got.BindingSelection != codexTitleSelection || !s.codexPrimaryCurrent(t.Context(), got) {
			t.Fatalf("title selection failed: %+v", got)
		}
		page, err := s.conversationM.ReadFor("codex", a.Cwd, got.SessionID, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(page)
		if !strings.Contains(string(encoded), "synthetic selected history") || strings.Contains(string(encoded), "synthetic main history") {
			t.Fatal("wrong history")
		}
		target := bindingDiagnosticTarget(got)
		diag, err := s.sessionBindingDiagnostics(t.Context(), protocol.Inbound{PaneID: got.PaneID, Target: &target})
		if err != nil || diag.Details["selection_evidence"] != codexTitleSelection {
			t.Fatal("title evidence not observable", err)
		}
		encoded, _ = json.Marshal(diag)
		if strings.Contains(string(encoded), "Synthetic title") {
			t.Fatal("diagnostics leaked terminal title")
		}
	}
}

func TestCodexTitleNeverInventsCandidatesOrUsesNames(t *testing.T) {
	for _, test := range []struct{ name, title, reason string }{
		{"missing", "", "terminal_title_missing"}, {"name", "Synthetic title", "terminal_title_invalid"},
		{"foreign", shortTitle(childReportID), "terminal_title_conflict"}, {"child", shortTitle(childReportID), "terminal_title_conflict"},
		{"collision", shortTitle(reportID), "terminal_title_ambiguous"}, {"other-terminal", shortTitle(otherReportID), "terminal_title_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, f, a, _ := reportServer(t)
			enableTitleProbe(s)
			f.threads[otherReportID] = rootReportThread(otherReportID)
			f.pane.TerminalTitle = test.title
			switch test.name {
			case "child":
				child := rootReportThread(childReportID)
				child.ParentThreadID = reportID
				child.SessionID = reportID
				f.threads[childReportID] = child
			case "collision":
				id := reportID[:29] + "9999999"
				f.threads[id] = rootReportThread(id)
			case "other-terminal":
				f.origins[otherReportID] = "http://127.0.0.1:4321"
			}
			got := commitReport(s, a)
			if got.SessionID != "" || got.BindingReason != test.reason {
				t.Fatalf("%+v", got)
			}
		})
	}
	// No explicit layout: a UUID-like tab name cannot replace the original policy.
	s, f, a, _ := reportServer(t)
	f.threads[otherReportID] = rootReportThread(otherReportID)
	f.pane.TerminalTitle = shortTitle(otherReportID)
	f.pane.SessionRaw = herdr.AgentSession{}
	if got := commitReport(s, a); got.SessionID != "" || got.BindingReason != "runtime_ambiguous_no_report" {
		t.Fatal("arbitrary title adopted")
	}
}

func TestCodexTitleChangeRevokesOldHistoryAndMutationLease(t *testing.T) {
	s, f, a, _ := reportServer(t)
	enableTitleProbe(s)
	f.threads[otherReportID] = rootReportThread(otherReportID)
	f.pane.TerminalTitle = shortTitle(reportID)
	old := commitReport(s, a)
	f.mu.Lock()
	f.pane.TerminalTitle = shortTitle(otherReportID)
	f.mu.Unlock()
	if s.codexPrimaryCurrent(t.Context(), old) || s.codexBindings.current(old) {
		t.Fatal("old title lease stayed current")
	}
	current := commitReport(s, a)
	if current.SessionID != otherReportID || current.BindingEpoch == old.BindingEpoch {
		t.Fatal("title switch did not isolate history")
	}
	// An unrelated name update is not a thread switch.
	f.mu.Lock()
	f.pane.TerminalTitle = otherReportID[:29] + "... | Renamed"
	f.mu.Unlock()
	if !s.codexPrimaryCurrent(t.Context(), current) {
		t.Fatal("renaming invalidated a stable ID")
	}
	f.mu.Lock()
	f.pane.TerminalTitle = ""
	f.mu.Unlock()
	if s.codexPrimaryCurrent(t.Context(), current) {
		t.Fatal("missing title fell back to hook")
	}
}

func TestCodexTitleAndProcessMustStayStableAcrossValidation(t *testing.T) {
	for _, change := range []string{"title", "process", "layout"} {
		t.Run(change, func(t *testing.T) {
			s, f, a, _ := reportServer(t)
			enableTitleProbe(s)
			f.pane.TerminalTitle = shortTitle(reportID)
			f.threads[otherReportID] = rootReportThread(otherReportID)
			switch change {
			case "title":
				f.titleChangeAt = 2
				f.changedTitle = shortTitle(otherReportID)
			case "process":
				f.processChangeAt = 2
			case "layout":
				old := s.codexProcessProbe
				calls := 0
				s.codexProcessProbe = func(ctx context.Context, a *coordinator.AgentState) (codexRuntimeProbe, string) {
					p, r := old(ctx, a)
					calls++
					if calls > 1 {
						p.titleID = false
					}
					return p, r
				}
			}
			if got := commitReport(s, a); got.SessionID != "" || got.BindingReason != "runtime_changed" {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestStandaloneTitleBreaksRetainedWriterTieWithoutSharedRuntime(t *testing.T) {
	s, f, a, writers := standaloneServer(t)
	enableTitleProbe(s)
	writers[otherReportID] = "dev:inode-2"
	standaloneRollout(t, s.home, otherReportID, a.Cwd, nil)
	f.pane.TerminalTitle = shortTitle(otherReportID) // Retained hook still names the first root.
	if err := os.Remove(filepath.Join(s.home, ".codex", "app-server-control", "app-server-control.sock")); err != nil {
		t.Fatal(err)
	}
	got := commitReport(s, a)
	if got.SessionID != otherReportID || got.BindingSelection != codexTitleSelection || !s.codexPrimaryCurrent(t.Context(), got) {
		t.Fatalf("%+v", got)
	}
	assertNoSharedCalls(t, f)
	f.mu.Lock()
	f.pane.TerminalTitle = shortTitle(reportID)
	f.mu.Unlock()
	if s.codexPrimaryCurrent(t.Context(), got) {
		t.Fatal("standalone old ID accepted")
	}
	if current := commitReport(s, a); current.SessionID != reportID || current.BindingEpoch == got.BindingEpoch {
		t.Fatal("standalone title did not switch")
	}
}

func TestStandaloneTitleCannotBypassWriterOrMetadataProof(t *testing.T) {
	for _, change := range []string{"child", "foreign", "collision", "missing-meta", "process", "late-title"} {
		t.Run(change, func(t *testing.T) {
			s, f, a, writers := standaloneServer(t)
			enableTitleProbe(s)
			f.pane.TerminalTitle = shortTitle(reportID)
			writers[otherReportID] = "dev:inode-2"
			standaloneRollout(t, s.home, otherReportID, a.Cwd, nil)
			switch change {
			case "child":
				writers[childReportID] = "dev:inode-3"
				standaloneRollout(t, s.home, childReportID, a.Cwd, map[string]any{"session_id": reportID, "parent_thread_id": reportID, "thread_source": "subagent"})
				f.pane.TerminalTitle = shortTitle(childReportID)
			case "foreign":
				f.pane.TerminalTitle = shortTitle(childReportID)
			case "collision":
				id := reportID[:29] + "9999999"
				writers[id] = "dev:inode-3"
				standaloneRollout(t, s.home, id, a.Cwd, nil)
			case "missing-meta":
				writers[childReportID] = "dev:inode-3"
				f.pane.TerminalTitle = shortTitle(childReportID)
			case "process":
				old := s.codexProcessProbe
				calls := 0
				s.codexProcessProbe = func(ctx context.Context, a *coordinator.AgentState) (codexRuntimeProbe, string) {
					p, r := old(ctx, a)
					calls++
					if calls > 1 {
						p.process = "changed"
					}
					return p, r
				}
			case "late-title":
				f.titleChangeAt = 3
				f.changedTitle = shortTitle(otherReportID)
			}
			if got := commitReport(s, a); got.SessionID != "" || got.BindingState == "verified" {
				t.Fatalf("%+v", got)
			}
			assertNoSharedCalls(t, f)
		})
	}
}

func TestCodexTitleCannotBypassMetadataOrDisableTerminalRecovery(t *testing.T) {
	for _, change := range []string{"project", "ancestry", "runtime", "missing-title"} {
		t.Run(change, func(t *testing.T) {
			s, f, a, _ := reportServer(t)
			enableTitleProbe(s)
			f.pane.TerminalTitle = shortTitle(reportID)
			switch change {
			case "project":
				thread := f.threads[reportID]
				thread.CWD = "/other"
				f.threads[reportID] = thread
			case "ancestry":
				child := rootReportThread(childReportID)
				child.ParentThreadID = otherReportID
				child.SessionID = otherReportID
				f.threads[childReportID] = child
			case "runtime":
				if err := os.Remove(filepath.Join(s.home, ".codex", "app-server-control", "app-server-control.sock")); err != nil {
					t.Fatal(err)
				}
			case "missing-title":
				f.pane.TerminalTitle = ""
			}
			got := commitReport(s, a)
			if got.SessionID != "" || got.BindingState == "verified" {
				t.Fatal("title bypassed process/runtime metadata")
			}
			for _, name := range []string{"send_terminal_text", "send_keys", "agent_stop", "agent_restart"} {
				action, _ := protocol.ClassifyAction(name)
				if !s.codexInteractionCurrent(t.Context(), protocol.Inbound{Type: name, PaneID: got.PaneID, Target: registryTarget(got)}, action) {
					t.Fatal("unresolved title blocked terminal", name)
				}
			}
			for _, name := range []string{"submit_prompt", "respond", "set_agent_settings"} {
				action, _ := protocol.ClassifyAction(name)
				if s.codexInteractionCurrent(t.Context(), protocol.Inbound{Type: name, PaneID: got.PaneID, Target: registryTarget(got)}, action) {
					t.Fatal("unresolved title permitted conversation mutation", name)
				}
			}
		})
	}
}
