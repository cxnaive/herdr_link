package codexruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var ErrRunStateUnavailable = errors.New("current Codex turn state is unavailable")

type RunState struct {
	Evidence            string `json:"evidence,omitempty"`
	CompletionPending   bool   `json:"completion_pending,omitempty"`
	CompletionConfirmed bool   `json:"completion_confirmed"`
	Status              string `json:"status"`
	Source              string `json:"source"`
	Attention           string `json:"attention_kind,omitempty"`
	TurnID              string `json:"turn_id,omitempty"`
	StartedAt           int64  `json:"started_at,omitempty"`
	FinishedAt          int64  `json:"finished_at,omitempty"`
	ElapsedMS           *int64 `json:"elapsed_ms"`
	Ticking             bool   `json:"ticking"`
	ObservedAt          int64  `json:"observed_at"`
	Timing              string `json:"timing"`
}
type runtimeStatus struct {
	Type  string   `json:"type"`
	Flags []string `json:"activeFlags"`
}
type runThread struct {
	ID     string        `json:"id"`
	CWD    string        `json:"cwd"`
	Status runtimeStatus `json:"status"`
}
type runTurn struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Started   *int64 `json:"startedAt"`
	Completed *int64 `json:"completedAt"`
	Duration  *int64 `json:"durationMs"`
	ItemsView string `json:"itemsView"`
}

func (r *rpc) runThread(ctx context.Context, id, cwd string) (runThread, error) {
	var data struct {
		Thread runThread `json:"thread"`
	}
	if err := r.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &data); err != nil {
		return runThread{}, ErrRunStateUnavailable
	}
	if data.Thread.ID != id || !pathidentity.SameDirectory(data.Thread.CWD, cwd) {
		return runThread{}, ErrRunStateUnavailable
	}
	switch data.Thread.Status.Type {
	case "active", "idle", "systemError", "notLoaded":
		return data.Thread, nil
	default:
		return runThread{}, ErrRunStateUnavailable
	}
}

func deriveRunState(status runtimeStatus, turn *runTurn, now time.Time) RunState {
	result := RunState{Status: "unknown", Source: "codex_runtime", ObservedAt: now.UnixMilli(), Timing: "unknown"}
	switch status.Type {
	case "active":
		result.Status = "working"
		for _, flag := range status.Flags {
			if flag == "waitingOnApproval" {
				result.Status = "blocked"
				result.Attention = "approval"
				break
			}
			if flag == "waitingOnUserInput" {
				result.Status = "blocked"
				result.Attention = "question"
			}
		}
	case "idle":
		result.Status = "idle"
	case "systemError":
		result.Status = "failed"
	// An unloaded thread is not proof that its foreground process ended.
	default:
		return result
	}
	if turn == nil || turn.ID == "" || turn.ItemsView != "notLoaded" {
		return result
	}
	active := status.Type == "active"
	if active && turn.Status != "inProgress" {
		return result
	}
	if !active {
		if status.Type != "idle" {
			return result
		}
		switch turn.Status {
		case "completed":
			result.Status = "done"
		case "interrupted":
			result.Status = "interrupted"
		case "failed":
			result.Status = "failed"
		default:
			return result
		}
	}
	result.TurnID = turn.ID
	result.Evidence = "runtime_turn"
	if turn.Started == nil || *turn.Started <= 0 || *turn.Started > now.Unix()+1 {
		return result
	}
	start := *turn.Started * 1000
	result.StartedAt = start
	var elapsed int64
	if active {
		if turn.Completed != nil {
			return result
		}
		elapsed = max(int64(0), now.UnixMilli()-start)
		result.Ticking = true
	} else {
		if turn.Completed == nil || *turn.Completed < *turn.Started || *turn.Completed > now.Unix()+1 {
			return result
		}
		result.FinishedAt = *turn.Completed * 1000
		if result.Status == "done" {
			result.CompletionConfirmed = now.UnixMilli()-result.FinishedAt >= 3000
			if !result.CompletionConfirmed {
				result.Status = "working"
				result.CompletionPending = true
			}
		}
		elapsed = result.FinishedAt - start
		// The runtime's ms precision is preferable to rounded second timestamps.
		if turn.Duration != nil && *turn.Duration >= 0 && *turn.Duration <= elapsed+2000 {
			elapsed = *turn.Duration
		}
	}
	result.ElapsedMS = &elapsed
	result.Timing = "turn_elapsed"
	return result
}

// CurrentRun reads metadata only, never tool outputs or conversation bodies.
// One latest turn with itemsView=notLoaded bounds work independently of history size.
func CurrentRun(ctx context.Context, socket, id, cwd string, guard func() error) (RunState, error) {
	if !threadID.MatchString(id) || cwd == "" || guard == nil {
		return RunState{}, ErrRunStateUnavailable
	}
	if err := guard(); err != nil {
		return RunState{}, err
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRunStateUnavailable }}})
	if err != nil {
		return RunState{}, ErrRunStateUnavailable
	}
	defer conn.CloseNow()
	conn.SetReadLimit(128 * 1024)
	r := rpc{conn: conn}
	var init json.RawMessage
	if r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "herdr_mobile_relay_run_state", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, &init) != nil {
		return RunState{}, ErrRunStateUnavailable
	}
	if wsjson.Write(ctx, conn, map[string]any{"method": "initialized"}) != nil {
		return RunState{}, ErrRunStateUnavailable
	}
	before, err := r.runThread(ctx, id, cwd)
	if err != nil {
		return RunState{}, err
	}
	// Not-loaded/failed states do not require history hydration or thread resume.
	if before.Status.Type == "notLoaded" || before.Status.Type == "systemError" {
		if err := guard(); err != nil {
			return RunState{}, err
		}
		return deriveRunState(before.Status, nil, time.Now()), nil
	}
	var turns struct {
		Data []runTurn `json:"data"`
	}
	turnErr := r.call(ctx, "thread/turns/list", map[string]any{"threadId": id, "itemsView": "notLoaded", "sortDirection": "desc", "limit": 1}, &turns)
	after, err := r.runThread(ctx, id, cwd)
	if err != nil {
		return RunState{}, err
	}
	if err := guard(); err != nil {
		return RunState{}, err
	}
	var turn *runTurn
	// A concurrent status transition invalidates the duration, not the fresh status.
	if turnErr == nil && len(turns.Data) == 1 && reflect.DeepEqual(before.Status, after.Status) {
		turn = &turns.Data[0]
	}
	return deriveRunState(after.Status, turn, time.Now()), nil
}
