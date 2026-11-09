// Package nativeconn carries existing gateway messages over bounded TCP records.
// It provides framing, not encryption: payload confidentiality and device identity
// remain the existing authenticated E2EE v2 protocol between phone and computer.
package nativeconn

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	Magic           = "HRDRTCP1"
	RoleRelay  byte = 1
	RolePhone  byte = 2
	RoleProbe  byte = 3
	RoleEnroll byte = 4 // Pinned TLS enrollment; not a Relay or phone transport.
	MaxRecord       = (1 << 20) + 6
)

var ErrProtocol = errors.New("native TCP record protocol violation")

// MessageConn is the shared message surface, implemented by websocket.Conn too.
type MessageConn interface {
	Read(context.Context) (websocket.MessageType, []byte, error)
	Write(context.Context, websocket.MessageType, []byte) error
	Ping(context.Context) error
	SetReadLimit(int64)
	Close(websocket.StatusCode, string) error
	CloseNow() error
}

type Conn struct {
	net       net.Conn
	limit     atomic.Int64
	readMu    sync.Mutex
	writeSlot chan struct{}
	closeOnce sync.Once
	closed    chan struct{}
	pingMu    sync.Mutex
	pings     map[string]chan struct{}
}

func Endpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "tcp" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", ErrProtocol
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", ErrProtocol
	}
	if strings.ContainsAny(u.Hostname(), "\x00\r\n%") {
		return "", ErrProtocol
	}
	return net.JoinHostPort(u.Hostname(), strconv.Itoa(port)), nil
}
func newConn(n net.Conn) *Conn {
	c := &Conn{net: n, writeSlot: make(chan struct{}, 1), closed: make(chan struct{}), pings: map[string]chan struct{}{}}
	c.limit.Store(MaxRecord)
	return c
}
func Dial(ctx context.Context, raw string, role byte) (*Conn, error) {
	address, err := Endpoint(raw)
	if err != nil {
		return nil, err
	}
	if role < RoleRelay || role > RoleEnroll {
		return nil, ErrProtocol
	}
	n, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	c := newConn(n)
	stop := context.AfterFunc(ctx, func() { _ = c.CloseNow() })
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = n.SetWriteDeadline(deadline)
	if err = writeAll(n, append([]byte(Magic), role)); err != nil {
		_ = c.CloseNow()
		return nil, err
	}
	_ = n.SetWriteDeadline(time.Time{})
	if ctx.Err() != nil {
		_ = c.CloseNow()
		return nil, ctx.Err()
	}
	return c, nil
}
func Accept(ctx context.Context, n net.Conn) (*Conn, byte, error) {
	c := newConn(n)
	stop := context.AfterFunc(ctx, func() { _ = c.CloseNow() })
	defer stop()
	_ = n.SetReadDeadline(time.Now().Add(10 * time.Second))
	var preface [9]byte
	_, err := io.ReadFull(n, preface[:])
	if err != nil || string(preface[:8]) != Magic || preface[8] < RoleRelay || preface[8] > RoleEnroll {
		_ = c.CloseNow()
		return nil, 0, ErrProtocol
	}
	_ = n.SetReadDeadline(time.Time{})
	if ctx.Err() != nil {
		_ = c.CloseNow()
		return nil, 0, ctx.Err()
	}
	return c, preface[8], nil
}
func (c *Conn) SetReadLimit(n int64) {
	if n < 0 || n > MaxRecord {
		n = MaxRecord
	}
	c.limit.Store(n)
}
func (c *Conn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if ctx.Err() != nil {
		return 0, nil, ctx.Err()
	}
	stop := context.AfterFunc(ctx, func() { _ = c.CloseNow() })
	defer stop()
	for {
		// Idle streams remain connected; a partial header/body gets a bounded deadline.
		_ = c.net.SetReadDeadline(time.Time{})
		var header [5]byte
		if _, err := io.ReadFull(c.net, header[:1]); err != nil {
			return 0, nil, err
		}
		_ = c.net.SetReadDeadline(time.Now().Add(30 * time.Second))
		if _, err := io.ReadFull(c.net, header[1:]); err != nil {
			_ = c.CloseNow()
			return 0, nil, err
		}
		kind := header[0]
		size := int64(binary.BigEndian.Uint32(header[1:]))
		limit := c.limit.Load()
		if kind >= 3 {
			limit = 8
		}
		if kind < 1 || kind > 4 || size > limit || (kind >= 3 && size != 8) {
			_ = c.CloseNow()
			return 0, nil, ErrProtocol
		}
		data := make([]byte, int(size))
		if _, err := io.ReadFull(c.net, data); err != nil {
			_ = c.CloseNow()
			return 0, nil, err
		}
		_ = c.net.SetReadDeadline(time.Time{})
		switch kind {
		case 1, 2:
			return websocket.MessageType(kind), data, nil
		case 3:
			if err := c.write(ctx, 4, data); err != nil {
				return 0, nil, err
			}
		case 4:
			c.pingMu.Lock()
			if ch := c.pings[string(data)]; ch != nil {
				delete(c.pings, string(data))
				close(ch)
			}
			c.pingMu.Unlock()
		}
	}
}
func (c *Conn) Write(ctx context.Context, kind websocket.MessageType, data []byte) error {
	if kind != websocket.MessageText && kind != websocket.MessageBinary {
		return ErrProtocol
	}
	return c.write(ctx, byte(kind), data)
}
func (c *Conn) write(ctx context.Context, kind byte, data []byte) error {
	if len(data) > MaxRecord {
		return ErrProtocol
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	select {
	case c.writeSlot <- struct{}{}:
		defer func() { <-c.writeSlot }()
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return net.ErrClosed
	}
	stop := context.AfterFunc(ctx, func() { _ = c.CloseNow() })
	defer stop()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = c.net.SetWriteDeadline(deadline)
	var header [5]byte
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))
	err := writeAll(c.net, header[:])
	if err == nil {
		err = writeAll(c.net, data)
	}
	if err != nil {
		_ = c.CloseNow()
		return err
	}
	_ = c.net.SetWriteDeadline(time.Time{})
	return nil
}
func (c *Conn) Ping(ctx context.Context) error {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	key := string(nonce[:])
	ch := make(chan struct{})
	c.pingMu.Lock()
	if len(c.pings) >= 4 {
		c.pingMu.Unlock()
		return ErrProtocol
	}
	c.pings[key] = ch
	c.pingMu.Unlock()
	defer func() { c.pingMu.Lock(); delete(c.pings, key); c.pingMu.Unlock() }()
	if err := c.write(ctx, 3, nonce[:]); err != nil {
		return err
	}
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return net.ErrClosed
	}
}
func (c *Conn) Close(_ websocket.StatusCode, _ string) error { return c.CloseNow() }
func (c *Conn) CloseNow() error {
	var err error
	c.closeOnce.Do(func() { close(c.closed); err = c.net.Close() })
	return err
}
func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
