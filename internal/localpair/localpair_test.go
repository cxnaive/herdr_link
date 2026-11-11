package localpair

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/deviceauth"
)

func TestPrivateSocketCreatesInvitationAndCleansUp(t *testing.T) {
	runtime := t.TempDir()
	var calls atomic.Int32
	stop, err := Listen(runtime, "127.0.0.1:8375", func(name string) (Response, error) {
		calls.Add(1)
		return Response{Link: "herdr-tcp://127.0.0.1:8377/#test=" + name}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	info, err := os.Stat(socketPath(runtime))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("socket permissions")
	}
	response, err := Create(t.Context(), runtime, "phone")
	if err != nil || response.Link != "herdr-tcp://127.0.0.1:8377/#test=phone" {
		t.Fatalf("create: %v", err)
	}
	if other, err := Listen(runtime, "127.0.0.1:8375", func(string) (Response, error) { return Response{}, nil }); err == nil {
		other()
		t.Fatal("replaced active socket")
	}
	c, err := net.Dial("unix", socketPath(runtime))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte(`{"action":"delete","name":"x"}`))
	_ = c.Close()
	stop()
	if calls.Load() != 1 {
		t.Fatal("invalid method created invitation")
	}
	if _, err := os.Lstat(socketPath(runtime)); !os.IsNotExist(err) {
		t.Fatal("socket not cleaned up")
	}
}
func TestPrivateSocketRefusesUnsafePaths(t *testing.T) {
	for _, kind := range []string{"symlink", "public-directory", "regular-file"} {
		t.Run(kind, func(t *testing.T) {
			runtime := t.TempDir()
			dir := filepath.Dir(socketPath(runtime))
			switch kind {
			case "symlink":
				_ = os.Symlink(t.TempDir(), dir)
			case "public-directory":
				_ = os.Mkdir(dir, 0755)
			case "regular-file":
				_ = os.Mkdir(dir, 0700)
				_ = os.WriteFile(socketPath(runtime), []byte("keep"), 0600)
			}
			if stop, err := Listen(runtime, "127.0.0.1:8375", func(string) (Response, error) { return Response{}, nil }); err == nil {
				stop()
				t.Fatal("unsafe path accepted")
			}
		})
	}
}
func TestInvitationLinkNativeIPv6AndNoMasterKey(t *testing.T) {
	invitation := deviceauth.Invitation{InvitationID: strings.Repeat("i", 16), Version: 1, Secret: strings.Repeat("s", 43), ExpiresAt: time.Now().Add(10 * time.Minute), Name: "手机"}
	link, err := InvitationLink("tcp://[::1]:8377", strings.Repeat("t", 32), invitation)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "herdr-tcp" || u.Host != "[::1]:8377" {
		t.Fatal("bad URI")
	}
	p, err := url.ParseQuery(u.Fragment)
	if err != nil || p.Get("setup") != invitation.Secret || p.Get("invite") != invitation.InvitationID || p.Get("gateways") != "" {
		t.Fatal("bad parameters")
	}
	if strings.Contains(link, strings.Repeat("t", 32)) {
		t.Fatal("master key disclosed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = Create(ctx, t.TempDir(), "x"); err == nil {
		t.Fatal("cancel ignored")
	}
}
