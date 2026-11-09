package relay

import (
	"encoding/json"
	"strings"
)

type Target struct {
	ServerSessionID string `json:"server_session_id"`
	PaneID          string `json:"pane_id"`
	TerminalID      string `json:"terminal_id"`
	Generation      int64  `json:"generation"`
	AgentSessionID  string `json:"agent_session_id"`
	BindingEpoch    uint64 `json:"binding_epoch"`
}

// RunEvidence is metadata for the exact foreground turn, not a terminal-idle guess.
type RunEvidence struct {
	Status              string `json:"status"`
	Source              string `json:"source"`
	Evidence            string `json:"evidence"`
	TurnID              string `json:"turn_id"`
	CompletionPending   bool   `json:"completion_pending"`
	CompletionConfirmed bool   `json:"completion_confirmed"`
	ObservedAt          int64  `json:"observed_at"`
}

func (r *RunEvidence) Trusted() bool {
	return r != nil && r.Source == "codex_runtime" && r.TurnID != "" && (r.Evidence == "runtime_turn" || r.Evidence == "rollout_lifecycle")
}

type Agent struct {
	TerminalOnly bool         `json:"terminal_only,omitempty"`
	RunState     *RunEvidence `json:"run_state,omitempty"`
	Target
	Agent               string       `json:"agent"`
	Name                string       `json:"name"`
	TabLabel            string       `json:"tab_label"`
	TabName             string       `json:"tab_name"`
	TabID               string       `json:"tab_id"`
	TabOrder            int          `json:"tab_order"`
	TabNumber           int          `json:"tab_number"`
	SessionName         string       `json:"session_name"`
	Status              string       `json:"status"`
	Cwd                 string       `json:"cwd"`
	WorkspaceID         string       `json:"workspace_id"`
	WorkspaceLabel      string       `json:"workspace_label"`
	Project             string       `json:"project"`
	BindingState        string       `json:"binding_state"`
	BindingReason       string       `json:"binding_reason"`
	Attention           string       `json:"attention_kind"`
	Prompt              string       `json:"prompt"`
	Command             string       `json:"command"`
	Options             []string     `json:"options"`
	EventID             string       `json:"event_id"`
	ApprovalFingerprint string       `json:"approval_fingerprint"`
	Interaction         *Interaction `json:"interaction"`
}

func (a Agent) Title() string {
	for _, name := range []string{a.TabLabel, a.TabName, a.Name, a.SessionName} {
		if strings.TrimSpace(name) != "" {
			return name
		}
	}
	return a.Provider()
}
func (a Agent) Provider() string {
	if a.TerminalOnly {
		return "终端"
	}
	switch a.Agent {
	case "codex":
		return "Codex"
	case "claude":
		return "Claude"
	case "pi":
		return "Pi"
	case "":
		return "Agent"
	}
	return a.Agent
}

type Workspace struct {
	ID              string `json:"workspace_id"`
	ServerSessionID string `json:"server_session_id"`
	Label           string `json:"label"`
	Cwd             string `json:"cwd"`
	Number          int    `json:"number"`
}

func ParseWorkspaces(v any) []Workspace {
	b, _ := json.Marshal(v)
	var rows []Workspace
	_ = json.Unmarshal(b, &rows)
	for i := range rows {
		if rows[i].ServerSessionID == "" {
			rows[i].ServerSessionID = "primary"
		}
	}
	return rows
}
func (a Agent) CanChat() bool {
	return !a.TerminalOnly && (a.Agent != "codex" || a.BindingState == "" || a.BindingState == "verified")
}
func (a Agent) Identity() string {
	t := a.Target
	t.BindingEpoch = 0
	b, _ := json.Marshal(t)
	return string(b)
}
func (a Agent) Request(action string) Message {
	return Message{"type": action, "pane_id": a.PaneID, "target": a.Target, "server_session_id": a.ServerSessionID, "cwd": a.Cwd}
}
func ParseAgents(v any) []Agent {
	b, _ := json.Marshal(v)
	var result []Agent
	_ = json.Unmarshal(b, &result)
	for i := range result {
		if result[i].ServerSessionID == "" {
			result[i].ServerSessionID = "primary"
		}
	}
	return result
}
