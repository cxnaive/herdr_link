package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/protocol"
	"github.com/0cv/herdr-mobile-relay/internal/transport"
	"github.com/coder/websocket"
)

func TestRetainedTerminalInventoryReachesClientWithExactTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	panes := json.RawMessage(`[{"pane_id":"pane-1","terminal_id":"terminal-1","workspace_id":"workspace-1","tab_id":"tab-1","agent":"codex","agent_session":{"kind":"id","value":"departed-session"},"agent_status":"blocked","cwd":"/work/v1"}]`)
	server, _ := inventoryBarrierFixture(t, ctx, json.RawMessage(`[]`), panes)
	done := make(chan struct{})
	go func() { defer close(done); server.poller.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for server.committedInventoryStatus()["state"] != "ready" {
		select {
		case <-ctx.Done():
			t.Fatal("terminal inventory never ready")
		case <-time.After(time.Millisecond):
		}
	}
	server.hub.SetOnConnect(func(client *transport.ClientConn) { server.sendConnectionSnapshot(client) })
	httpServer := httptest.NewServer(http.HandlerFunc(server.hub.HandleWebSocket))
	defer httpServer.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var message struct {
			Type   string           `json:"type"`
			Agents []map[string]any `json:"agents"`
		}
		if err = json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Type != "agents" {
			continue
		}
		if len(message.Agents) != 1 {
			t.Fatalf("lost terminal: %s", data)
		}
		row := message.Agents[0]
		if row["terminal_only"] != true || row["agent"] != "" || row["status"] != "idle" || row["terminal_id"] != "terminal-1" || row["conversation_history_available"] == true {
			t.Fatalf("incorrect terminal projection: %s", data)
		}
		if row["agent_session_id"] != nil && row["agent_session_id"] != "" {
			t.Fatal("departed conversation was exposed")
		}
		current, _ := server.state.Agent("pane-1")
		target := protocol.TargetRef{ServerSessionID: row["server_session_id"].(string), PaneID: current.PaneID, TerminalID: current.TerminalID, Generation: current.Generation}
		if err := validateExactPaneTarget(server.state, protocol.Inbound{PaneID: current.PaneID, Target: &target}, true); err != nil {
			t.Fatalf("retained terminal is not addressable: %v", err)
		}
		return
	}
}
