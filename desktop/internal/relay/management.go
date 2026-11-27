package relay

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Profile struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type EffortOption struct {
	Value       string `json:"reasoningEffort"`
	Description string `json:"description"`
}
type ModelOption struct {
	Model         string         `json:"model"`
	DisplayName   string         `json:"displayName"`
	DefaultEffort string         `json:"defaultReasoningEffort"`
	Efforts       []EffortOption `json:"supportedReasoningEfforts"`
}
type AgentSettings struct {
	Available bool          `json:"available"`
	Reason    string        `json:"reason"`
	Model     string        `json:"model"`
	Effort    string        `json:"effort"`
	Revision  string        `json:"settings_revision"`
	Models    []ModelOption `json:"models"`
	NextTurn  bool          `json:"next_turn"`
}

func Decode[T any](v any) T {
	var result T
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, &result)
	return result
}
func (s AgentSettings) Payload(model, effort string) (Message, error) {
	if !s.Available || s.Revision == "" {
		return nil, errors.New("请重新读取当前设置")
	}
	for _, m := range s.Models {
		if m.Model == model && slices.ContainsFunc(m.Efforts, func(e EffortOption) bool { return e.Value == effort }) {
			return Message{"model": model, "effort": effort, "settings_revision": s.Revision}, nil
		}
	}
	return nil, errors.New("请选择当前模型支持的思考强度")
}
func RenamePayload(name string) (Message, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 128 || strings.ContainsFunc(name, unicode.IsControl) {
		return nil, errors.New("名称须为 1–128 字，不能包含换行或控制字符")
	}
	return Message{"name": name}, nil
}

var launchName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

var driveAbsolute = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// RemoteDirectoryPath checks the Relay's path syntax, not the client's OS.
func RemoteDirectoryPath(p string) bool {
	return !strings.ContainsFunc(p, unicode.IsControl) && ((strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//")) || driveAbsolute.MatchString(p))
}

func LaunchPayload(w Workspace, profile, name, prompt, extra string, argsSupported bool) (Message, error) {
	if w.ServerSessionID != "primary" || w.ID == "" || strings.HasPrefix(w.ID, "cwd:") || !RemoteDirectoryPath(w.Cwd) {
		return nil, errors.New("项目身份或目录已变化，请刷新列表")
	}
	if profile == "" {
		return nil, errors.New("请选择 Agent 类型")
	}
	if !launchName.MatchString(name) {
		return nil, errors.New("名称须以小写字母开头，最多 32 位，可含数字、_ 和 -")
	}
	if utf8.RuneCountInString(prompt) > 100000 {
		return nil, errors.New("初始任务过长")
	}
	args, err := ParseLaunchArgs(extra)
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && !argsSupported {
		return nil, errors.New("请更新此电脑的 Relay 后再使用额外启动参数")
	}
	p := Message{"type": "agent_start", "server_session_id": w.ServerSessionID, "workspace_id": w.ID, "cwd": w.Cwd, "profile_id": profile, "name": name, "prompt": prompt}
	if len(args) > 0 {
		p["agent_args"] = args
	}
	return p, nil
}

// ParseLaunchArgs parses argv data only. Shell expansion is never performed.
func ParseLaunchArgs(line string) ([]string, error) {
	if len(line) > 16384 || strings.ContainsFunc(line, func(r rune) bool { return unicode.IsControl(r) && r != '\t' }) {
		return nil, errors.New("启动参数过长或包含控制字符")
	}
	var args []string
	var word strings.Builder
	var quote rune
	started, total := false, 0
	finish := func() error {
		if !started {
			return nil
		}
		s := word.String()
		total += len(s)
		if strings.ContainsFunc(s, unicode.IsControl) || total > 8192 || len(args) >= 64 {
			return errors.New("启动参数最多 64 项、8192 字节，且不能包含控制字符")
		}
		args = append(args, s)
		word.Reset()
		started = false
		return nil
	}
	chars := []rune(line)
	for i := 0; i < len(chars); i++ {
		r := chars[i]
		switch {
		case r == quote:
			quote = 0
			started = true
		case quote == '\'':
			word.WriteRune(r)
			started = true
		case r == '\\':
			if i+1 == len(chars) {
				return nil, errors.New("末尾反斜杠需要转义字符")
			}
			next := chars[i+1]
			if quote == '"' && !strings.ContainsRune("\\\"$`", next) {
				word.WriteRune(r)
			} else {
				word.WriteRune(next)
				i++
			}
			started = true
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
			started = true
		case quote == 0 && (r == ' ' || r == '\t'):
			if err := finish(); err != nil {
				return nil, err
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("启动参数的引号未闭合")
	}
	if err := finish(); err != nil {
		return nil, err
	}
	return args, nil
}
func (a Agent) OperationWritable(action string) bool {
	if a.PaneID == "" || a.TerminalID == "" {
		return false
	}
	switch action {
	case "agent_rename", "agent_stop", "agent_restart", "agent_clear":
		return true
	}
	return a.CanChat()
}
