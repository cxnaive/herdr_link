package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"
)

type Message map[string]any

func Text(m Message, key string) string { s, _ := m[key].(string); return s }
func Number(m Message, key string) int64 {
	switch n := m[key].(type) {
	case float64:
		if !math.IsNaN(n) && !math.IsInf(n, 0) && n == math.Trunc(n) && n >= -9007199254740991 && n <= 9007199254740991 {
			return int64(n)
		}
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
func Object(v any) Message {
	if m, ok := v.(map[string]any); ok {
		return Message(m)
	}
	if m, ok := v.(Message); ok {
		return m
	}
	return nil
}
func unwrap(m Message) Message {
	if v := Object(m["data"]); v != nil {
		return v
	}
	if v := Object(m["result"]); v != nil {
		return v
	}
	return m
}
func parseMessage(b []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(b, &m)
	if err == nil && m == nil {
		err = errors.New("empty message")
	}
	return m, err
}

type Session struct {
	wire       logicalWire
	cipher     *sessionCipher
	sendMu     sync.Mutex
	mu         sync.Mutex
	pending    map[string]chan Message
	closed     chan struct{}
	once       sync.Once
	err        error
	Events     chan Message
	terminalMu sync.Mutex
	terminal   *TerminalConn
}

// Connect persists enrollment before confirming the new credential. A lost
// invitation response can therefore be retried without creating another device.
func Connect(ctx context.Context, computer Computer, persist func(Auth) error) (*Session, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if computer.Auth.Kind == "invitation" && computer.Auth.ExpiresAt <= time.Now().UnixMilli() {
			return nil, errors.New("配对邀请已过期")
		}
		s, auth, err := dial(ctx, computer)
		if err != nil {
			return nil, err
		}
		if err = persist(auth); err != nil {
			s.Close()
			return nil, fmt.Errorf("无法保存设备凭据: %w", err)
		}
		if computer.Auth.Kind == "invitation" {
			s.Close()
			computer.Auth = auth
			continue
		}
		go s.readLoop()
		go func() {
			select {
			case <-ctx.Done():
				s.Close()
			case <-s.closed:
			}
		}()
		return s, nil
	}
	return nil, errors.New("credential confirmation failed")
}
func dialGateway(ctx context.Context, c Computer) (_ logicalWire, retErr error) {
	conn, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return nil, errors.New("无法连接网关")
	}
	defer func() {
		if retErr != nil {
			conn.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	timer := time.AfterFunc(20*time.Second, func() { conn.Close() })
	defer timer.Stop()
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err = writeAll(conn, append([]byte("HRDRTCP1"), 2)); err != nil {
		return nil, err
	}
	w := &wire{conn: conn}
	kind, b, err := w.read()
	if err != nil {
		return nil, err
	}
	m, err := parseMessage(b)
	if err != nil || kind != 1 || Text(m, "type") != "gateway_hello" || Number(m, "proto") != 1 {
		return nil, errors.New("不兼容的网关握手")
	}
	nonce, err := decode(Text(m, "nonce"), 32)
	if err != nil {
		return nil, err
	}
	key, err := decode(c.Rendezvous, 32)
	if err != nil {
		return nil, err
	}
	request, _ := json.Marshal(Message{"type": "connect", "proto": 1, "relay_id": c.RelayID, "proof": encode(mac(key, []byte("herdr-gw-connect\x00"), []byte(c.RelayID), nonce))})
	if err = w.write(1, request); err != nil {
		return nil, err
	}
	kind, b, err = w.read()
	if err != nil {
		return nil, err
	}
	m, err = parseMessage(b)
	if err != nil || kind != 1 || Text(m, "type") != "ready" {
		return nil, errors.New("网关暂时无法连接这台电脑")
	}
	return w, nil
}

func dial(ctx context.Context, c Computer) (*Session, Auth, error) {
	var w logicalWire
	var err error
	if c.LocalSocket != "" {
		w, err = dialLocal(ctx, c.LocalSocket)
	} else {
		w, err = dialGateway(ctx, c)
	}
	if err != nil {
		return nil, Auth{}, err
	}
	return authenticateWire(ctx, w, c.Auth)
}

func authenticateWire(ctx context.Context, w logicalWire, authInput Auth) (_ *Session, _ Auth, retErr error) {
	defer func() {
		if retErr != nil {
			w.close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { w.close() })
	defer stop()
	timer := time.AfterFunc(20*time.Second, func() { w.close() })
	defer timer.Stop()
	hs, err := newHandshake(authInput)
	if err != nil {
		return nil, Auth{}, err
	}
	b, _ := json.Marshal(hs.hello())
	if err = w.logicalWrite(b); err != nil {
		return nil, Auth{}, err
	}
	b, err = w.logicalRead()
	if err != nil {
		return nil, Auth{}, err
	}
	m, err := parseMessage(b)
	if err != nil {
		return nil, Auth{}, err
	}
	cipher, err := hs.finish(m)
	if err != nil {
		return nil, Auth{}, err
	}
	b, err = cipher.seal([]byte("{\"type\":\"e2ee_client_finish\",\"version\":2}"))
	if err != nil {
		return nil, Auth{}, err
	}
	if err = w.logicalWrite(b); err != nil {
		return nil, Auth{}, err
	}
	b, err = w.logicalRead()
	if err != nil {
		return nil, Auth{}, err
	}
	b, err = cipher.open(b)
	if err != nil {
		return nil, Auth{}, err
	}
	m, err = parseMessage(b)
	if err != nil || Text(m, "type") != "e2ee_server_finish" || Number(m, "version") != 2 || Text(m, "device_id") == "" || Text(m, "credential_id") == "" || Number(m, "credential_version") < 1 || (Text(m, "role") != "controller" && Text(m, "role") != "reader") {
		return nil, Auth{}, errors.New("无效的设备凭据确认")
	}
	auth := Auth{Kind: "credential", ID: Text(m, "credential_id"), Version: Number(m, "credential_version"), Secret: authInput.Secret, DeviceID: Text(m, "device_id"), Role: Text(m, "role")}
	if authInput.Kind == "invitation" {
		auth.Secret = Text(m, "credential_secret")
		if _, err = decode(auth.Secret, 32); err != nil {
			return nil, Auth{}, err
		}
	} else if auth.ID != authInput.ID || auth.Version != authInput.Version || m["credential_secret"] != nil {
		return nil, Auth{}, errors.New("设备凭据发生变化")
	}
	return &Session{wire: w, cipher: cipher, pending: map[string]chan Message{}, closed: make(chan struct{}), Events: make(chan Message, 128)}, auth, nil
}
func (s *Session) Done() <-chan struct{} { return s.closed }
func (s *Session) Err() error            { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Session) stop(err error) {
	s.once.Do(func() { s.mu.Lock(); s.err = err; s.mu.Unlock(); close(s.closed); s.wire.close() })
}
func (s *Session) Close() { s.stop(errors.New("connection closed")) }
func (s *Session) readLoop() {
	defer close(s.Events)
	for {
		b, err := s.wire.logicalRead()
		if err != nil {
			s.stop(err)
			return
		}
		b, err = s.cipher.open(b)
		if err != nil {
			s.stop(err)
			return
		}
		m, err := parseMessage(b)
		if err != nil {
			s.stop(err)
			return
		}
		id := Text(m, "request_id")
		s.mu.Lock()
		ch := s.pending[id]
		s.mu.Unlock()
		if ch != nil && (strings.HasSuffix(Text(m, "type"), "_result") || Text(m, "type") == "error" || Text(m, "type") == "activity_history") {
			select {
			case ch <- m:
			default:
			}
			continue
		}
		if s.routeTerminalEvent(m) {
			continue
		}
		select {
		case s.Events <- m:
		case <-s.closed:
			return
		}
	}
}
func (s *Session) Send(m Message) error {
	return s.send(context.Background(), m)
}
func (s *Session) send(ctx context.Context, m Message) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if ctx.Err() != nil {
		return &RequestError{Message: "请求已取消"}
	}
	select {
	case <-s.closed:
		return errors.New("连接已关闭")
	default:
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b, err = s.cipher.seal(b)
	if err != nil {
		return err
	}
	if err = s.wire.logicalWrite(b); err != nil {
		s.stop(err)
	}
	return err
}

// Request never retries writes. Callers retain drafts when delivery is unknown.
func (s *Session) Request(ctx context.Context, m Message) (Message, error) {
	wait, err := s.beginRequest(ctx, m)
	if err != nil {
		return nil, err
	}
	return wait()
}

// beginRequest writes in the caller's order but leaves acknowledgment waiting
// separate, so a terminal can keep a bounded number of ordered writes in flight.
func (s *Session) beginRequest(ctx context.Context, m Message) (func() (Message, error), error) {
	if err := ctx.Err(); err != nil {
		return nil, &RequestError{false, "请求已取消"}
	}
	copy := Message{}
	for k, v := range m {
		copy[k] = v
	}
	id := NewID()
	copy["request_id"] = id
	copy["protocol"] = 3
	copy["client_id"] = "herdr-desktop"
	ch := make(chan Message, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()
	cleanup := func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }
	if err := s.send(ctx, copy); err != nil {
		cleanup()
		return nil, err
	}
	return func() (Message, error) {
		defer cleanup()
		var reply Message
		select {
		case <-ctx.Done():
			return nil, errors.New("请求超时，执行结果未确认；请先核对终端")
		case <-s.closed:
			// A peer may close immediately after sending its final response.
			select {
			case reply = <-ch:
			default:
				return nil, errors.New("连接中断，执行结果未确认；请求不会自动重发")
			}
		case reply = <-ch:
		}
		return commandReply(reply)
	}, nil
}

func (s *Session) routeTerminalEvent(m Message) bool {
	kind := Text(m, "type")
	if kind != "terminal_output" && kind != "terminal_closed" {
		return false
	}
	s.terminalMu.Lock()
	terminal := s.terminal
	s.terminalMu.Unlock()
	if terminal != nil {
		terminal.Accept(m)
	}
	return true
}
func (s *Session) attachTerminal(t *TerminalConn) error {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminal != nil && s.terminal != t {
		return errors.New("请先断开当前终端")
	}
	s.terminal = t
	return nil
}
func (s *Session) detachTerminal(t *TerminalConn) {
	s.terminalMu.Lock()
	defer s.terminalMu.Unlock()
	if s.terminal == t {
		s.terminal = nil
	}
}
