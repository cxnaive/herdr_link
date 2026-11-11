package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/codexruntime"
	"github.com/0cv/herdr-mobile-relay/internal/coordinator"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/protocol"
	"github.com/0cv/herdr-mobile-relay/internal/transport"
)

var errStandaloneSettings = errors.New("standalone Codex has no external settings endpoint")

var agentSettingsGate = make(chan struct{}, 1)

func (s *Server) agentSettings(ctx context.Context, in protocol.Inbound) (codexruntime.AgentSettings, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	select {
	case agentSettingsGate <- struct{}{}:
		defer func() { <-agentSettingsGate }()
	case <-ctx.Done():
		return codexruntime.AgentSettings{}, ctx.Err()
	}
	socket, id, cwd, guard, err := s.codexRuntimeTarget(ctx, in)
	if err != nil {
		return codexruntime.AgentSettings{}, err
	}
	var update *codexruntime.SettingsUpdate
	if in.Type == "set_agent_settings" {
		update = &codexruntime.SettingsUpdate{Model: in.Model, Effort: in.Effort, Revision: in.SettingsRevision}
	}
	return codexruntime.Settings(ctx, socket, id, cwd, update, guard)
}
func (s *Server) handleAgentSettings(ctx context.Context, client *transport.ClientConn, in protocol.Inbound, msg map[string]any) {
	settings, err := s.agentSettings(ctx, in)
	if in.Type == "get_agent_settings" && err != nil {
		reason := "无法确认此 Agent 的运行时设置。当前仅支持会话身份明确、仍在运行的 Codex。"
		if errors.Is(err, errStandaloneSettings) {
			reason = "此 Codex 使用内嵌/独立运行时，未提供外部模型设置接口；请在电脑终端修改。会话历史与终端交互仍可使用。"
		}
		s.sendCommandResult(client, in.RequestID, in.Type, true, "completed", "", in.PaneID, map[string]any{"available": false, "reason": reason})
		return
	}
	phase, message := "completed", ""
	if err != nil {
		phase = "failed"
		message = "无法修改此 Agent 的设置，请刷新并确认它仍在运行。"
		if errors.Is(err, errStandaloneSettings) {
			message = "独立 Codex 未提供外部设置接口，请在电脑终端修改模型或思考强度。"
		}
		if errors.Is(err, codexruntime.ErrSettingsChanged) {
			message = "模型设置已在电脑端变化，请重新打开设置后选择。"
		}
		if errors.Is(err, codexruntime.ErrInvalidSettings) {
			message = "此模型或思考强度不在当前运行时提供的选项中。"
		}
		if errors.Is(err, codexruntime.ErrSettingsUnknown) {
			phase = "dispatched_unknown"
			message = "设置可能已生效，请重新读取当前设置确认，不要直接重复提交。"
		}
	}
	if in.Type == "set_agent_settings" {
		s.sendAuditedCommandResult(client, msg, in.RequestID, in.Type, err == nil, phase, message, in.PaneID, settings)
	} else {
		s.sendCommandResult(client, in.RequestID, in.Type, err == nil, phase, message, in.PaneID, settings)
	}
}

// codexRuntimeTarget resolves only the exact live pane's configured local daemon.
func (s *Server) codexRuntimeTarget(ctx context.Context, in protocol.Inbound) (string, string, string, func() error, error) {
	agent, ok := s.state.Agent(in.PaneID)
	if !ok || agent.Agent != "codex" || agent.SessionID == "" || (agent.BindingState != "" && agent.BindingState != "verified") || in.Target == nil || s.conversationM == nil || s.herdrC == nil {
		return "", "", "", nil, codexruntime.ErrSettingsUnavailable
	}
	guard := func() error {
		if err := herdr.CheckDispatch(ctx); err != nil {
			return err
		}
		if validateExactPaneTarget(s.state, in, true) != nil {
			return coordinator.ErrPaneReplaced
		}
		current, ok := s.state.Agent(in.PaneID)
		if !ok || !sameSlashTarget(agent, current) || !s.conversationSelectionCurrent(ctx, current) {
			return coordinator.ErrPaneReplaced
		}
		pane, err := s.herdrC.PaneGet(ctx, in.PaneID)
		if err != nil || pane == nil || pane.TerminalID != agent.TerminalID || pane.Agent != "codex" || pane.Cwd != agent.Cwd {
			return coordinator.ErrPaneReplaced
		}
		return nil
	}
	if err := guard(); err != nil {
		return "", "", "", nil, err
	}
	if agent.BindingMode == "standalone" {
		return "", "", "", nil, errStandaloneSettings
	}
	var root string
	if agent.BindingState == "verified" {
		probe, reason := s.inspectCodexProcess(ctx, agent)
		if probe.mode == "standalone" {
			return "", "", "", nil, errStandaloneSettings
		}
		if reason != "" || probe.process != agent.ConversationProcess || codexStoreID(probe.root) != agent.BindingStoreID {
			return "", "", "", nil, codexruntime.ErrSettingsUnavailable
		}
		root = probe.root
	} else {
		if !s.conversationM.CodexSessionProjectMatches(agent.Cwd, agent.SessionID) {
			return "", "", "", nil, codexruntime.ErrSettingsUnavailable
		}
		root = s.conversationM.Locate("codex", agent.Cwd, agent.SessionID).Root
	}
	if root == "" {
		return "", "", "", nil, codexruntime.ErrSettingsUnavailable
	}
	socket := filepath.Join(filepath.Dir(root), "app-server-control", "app-server-control.sock")
	info, err := os.Stat(socket)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return "", "", "", nil, codexruntime.ErrSettingsUnavailable
	}
	return socket, agent.SessionID, agent.Cwd, guard, nil
}
