//go:build windows

package windows_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/childenv"
	"github.com/0cv/herdr-mobile-relay/internal/gatewayaccess"
	"github.com/0cv/herdr-mobile-relay/internal/gatewayenroll"
	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/localpair"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
)

func TestNativeWindowsHerdrRelayGatewayAndClient(t *testing.T) {
	if os.Getenv("HERDR_WINDOWS_INTEGRATION") != "1" {
		t.Skip("requires isolated native Windows CI")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	herdrBin := os.Getenv("HERDR_WINDOWS_TEST_BIN")
	relayBin := filepath.Join(root, "dist", "windows", "herdr-mobile-relay.exe")
	gatewayBin := filepath.Join(root, "dist", "windows", "herdr-gateway.exe")
	for _, p := range []string{herdrBin, relayBin, gatewayBin} {
		if !filepath.IsAbs(p) {
			t.Fatal("test requires absolute executable paths")
		}
	}
	dir := t.TempDir()
	if err = privatepath.Protect(dir, 0700); err != nil {
		t.Fatal(err)
	}
	configHome := filepath.Join(dir, "config")
	herdrDir := filepath.Join(configHome, "herdr")
	runtimeDir := filepath.Join(configHome, "herdr-mobile-relay")
	for _, d := range []string{herdrDir, runtimeDir, filepath.Join(dir, "home")} {
		if err = os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// No real user profile, sockets, agents, gateway or credentials are touched.
	for key, value := range map[string]string{"HOME": filepath.Join(dir, "home"), "USERPROFILE": filepath.Join(dir, "home"), "XDG_CONFIG_HOME": configHome, "XDG_CACHE_HOME": filepath.Join(dir, "cache"), "XDG_DATA_HOME": filepath.Join(dir, "data"), "HERDR_BIN": herdrBin, "HERDR_SOCKET_PATH": filepath.Join(herdrDir, "herdr.sock"), "HERDR_RELAY_ENV": filepath.Join(runtimeDir, "relay.env"), "HERDR_RELAY_HOST": "127.0.0.1", "HERDR_REACHABILITY_PORT_MAPPING": "false", "HERDR_WEB_ROOT": filepath.Join(root, "web"), "HERDR_RELAY_TOKEN": "", "HERDR_GATEWAY_REGISTRATION_TOKEN": "", "HERDR_GATEWAY_URL": "", "HERDR_SESSION": ""} {
		t.Setenv(key, value)
	}
	cfg := "onboarding = false\n[terminal]\ndefault_shell = 'cmd.exe'\n[update]\nversion_check = false\nmanifest_check = false\n"
	if err = os.WriteFile(filepath.Join(herdrDir, "config.toml"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	logPaths := []string{}
	defer func() {
		if !t.Failed() {
			return
		}
		for _, path := range logPaths {
			b, e := os.ReadFile(path)
			if e == nil {
				if len(b) > 12000 {
					b = b[len(b)-12000:]
				}
				t.Logf("isolated %s: %s", filepath.Base(path), b)
			}
		}
	}()
	start := func(label, bin string, args ...string) func() {
		t.Helper()
		logPaths = append(logPaths, filepath.Join(dir, label+".log"))
		log, err := os.Create(filepath.Join(dir, label+".log"))
		if err != nil {
			t.Fatal(err)
		}
		cmd := childenv.Command(bin, args...)
		cmd.Stdout = log
		cmd.Stderr = log
		if err = cmd.Start(); err != nil {
			log.Close()
			t.Fatal(label, err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				log.Close()
			}
		}
		t.Cleanup(stop)
		return stop
	}
	wait := func(label string, f func() bool) {
		t.Helper()
		until := time.Now().Add(25 * time.Second)
		for time.Now().Before(until) && ctx.Err() == nil {
			if f() {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal(label + " did not become ready")
	}
	start("herdr", herdrBin, "server")
	t.Cleanup(func() {
		stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = childenv.CommandContext(stopCtx, herdrBin, "server", "stop").Run()
	})
	client := herdr.NewClient(herdrBin, os.Getenv("HERDR_SOCKET_PATH"))
	defer client.Close()
	wait("Herdr named pipe", func() bool {
		c, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		_, e := client.PaneList(c)
		return e == nil
	})
	events := herdr.NewEventClient(os.Getenv("HERDR_SOCKET_PATH"))
	stream, _, _, err := events.Bootstrap(ctx)
	if err != nil {
		t.Fatal("event stream", err)
	}
	defer stream.Close()
	created, err := client.WorkspaceCreate(ctx, dir, "windows-smoke")
	if err != nil {
		t.Fatal("workspace creation", err)
	}
	eventCtx, eventCancel := context.WithTimeout(ctx, 10*time.Second)
	defer eventCancel()
	for {
		ev, e := stream.Next(eventCtx)
		if e != nil {
			t.Fatal("no topology event", e)
		}
		if ev.Event == "pane.created" || ev.Event == "workspace.created" {
			break
		}
	}
	pane, err := client.PaneGet(ctx, created.PaneID)
	if err != nil || pane.TerminalID == "" {
		t.Fatal("terminal identity unavailable", err)
	}
	if _, err = client.PaneProcessInfoFresh(ctx, pane.ID); err != nil {
		t.Fatal("native process info", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := childenv.CommandContext(ctx, herdrBin, args...)
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("Herdr operation failed: %v %s", e, out)
		}
	}
	run("pane", "report-agent", pane.ID, "--source", "relay-ci", "--agent", "codex", "--state", "idle", "--agent-session-id", "00000000-0000-0000-0000-000000000001")
	management := filepath.Join(dir, "management")
	if err = os.Mkdir(management, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = gatewayaccess.Init(management); err != nil {
		t.Fatal(err)
	}
	store, err := gatewayaccess.Open(management)
	if err != nil {
		t.Fatal(err)
	}
	invite, secret, err := store.Invite("Windows CI", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "tcp://127.0.0.1:" + freePort(t)
	code := gatewayenroll.Encode(gatewayenroll.Code{Endpoint: endpoint, ID: invite.ID, Secret: secret, Pin: store.Fingerprint(), ExpiresAt: invite.ExpiresAt})
	store.Close()
	t.Setenv("HERDR_GATEWAY_TCP_ADDR", strings.TrimPrefix(endpoint, "tcp://"))
	t.Setenv("HERDR_GATEWAY_ADDR", "127.0.0.1:"+freePort(t))
	t.Setenv("HERDR_GATEWAY_STUN_ADDR", "")
	t.Setenv("HERDR_GATEWAY_ADMIN_STATE", management)
	t.Setenv("HERDR_GATEWAY_ADMIN_ADDR", "127.0.0.1:"+freePort(t))
	t.Setenv("HERDR_GATEWAY_PUBLIC_ENDPOINT", endpoint)
	start("gateway", gatewayBin)
	wait("Gateway", func() bool { return healthy("http://"+os.Getenv("HERDR_GATEWAY_ADDR"), nil) })
	join := childenv.CommandContext(ctx, relayBin, "gateway-join")
	join.Stdin = strings.NewReader(code + "\n")
	if out, e := join.CombinedOutput(); e != nil {
		t.Fatalf("Gateway join failed: %v %s", e, out)
	}
	saved, err := gatewayenroll.LoadLocal(runtimeDir)
	if err != nil {
		t.Fatal("saved Windows gateway credential", err)
	}
	if saved.Endpoint != endpoint {
		t.Fatal("joined wrong endpoint")
	}
	port := freePort(t)
	t.Setenv("HERDR_RELAY_PORT", port)
	t.Setenv("HERDR_RELAY_PLUGIN_PORT", freePort(t))
	startRelay := func() func() {
		stop := start("relay", relayBin, "serve")
		wait("Relay registration", func() bool {
			return healthy("http://127.0.0.1:"+port, func(h map[string]any) bool { g, _ := h["gateway"].(map[string]any); return g["registered"] == true })
		})
		return stop
	}
	stopRelay := startRelay()
	pair, err := localpair.Create(ctx, runtimeDir, "Windows CI client")
	if err != nil {
		t.Fatal("private pipe pairing", err)
	}
	fixture := filepath.Join(dir, "client-fixture.json")
	authFile := filepath.Join(dir, "client-auth.json")
	verify := func(phase string) {
		t.Helper()
		data, _ := json.Marshal(map[string]any{"invitation": pair.Link, "pane": pane.ID, "terminal": pane.TerminalID, "cwd": dir, "phase": phase, "auth_file": authFile})
		if e := os.WriteFile(fixture, data, 0600); e != nil {
			t.Fatal(e)
		}
		cmd := childenv.CommandContext(ctx, "go", "test", "-count=1", "-timeout=60s", "./internal/relay", "-run", "^TestWindowsRelayEndToEnd$")
		cmd.Dir = filepath.Join(root, "desktop")
		cmd.Env = append(cmd.Env, "HERDR_WINDOWS_CLIENT_FIXTURE="+fixture)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("encrypted Windows client phase %s: %v\n%s", phase, e, out)
		}
		t.Log("passed encrypted client phase:", phase)
	}
	verify("agent")
	run("pane", "release-agent", pane.ID, "--source", "relay-ci", "--agent", "codex")
	verify("shell")
	stopRelay()
	stopRelay = startRelay()
	defer stopRelay()
	verify("reconnect")
	current, err := gatewayenroll.LoadLocal(runtimeDir)
	if err != nil || current != saved {
		t.Fatal("restart changed Gateway identity")
	}
	if err = client.StopPane(ctx, pane.ID); err != nil {
		t.Fatal("close retained panel", err)
	}
	verify("closed")
	run("server", "stop")
}

func freePort(t *testing.T) string {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}
func healthy(base string, check func(map[string]any) bool) bool {
	c := http.Client{Timeout: time.Second}
	r, e := c.Get(base + "/healthz")
	if e != nil {
		return false
	}
	defer r.Body.Close()
	var value map[string]any
	if json.NewDecoder(io.LimitReader(r.Body, 65536)).Decode(&value) != nil || r.StatusCode != 200 {
		return false
	}
	return check == nil || check(value)
}
