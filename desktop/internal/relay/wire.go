package relay

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const maxMessage = 21 * 1024 * 1024
const maxRecord = 1024*1024 + 6

// logicalWire keeps E2EE, request ordering and terminal streams identical on
// the gateway and authenticated local WebSocket routes.
type logicalWire interface {
	logicalWrite([]byte) error
	logicalRead() ([]byte, error)
	close()
}

func (w *wire) close() { _ = w.conn.Close() }

type wire struct {
	conn net.Conn
	mu   sync.Mutex
}

func (w *wire) write(kind byte, data []byte) error {
	if len(data) > maxRecord {
		return errors.New("record too large")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.conn.SetWriteDeadline(time.Now().Add(20 * time.Second))
	header := make([]byte, 5)
	header[0] = kind
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))
	if err := writeAll(w.conn, header); err != nil {
		return err
	}
	return writeAll(w.conn, data)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func (w *wire) read() (byte, []byte, error) {
	for {
		w.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		h := make([]byte, 5)
		if _, err := io.ReadFull(w.conn, h[:1]); err != nil {
			return 0, nil, err
		}
		w.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if _, err := io.ReadFull(w.conn, h[1:]); err != nil {
			return 0, nil, err
		}
		n := binary.BigEndian.Uint32(h[1:])
		if h[0] < 1 || h[0] > 4 || n > maxRecord || (h[0] >= 3 && n != 8) {
			return 0, nil, errors.New("invalid TCP record")
		}
		b := make([]byte, int(n))
		if _, err := io.ReadFull(w.conn, b); err != nil {
			return 0, nil, err
		}
		if h[0] == 3 {
			if err := w.write(4, b); err != nil {
				return 0, nil, err
			}
			continue
		}
		if h[0] == 4 {
			continue
		}
		return h[0], b, nil
	}
}
func (w *wire) logicalWrite(data []byte) error {
	if len(data) > maxMessage {
		return errors.New("message too large")
	}
	for offset := 0; ; {
		header := 2
		if offset == 0 {
			header = 6
		}
		n := min(len(data)-offset, 262144-header)
		b := make([]byte, header+n)
		b[0] = 1
		if offset == 0 {
			b[1] |= 1
			binary.BigEndian.PutUint32(b[2:6], uint32(len(data)))
		}
		if offset+n == len(data) {
			b[1] |= 2
		}
		copy(b[header:], data[offset:offset+n])
		if err := w.write(2, b); err != nil {
			return err
		}
		offset += n
		if offset == len(data) {
			return nil
		}
	}
}
func (w *wire) logicalRead() ([]byte, error) {
	var out []byte
	expected := -1
	var start time.Time
	for {
		kind, b, err := w.read()
		if err != nil {
			return nil, err
		}
		if kind != 2 || len(b) < 2 || b[0] != 1 || b[1] > 3 {
			return nil, errors.New("invalid gateway chunk")
		}
		flags := b[1]
		header := 2
		if flags&1 != 0 {
			start = time.Now()
			if expected != -1 || len(b) < 6 {
				return nil, errors.New("unexpected chunk start")
			}
			expected = int(binary.BigEndian.Uint32(b[2:6]))
			if expected > maxMessage {
				return nil, errors.New("message too large")
			}
			out = make([]byte, 0, expected)
			header = 6
		}
		if expected < 0 || len(out)+len(b)-header > expected || time.Since(start) > 2*time.Minute {
			return nil, errors.New("invalid chunk progress")
		}
		out = append(out, b[header:]...)
		if flags&2 != 0 {
			if len(out) != expected {
				return nil, errors.New("incomplete message")
			}
			return out, nil
		}
	}
}
