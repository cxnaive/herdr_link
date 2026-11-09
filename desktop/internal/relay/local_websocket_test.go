package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLocalWebSocketUsesSharedJSONAndBinaryVectors(t *testing.T) {
	b, err := os.ReadFile("../../../contracts/fixtures/e2ee/v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture Message
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	for name, value := range Object(fixture["records"]) {
		record := Object(value)
		raw, err := base64.RawURLEncoding.DecodeString(Text(record, "binary_frame"))
		if err != nil {
			t.Fatal(err)
		}
		expected, _ := json.Marshal(record["frame"])
		got, err := binaryToJSONFrame(raw)
		if err != nil {
			t.Fatal(name, err)
		}
		var a, b Message
		json.Unmarshal(got, &a)
		json.Unmarshal(expected, &b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s JSON differs from shared fixture", name)
		}
		decoded, err := jsonToBinaryFrame(expected)
		if err != nil || !bytes.Equal(decoded, raw) {
			t.Fatalf("%s binary differs: %v", name, err)
		}
	}
	for _, bad := range []string{`{}`, `{"type":"e2ee","version":2,"ciphertext":""}`, `{"type":"e2ee","version":2,"sequence":-1,"ciphertext":"abc"}`, `{"type":"agents"}`, `{"type":"e2ee","version":2,"sequence":1.5,"ciphertext":"abc"}`} {
		if _, err := jsonToBinaryFrame([]byte(bad)); err == nil {
			t.Fatal("unprotected/malformed frame accepted")
		}
	}
}

func TestEncryptedLoopbackHandshakeAndRequestWithoutGateway(t *testing.T) {
	secret := bytes.Repeat([]byte{9}, 32)
	done := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"herdr-e2ee-v2"}})
		if err != nil {
			done <- err
			return
		}
		defer conn.CloseNow()
		done <- serveAuthenticatedFixture(&localWebSocket{conn: conn}, secret, secret, "credential", true)
	}))
	defer server.Close()
	// An environment HTTP proxy must never see local pairing traffic.
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:1")
	t.Setenv("HTTPS_PROXY", "http://192.0.2.1:1")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	w, err := dialLoopbackWebSocket(ctx, strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	session, auth, err := authenticateWire(ctx, w, Auth{Kind: "credential", ID: "fixture-credential", Version: 1, Secret: encode(secret)})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if auth.Role != "controller" {
		t.Fatal("no device authorization")
	}
	go session.readLoop()
	reply, err := session.Request(ctx, Message{"type": "get_agent_run_state"})
	if err != nil || Text(reply, "fixture") != "ok" {
		t.Fatal("local encrypted request failed", reply, err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
func TestLocalRouteRejectsNonLoopbackPlaintextAndRedirects(t *testing.T) {
	for _, address := range []string{"example.com:80", "localhost:80", "192.168.1.1:8375", "[::]:8375"} {
		if w, err := dialLoopbackWebSocket(t.Context(), address); err == nil {
			w.close()
			t.Fatal("non literal-loopback dial accepted")
		}
	}
	for _, kind := range []string{"plaintext", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == "redirect" {
					http.Redirect(w, r, "http://192.0.2.1/ws", http.StatusTemporaryRedirect)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err == nil {
					defer conn.CloseNow()
					conn.Read(r.Context())
				}
			}))
			defer server.Close()
			if w, err := dialLoopbackWebSocket(t.Context(), strings.TrimPrefix(server.URL, "http://")); err == nil {
				w.close()
				t.Fatal("untrusted route accepted")
			}
		})
	}
}
func TestLocalRouteFailureNeverFallsBackToGateway(t *testing.T) {
	gateway, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close()
	gateway.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	_, err = Connect(t.Context(), Computer{LocalSocket: "/missing/tcp-pair/admin.sock", Address: gateway.Addr().String(), Auth: Auth{Kind: "credential"}}, func(Auth) error { t.Fatal("unexpected credentials"); return nil })
	if err == nil {
		t.Fatal("missing local relay connected")
	}
	if c, err := gateway.Accept(); err == nil {
		c.Close()
		t.Fatal("silently connected via gateway")
	}
}
