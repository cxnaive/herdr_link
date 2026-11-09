//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/herdr-terminal"
)

// Run only on the isolated session bus created by scripts/smoke-linux.sh.
func TestLinuxSecretServiceRoundTrip(t *testing.T) {
	if os.Getenv("HERDR_LINUX_INTEGRATION") != "1" {
		t.Skip("requires an isolated Linux Secret Service")
	}
	store := mygo.Secrets.Service(fmt.Sprintf("dev.herdr.desktop.linux-test-%d", os.Getpid()))
	want := []byte("synthetic Linux credential, not a pairing secret")
	if err := store.Set("test-key", want); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete("test-key") })
	got, err := store.Get("test-key")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("credential round-trip: %v", err)
	}
	if err := store.Delete("test-key"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("test-key"); !errors.Is(err, mygo.ErrSecretNotFound) {
		t.Fatalf("deleted credential returned: %v", err)
	}
}

func TestLinuxLocalPTYUsesNativeLibraryAndPreservesUnicode(t *testing.T) {
	home := t.TempDir()
	term, err := terminal.New(terminal.Options{
		Command: []string{"/bin/sh", "-c", "printf '\\033[32mLINUX_READY\\033[0m\\n'; IFS= read -r value; printf 'INPUT=%s\\n' \"$value\"; stty size; sleep 1"},
		Dir:     home, Env: []string{"HOME=" + home, "ENV=/dev/null"}, NoClipboardWrite: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !strings.Contains(term.Text(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("PTY output missing %q: %q", want, term.Text())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFor("LINUX_READY")
	term.Resize(100, 42)
	term.Send([]byte("中文\n"))
	waitFor("INPUT=中文")
	waitFor("42 100")
}
