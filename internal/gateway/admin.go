package gateway

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/gatewayaccess"
	"github.com/0cv/herdr-mobile-relay/internal/gatewayenroll"
	"github.com/0cv/herdr-mobile-relay/internal/nativeconn"
)

//go:embed adminweb/*
var adminFiles embed.FS

const adminCookie = "herdr_gateway_admin"

type adminSession struct {
	csrf    string
	expires time.Time
}
type adminHandler struct {
	server                 *Server
	origin, host, endpoint string
	secure                 bool
	mu                     sync.Mutex
	sessions               map[string]adminSession
	loginLimit             *rateLimiter
}

func NewAdminHandler(s *Server, origin, endpoint string) (http.Handler, error) {
	if s.opts.Access == nil || s.TCPAddress() == "" {
		return nil, errors.New("gateway management requires an initialized access store and an enabled native TCP listener")
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("admin origin must be one exact HTTPS or loopback HTTP origin")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("remote admin origin requires HTTPS")
		}
	}
	if _, err = nativeconn.Endpoint(endpoint); err != nil {
		return nil, errors.New("set the public native TCP gateway endpoint for enrollment codes")
	}
	a := &adminHandler{server: s, origin: origin, host: u.Host, endpoint: endpoint, secure: u.Scheme == "https", sessions: map[string]adminSession{}, loginLimit: newRateLimiter(10, time.Minute)}
	return a, nil
}
func adminSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func adminJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func adminError(w http.ResponseWriter, status int, message string) {
	adminJSON(w, status, map[string]string{"error": message})
}
func adminBody(w http.ResponseWriter, r *http.Request, v any) bool {
	mediaType, _, contentErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if contentErr != nil || mediaType != "application/json" {
		adminError(w, 415, "需要 JSON 请求")
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		adminError(w, 400, "请求格式无效")
		return false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		adminError(w, 400, "请求格式无效")
		return false
	}
	return true
}
func (a *adminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	if a.server.stopped() {
		adminError(w, 503, "Gateway 正在停止")
		return
	}
	if r.Host != a.host {
		adminError(w, 403, "管理地址不匹配")
		return
	}
	if r.Method != "GET" && (r.Header.Get("Origin") != a.origin || r.Header.Get("Sec-Fetch-Site") == "cross-site") {
		adminError(w, 403, "拒绝跨站管理请求")
		return
	}
	if r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "/admin.js" || r.URL.Path == "/admin.css") {
		file := "index.html"
		kind := "text/html; charset=utf-8"
		if r.URL.Path != "/" {
			file = strings.TrimPrefix(r.URL.Path, "/")
			kind = "text/javascript; charset=utf-8"
			if file == "admin.css" {
				kind = "text/css; charset=utf-8"
			}
		}
		b, err := adminFiles.ReadFile("adminweb/" + file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", kind)
		_, _ = w.Write(b)
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/login" {
		a.login(w, r)
		return
	}
	cookie, err := r.Cookie(adminCookie)
	if err != nil {
		adminError(w, 401, "请先登录")
		return
	}
	a.mu.Lock()
	session, ok := a.sessions[cookie.Value]
	if ok && !time.Now().Before(session.expires) {
		delete(a.sessions, cookie.Value)
		ok = false
	}
	a.mu.Unlock()
	if !ok {
		adminError(w, 401, "登录已过期，请重新登录")
		return
	}
	if r.Method != "GET" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.csrf)) != 1 {
		adminError(w, 403, "请求校验失败，请刷新页面")
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/session" {
		adminJSON(w, 200, map[string]string{"csrf": session.csrf})
		return
	}
	if r.Method == "GET" && r.URL.Path == "/api/state" {
		adminJSON(w, 200, a.server.managementSnapshot())
		return
	}
	if r.Method == "POST" && r.URL.Path == "/api/logout" {
		a.mu.Lock()
		delete(a.sessions, cookie.Value)
		a.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.secure})
		adminJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != "POST" {
		adminError(w, 404, "接口不存在")
		return
	}
	a.server.adminOperations.Lock()
	defer a.server.adminOperations.Unlock()
	switch r.URL.Path {
	case "/api/invites":
		var input struct {
			Name   string `json:"name"`
			Target string `json:"target"`
		}
		if !adminBody(w, r, &input) {
			return
		}
		invite, key, err := a.server.opts.Access.Invite(input.Name, input.Target, a.server.now())
		if err != nil {
			adminError(w, 400, "无法创建接入码，请检查名称、电脑或待接入数量")
			return
		}
		code := gatewayenroll.Encode(gatewayenroll.Code{Endpoint: a.endpoint, ID: invite.ID, Secret: key, Pin: a.server.opts.Access.Fingerprint(), ExpiresAt: invite.ExpiresAt})
		adminJSON(w, 201, map[string]any{"invite": invite, "code": code})
	case "/api/invites/cancel":
		var input struct {
			ID string `json:"id"`
		}
		if !adminBody(w, r, &input) {
			return
		}
		pending := a.server.opts.Access.Snapshot().Invites[input.ID]
		if a.server.opts.Access.CancelInvite(input.ID, a.server.now()) != nil {
			adminError(w, 400, "接入码不存在或已消费")
			return
		}
		if pending.UsedBy != "" {
			a.server.disconnectStale(pending.UsedBy)
		}
		adminJSON(w, 200, map[string]bool{"ok": true})
	case "/api/computers":
		var input struct {
			ID     string `json:"id"`
			Action string `json:"action"`
			Name   string `json:"name"`
		}
		if !adminBody(w, r, &input) {
			return
		}
		if a.server.opts.Access.Update(input.ID, input.Action, input.Name, a.server.now()) != nil {
			adminError(w, 400, "操作失败；已撤销的电脑需要重新接入，不能直接恢复")
			return
		}
		a.server.disconnectStale(input.ID)
		adminJSON(w, 200, map[string]bool{"ok": true})
	case "/api/limits":
		var limits gatewayaccess.Limits
		if !adminBody(w, r, &limits) {
			return
		}
		if a.server.opts.Access.SetLimits(limits, a.server.now()) != nil {
			adminError(w, 400, "数量限制无效")
			return
		}
		adminJSON(w, 200, map[string]bool{"ok": true})
	default:
		adminError(w, 404, "接口不存在")
	}
}
func (a *adminHandler) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.loginLimit.allow(ip, time.Now()) {
		adminError(w, 429, "登录尝试过多，请稍后重试")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !adminBody(w, r, &input) {
		return
	}
	if !a.server.opts.Access.Login(input.Password) {
		adminError(w, 401, "管理员密钥或密码无效")
		return
	}
	token := adminSecret()
	session := adminSession{csrf: adminSecret(), expires: time.Now().Add(8 * time.Hour)}
	a.mu.Lock()
	for id, old := range a.sessions {
		if !time.Now().Before(old.expires) {
			delete(a.sessions, id)
		}
	}
	if len(a.sessions) >= 8 {
		a.mu.Unlock()
		adminError(w, 429, "管理员会话已达上限，请先退出其他会话")
		return
	}
	if previous, err := r.Cookie(adminCookie); err == nil {
		delete(a.sessions, previous.Value)
	}
	a.sessions[token] = session
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Value: token, Path: "/", MaxAge: 8 * 60 * 60, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.secure})
	adminJSON(w, 200, map[string]string{"csrf": session.csrf})
}
