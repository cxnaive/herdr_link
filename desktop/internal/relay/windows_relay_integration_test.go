//go:build windows

package relay

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWindowsRelayEndToEnd(t *testing.T) {
	path := os.Getenv("HERDR_WINDOWS_CLIENT_FIXTURE")
	if path == "" {
		t.Skip("requires the isolated Windows Relay fixture")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Invitation, Pane, Terminal, Cwd, Phase string
		AuthFile                               string `json:"auth_file"`
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	computer, err := ParseInvitation(f.Invitation, "CI", time.Now())
	if err != nil {
		t.Fatal("fixture invitation invalid")
	}
	if data, err = os.ReadFile(f.AuthFile); err == nil {
		if json.Unmarshal(data, &computer.Auth) != nil {
			t.Fatal("invalid saved auth")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	s, err := Connect(ctx, computer, func(a Auth) error {
		computer.Auth = a
		b, e := json.Marshal(a)
		if e != nil {
			return e
		}
		return os.WriteFile(f.AuthFile, b, 0600)
	})
	if err != nil {
		t.Fatal("E2EE connect", err)
	}
	defer s.Close()
	var agent Agent
	refresh := time.NewTicker(500 * time.Millisecond)
	defer refresh.Stop()
	if err = s.Send(Message{"type": "refresh_agents", "protocol": 3}); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case <-refresh.C:
			if err = s.Send(Message{"type": "refresh_agents", "protocol": 3}); err != nil {
				t.Fatal(err)
			}
			continue
		case <-ctx.Done():
			t.Fatal("current inventory not received")
		case m := <-s.Events:
			if Text(m, "type") != "agents" {
				continue
			}
			found := false
			for _, a := range ParseAgents(m["agents"]) {
				if a.PaneID == f.Pane {
					agent = a
					found = true
				}
			}
			if f.Phase == "closed" {
				if !found {
					return
				}
				continue
			}
			if !found {
				continue
			}
			if f.Phase == "agent" {
				if agent.Agent != "codex" {
					continue
				}
			} else if !agent.TerminalOnly {
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}
		if agent.PaneID != "" && ((f.Phase == "agent" && agent.Agent == "codex") || (f.Phase != "agent" && agent.TerminalOnly)) {
			break
		}
	}
	// Continue draining unsolicited inventory/activity events while waiting for
	// request replies, just as the application event loop does.
	go func() {
		for range s.Events {
		}
	}()
	if agent.TerminalID != f.Terminal {
		t.Fatal("inventory terminal mismatch")
	}
	if f.Phase == "agent" && (agent.BindingReason != "runtime_platform_unsupported" || agent.CanChat()) {
		t.Fatalf("Windows must not invent Codex session proof: %s", agent.BindingReason)
	}
	if f.Phase != "agent" && (agent.AgentSessionID != "" || agent.Agent != "") {
		t.Fatal("released agent identity leaked into shell")
	}
	stale := agent
	stale.Generation++
	bad := stale.Request("send_terminal_text")
	bad["text"] = "NEVER_DELIVER_STALE"
	if _, err = s.Request(ctx, bad); err == nil {
		t.Fatal("stale write was accepted")
	}
	literal := agent.Request("send_terminal_text")
	literal["text"] = "set /a 1300+37"
	if _, err = s.Request(ctx, literal); err != nil {
		t.Fatal("literal terminal input", err)
	}
	enter := agent.Request("send_keys")
	enter["keys"] = []string{"Enter"}
	if _, err = s.Request(ctx, enter); err != nil {
		t.Fatal("Enter", err)
	}
	until := time.Now().Add(10 * time.Second)
	for {
		read := agent.Request("read_pane")
		read["lines"] = 100
		read["format"] = "plain"
		reply, e := s.Request(ctx, read)
		if e != nil {
			t.Fatal("read pane", e)
		}
		text := Text(reply, "content")
		if strings.Contains(text, "NEVER_DELIVER_STALE") {
			t.Fatal("stale text reached terminal")
		}
		if strings.Contains(text, "1337") {
			break
		}
		if time.Now().After(until) {
			t.Fatal("shell did not execute arithmetic input")
		}
		time.Sleep(100 * time.Millisecond)
	}
	term := NewTerminalConn(s, agent)
	if err = term.Open(ctx); err != nil {
		t.Fatal("native terminal open", err)
	}
	defer term.Close()
	term.Start(nil)
	output := make(chan string, 8)
	go func() {
		for {
			b := make([]byte, 65536)
			n, e := term.Read(b)
			if e != nil {
				return
			}
			select {
			case output <- string(b[:n]):
			default:
			}
		}
	}()
	select {
	case text := <-output:
		if text == "" {
			t.Fatal("empty native terminal frame")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("native terminal did not render")
	}
	if _, err = term.Write([]byte("set /a 1400+38\r")); err != nil {
		t.Fatal("native terminal input", err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case text := <-output:
			if strings.Contains(text, "1438") {
				return
			}
		case <-deadline.C:
			t.Fatal("native terminal input did not execute")
		}
	}

}
