package app

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/0cv/herdr-mobile-relay/internal/codexruntime"
)

const codexTitleSelection = "terminal_title_id"

var codexTitlePrefix = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{5}\.\.\.$`)

// Accept the Windows launcher's TOML literal strings as well as the original
// JSON-compatible spelling, without treating arbitrary TOML as identity evidence.
var literalTitleIDLayout = regexp.MustCompile(`^\[[ \t]*'thread-id'[ \t]*(,[ \t]*'thread-name'[ \t]*)?\]$`)

// Only a known ID-first title layout explicitly configured on this native TUI
// enables the signal. Do not interpret arbitrary tab names or terminal output.
func codexTitleIDConfigured(argv []string) bool {
	if len(argv) == 0 || filepath.Base(argv[0]) != "codex" {
		return false
	}
	enabled := false
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			break
		}
		key, value, attached := strings.Cut(arg, "=")
		config := key == "-c" || key == "--config"
		if strings.HasPrefix(arg, "-c") && len(arg) > 2 && !strings.HasPrefix(arg, "--") {
			config, attached, value = true, true, strings.TrimPrefix(arg[2:], "=")
		}
		if config {
			if !attached {
				i++
				if i >= len(argv) {
					return false
				}
				value = argv[i]
			}
			path, setting, ok := strings.Cut(value, "=")
			if !ok {
				return false
			}
			path = strings.TrimSpace(path)
			switch path {
			case "tui.terminal_title":
				var items []string
				enabled = literalTitleIDLayout.MatchString(strings.TrimSpace(setting)) ||
					(json.Unmarshal([]byte(setting), &items) == nil && (slices.Equal(items, []string{"thread-id", "thread-name"}) || slices.Equal(items, []string{"thread-id"})))
			case "tui":
				// A table override may replace the title. Leave TOML interpretation to Codex.
				enabled = false
			default:
				if strings.Contains(path, "terminal_title") || strings.ContainsAny(path, "\"'") {
					return false
				}
			}
			continue
		}
		switch key {
		case "--remote", "--remote-auth-token-env":
			return false
		case "-m", "--model", "-p", "--profile", "-C", "--cd", "-s", "--sandbox", "-a", "--ask-for-approval", "-i", "--image", "--add-dir", "--enable", "--disable", "--local-provider":
			if !attached {
				i++
				if i >= len(argv) {
					return false
				}
			}
		case "--no-daemon", "--no-alt-screen", "--strict-config", "--search", "--full-auto", "--dangerously-bypass-approvals-and-sandbox", "--yolo", "--oss", "--last", "--all", "--include-non-interactive":
			if attached {
				return false
			}
		default:
			if slices.Contains([]string{"exec", "e", "app-server", "mcp", "mcp-server", "agents", "login", "logout", "debug", "review", "remote-control", "completion", "sandbox", "apply", "cloud", "features", "help"}, arg) {
				return false
			}
			if strings.HasPrefix(arg, "-") {
				if len(arg) > 2 && strings.ContainsRune("mpCsai", rune(arg[1])) {
					continue
				}
				return false
			}
		}
	}
	return enabled
}

// Codex 0.166.0 emits a 29-character UUID prefix plus "..." in its ID segment.
// Match only that shape or a complete UUID, and only against verified roots.
func selectCodexTitle(title string, candidates []codexruntime.Thread) (codexruntime.Thread, string) {
	bad := func(reason string) (codexruntime.Thread, string) { return codexruntime.Thread{}, reason }
	if title == "" {
		return bad("terminal_title_missing")
	}
	if len(title) > 1024 || strings.ContainsFunc(title, unicode.IsControl) {
		return bad("terminal_title_invalid")
	}
	id, _, _ := strings.Cut(title, " | ")
	prefix := codexTitlePrefix.MatchString(id)
	if !prefix && !codexBindingID.MatchString(id) {
		return bad("terminal_title_invalid")
	}
	id = strings.ToLower(strings.TrimSuffix(id, "..."))
	var selected codexruntime.Thread
	for _, candidate := range candidates {
		if !codexBindingID.MatchString(candidate.ID) {
			return bad("terminal_title_conflict")
		}
		actual := strings.ToLower(candidate.ID)
		if actual != id && !(prefix && strings.HasPrefix(actual, id)) {
			continue
		}
		if selected.ID != "" {
			return bad("terminal_title_ambiguous")
		}
		selected = candidate
	}
	if selected.ID == "" {
		return bad("terminal_title_conflict")
	}
	if !selected.Connected {
		return bad("runtime_disconnected")
	}
	return selected, ""
}
