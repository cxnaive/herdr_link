package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"runtime"
	"sync"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/directterminal"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/protocol"
	"github.com/0cv/herdr-mobile-relay/internal/transport"
)

type desktopTerminal struct {
	mu       sync.Mutex
	id       string
	target   protocol.TargetRef
	stream   *directterminal.Stream
	sequence uint64
	cancel   context.CancelFunc
}

func desktopTerminalAvailable(status herdr.ServerStatus) bool {
	return (runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows") && status.ServerProtocolKnown && status.ServerProtocol == directterminal.ProtocolVersion
}

func (s *Server) desktopTerminalCurrent(ctx context.Context, client *transport.ClientConn, in protocol.Inbound) bool {
	if ctx.Err() != nil {
		return false
	}
	action, _ := protocol.ClassifyAction("terminal_open")
	if s.authorizeDeviceAction(client, action, "") != nil || validateExactPaneTarget(s.state, in, true) != nil || in.Target == nil || s.herdrC == nil {
		return false
	}
	// This read validates the mutation guard itself; do not recursively run the
	// parent's dispatch guard when PaneGet sends its JSON query.
	check, cancel := context.WithTimeout(client.Context(), 2*time.Second)
	defer cancel()
	pane, err := s.herdrC.PaneGet(check, in.PaneID)
	return err == nil && ctx.Err() == nil && check.Err() == nil && pane != nil && pane.ID == in.PaneID && pane.TerminalID == in.Target.TerminalID && s.authorizeDeviceAction(client, action, "") == nil && validateExactPaneTarget(s.state, in, true) == nil
}

func (s *Server) handleDesktopTerminal(ctx context.Context, client *transport.ClientConn, in protocol.Inbound, admitted func()) {
	fail := func(message string) {
		s.sendCommandResult(client, in.RequestID, in.Type, false, "failed", message, in.PaneID, nil)
	}
	if in.Type == "terminal_close" {
		s.closeDesktopTerminal(client.ID(), in.StreamID)
		s.sendCommandResult(client, in.RequestID, in.Type, true, "completed", "", in.PaneID, nil)
		return
	}
	identity, authenticated := client.Identity()
	if !authenticated || identity.Role != "controller" {
		fail("原生终端需要已配对的控制者设备")
		return
	}
	if in.Type == "terminal_open" {
		if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
			fail("此 Relay 主机暂不支持原生终端桥接")
			return
		}
		s.desktopTerminalMu.Lock()
		defer s.desktopTerminalMu.Unlock()
		admitted()
		if s.desktopTerminals == nil {
			s.desktopTerminals = make(map[string]*desktopTerminal)
		}
		if _, exists := s.desktopTerminals[client.ID()]; exists {
			fail("请先断开当前原生终端")
			return
		}
		if len(s.desktopTerminals) >= 64 {
			fail("原生终端连接数已达上限")
			return
		}
		check, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		if !s.desktopTerminalCurrent(check, client, in) {
			fail("终端身份已变化，请刷新后重试")
			return
		}
		stream, err := directterminal.Dial(check, directterminal.SocketPath(s.cfg.SocketPath), in.Target.TerminalID, in.Columns, in.Rows)
		if err != nil {
			fail("无法附加终端：需要 Herdr 协议 22，且终端没有其他写入控制者")
			return
		}
		if !s.desktopTerminalCurrent(check, client, in) {
			stream.Close()
			fail("终端在连接期间已变化")
			return
		}
		life, stop := context.WithCancel(client.Context())
		st := &desktopTerminal{id: rand.Text(), target: *in.Target, stream: stream, cancel: stop}
		s.desktopTerminals[client.ID()] = st
		s.sendCommandResult(client, in.RequestID, in.Type, true, "completed", "", in.PaneID, map[string]any{"stream_id": st.id})
		go s.readDesktopTerminal(life, client, in, st)
		return
	}
	s.desktopTerminalMu.Lock()
	st := s.desktopTerminals[client.ID()]
	if st != nil {
		st.mu.Lock()
	}
	s.desktopTerminalMu.Unlock()
	admitted()
	if st == nil {
		fail("终端连接已关闭")
		return
	}
	defer st.mu.Unlock()
	if in.StreamID != st.id || in.Target == nil || *in.Target != st.target {
		fail("终端连接身份不匹配")
		return
	}
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if !s.desktopTerminalCurrent(check, client, in) {
		st.cancel()
		st.stream.Close()
		fail("终端身份或权限已变化")
		return
	}
	if in.TerminalSequence != st.sequence+1 {
		st.cancel()
		st.stream.Close()
		fail("终端输入序号不连续，连接已关闭")
		return
	}
	var err error
	switch in.Type {
	case "terminal_input":
		var data []byte
		if len(in.Data) > 45000 {
			err = errors.New("input too large")
		} else {
			data, err = base64.StdEncoding.DecodeString(in.Data)
		}
		if err == nil {
			err = st.stream.Input(data)
		}
	case "terminal_resize":
		err = st.stream.Resize(in.Columns, in.Rows)
	case "terminal_scroll":
		err = st.stream.Scroll(in.Lines)
	}
	if err != nil {
		st.cancel()
		st.stream.Close()
		fail("终端操作结果未确认，连接已关闭；不会自动重发")
		return
	}
	st.sequence++
	s.sendCommandResult(client, in.RequestID, in.Type, true, "completed", "", in.PaneID, nil)
}

func (s *Server) closeDesktopTerminal(clientID, streamID string) {
	s.desktopTerminalMu.Lock()
	st := s.desktopTerminals[clientID]
	if st != nil && (streamID == "" || st.id == streamID) {
		delete(s.desktopTerminals, clientID)
	} else {
		st = nil
	}
	s.desktopTerminalMu.Unlock()
	if st != nil {
		st.cancel()
		st.stream.Close()
	}
}
func (s *Server) readDesktopTerminal(ctx context.Context, client *transport.ClientConn, in protocol.Inbound, st *desktopTerminal) {
	defer s.closeDesktopTerminal(client.ID(), st.id)
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				st.stream.Close()
				return
			case <-tick.C:
				action, _ := protocol.ClassifyAction("terminal_open")
				if s.authorizeDeviceAction(client, action, "") != nil || validateExactPaneTarget(s.state, in, true) != nil {
					st.stream.Close()
					return
				}
			}
		}
	}()
	sequence := uint64(0)
	for {
		frame, err := st.stream.Read()
		if err != nil {
			s.hub.Send(client, map[string]any{"type": "terminal_closed", "stream_id": st.id, "reason": "终端已断开；远端 Agent 继续运行。"})
			return
		}
		action, _ := protocol.ClassifyAction("terminal_open")
		if s.authorizeDeviceAction(client, action, "") != nil || validateExactPaneTarget(s.state, in, true) != nil {
			return
		}
		sequence++
		if !s.hub.Send(client, map[string]any{"type": "terminal_output", "stream_id": st.id, "sequence": sequence, "columns": frame.Columns, "rows": frame.Rows, "data": base64.StdEncoding.EncodeToString(frame.Data)}) {
			return
		}
	}
}
