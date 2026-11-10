// Package directterminal adapts Herdr's private protocol 22 terminal connection.
// The terminal owner supplies ANSI frames and owns resizing; no PTY ioctl is used.
package directterminal

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/0cv/herdr-mobile-relay/internal/localipc"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const ProtocolVersion = 22
const maxFrame = 2 * 1024 * 1024

type Frame struct {
	Data          []byte
	Columns, Rows int
	Sequence      uint64
}
type Stream struct {
	conn     net.Conn
	mu       sync.Mutex
	previous uint64
	seen     bool
}

func SocketPath(apiPath string) string {
	base := filepath.Base(apiPath)
	return filepath.Join(filepath.Dir(apiPath), strings.TrimSuffix(base, filepath.Ext(base))+"-client.sock")
}
func Dial(ctx context.Context, path, terminal string, columns, rows int) (*Stream, error) {
	if terminal == "" || len(terminal) > 256 || !validSize(columns, rows) {
		return nil, errors.New("invalid terminal target or size")
	}
	conn, err := localipc.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	s := &Stream{conn: conn}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if err = s.send(geometry(0, columns, rows)); err != nil {
		conn.Close()
		return nil, err
	}
	b, err := readPacket(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	d := decoder{b: b}
	tag, version, encoding := d.number(), d.number(), d.number()
	hasError := d.byte()
	if tag != 0 || version != ProtocolVersion || encoding != 1 || hasError != 0 || d.finish() != nil {
		conn.Close()
		return nil, errors.New("Herdr 原生终端需要协议 22，且必须允许 direct attach")
	}
	b = appendNumber(nil, 5)
	b = appendBytes(b, []byte(terminal))
	b = append(b, 0) // never take over another controller
	if err = s.send(b); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return s, nil
}
func validSize(cols, rows int) bool { return cols >= 2 && cols <= 512 && rows >= 2 && rows <= 256 }
func geometry(tag uint64, cols, rows int) []byte {
	b := appendNumber(nil, tag)
	if tag == 0 {
		b = appendNumber(b, ProtocolVersion)
	}
	b = appendNumber(b, uint64(cols))
	b = appendNumber(b, uint64(rows))
	return append(b, 0, 0, 0) // cell pixels unknown; SGR cell mouse is supported
}
func (s *Stream) Close() error { return s.conn.Close() }
func (s *Stream) Input(data []byte) error {
	if len(data) == 0 || len(data) > 32768 {
		return errors.New("terminal input exceeds limit")
	}
	return s.send(appendBytes(appendNumber(nil, 1), data))
}
func (s *Stream) Resize(cols, rows int) error {
	if !validSize(cols, rows) {
		return errors.New("invalid terminal dimensions")
	}
	return s.send(geometry(3, cols, rows))
}
func (s *Stream) Scroll(lines int) error {
	if lines == 0 || lines < -1000 || lines > 1000 {
		return errors.New("invalid scroll distance")
	}
	direction := uint64(1)
	if lines < 0 {
		direction = 0
		lines = -lines
	}
	b := appendNumber(nil, 6)
	b = appendNumber(b, 0)
	b = appendNumber(b, direction)
	b = appendNumber(b, uint64(lines))
	return s.send(append(b, 0, 0, 0)) // no pointer position or modifiers
}
func (s *Stream) send(b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	h := make([]byte, 4)
	binary.LittleEndian.PutUint32(h, uint32(len(b)))
	for _, part := range [][]byte{h, b} {
		for len(part) > 0 {
			n, err := s.conn.Write(part)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}
func (s *Stream) Read() (Frame, error) {
	for {
		b, err := readPacket(s.conn)
		if err != nil {
			return Frame{}, err
		}
		d := decoder{b: b}
		tag := d.number()
		switch tag {
		case 1:
			seq, cols, rows := d.number(), d.number(), d.number()
			full := d.byte()
			data := d.bytes()
			if err = d.finish(); err != nil {
				return Frame{}, err
			}
			if full > 1 || !validSize(int(cols), int(rows)) || (!s.seen && (seq != 1 || full != 1)) || (s.seen && seq != s.previous+1) {
				return Frame{}, errors.New("invalid terminal frame")
			}
			s.previous = seq
			s.seen = true
			return Frame{Data: data, Columns: int(cols), Rows: int(rows), Sequence: seq}, nil
		case 3:
			return Frame{}, errors.New("远程终端已关闭或控制权不可用")
		case 8:
			enabled, pixels := d.byte(), d.byte()
			if d.finish() != nil || enabled > 1 || pixels > 1 {
				return Frame{}, errors.New("invalid mouse mode")
			}
			data := []byte("\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1016l")
			if enabled == 1 {
				data = append(data, []byte("\x1b[?1002h\x1b[?1006h")...)
			}
			return Frame{Data: data}, nil
		case 9:
			count := d.number()
			if d.finish() != nil || count > 256 {
				return Frame{}, errors.New("invalid bell")
			}
			return Frame{Data: []byte("\a")}, nil
		case 15:
			return Frame{}, errors.New("Herdr 拒绝终端连接，请检查控制权和终端身份")
		case 16:
			flags, level := d.number(), d.byte()
			if d.finish() != nil || flags > 65535 || level > 2 {
				return Frame{}, errors.New("invalid keyboard mode")
			}
			return Frame{Data: fmt.Appendf(nil, "\x1b[=%du\x1b[>4;%dm", flags, level)}, nil
		// Notifications, clipboard and file graphics are deliberately not interpreted.
		// They never cause host filesystem or clipboard access through this adapter.
		default:
			if d.err != nil {
				return Frame{}, d.err
			}
		}
	}
}
func readPacket(r io.Reader) ([]byte, error) {
	var h [4]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(h[:])
	if n == 0 || n > maxFrame {
		return nil, errors.New("invalid terminal packet length")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

// Bincode 2 standard unsigned varints (distinct from protobuf varints).
func appendNumber(b []byte, n uint64) []byte {
	switch {
	case n < 251:
		return append(b, byte(n))
	case n <= 65535:
		return binary.LittleEndian.AppendUint16(append(b, 251), uint16(n))
	case n <= 0xffffffff:
		return binary.LittleEndian.AppendUint32(append(b, 252), uint32(n))
	default:
		return binary.LittleEndian.AppendUint64(append(b, 253), n)
	}
}
func appendBytes(b, data []byte) []byte { return append(appendNumber(b, uint64(len(data))), data...) }

type decoder struct {
	b      []byte
	offset int
	err    error
}

func (d *decoder) take(n int) []byte {
	if n < 0 || n > len(d.b)-d.offset {
		d.err = io.ErrUnexpectedEOF
		return nil
	}
	b := d.b[d.offset : d.offset+n]
	d.offset += n
	return b
}
func (d *decoder) byte() byte {
	b := d.take(1)
	if len(b) == 0 {
		return 0
	}
	return b[0]
}
func (d *decoder) number() uint64 {
	x := d.byte()
	if d.err != nil {
		return 0
	}
	switch x {
	case 251:
		b := d.take(2)
		if d.err == nil {
			return uint64(binary.LittleEndian.Uint16(b))
		}
	case 252:
		b := d.take(4)
		if d.err == nil {
			return uint64(binary.LittleEndian.Uint32(b))
		}
	case 253:
		b := d.take(8)
		if d.err == nil {
			return binary.LittleEndian.Uint64(b)
		}
	case 254, 255:
		d.err = errors.New("unsupported bincode integer")
	default:
		return uint64(x)
	}
	return 0
}
func (d *decoder) bytes() []byte {
	n := d.number()
	if n > maxFrame {
		d.err = errors.New("oversize terminal bytes")
		return nil
	}
	return d.take(int(n))
}
func (d *decoder) finish() error {
	if d.err != nil {
		return d.err
	}
	if d.offset != len(d.b) {
		return errors.New("unexpected trailing bytes")
	}
	return nil
}
