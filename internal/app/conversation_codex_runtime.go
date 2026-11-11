package app

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/agentroots"
	"github.com/0cv/herdr-mobile-relay/internal/codexruntime"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
)

const codexRuntimeSource = "codex-tui-runtime:"

type codexRuntimeProbe struct {
	pid           int
	process, root string
	mode          string
	titleID       bool
	writers       map[string]string
	listeners     map[string]string
}
type codexObservation struct {
	probe           codexRuntimeProbe
	report          herdr.AgentSession
	title           string
	herdrStatus     string
	thread          codexruntime.Thread
	run             *codexruntime.RunState
	runtime, reason string
}
type codexStoreResult struct {
	threads  map[string]codexruntime.Thread
	instance string
	err      error
}

func canonicalCodexRoot(root string) (string, error) {
	path, err := filepath.EvalSymlinks(root)
	if os.IsNotExist(err) {
		parent, e := filepath.EvalSymlinks(filepath.Dir(root))
		if e != nil {
			return "", e
		}
		return filepath.Join(parent, filepath.Base(root)), nil
	}
	return path, err
}

func (s *Server) inspectCodexProcess(ctx context.Context, a *coordinator.AgentState) (codexRuntimeProbe, string) {
	if s.codexProcessProbe != nil {
		return s.codexProcessProbe(ctx, a)
	}
	bad := func(reason string) (codexRuntimeProbe, string) { return codexRuntimeProbe{}, reason }
	if !codexProcessProofSupported() {
		return bad("runtime_platform_unsupported")
	}
	if s.herdrC == nil || s.home == "" {
		return bad("process_unavailable")
	}
	info, err := s.herdrC.PaneProcessInfoFresh(ctx, a.PaneID)
	if err != nil {
		return bad("process_unavailable")
	}
	process, err := codexRuntimeForegroundProcess(info, a.PaneID)
	if err != nil {
		return bad("process_unavailable")
	}
	if !pathidentity.SameDirectory(process.Cwd, a.Cwd) {
		return bad("process_project_conflict")
	}
	birth, err := codexProcessStart(process.PID)
	if err != nil {
		return bad("process_unavailable")
	}
	home, err := codexProcessHome(process.PID, s.home)
	if err != nil {
		return bad("provider_store_unavailable")
	}
	root, err := canonicalCodexRoot(filepath.Join(home, "sessions"))
	if err != nil {
		return bad("provider_store_unavailable")
	}
	allowed := false
	for _, candidate := range agentroots.Codex(s.home) {
		canonical, e := canonicalCodexRoot(candidate)
		allowed = allowed || (e == nil && root == canonical)
	}
	if !allowed {
		return bad("provider_store_unavailable")
	}
	probe := codexRuntimeProbe{pid: process.PID, process: birth, root: root, mode: "shared", titleID: codexTitleIDConfigured(process.Argv)}
	return inspectCodexRuntimeOwnership(probe, codexNoDaemonArgs(process.Argv), codexProcessWriters, codexProcessListeners)
}

// Codex can choose an embedded runtime for a -c override without --no-daemon.
// Only actual exclusive writers owned by this TUI enable that inferred mode.
func inspectCodexRuntimeOwnership(probe codexRuntimeProbe, explicitStandalone bool, writers func(int, string) (map[string]string, error), listeners func(int) (map[string]string, error)) (codexRuntimeProbe, string) {
	if explicitStandalone || probe.titleID {
		owned, err := writers(probe.pid, probe.root)
		if explicitStandalone || err == nil && len(owned) > 0 {
			probe.mode = "standalone"
			probe.writers = owned
			if err != nil || len(owned) == 0 {
				return probe, "standalone_runtime_unavailable"
			}
			return probe, ""
		}
	}
	owned, err := listeners(probe.pid)
	probe.listeners = owned
	if err != nil || len(owned) == 0 {
		return probe, "runtime_unavailable"
	}
	return probe, ""
}

func codexPaneMatches(p *herdr.Pane, a *coordinator.AgentState) bool {
	return p != nil && p.ID == a.PaneID && p.TerminalID == a.TerminalID && p.Agent == "codex" && p.Cwd == a.Cwd
}

// Candidate IDs have already been restricted to this TUI's owned endpoint and
// reduced by explicit parent relationships. A configured title ID selects within
// those roots; without it, reports only break remaining ties.
func selectCodexPrimary(probe codexRuntimeProbe, threads map[string]codexruntime.Thread, report herdr.AgentSession, title string) (codexruntime.Thread, string, string) {
	candidates := map[string]codexruntime.Thread{}
	origins := map[string]string{}
	for origin := range probe.listeners {
		t, ok := threads[origin]
		if !ok || t.Unavailable == "runtime_unavailable" {
			continue
		}
		if t.Unavailable != "" && t.Unavailable != "runtime_ambiguous" && t.Unavailable != "runtime_disconnected" {
			return codexruntime.Thread{}, "", t.Unavailable
		}
		for _, root := range t.Candidates {
			if old, exists := candidates[root.ID]; exists && (old.CWD != root.CWD || old.SessionID != root.SessionID || old.Connected != root.Connected || origins[root.ID] != origin) {
				return codexruntime.Thread{}, "", "runtime_ambiguous"
			}
			candidates[root.ID] = root
			origins[root.ID] = origin
		}
	}
	if len(candidates) == 0 {
		return codexruntime.Thread{}, "", "runtime_unavailable"
	}
	var selected codexruntime.Thread
	if probe.titleID {
		var reason string
		selected, reason = selectCodexTitle(title, slices.Collect(maps.Values(candidates)))
		if reason != "" {
			return selected, "", reason
		}
	} else if len(candidates) == 1 {
		for _, t := range candidates {
			selected = t
		}
	} else {
		if reason := codexReportReason(report); reason != "" {
			if reason == "herdr_session_missing" {
				reason = "runtime_ambiguous_no_report"
			}
			return selected, "", reason
		}
		for id, t := range candidates {
			if strings.EqualFold(id, report.Value) {
				selected = t
				break
			}
		}
		if selected.ID == "" {
			return selected, "", "reported_session_not_candidate"
		}
	}
	if !selected.Connected {
		return codexruntime.Thread{}, "", "runtime_disconnected"
	}
	return selected, origins[selected.ID], ""
}

func (s *Server) observeCodexRuntime(ctx context.Context, agents []*coordinator.AgentState) map[string]codexObservation {
	observations := map[string]codexObservation{}
	wanted := map[string]map[string]bool{}
	for _, a := range agents {
		if a.Agent != "codex" || (a.ServerSessionID != "" && a.ServerSessionID != "primary") {
			continue
		}
		obs := codexObservation{reason: "herdr_report_unavailable"}
		observations[a.PaneID] = obs
		if s.herdrC == nil {
			continue
		}
		pane, err := s.herdrC.PaneGet(ctx, a.PaneID)
		if err != nil {
			continue
		}
		if !codexPaneMatches(pane, a) {
			obs.reason = "runtime_changed"
			observations[a.PaneID] = obs
			continue
		}
		obs.herdrStatus = pane.Status
		obs.report = pane.SessionRaw
		obs.title = pane.TerminalTitle
		if len(observations) > 128 {
			obs.reason = "runtime_unavailable"
			observations[a.PaneID] = obs
			continue
		}
		obs.probe, obs.reason = s.inspectCodexProcess(ctx, a)
		observations[a.PaneID] = obs
		if obs.reason != "" || obs.probe.mode == "standalone" {
			continue
		}
		if wanted[obs.probe.root] == nil {
			wanted[obs.probe.root] = map[string]bool{}
		}
		for origin := range obs.probe.listeners {
			wanted[obs.probe.root][origin] = true
		}
	}
	stores := queryCodexStores(ctx, wanted, readCodexStore)
	for _, a := range agents {
		obs, managed := observations[a.PaneID]
		if !managed || obs.reason != "" {
			continue
		}
		if obs.probe.mode == "standalone" {
			observations[a.PaneID] = s.observeCodexStandalone(ctx, a, obs)
			continue
		}
		store := stores[obs.probe.root]
		if store.err != nil || store.threads == nil {
			obs.reason = "runtime_unavailable"
			observations[a.PaneID] = obs
			continue
		}
		var origin string
		obs.thread, origin, obs.reason = selectCodexPrimary(obs.probe, store.threads, obs.report, obs.title)
		if obs.reason == "" && !pathidentity.SameDirectory(obs.thread.CWD, a.Cwd) {
			obs.reason = "runtime_metadata_conflict"
		}
		if obs.reason != "" {
			observations[a.PaneID] = obs
			continue
		}
		// Missing/unmaterialized history does not make a runtime-confirmed ID unknown.
		if s.conversationM != nil {
			location, reason := s.conversationM.ValidateCodexReport(a.Cwd, obs.thread.ID)
			if reason != "" && reason != "herdr_history_unavailable" {
				obs.reason = "runtime_metadata_conflict"
			}
			if reason == "" {
				root, err := canonicalCodexRoot(location.Root)
				if err != nil || root != obs.probe.root {
					obs.reason = "runtime_metadata_conflict"
				}
			}
		}
		if obs.reason == "" {
			run := s.recordedCodexRun(ctx, a, obs.probe, obs.thread.ID, obs.herdrStatus, obs.thread.AgentStatus())
			obs.run = &run
		}
		after, err := s.herdrC.PaneGet(ctx, a.PaneID)
		if err != nil || !codexPaneMatches(after, a) {
			obs.reason = "runtime_changed"
		} else {
			// Re-select from a fresh pane read, including its title when explicitly
			// configured. A cached title is never an admission or dispatch lease.
			selected, selectedOrigin, reason := selectCodexPrimary(obs.probe, store.threads, after.SessionRaw, after.TerminalTitle)
			if reason != "" || selected.ID != obs.thread.ID || selectedOrigin != origin {
				obs.reason = "runtime_changed"
			}
			obs.report = after.SessionRaw
		}
		current, reason := s.inspectCodexProcess(ctx, a)
		if reason != "" || current.mode != obs.probe.mode || current.titleID != obs.probe.titleID || current.pid != obs.probe.pid || current.process != obs.probe.process || current.root != obs.probe.root || !maps.Equal(current.listeners, obs.probe.listeners) || ctx.Err() != nil {
			obs.reason = "runtime_changed"
		}
		obs.runtime = store.instance + ":" + origin + ":" + obs.probe.listeners[origin]
		observations[a.PaneID] = obs
	}
	return observations
}

func (s *Server) enrichCodexRuntime(ctx context.Context, agents []*coordinator.AgentState) {
	observations := s.observeCodexRuntime(ctx, agents)
	for _, a := range agents {
		obs, managed := observations[a.PaneID]
		if !managed {
			continue
		}
		a.RunState = nil
		a.ReportedSession = obs.report
		a.Session = ""
		a.SessionKind = ""
		a.ConversationSource = ""
		a.ConversationProcess = obs.probe.process
		a.BindingRuntime = obs.runtime
		a.BindingMode = obs.probe.mode
		a.BindingSelection = ""
		if obs.probe.titleID {
			a.BindingSelection = codexTitleSelection
		}
		a.BindingStoreID = ""
		if obs.probe.root != "" {
			a.BindingStoreID = codexStoreID(obs.probe.root)
		}
		a.BindingReason = obs.reason
		a.ConversationReason = obs.reason
		a.BindingState = "verifying"
		if strings.Contains(obs.reason, "ambiguous") || strings.HasSuffix(obs.reason, "ancestry_unavailable") || obs.reason == "primary_thread_unconfirmed" {
			a.BindingState = "ambiguous"
		}
		if strings.Contains(obs.reason, "conflict") || obs.reason == "herdr_session_invalid" || obs.reason == "reported_session_not_candidate" {
			a.BindingState = "conflict"
		}
		if obs.reason != "" {
			continue
		}
		a.Session = obs.thread.ID
		a.SessionKind = "id"
		a.ConversationSource = codexBindingSource(obs.probe) + obs.thread.ID
		a.BindingState = "verified"
		a.BindingVerifiedAt = time.Now().UnixMilli()
		a.RunState = obs.run
		if status := obs.thread.AgentStatus(); status != "" {
			a.Status = status
		}
		if obs.run != nil {
			a.Status = obs.run.Status
		}
	}
}

func (s *Server) codexPrimaryCurrent(ctx context.Context, a *coordinator.AgentState) bool {
	if a == nil {
		return false
	}
	if a.Agent != "codex" {
		return true
	}
	if a.BindingState == "" {
		return s.herdrC == nil && (s.conversationM == nil || a.SessionID == "" || s.conversationM.CodexSessionProjectMatches(a.Cwd, a.SessionID))
	}
	if a.BindingState != "verified" || !s.codexBindings.current(a) {
		return false
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	obs, ok := s.observeCodexRuntime(check, []*coordinator.AgentState{a})[a.PaneID]
	valid := ok && obs.reason == "" && obs.thread.ID == a.SessionID && obs.probe.process == a.ConversationProcess && obs.runtime == a.BindingRuntime && (obs.probe.titleID == (a.BindingSelection == codexTitleSelection)) && codexStoreID(obs.probe.root) == a.BindingStoreID && a.ConversationSource == codexBindingSource(obs.probe)+a.SessionID
	if !valid {
		s.codexBindings.invalidate(a)
		if s.poller != nil {
			s.poller.Wake()
		}
	}
	return valid
}

func (s *Server) conversationSelectionCurrent(ctx context.Context, a *coordinator.AgentState) bool {
	return s.codexPrimaryCurrent(ctx, a) && s.kimiSelectionCurrent(ctx, a)
}

func readCodexStore(ctx context.Context, root string, wanted map[string]bool) codexStoreResult {
	socket := filepath.Join(filepath.Dir(root), "app-server-control", "app-server-control.sock")
	before, err := os.Stat(socket)
	if err != nil || before.Mode()&os.ModeSocket == 0 {
		return codexStoreResult{err: errCodexProcessStart}
	}
	threads, err := codexruntime.Discover(ctx, socket, wanted)
	after, e := os.Stat(socket)
	if e != nil || !os.SameFile(before, after) {
		return codexStoreResult{err: errCodexProcessStart}
	}
	return codexStoreResult{threads: threads, instance: codexSocketIdentity(before), err: err}
}
func queryCodexStores(ctx context.Context, wanted map[string]map[string]bool, query func(context.Context, string, map[string]bool) codexStoreResult) map[string]codexStoreResult {
	results := map[string]codexStoreResult{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	roots := make([]string, 0, len(wanted))
	for root := range wanted {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	for _, root := range roots {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			check, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			result := query(check, root, wanted[root])
			mu.Lock()
			results[root] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}
