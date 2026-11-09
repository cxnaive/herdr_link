package relay

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

type localWebSocket struct{ conn *websocket.Conn }

// Only literal loopback addresses are permitted. This route cannot become a
// remote WebSocket client through DNS, HTTP proxy settings or redirects.
func dialLoopbackWebSocket(ctx context.Context, address string) (logicalWire, error) {
	host, port, err := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if err != nil || ip == nil || !ip.IsLoopback() || port == "" {
		return nil, ErrLocalUnsafe
	}
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != address {
			return nil, ErrLocalUnsafe
		}
		return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", address)
	}}
	defer transport.CloseIdleConnections()
	dialCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(dialCtx, "ws://"+address+"/ws", &websocket.DialOptions{
		HTTPClient:   &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrLocalUnsafe }},
		Subprotocols: []string{"herdr-e2ee-v2"}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return nil, errors.New("本机 Relay 加密连接失败，请确认服务正在运行")
	}
	if conn.Subprotocol() != "herdr-e2ee-v2" {
		conn.CloseNow()
		return nil, errors.New("本机 Relay 未启用设备鉴权，拒绝明文连接")
	}
	conn.SetReadLimit(maxMessage)
	return &localWebSocket{conn: conn}, nil
}
func (w *localWebSocket) close() { _ = w.conn.CloseNow() }
func (w *localWebSocket) logicalRead() ([]byte, error) {
	typ, data, err := w.conn.Read(context.Background())
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, errors.New("invalid local Relay frame")
	}
	return jsonToBinaryFrame(data)
}
func (w *localWebSocket) logicalWrite(data []byte) error {
	data, err := binaryToJSONFrame(data)
	if err != nil {
		return err
	}
	if len(data) > maxMessage {
		return errors.New("local Relay frame too large")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return w.conn.Write(ctx, websocket.MessageText, data)
}

// The existing session cipher stays byte-for-byte identical to the shared
// binary vectors. Only this adapter changes the encrypted envelope's encoding.
func binaryToJSONFrame(data []byte) ([]byte, error) {
	if len(data) > 0 && data[0] == '{' {
		return data, nil
	} // Initial signed hello only.
	if len(data) < 26 || data[0] != 2 || data[1] != 0 {
		return nil, errors.New("invalid encrypted local frame")
	}
	return json.Marshal(struct {
		Type       string `json:"type"`
		Version    int    `json:"version"`
		Sequence   uint64 `json:"sequence"`
		Ciphertext string `json:"ciphertext"`
	}{"e2ee", 2, binary.BigEndian.Uint64(data[2:10]), encode(data[10:])})
}
func jsonToBinaryFrame(data []byte) ([]byte, error) {
	var frame struct {
		Type       string  `json:"type"`
		Version    int     `json:"version"`
		Sequence   *uint64 `json:"sequence"`
		Ciphertext string  `json:"ciphertext"`
	}
	if json.Unmarshal(data, &frame) != nil {
		return nil, errors.New("invalid local frame JSON")
	}
	if frame.Type == "e2ee_server_hello" || frame.Type == "e2ee_client_hello" {
		return data, nil
	}
	if frame.Type != "e2ee" || frame.Version != 2 || frame.Sequence == nil || *frame.Sequence >= 1<<53-1 {
		return nil, errors.New("invalid local frame envelope")
	}
	cipher, err := decodeCiphertext(frame.Ciphertext)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 10, len(cipher)+10)
	out[0] = 2
	binary.BigEndian.PutUint64(out[2:], *frame.Sequence)
	return append(out, cipher...), nil
}

func decodeCiphertext(value string) ([]byte, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(data) < 16 || len(data) > maxMessage-10 {
		return nil, errors.New("invalid local ciphertext")
	}
	return data, nil
}
