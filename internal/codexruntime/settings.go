package codexruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"unicode"

	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var (
	ErrSettingsUnavailable = errors.New("live Codex settings unavailable")
	ErrSettingsChanged     = errors.New("Codex settings changed; refresh before applying")
	ErrInvalidSettings     = errors.New("model or reasoning effort is not advertised by this Codex runtime")
	ErrSettingsUnknown     = errors.New("settings update result is unconfirmed; read current settings before retrying")
)

type EffortOption struct {
	Value       string `json:"reasoningEffort"`
	Description string `json:"description"`
}
type ModelOption struct {
	ID            string         `json:"id"`
	Model         string         `json:"model"`
	DisplayName   string         `json:"displayName"`
	DefaultEffort string         `json:"defaultReasoningEffort"`
	Efforts       []EffortOption `json:"supportedReasoningEfforts"`
	Hidden        bool           `json:"hidden"`
}
type AgentSettings struct {
	Available bool          `json:"available"`
	Model     string        `json:"model"`
	Effort    string        `json:"effort"`
	Provider  string        `json:"provider"`
	Revision  string        `json:"settings_revision"`
	Models    []ModelOption `json:"models"`
	NextTurn  bool          `json:"next_turn"`
}
type SettingsUpdate struct{ Model, Effort, Revision string }
type settingsThread struct {
	ID         string `json:"id"`
	CWD        string `json:"cwd"`
	Originator string `json:"originator"`
	Model      string `json:"model"`
	Effort     string `json:"reasoningEffort"`
	Provider   string `json:"modelProvider"`
	Status     struct {
		Type string `json:"type"`
	} `json:"status"`
}

func settingValue(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsFunc(value, unicode.IsControl)
}
func (r *rpc) loaded(ctx context.Context, id string) error {
	cursor := ""
	for page := 0; page < 8; page++ {
		var result struct {
			Data []string `json:"data"`
			Next *string  `json:"nextCursor"`
		}
		params := map[string]any{"limit": 128}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := r.call(ctx, "thread/loaded/list", params, &result); err != nil {
			return ErrSettingsUnavailable
		}
		for _, item := range result.Data {
			if item == id {
				return nil
			}
		}
		if result.Next == nil || *result.Next == "" || *result.Next == cursor {
			return ErrSettingsUnavailable
		}
		cursor = *result.Next
	}
	return ErrSettingsUnavailable
}
func (r *rpc) settingsThread(ctx context.Context, id, cwd string) (settingsThread, error) {
	var result struct {
		Thread settingsThread `json:"thread"`
	}
	if err := r.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &result); err != nil {
		return settingsThread{}, ErrSettingsUnavailable
	}
	// Originator is the creation client; resumed Desktop threads can run in a TUI.
	t := result.Thread
	if t.ID != id || !pathidentity.SameDirectory(t.CWD, cwd) || !settingValue(t.Model) || (t.Status.Type != "active" && t.Status.Type != "idle") {
		return settingsThread{}, ErrSettingsUnavailable
	}
	return t, nil
}
func (r *rpc) models(ctx context.Context) ([]ModelOption, error) {
	models := []ModelOption{}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 4; page++ {
		var result struct {
			Data []ModelOption `json:"data"`
			Next *string       `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100, "includeHidden": false}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := r.call(ctx, "model/list", params, &result); err != nil {
			return nil, ErrSettingsUnavailable
		}
		for _, m := range result.Data {
			if m.Hidden {
				continue
			}
			if !settingValue(m.Model) || seen[m.Model] || len(m.Efforts) > 16 || len(m.DisplayName) > 256 {
				return nil, ErrSettingsUnavailable
			}
			seen[m.Model] = true
			for _, e := range m.Efforts {
				if !settingValue(e.Value) || len(e.Description) > 4096 {
					return nil, ErrSettingsUnavailable
				}
			}
			models = append(models, m)
			if len(models) > 256 {
				return nil, ErrSettingsUnavailable
			}
		}
		if result.Next == nil || *result.Next == "" {
			return models, nil
		}
		if *result.Next == cursor {
			return nil, ErrSettingsUnavailable
		}
		cursor = *result.Next
	}
	return nil, ErrSettingsUnavailable
}
func settingsResult(t settingsThread, models []ModelOption) AgentSettings {
	hash := sha256.Sum256([]byte(t.ID + "\x00" + t.CWD + "\x00" + t.Provider + "\x00" + t.Model + "\x00" + t.Effort))
	return AgentSettings{Available: true, Model: t.Model, Effort: t.Effort, Provider: t.Provider, Revision: hex.EncodeToString(hash[:]), Models: models, NextTurn: true}
}
func validUpdate(u SettingsUpdate, models []ModelOption) bool {
	for _, m := range models {
		if m.Model == u.Model {
			for _, e := range m.Efforts {
				if e.Value == u.Effort {
					return true
				}
			}
		}
	}
	return false
}

// Settings never starts or resumes a thread. Only model and effort may be
// updated, on the exact loaded thread, after rechecking the caller's target.
func Settings(ctx context.Context, socket, id, cwd string, update *SettingsUpdate, guard func() error) (AgentSettings, error) {
	if !threadID.MatchString(id) || cwd == "" || guard == nil {
		return AgentSettings{}, ErrSettingsUnavailable
	}
	if err := guard(); err != nil {
		return AgentSettings{}, err
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrSettingsUnavailable }}})
	if err != nil {
		return AgentSettings{}, ErrSettingsUnavailable
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1024 * 1024)
	r := rpc{conn: conn}
	var initialized json.RawMessage
	if r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "herdr_mobile_relay_settings", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}}, &initialized) != nil {
		return AgentSettings{}, ErrSettingsUnavailable
	}
	if wsjson.Write(ctx, conn, map[string]any{"method": "initialized"}) != nil {
		return AgentSettings{}, ErrSettingsUnavailable
	}
	if err := r.loaded(ctx, id); err != nil {
		return AgentSettings{}, err
	}
	thread, err := r.settingsThread(ctx, id, cwd)
	if err != nil {
		return AgentSettings{}, err
	}
	models, err := r.models(ctx)
	if err != nil || len(models) == 0 {
		return AgentSettings{}, ErrSettingsUnavailable
	}
	result := settingsResult(thread, models)
	if update == nil {
		if err := guard(); err != nil {
			return AgentSettings{}, err
		}
		return result, nil
	}
	if update.Revision == "" || update.Revision != result.Revision {
		return result, ErrSettingsChanged
	}
	if !validUpdate(*update, models) {
		return result, ErrInvalidSettings
	}
	// A catalog fetch can take time: do not overwrite a concurrent desktop change.
	thread, err = r.settingsThread(ctx, id, cwd)
	if err != nil {
		return result, err
	}
	if settingsResult(thread, models).Revision != update.Revision {
		return result, ErrSettingsChanged
	}
	if err := guard(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var ack json.RawMessage
	if r.call(ctx, "thread/settings/update", map[string]any{"threadId": id, "model": update.Model, "effort": update.Effort}, &ack) != nil {
		return result, ErrSettingsUnknown
	}
	after, err := r.settingsThread(ctx, id, cwd)
	if err != nil || after.Model != update.Model || after.Effort != update.Effort {
		return result, ErrSettingsUnknown
	}
	if guard() != nil {
		return result, ErrSettingsUnknown
	}
	return settingsResult(after, models), nil
}
