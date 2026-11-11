package conversation

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
	"github.com/0cv/herdr-mobile-relay/internal/sqliteexec"
)

var codexStateDatabase = regexp.MustCompile(`^state_([0-9]+)\.sqlite$`)

// Codex can rotate a thread's rollout to <thread-id>_<segment-id>.jsonl.
// The read-only thread index, not the newest similarly named file, selects its
// active segment. Never guess a thread by cwd, title, or a matching suffix.
func indexedCodexSession(root, sessionID string) (Location, bool) {
	home := filepath.Dir(root)
	entries, err := os.ReadDir(home)
	if err != nil {
		return Location{}, false
	}
	type database struct {
		path    string
		version uint64
	}
	var databases []database
	for _, entry := range entries {
		match := codexStateDatabase.FindStringSubmatch(entry.Name())
		if match == nil || entry.IsDir() {
			continue
		}
		version, err := strconv.ParseUint(match[1], 10, 64)
		if err != nil {
			continue
		}
		if path := containedRegularFile(filepath.Join(home, entry.Name()), home); path != "" {
			databases = append(databases, database{path, version})
		}
	}
	sort.Slice(databases, func(i, j int) bool { return databases[i].version > databases[j].version })
	if len(databases) > 4 {
		databases = databases[:4]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	for _, db := range databases {
		// Caller has validated the UUID; preserve that invariant even for direct tests.
		if !canonicalSessionID.MatchString(sessionID) {
			return Location{}, true
		}
		query := "SELECT rollout_path FROM threads WHERE id = '" + sessionID + "' LIMIT 1"
		raw, err := sqliteexec.MustFromEnv().QueryJSON(ctx, db.path, query, 16*1024)
		if err != nil {
			continue
		}
		var rows []struct {
			Path string `json:"rollout_path"`
		}
		if json.Unmarshal(raw, &rows) != nil || len(rows) == 0 {
			continue
		}
		// An indexed but unavailable segment must not silently fall back to an
		// obsolete log or to a different session in another configured root.
		path := rows[0].Path
		if !filepath.IsAbs(path) || !strings.HasSuffix(strings.ToLower(path), ".jsonl") {
			return Location{}, true
		}
		path = containedRegularFile(path, root)
		if path == "" {
			return Location{}, true
		}
		if !codexSessionHeaderMatches(path, sessionID) {
			return Location{ValidationError: "herdr_session_mismatch"}, true
		}
		return Location{Path: path, Root: root}, true
	}
	return Location{}, false
}

func codexSessionHeaderMatches(path, sessionID string) bool {
	file, err := openConversationSource(path)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return false
	}
	var record struct {
		Type    string `json:"type"`
		Payload struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	return json.Unmarshal(scanner.Bytes(), &record) == nil && record.Type == "session_meta" && strings.EqualFold(record.Payload.ID, sessionID)
}

func resumedCodexFilename(name, sessionID string) bool {
	prefix := "-" + strings.ToLower(sessionID) + "_"
	stem := strings.TrimSuffix(strings.ToLower(name), ".jsonl")
	pos := strings.LastIndex(stem, prefix)
	return strings.HasPrefix(stem, "rollout-") && strings.HasSuffix(strings.ToLower(name), ".jsonl") && pos > 0 && canonicalSessionID.MatchString(stem[pos+len(prefix):])
}

// CodexSessionProjectMatches checks the operator-selected logical thread against
// the rollout's immutable metadata before permitting a local terminal binding.
func (r *Reader) CodexSessionProjectMatches(cwd, sessionID string) bool {
	if !canonicalSessionID.MatchString(sessionID) || !filepath.IsAbs(cwd) {
		return false
	}
	location := r.Locate("codex", cwd, sessionID)
	if location.Path == "" {
		return false
	}
	file, err := openConversationSource(location.Path)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return false
	}
	var row struct {
		Type    string `json:"type"`
		Payload struct {
			ID  string `json:"id"`
			CWD string `json:"cwd"`
		} `json:"payload"`
	}
	return json.Unmarshal(scanner.Bytes(), &row) == nil && row.Type == "session_meta" && row.Payload.ID == sessionID && pathidentity.SameDirectory(row.Payload.CWD, cwd)
}

// A local pin is refused if a title is subsequently reused for another logical
// thread. This is a revocation guard, not a title-based discovery mechanism.
func (r *Reader) CodexPinnedTitleCurrent(cwd, sessionID, terminalTitle string) bool {
	suffix := " | " + filepath.Base(cwd)
	if !strings.HasSuffix(terminalTitle, suffix) {
		return false
	}
	expected := strings.TrimSuffix(terminalTitle, suffix)
	if expected == "" {
		return false
	}
	location := r.Locate("codex", cwd, sessionID)
	if location.Path == "" {
		return false
	}
	path := containedRegularFile(filepath.Join(filepath.Dir(location.Root), "session_index.jsonl"), filepath.Dir(location.Root))
	if path == "" {
		return false
	}
	file, err := openConversationSource(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() > 8*1024*1024 {
		return false
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 256*1024)
	names := make(map[string]string)
	for scanner.Scan() {
		var row struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(scanner.Bytes(), &row) != nil || row.ID == "" {
			return false
		}
		names[row.ID] = row.Name
		if len(names) > 20000 {
			return false
		}
	}
	if scanner.Err() != nil || names[sessionID] != expected {
		return false
	}
	matches := 0
	for _, name := range names {
		if name == expected {
			matches++
		}
	}
	return matches == 1
}

// CodexThreadMetadata contains only the immutable identity/ancestry header.
// Reading it cannot choose a thread or disclose conversation contents.
type CodexThreadMetadata struct {
	ID             string          `json:"id"`
	CWD            string          `json:"cwd"`
	SessionID      string          `json:"session_id"`
	ParentThreadID string          `json:"parent_thread_id"`
	ForkedFromID   string          `json:"forked_from_id"`
	Source         json.RawMessage `json:"source"`
	ThreadSource   string          `json:"thread_source"`
}

func (r *Reader) CodexBindingMetadata(cwd, id string) (CodexThreadMetadata, Location, string) {
	bad := func(loc Location, reason string) (CodexThreadMetadata, Location, string) {
		return CodexThreadMetadata{}, loc, reason
	}
	if !canonicalSessionID.MatchString(id) || !filepath.IsAbs(cwd) {
		return bad(Location{}, "herdr_session_invalid")
	}
	loc := r.Locate("codex", cwd, id)
	if loc.ValidationError != "" {
		return bad(loc, loc.ValidationError)
	}
	if loc.Path == "" {
		return bad(loc, "herdr_history_unavailable")
	}
	file, err := openConversationSource(loc.Path)
	if err != nil {
		return bad(loc, "herdr_history_unavailable")
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return bad(loc, "herdr_history_unavailable")
	}
	var row struct {
		Type    string              `json:"type"`
		Payload CodexThreadMetadata `json:"payload"`
	}
	if json.Unmarshal(scanner.Bytes(), &row) != nil || row.Type != "session_meta" || !strings.EqualFold(row.Payload.ID, id) {
		return bad(loc, "herdr_session_mismatch")
	}
	if !filepath.IsAbs(row.Payload.CWD) {
		return bad(loc, "herdr_project_mismatch")
	}
	return row.Payload, loc, ""
}

// ValidateCodexReport validates only the supplied Codex ID, never choosing another
// session. Missing history is distinct from conflicting ID/project metadata.
func (r *Reader) ValidateCodexReport(cwd, id string) (Location, string) {
	meta, loc, reason := r.CodexBindingMetadata(cwd, id)
	if reason == "" && !pathidentity.SameDirectory(meta.CWD, cwd) {
		reason = "herdr_project_mismatch"
	}
	return loc, reason
}
