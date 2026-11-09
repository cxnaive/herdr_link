package relay

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Uses a real Relay binary with isolated configuration and no reachable Gateway.
// It never points at a user's Herdr socket or creates/stops a real Agent.
func TestLocalActualRelayEnrollmentReadAndRestartWithoutGateway(t *testing.T) {
	binary := os.Getenv("HERDR_TEST_RELAY_BINARY")
	if binary == "" {
		t.Skip("set HERDR_TEST_RELAY_BINARY to an isolated-test Relay binary")
	}
	if !filepath.IsAbs(binary) || filepath.Base(binary) != "herdr-mobile-relay" {
		t.Fatal("expected absolute Relay binary path")
	}
	home, err := os.MkdirTemp("", "herdr-direct-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	port := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	}
	socket := filepath.Join(home, "c", "herdr-mobile-relay", "tcp-pair", "admin.sock")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var process *exec.Cmd
	stop := func() {
		if process != nil {
			process.Process.Signal(os.Interrupt)
			done := make(chan struct{})
			go func() { process.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				process.Process.Kill()
				<-done
			}
			process = nil
		}
	}
	defer stop()
	start := func() {
		process = exec.Command(binary, "serve")
		process.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, "c"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "XDG_DATA_HOME=" + filepath.Join(home, "data"), "HERDR_BIN=/bin/false", "HERDR_SOCKET_PATH=" + filepath.Join(home, "absent-herdr.sock"), "HERDR_RELAY_TOKEN=" + strings.Repeat("L", 32), "HERDR_GATEWAY_REGISTRATION_TOKEN=" + strings.Repeat("R", 32), "HERDR_RELAY_PORT=" + port(), "HERDR_RELAY_HOST=127.0.0.1", "HERDR_RELAY_PLUGIN_PORT=" + port(), "HERDR_RELAY_INSTANCE_ID=isolated-local-test", "HERDR_GATEWAY_URL=tcp://127.0.0.1:" + port(), "HERDR_WEB_ROOT=" + filepath.Join(home, "absent-web"), "HERDR_REACHABILITY_PORT_MAPPING=false"}
		logFile, err := os.Create(filepath.Join(home, "relay-test.log"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { logFile.Close() })
		process.Stdout = logFile
		process.Stderr = logFile
		if err := process.Start(); err != nil {
			t.Fatal(err)
		}
		for {
			c, _, err := connectLocalAdmin(ctx, socket, inspectRelayProcess)
			if err == nil {
				c.Close()
				break
			}
			select {
			case <-ctx.Done():
				logData, _ := os.ReadFile(filepath.Join(home, "relay-test.log"))
				t.Fatalf("isolated Relay did not start: %s", logData)
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	start()
	found, err := DiscoverLocal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	matched := false
	for _, r := range found {
		matched = matched || r.Socket == socket
	}
	if !matched {
		t.Fatal("running instance not discovered")
	}
	computer, err := PairLocal(ctx, LocalRelay{Socket: socket}, "Linux isolated client")
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately unusable network address: local Connect must ignore the Gateway.
	computer.Address = "192.0.2.1:1"
	saved := computer.Auth
	s, err := Connect(ctx, computer, func(next Auth) error { saved = next; return nil })
	if err != nil {
		t.Fatal(err)
	}
	verify := func(s *Session) {
		t.Helper()
		reply, err := s.Request(ctx, Message{"type": "device_list"})
		if err != nil {
			t.Fatal(err)
		}
		rows, ok := reply["devices"].([]any)
		if !ok || len(rows) != 1 {
			t.Fatal("expected one separately enrolled local device")
		}
		if Text(Object(rows[0]), "device_id") != saved.DeviceID {
			t.Fatal("different device identity")
		}
	}
	verify(s)
	s.Close()
	wrong := computer
	wrong.Auth = saved
	wrong.Auth.Secret = encode(make([]byte, 32))
	if invalid, err := Connect(ctx, wrong, func(Auth) error { t.Fatal("invalid credential was automatically replaced"); return nil }); err == nil {
		invalid.Close()
		t.Fatal("wrong credential was accepted")
	}
	if saved.Kind != "credential" || saved.Role != "controller" {
		t.Fatal("credential was not confirmed")
	}
	// Same stored credential, new PID, new loopback port. No new invitation.
	computer.Auth = saved
	stop()
	start()
	s, err = Connect(ctx, computer, func(next Auth) error {
		if next != saved {
			t.Error("credential changed after restart")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	verify(s)
}
