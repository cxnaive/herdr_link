package coordinator

import (
	"errors"
	"runtime"

	"github.com/0cv/herdr-mobile-relay/internal/profiles"
)

const codexTerminalTitleDefault = `tui.terminal_title=["thread-id","thread-name"]`
const windowsCodexTerminalTitleDefault = `tui.terminal_title=['thread-id','thread-name']`

func codexLaunchArgs(user []string, platform string) []string {
	title := codexTerminalTitleDefault
	if platform == "windows" {
		// PowerShell 5.1 strips embedded double quotes when codex.ps1 forwards
		// argv to node.exe. TOML literal strings survive that native boundary.
		title = windowsCodexTerminalTitleDefault
	}
	// Ownership records include the effective prefix. Replace only that exact
	// pair on restart (including the pre-fix Windows spelling), never later overrides.
	if len(user) >= 2 && user[0] == "-c" && (user[1] == title || user[1] == codexTerminalTitleDefault) {
		user = user[2:]
	}
	return append([]string{"-c", title}, user...)
}

// AgentLaunchDefaults advertises the same built-in defaults used at launch.
// Clients display these separately instead of copying them into user arguments.
func AgentLaunchDefaults() map[string][]string {
	return map[string][]string{"codex": codexLaunchArgs(nil, runtime.GOOS)}
}

func decodeAgentArgs(message map[string]any) ([]string, error) {
	raw, present := message["agent_args"]
	if !present {
		return nil, nil
	}
	var args []string
	switch values := raw.(type) {
	case []string:
		args = append(args, values...)
	case []any:
		if len(values) > profiles.MaxLaunchArgs {
			return nil, errors.New("agent_args must contain at most 64 arguments")
		}
		for _, value := range values {
			arg, ok := value.(string)
			if !ok {
				return nil, errors.New("agent_args must be an array of strings")
			}
			args = append(args, arg)
		}
	default:
		return nil, errors.New("agent_args must be an array of strings")
	}
	if err := profiles.ValidateLaunchArgs(args); err != nil {
		return nil, err
	}
	return args, nil
}
