package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/gatewayaccess"
	"github.com/0cv/herdr-mobile-relay/internal/gatewayenroll"
	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
	"github.com/0cv/herdr-mobile-relay/internal/nativeconn"
	"github.com/coder/websocket"
)

const managementOrigin = "http://127.0.0.1:18378"

func managedFixture(t *testing.T) (*Server, *gatewayaccess.Store, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	password, err := gatewayaccess.Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := gatewayaccess.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	id := testID(t, strings.Repeat("Z", 32))
	server, err := New(Options{Access: store, TCPAddr: "127.0.0.1:0", AllowedRelayIDs: map[string]bool{id: true}, RegistrationToken: strings.Repeat("L", 32), MonthlyBytes: -1, ConnectRatePerMinute: 1000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	handler, err := NewAdminHandler(server, managementOrigin, "tcp://"+server.TCPAddress())
	if err != nil {
		t.Fatal(err)
	}
	return server, store, handler, password
}
func adminRequest(h http.Handler, method, path string, body any, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	var input bytes.Buffer
	if body != nil {
		json.NewEncoder(&input).Encode(body)
	}
	r := httptest.NewRequest(method, managementOrigin+path, &input)
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	r.RemoteAddr = "127.0.0.1:4444"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func loginAdmin(t *testing.T, h http.Handler, password string) (*http.Cookie, string) {
	t.Helper()
	w := adminRequest(h, "POST", "/api/login", map[string]string{"password": password}, nil, "", managementOrigin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var data map[string]string
	json.Unmarshal(w.Body.Bytes(), &data)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("no session")
	}
	return cookies[0], data["csrf"]
}
func TestAdminAuthenticationCSRFPrivateAssetsAndRedactedInventory(t *testing.T) {
	s, _, h, password := managedFixture(t)
	if w := adminRequest(h, "GET", "/api/state", nil, nil, "", ""); w.Code != 401 {
		t.Fatal("unauthenticated inventory")
	}
	if w := adminRequest(h, "POST", "/api/login", map[string]string{"password": password}, nil, "", "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-site login")
	}
	cookie, csrf := loginAdmin(t, h, password)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == password {
		t.Fatal("unsafe login cookie")
	}
	input := map[string]any{"name": "开发电脑"}
	if w := adminRequest(h, "POST", "/api/invites", input, cookie, "", managementOrigin); w.Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if w := adminRequest(h, "POST", "/api/invites", input, cookie, csrf, "https://evil.example"); w.Code != 403 {
		t.Fatal("cross-site mutation")
	}
	invite := adminRequest(h, "POST", "/api/invites", input, cookie, csrf, managementOrigin)
	if invite.Code != 201 {
		t.Fatal(invite.Body.String())
	}
	var result struct {
		Code string `json:"code"`
	}
	json.Unmarshal(invite.Body.Bytes(), &result)
	code, err := gatewayenroll.Parse(result.Code, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	state := adminRequest(h, "GET", "/api/state", nil, cookie, "", "")
	if state.Code != 200 {
		t.Fatal(state.Code)
	}
	for _, private := range []string{password, code.Secret, "registration_token", "private_key", "secret_hash"} {
		if strings.Contains(state.Body.String(), private) {
			t.Fatal("management snapshot leaked secrets")
		}
	}
	for _, path := range []string{"/", "/admin.js", "/admin.css"} {
		w := adminRequest(h, "GET", path, nil, nil, "", "")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("unsafe admin asset", path)
		}
	}
	public := httptest.NewRecorder()
	s.Handler().ServeHTTP(public, httptest.NewRequest("GET", "http://example.test/api/state", nil))
	if public.Code != 404 {
		t.Fatal("admin API leaked onto public listener")
	}
	r := httptest.NewRequest("GET", "http://evil.example/api/state", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("Host rebinding accepted")
	}
	adminRequest(h, "POST", "/api/logout", map[string]string{}, cookie, csrf, managementOrigin)
	if w := adminRequest(h, "GET", "/api/state", nil, cookie, "", ""); w.Code != 401 {
		t.Fatal("logged-out cookie accepted")
	}
}
func TestAdminLoginRateLimitAndSessionExpiry(t *testing.T) {
	_, _, h, password := managedFixture(t)
	cookie, _ := loginAdmin(t, h, password)
	a := h.(*adminHandler)
	a.mu.Lock()
	session := a.sessions[cookie.Value]
	session.expires = time.Now().Add(-time.Second)
	a.sessions[cookie.Value] = session
	a.mu.Unlock()
	if w := adminRequest(h, "GET", "/api/session", nil, cookie, "", ""); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
	for i := 0; i < 10; i++ {
		adminRequest(h, "POST", "/api/login", map[string]string{"password": "wrong"}, nil, "", managementOrigin)
	}
	if w := adminRequest(h, "POST", "/api/login", map[string]string{"password": password}, nil, "", managementOrigin); w.Code != 429 {
		t.Fatal("unlimited login attempts")
	}
}
func registerManaged(t *testing.T, s *Server, id, token string) (*nativeconn.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, err := nativeconn.Dial(ctx, "tcp://"+s.TCPAddress(), nativeconn.RoleRelay)
	if err != nil {
		t.Fatal(err)
	}
	hello := tcpControl(t, ctx, conn)
	nonce, _ := base64.RawURLEncoding.DecodeString(hello.Nonce)
	if err = writeJSON(ctx, conn, gatewaywire.RegisterHello{Type: gatewaywire.TypeRegister, Proto: 1, RelayID: id, RegistrationProof: base64.RawURLEncoding.EncodeToString(gatewaywire.RegistrationProof(token, id, nonce))}); err != nil {
		t.Fatal(err)
	}
	reply := tcpControl(t, ctx, conn)
	if reply.Type != gatewaywire.TypeReady {
		conn.CloseNow()
		return nil, reply.Code
	}
	return conn, ""
}
func TestManagedEnrollmentPinnedTLSIdentityRecoveryDisableAndRevoke(t *testing.T) {
	s, store, h, password := managedFixture(t)
	cookie, csrf := loginAdmin(t, h, password)
	response := adminRequest(h, "POST", "/api/invites", map[string]string{"name": "工作站"}, cookie, csrf, managementOrigin)
	var invitation struct {
		Code string `json:"code"`
	}
	json.Unmarshal(response.Body.Bytes(), &invitation)
	runtime := t.TempDir()
	os.Chmod(runtime, 0700)
	key := strings.Repeat("N", 32)
	// A code with a different pin cannot send its secret to an impostor server.
	code, _ := gatewayenroll.Parse(invitation.Code, time.Now())
	bad := code
	bad.Pin = strings.Repeat("0", 64)
	if _, err := gatewayenroll.Redeem(t.Context(), bad, testID(t, key), gatewayenroll.RandomSecret()); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
	saved, err := gatewayenroll.Join(t.Context(), invitation.Code, runtime, key)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RelayKey != key || saved.RelayID != testID(t, key) {
		t.Fatal("Relay identity changed")
	}
	// Retry before first registration recovers exactly the same credential from pending state.
	id := saved.RelayID
	oldEpoch, ok := store.Verify(id, "", bytes.Repeat([]byte{1}, 32), gatewaywire.RegistrationProof(saved.RegistrationToken, id, bytes.Repeat([]byte{1}, 32)))
	if !ok {
		t.Fatal("credential not installed")
	}
	conn, reason := registerManaged(t, s, id, saved.RegistrationToken)
	if reason != "" {
		t.Fatal(reason)
	}
	defer conn.CloseNow()
	if _, err = gatewayenroll.Redeem(t.Context(), code, id, gatewayenroll.RandomSecret()); err == nil {
		t.Fatal("confirmed invitation reused")
	}
	if c, reason := registerManaged(t, s, id, strings.Repeat("L", 32)); reason != gatewaywire.CodeUnauthorized {
		if c != nil {
			c.CloseNow()
		}
		t.Fatal("legacy secret admitted managed computer")
	}
	for _, action := range []string{"disable", "enable", "revoke"} {
		w := adminRequest(h, "POST", "/api/computers", map[string]string{"id": id, "action": action}, cookie, csrf, managementOrigin)
		if w.Code != 200 {
			t.Fatal(action, w.Body.String())
		}
		if action == "disable" {
			if s.lookupRelay(id) != nil {
				t.Fatal("disabled link survived")
			}
			if _, reason := s.registerRelay(t.Context(), id, nil, oldEpoch); reason != gatewaywire.CodeUnauthorized {
				t.Fatal("late registration raced disable")
			}
		}
		c, reason := registerManaged(t, s, id, saved.RegistrationToken)
		if action == "enable" {
			if reason != "" {
				t.Fatal("valid restore refused", reason)
			}
			c.CloseNow()
		} else if reason != gatewaywire.CodeUnauthorized {
			if c != nil {
				c.CloseNow()
			}
			t.Fatal("revoked/disabled credential registered")
		}
	}
	limits := adminRequest(h, "POST", "/api/limits", gatewayaccess.Limits{MaxRelays: 7, MaxClients: 15, MaxClientsPerRelay: 3}, cookie, csrf, managementOrigin)
	if limits.Code != 200 || s.limits().MaxRelays != 7 {
		t.Fatal("dynamic limits not applied")
	}
	s.quota.add(id, 123, time.Now())
	snapshot := s.managementSnapshot().(map[string]any)
	rows := snapshot["computers"].([]managedComputer)
	for _, row := range rows {
		if row.ID == id && row.Bytes != 123 {
			t.Fatal("unlimited mode lost traffic accounting")
		}
	}
}
func testID(t *testing.T, key string) string {
	t.Helper()
	id, err := gatewaywire.DeriveRelayID(key)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestManagedMigrationKeepsLegacyComputerUntilExplicitEnrollment(t *testing.T) {
	s, store, _, _ := managedFixture(t)
	id := testID(t, strings.Repeat("Z", 32))
	legacy, code := registerManaged(t, s, id, strings.Repeat("L", 32))
	if code != "" {
		t.Fatal("existing computer disconnected on migration", code)
	}
	defer legacy.CloseNow()
	invite, secret, err := store.Invite("旧电脑", id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw := gatewayenroll.Encode(gatewayenroll.Code{Endpoint: "tcp://" + s.TCPAddress(), ID: invite.ID, Secret: secret, Pin: store.Fingerprint(), ExpiresAt: invite.ExpiresAt})
	runtime := t.TempDir()
	os.Chmod(runtime, 0700)
	saved, err := gatewayenroll.Join(t.Context(), raw, runtime, strings.Repeat("Z", 32))
	if err != nil {
		t.Fatal(err)
	}
	if saved.RelayID != id {
		t.Fatal("migration changed routing identity")
	}
	conn, reason := registerManaged(t, s, id, saved.RegistrationToken)
	if reason != "" {
		t.Fatal(reason)
	}
	defer conn.CloseNow()
}
func TestEnrollmentMalformedCodeAndNonTLSClientDoNotConsumeInvite(t *testing.T) {
	s, store, _, _ := managedFixture(t)
	i, key, _ := store.Invite("一次", "", time.Now())
	code := gatewayenroll.Encode(gatewayenroll.Code{Endpoint: "tcp://" + s.TCPAddress(), ID: i.ID, Secret: key, Pin: store.Fingerprint(), ExpiresAt: i.ExpiresAt})
	for _, bad := range []string{strings.Replace(code, "herdr-gateway", "http", 1), code + "&secret=duplicate", strings.Replace(code, "expires=", "unknown=", 1)} {
		if _, err := gatewayenroll.Parse(bad, time.Now()); err == nil {
			t.Fatal("malformed code accepted")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	c, err := nativeconn.Dial(ctx, "tcp://"+s.TCPAddress(), nativeconn.RoleEnroll)
	if err != nil {
		t.Fatal(err)
	}
	c.Write(ctx, websocket.MessageText, []byte(`{"secret":"not-tls"}`))
	c.CloseNow()
	if store.Snapshot().Invites[i.ID].UsedBy != "" {
		t.Fatal("unauthenticated exchange consumed invite")
	}
}

func TestDisableClosesOnlySelectedRelayAndItsClients(t *testing.T) {
	s, store, h, password := managedFixture(t)
	cookie, csrf := loginAdmin(t, h, password)
	enroll := func(key string) (string, string) {
		id := testID(t, strings.Repeat(key, 32))
		i, k, e := store.Invite("电脑 "+key, "", time.Now())
		if e != nil {
			t.Fatal(e)
		}
		token, e := store.Redeem(i.ID, k, id, strings.Repeat("r", 43), time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return id, token
	}
	a, at := enroll("A")
	b, bt := enroll("B")
	ar, reason := registerManaged(t, s, a, at)
	if reason != "" {
		t.Fatal(reason)
	}
	defer ar.CloseNow()
	br, reason := registerManaged(t, s, b, bt)
	if reason != "" {
		t.Fatal(reason)
	}
	defer br.CloseNow()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	phone, err := nativeconn.Dial(ctx, "tcp://"+s.TCPAddress(), nativeconn.RolePhone)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.CloseNow()
	tcpControl(t, ctx, phone)
	// Gateway routes this opaque proof; the real Relay is the authority for it.
	writeJSON(ctx, phone, gatewaywire.ConnectHello{Type: gatewaywire.TypeConnect, Proto: 1, RelayID: a, Proof: base64.RawURLEncoding.EncodeToString(make([]byte, 32))})
	if tcpControl(t, ctx, phone).Type != gatewaywire.TypeReady {
		t.Fatal("phone not routed")
	}
	if _, _, err = ar.Read(ctx); err != nil {
		t.Fatal("missing open frame", err)
	}
	response := adminRequest(h, "POST", "/api/computers", map[string]string{"id": a, "action": "disable"}, cookie, csrf, managementOrigin)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if s.lookupRelay(a) != nil || s.lookupRelay(b) == nil {
		t.Fatal("disable crossed computer scope")
	}
	if _, _, err = phone.Read(ctx); err == nil {
		t.Fatal("disabled computer kept phone connection")
	}
	if relays, clients := s.counts(); relays != 1 || clients != 0 {
		t.Fatalf("relay/client counts = %d/%d", relays, clients)
	}
}
func TestManagedLimitsApplyToNewRegistrationsWithoutEviction(t *testing.T) {
	s, store, _, _ := managedFixture(t)
	now := time.Now()
	id1 := testID(t, strings.Repeat("1", 32))
	id2 := testID(t, strings.Repeat("2", 32))
	enroll := func(id string) string {
		i, k, _ := store.Invite("电脑", "", now)
		token, err := store.Redeem(i.ID, k, id, strings.Repeat("r", 43), now)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token1, token2 := enroll(id1), enroll(id2)
	c, reason := registerManaged(t, s, id1, token1)
	if reason != "" {
		t.Fatal(reason)
	}
	defer c.CloseNow()
	if err := store.SetLimits(gatewayaccess.Limits{MaxRelays: 1, MaxClients: 2, MaxClientsPerRelay: 1}, now); err != nil {
		t.Fatal(err)
	}
	if rejected, reason := registerManaged(t, s, id2, token2); reason != gatewaywire.CodeAtCapacity {
		if rejected != nil {
			rejected.CloseNow()
		}
		t.Fatal("new registration bypassed live limit", reason)
	}
	if s.lookupRelay(id1) == nil {
		t.Fatal("limit edit evicted existing computer")
	}
}
