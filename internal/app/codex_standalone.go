package app

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/0cv/herdr-mobile-relay/internal/codexruntime"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
)

const codexStandaloneSource = "codex-tui-standalone:"

// Only an explicit CLI option enables this path. Unknown options, option values,
// prompts and non-TUI subcommands must not accidentally enable standalone trust.
func codexNoDaemonArgs(argv []string) bool {
	if len(argv) == 0 || filepath.Base(argv[0]) != "codex" {
		return false
	}
	standalone, subcommand := false, false
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			break
		}
		key, _, attached := strings.Cut(arg, "=")
		switch key {
		case "--no-daemon":
			if attached {
				return false
			}
			standalone = true
		case "--remote", "--remote-auth-token-env":
			return false
		case "-c", "--config", "-m", "--model", "-p", "--profile", "-C", "--cd", "-s", "--sandbox", "-a", "--ask-for-approval", "-i", "--image", "--add-dir", "--enable", "--disable", "--local-provider":
			if !attached {
				i++
				if i >= len(argv) {
					return false
				}
			}
		case "--no-alt-screen", "--strict-config", "--search", "--full-auto", "--dangerously-bypass-approvals-and-sandbox", "--yolo", "--oss", "--last", "--all", "--include-non-interactive":
			if attached {
				return false
			}
		default:
			if strings.HasPrefix(arg, "-") {
				// Attached short-option values, e.g. -cfeature=true.
				if len(arg) > 2 && strings.ContainsRune("cmpCsai", rune(arg[1])) && arg[1] != '-' {
					continue
				}
				return false
			}
			if !subcommand && (arg == "resume" || arg == "fork") {
				subcommand = true
				continue
			}
			if !subcommand && slices.Contains([]string{"exec", "e", "app-server", "mcp", "mcp-server", "agents", "login", "logout", "debug", "review", "remote-control", "completion", "sandbox", "apply", "cloud", "features"}, arg) {
				return false
			}
			// A positional ID/name/prompt is never interpreted as an option.
		}
	}
	return standalone
}

func codexBindingSource(probe codexRuntimeProbe) string {
	if probe.mode == "standalone" {
		return codexStandaloneSource
	}
	return codexRuntimeSource
}

func (s *Server) standalonePrimary(ctx context.Context, a *coordinator.AgentState, probe codexRuntimeProbe, title string) (codexruntime.Thread, string) {
	bad := func(reason string) (codexruntime.Thread, string) { return codexruntime.Thread{}, reason }
	if s.conversationM == nil || len(probe.writers) == 0 || len(probe.writers) > 64 {
		return bad("standalone_runtime_unavailable")
	}
	ids := slices.Sorted(maps.Keys(probe.writers))
	if probe.titleID {
		owned := make([]codexruntime.Thread, 0, len(ids))
		for _, id := range ids {
			owned = append(owned, codexruntime.Thread{ID: id, Connected: true})
		}
		selected, reason := selectCodexTitle(title, owned)
		if reason != "" {
			return bad(reason)
		}
		// The exact selected writer must prove it is a primary. Other retained
		// empty threads cannot replace it or block an unambiguous title ID.
		ids = []string{selected.ID}
	}
	threads := make([]codexruntime.Thread, 0, len(ids))
	for _, id := range ids {
		if ctx.Err() != nil {
			return bad("runtime_changed")
		}
		meta, loc, reason := s.conversationM.CodexBindingMetadata(a.Cwd, id)
		if reason == "herdr_history_unavailable" {
			return bad("standalone_metadata_unavailable")
		}
		root, err := canonicalCodexRoot(loc.Root)
		if reason != "" || err != nil || root != probe.root {
			return bad("standalone_metadata_conflict")
		}
		t := codexruntime.Thread{ID: strings.ToLower(meta.ID), CWD: meta.CWD, SessionID: meta.SessionID, ParentThreadID: meta.ParentThreadID, ForkedFromID: meta.ForkedFromID, Source: meta.Source, Connected: true}
		if probe.titleID && (meta.ThreadSource == "subagent" || t.Delegated()) {
			return bad("terminal_title_conflict")
		}
		var source string
		_ = json.Unmarshal(meta.Source, &source)
		// A missing parent on a classified child is not evidence of a root.
		if meta.ThreadSource == "subagent" && !t.Delegated() {
			return bad("standalone_ancestry_unavailable")
		}
		if !t.Delegated() && (meta.ThreadSource != "" && meta.ThreadSource != "user" || !slices.Contains([]string{"cli", "vscode", "exec", "mcp"}, source)) {
			return bad("standalone_ancestry_unavailable")
		}
		threads = append(threads, t)
	}
	t := codexruntime.PrimaryForOwnedThreads(threads)
	switch t.Unavailable {
	case "runtime_ambiguous":
		if !probe.titleID {
			return bad("standalone_ambiguous")
		}
	case "runtime_ancestry_unavailable":
		return bad("standalone_ancestry_unavailable")
	case "":
	default:
		return bad("standalone_metadata_conflict")
	}
	if probe.titleID {
		var reason string
		t, reason = selectCodexTitle(title, t.Candidates)
		if reason != "" {
			return bad(reason)
		}
	}
	if !pathidentity.SameDirectory(t.CWD, a.Cwd) {
		return bad("standalone_metadata_conflict")
	}
	return t, ""
}

func (s *Server) observeCodexStandalone(ctx context.Context, a *coordinator.AgentState, obs codexObservation) codexObservation {
	obs.thread, obs.reason = s.standalonePrimary(ctx, a, obs.probe, obs.title)
	if obs.reason != "" {
		return obs
	}
	run := s.recordedCodexRun(ctx, a, obs.probe, obs.thread.ID, obs.herdrStatus, "")
	obs.run = &run
	after, err := s.herdrC.PaneGet(ctx, a.PaneID)
	if err != nil || !codexPaneMatches(after, a) {
		obs.reason = "runtime_changed"
		return obs
	}
	if obs.run.Status == "done" && (after.Status == "working" || after.Status == "blocked") {
		obs.run.Status, obs.run.CompletionConfirmed = after.Status, false
		obs.run.TurnID, obs.run.ElapsedMS, obs.run.Ticking = "", nil, false
		obs.run.Timing, obs.run.Evidence = "unknown", "unavailable"
	}
	obs.report = after.SessionRaw // Diagnostic input only; never standalone proof.
	current, reason := s.inspectCodexProcess(ctx, a)
	if reason != "" || current.mode != obs.probe.mode || current.titleID != obs.probe.titleID || current.pid != obs.probe.pid || current.process != obs.probe.process || current.root != obs.probe.root || !maps.Equal(current.writers, obs.probe.writers) {
		obs.reason = "runtime_changed"
		return obs
	}
	second, reason := s.standalonePrimary(ctx, a, current, after.TerminalTitle)
	if reason != "" || second.ID != obs.thread.ID || second.CWD != obs.thread.CWD || ctx.Err() != nil {
		obs.reason = "runtime_changed"
		return obs
	}
	final, reason := s.inspectCodexProcess(ctx, a)
	if reason != "" || final.mode != current.mode || final.titleID != current.titleID || final.pid != current.pid || final.process != current.process || final.root != current.root || !maps.Equal(final.writers, current.writers) || ctx.Err() != nil {
		obs.reason = "runtime_changed"
		return obs
	}
	if obs.probe.titleID {
		last, err := s.herdrC.PaneGet(ctx, a.PaneID)
		if err != nil || !codexPaneMatches(last, a) {
			obs.reason = "runtime_changed"
			return obs
		}
		selected, reason := selectCodexTitle(last.TerminalTitle, []codexruntime.Thread{obs.thread})
		if reason != "" || selected.ID != obs.thread.ID || ctx.Err() != nil {
			obs.reason = "runtime_changed"
			return obs
		}
	}
	obs.runtime = "standalone-writer:" + obs.probe.writers[obs.thread.ID]
	return obs
}
