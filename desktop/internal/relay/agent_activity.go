package relay

import (
	"fmt"
	"strings"
)

type AgentActivity struct {
	Agents, Terminals, Running, Waiting, Unknown int
}

// CountAgentActivity uses the full Relay inventory, not filtered sidebar rows.
func CountAgentActivity(agents []Agent) AgentActivity {
	var result AgentActivity
	for _, agent := range agents {
		if agent.TerminalOnly {
			result.Terminals++
			continue
		}
		result.Agents++
		if agent.Agent == "codex" && agent.BindingState != "" && agent.BindingState != "verified" {
			result.Unknown++
			continue
		}
		switch strings.ToLower(strings.TrimSpace(agent.Status)) {
		case "working", "running", "busy":
			result.Running++
		case "blocked", "waiting", "needs_input":
			result.Waiting++
		case "idle", "ready", "done", "completed", "finished", "interrupted", "failed", "error", "stopped", "exited", "ended":
		default:
			result.Unknown++
		}
	}
	return result
}

func (a AgentActivity) Summary(online, ready bool) string {
	if !online || !ready {
		return "运行数待确认"
	}
	text := fmt.Sprintf("运行中 %d", a.Running)
	if a.Waiting > 0 {
		text += fmt.Sprintf(" · 待处理 %d", a.Waiting)
	}
	if a.Unknown > 0 {
		text += fmt.Sprintf(" · 待确认 %d", a.Unknown)
	}
	return text
}
