package conversation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
)

const codexTurnTailBytes = 4 * 1024 * 1024

var errCodexTurnUnavailable = errors.New("Codex lifecycle evidence unavailable")

// CodexTurn contains lifecycle metadata only, never assistant/tool text. Reading
// it does not select or resume a thread; the caller must validate the live owner.
type CodexTurn struct {
	ID         string
	Status     string
	StartedAt  int64
	FinishedAt int64
	DurationMS *int64
	EventAt    int64
}

type codexTurnCacheEntry struct {
	info os.FileInfo
	turn CodexTurn
}
type codexTurnCache struct {
	mu      sync.Mutex
	entries map[string]codexTurnCacheEntry
}

// CodexTurnFor bounds each read to the current file's tail. If a start event is
// outside the window, timing is unknown rather than synthesized from output.
func (r *Reader) CodexTurnFor(ctx context.Context, cwd, id, root string) (CodexTurn, error) {
	if ctx.Err() != nil {
		return CodexTurn{}, ctx.Err()
	}
	meta, loc, reason := r.CodexBindingMetadata(cwd, id)
	if reason != "" || !pathidentity.SameDirectory(meta.CWD, cwd) || filepath.Clean(loc.Root) != filepath.Clean(root) {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	file, err := openConversationSource(loc.Path)
	if err != nil {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	key := cwd + "\x00" + id + "\x00" + loc.Path
	r.codexTurns.mu.Lock()
	cached, ok := r.codexTurns.entries[key]
	r.codexTurns.mu.Unlock()
	if ok && sameCodexTurnFile(cached.info, info) {
		named, err := os.Lstat(loc.Path)
		if err != nil || !named.Mode().IsRegular() || !sameCodexTurnFile(info, named) || ctx.Err() != nil {
			return CodexTurn{}, errCodexTurnUnavailable
		}
		return cloneCodexTurn(cached.turn), nil
	}
	// Validate the same open file, not just the location cache's previous inode.
	scanner := bufio.NewScanner(io.NewSectionReader(file, 0, min(info.Size(), 1024*1024)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	var header struct {
		Type    string              `json:"type"`
		Payload CodexThreadMetadata `json:"payload"`
	}
	if json.Unmarshal(scanner.Bytes(), &header) != nil || header.Type != "session_meta" || !strings.EqualFold(header.Payload.ID, id) || header.Payload.CWD != meta.CWD {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	start := max(int64(0), info.Size()-codexTurnTailBytes)
	var previous CodexTurn
	// Normal rollout writes append. Preserve the known start across large tool
	// outputs, while bounding new readers and unusually large appends to a tail.
	incremental := ok && os.SameFile(cached.info, info) && cached.info.Size() < info.Size() && cached.info.Size() >= start
	if incremental {
		start = cached.info.Size()
		previous = cached.turn
	}
	data := make([]byte, info.Size()-start)
	if _, err := file.ReadAt(data, start); err != nil {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	if start > 0 && !incremental {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return CodexTurn{}, errCodexTurnUnavailable
		}
		data = data[i+1:]
	}
	// An unfinished append can contain the next start or an abort. Do not report
	// the previous completed turn while the tail is incomplete.
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	turn, err := parseCodexTurn(ctx, data, previous)
	if err != nil {
		return CodexTurn{}, err
	}
	after, err := file.Stat()
	named, e := os.Lstat(loc.Path)
	if err != nil || e != nil || !named.Mode().IsRegular() || !sameCodexTurnFile(info, after) || !sameCodexTurnFile(info, named) || ctx.Err() != nil {
		return CodexTurn{}, errCodexTurnUnavailable
	}
	r.codexTurns.mu.Lock()
	if r.codexTurns.entries == nil || len(r.codexTurns.entries) >= 128 {
		r.codexTurns.entries = make(map[string]codexTurnCacheEntry)
	}
	r.codexTurns.entries[key] = codexTurnCacheEntry{info: info, turn: turn}
	r.codexTurns.mu.Unlock()
	return cloneCodexTurn(turn), nil
}

func sameCodexTurnFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func cloneCodexTurn(turn CodexTurn) CodexTurn {
	if turn.DurationMS != nil {
		v := *turn.DurationMS
		turn.DurationMS = &v
	}
	return turn
}

func parseCodexTurn(ctx context.Context, data []byte, latest CodexTurn) (CodexTurn, error) {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if ctx.Err() != nil {
			return CodexTurn{}, ctx.Err()
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(line, &row) != nil {
			return CodexTurn{}, errCodexTurnUnavailable
		}
		if row.Type != "event_msg" {
			continue
		}
		var event struct {
			Type      string          `json:"type"`
			ID        string          `json:"turn_id"`
			Root      string          `json:"root_turn_id"`
			Started   *int64          `json:"started_at"`
			Completed *int64          `json:"completed_at"`
			Duration  *int64          `json:"duration_ms"`
			Error     json.RawMessage `json:"error"`
		}
		if json.Unmarshal(row.Payload, &event) != nil {
			return CodexTurn{}, errCodexTurnUnavailable
		}
		switch event.Type {
		case "task_started", "turn_started", "task_complete", "turn_complete", "turn_aborted":
		default:
			continue
		}
		if event.ID == "" || len(event.ID) > 128 || strings.ContainsFunc(event.ID, unicode.IsControl) {
			latest = CodexTurn{}
			continue
		}
		if event.Root != "" && event.Root != event.ID {
			continue // A nested task cannot finish the foreground root turn.
		}
		stamp, err := time.Parse(time.RFC3339Nano, row.Timestamp)
		if err != nil {
			latest = CodexTurn{}
			continue
		}
		next := CodexTurn{ID: event.ID, EventAt: stamp.UnixMilli()}
		if event.Started != nil && *event.Started > 0 && *event.Started < 1<<50 {
			next.StartedAt = *event.Started * 1000
		}
		switch event.Type {
		case "task_started", "turn_started":
			next.Status = "working"
			if next.StartedAt == 0 {
				next.StartedAt = next.EventAt
			}
		default:
			next.Status = "done"
			if event.Type == "turn_aborted" {
				next.Status = "interrupted"
			} else if len(event.Error) > 0 && string(event.Error) != "null" {
				next.Status = "failed"
			}
			if latest.ID != "" && latest.ID != next.ID {
				latest = CodexTurn{}
				continue
			}
			if latest.ID == next.ID && next.StartedAt == 0 {
				next.StartedAt = latest.StartedAt
			}
			next.FinishedAt = next.EventAt
			if event.Completed != nil && *event.Completed > 0 && *event.Completed < 1<<50 {
				next.FinishedAt = *event.Completed * 1000
			}
			if event.Duration != nil {
				duration := *event.Duration
				next.DurationMS = &duration
			}
		}
		latest = next
	}
	return latest, nil
}
