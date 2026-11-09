package relay

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMatchesSharedE2EEVectors(t *testing.T) {
	b, err := os.ReadFile("../../../contracts/fixtures/e2ee/v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw Message
	if err = json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	client, server := Object(raw["client"]), Object(raw["server"])
	private, _ := base64.RawURLEncoding.DecodeString(Text(client, "private_key"))
	key, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	auth := Auth{Kind: "credential", ID: "credential-test", Version: 7, Secret: encode([]byte(Text(raw, "relay_key")))}
	h, err := newHandshake(auth)
	if err != nil {
		t.Fatal(err)
	}
	h.key = key
	h.nonce, _ = decode(Text(client, "nonce"), 32)
	if Text(h.hello(), "proof") != Text(Object(client["hello"]), "proof") {
		t.Fatal("client proof differs from Android/Relay fixture")
	}
	cipher, err := h.finish(Object(server["hello"]))
	if err != nil {
		t.Fatal(err)
	}
	records := Object(raw["records"])
	for _, name := range []string{"client_finish", "c2s"} {
		record := Object(records[name])
		expected, _ := base64.RawURLEncoding.DecodeString(Text(record, "binary_frame"))
		got, err := cipher.seal([]byte(Text(record, "plaintext")))
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("%s mismatch: %v", name, err)
		}
	}
	record := Object(records["s2c"])
	frame, _ := base64.RawURLEncoding.DecodeString(Text(record, "binary_frame"))
	got, err := cipher.open(frame)
	if err != nil || string(got) != Text(record, "plaintext") {
		t.Fatal("server vector mismatch", err)
	}
	if _, err = cipher.open(frame); err == nil {
		t.Fatal("replayed encrypted frame accepted")
	}
}
func TestInvitationValidation(t *testing.T) {
	base := "herdr-tcp://127.0.0.1:8377/#invite=abcdefghijklmnop&invite_version=1&invite_expires=9999999999999&setup=" + encode(make([]byte, 32)) + "&rendezvous=" + encode(make([]byte, 32)) + "&relay_id=" + encode(make([]byte, 16))
	c, err := ParseInvitation(base, "电脑", time.Now())
	if err != nil || c.Address != "127.0.0.1:8377" {
		t.Fatal(c, err)
	}
	for _, bad := range []string{base + "&invite=another", base + "&relay=wss://example.com", strings.Replace(base, ":8377/", ":0/", 1), strings.Replace(base, "9999999999999", "1", 1), strings.Replace(base, "herdr-tcp", "http", 1)} {
		if _, err := ParseInvitation(bad, "", time.Now()); err == nil {
			t.Fatal("accepted invalid invitation")
		}
	}
}
func TestRecordFragmentationAndBounds(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	go func() {
		defer right.Close()
		for _, b := range []byte{1, 0, 0, 0, 3, 'a', 'b', 'c'} {
			right.Write([]byte{b})
		}
	}()
	kind, b, err := (&wire{conn: left}).read()
	if err != nil || kind != 1 || string(b) != "abc" {
		t.Fatal(kind, string(b), err)
	}
	l, r := net.Pipe()
	defer l.Close()
	defer r.Close()
	go func() { r.Write([]byte{2, 0x7f, 0xff, 0xff, 0xff}) }()
	if _, _, err := (&wire{conn: l}).read(); err == nil {
		t.Fatal("oversize frame accepted")
	}
}

func TestRealTCPEnrollmentAndRequest(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	invitationSecret := bytes.Repeat([]byte{7}, 32)
	credentialSecret := bytes.Repeat([]byte{9}, 32)
	routing := bytes.Repeat([]byte{4}, 32)
	computer := Computer{ID: "test", Address: listener.Addr().String(), RelayID: encode(make([]byte, 16)), Rendezvous: encode(routing), Auth: Auth{Kind: "invitation", ID: "fixture-invite", Version: 1, Secret: encode(invitationSecret), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}}
	result := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				result <- err
				return
			}
			secret, kind := invitationSecret, "invitation"
			if i == 1 {
				secret, kind = credentialSecret, "credential"
			}
			err = serveFixture(conn, computer.RelayID, routing, secret, credentialSecret, kind, i == 1)
			conn.Close()
			if err != nil {
				result <- err
				return
			}
		}
		result <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	saves := 0
	s, err := Connect(ctx, computer, func(auth Auth) error {
		saves++
		if auth.Kind != "credential" || auth.Secret != encode(credentialSecret) {
			t.Error("invalid persisted credential")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	reply, err := s.Request(ctx, Message{"type": "get_conversation_history"})
	if err != nil || Text(reply, "fixture") != "ok" {
		t.Fatal(reply, err)
	}
	if saves != 2 {
		t.Fatalf("persist count %d", saves)
	}
	if err = <-result; err != nil {
		t.Fatal(err)
	}
}

func serveFixture(conn net.Conn, relayID string, routing, secret, credential []byte, kind string, rpc bool) error {
	var preface [9]byte
	if _, err := io.ReadFull(conn, preface[:]); err != nil {
		return err
	}
	if string(preface[:8]) != "HRDRTCP1" || preface[8] != 2 {
		return io.ErrUnexpectedEOF
	}
	w := &wire{conn: conn}
	sendJSON := func(m Message) error { b, _ := json.Marshal(m); return w.write(1, b) }
	nonce := bytes.Repeat([]byte{2}, 32)
	if err := sendJSON(Message{"type": "gateway_hello", "proto": 1, "nonce": encode(nonce)}); err != nil {
		return err
	}
	_, b, err := w.read()
	if err != nil {
		return err
	}
	m, err := parseMessage(b)
	if err != nil {
		return err
	}
	if Text(m, "proof") != encode(mac(routing, []byte("herdr-gw-connect\x00"), []byte(relayID), nonce)) {
		return io.ErrUnexpectedEOF
	}
	if err = sendJSON(Message{"type": "ready"}); err != nil {
		return err
	}
	return serveAuthenticatedFixture(w, secret, credential, kind, rpc)
}

func serveAuthenticatedFixture(w logicalWire, secret, credential []byte, kind string, rpc bool) error {
	b, err := w.logicalRead()
	if err != nil {
		return err
	}
	hello, err := parseMessage(b)
	if err != nil {
		return err
	}
	if Text(hello, "auth_kind") != kind {
		return io.ErrUnexpectedEOF
	}
	clientNonce, _ := decode(Text(hello, "nonce"), 32)
	clientPub, _ := decode(Text(hello, "public_key"), 65)
	binding := []byte("herdr-e2ee-v2 auth\x00" + kind + "\x00" + Text(hello, "auth_id") + "\x001\x00")
	if Text(hello, "proof") != encode(mac(secret, []byte("herdr-e2ee-v2 client\x00"), binding, clientNonce, clientPub)) {
		return io.ErrUnexpectedEOF
	}
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	serverNonce := bytes.Repeat([]byte{3}, 32)
	transcript := append(append(append(append([]byte{}, binding...), clientNonce...), clientPub...), serverNonce...)
	transcript = append(transcript, key.PublicKey().Bytes()...)
	b, _ = json.Marshal(Message{"type": "e2ee_server_hello", "version": 2, "nonce": encode(serverNonce), "public_key": encode(key.PublicKey().Bytes()), "proof": encode(mac(secret, []byte("herdr-e2ee-v2 server\x00"), transcript))})
	if err = w.logicalWrite(b); err != nil {
		return err
	}
	pub, err := ecdh.P256().NewPublicKey(clientPub)
	if err != nil {
		return err
	}
	shared, err := key.ECDH(pub)
	if err != nil {
		return err
	}
	salt := mac(secret, []byte("herdr-e2ee-v2 key\x00"), transcript)
	in, _ := newAEAD(hkdf(shared, salt, "herdr-e2ee-v2 c2s"))
	out, _ := newAEAD(hkdf(shared, salt, "herdr-e2ee-v2 s2c"))
	read := func(sequence uint64) (Message, error) {
		b, err := w.logicalRead()
		if err != nil {
			return nil, err
		}
		if len(b) < 26 || binary.BigEndian.Uint64(b[2:10]) != sequence {
			return nil, io.ErrUnexpectedEOF
		}
		nonce, aad := nonceAAD(sequence, "c2s")
		plain, err := in.Open(nil, nonce, b[10:], aad)
		if err != nil {
			return nil, err
		}
		return parseMessage(plain)
	}
	write := func(sequence uint64, m Message) error {
		data, _ := json.Marshal(m)
		nonce, aad := nonceAAD(sequence, "s2c")
		frame := make([]byte, 10)
		frame[0] = 2
		binary.BigEndian.PutUint64(frame[2:], sequence)
		return w.logicalWrite(out.Seal(frame, nonce, data, aad))
	}
	finish, err := read(0)
	if err != nil {
		return err
	}
	if Text(finish, "type") != "e2ee_client_finish" {
		return io.ErrUnexpectedEOF
	}
	enrollment := Message{"type": "e2ee_server_finish", "version": 2, "device_id": "device", "credential_id": "fixture-credential", "credential_version": 1, "role": "controller"}
	if kind == "invitation" {
		enrollment["credential_secret"] = encode(credential)
	}
	if err = write(0, enrollment); err != nil {
		return err
	}
	if !rpc {
		_, err = w.logicalRead()
		if err == nil {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
	request, err := read(1)
	if err != nil {
		return err
	}
	return write(1, Message{"type": "command_result", "request_id": Text(request, "request_id"), "ok": true, "phase": "completed", "data": Message{"fixture": "ok"}})
}
