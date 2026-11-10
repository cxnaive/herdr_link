//go:build windows

package windows_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/0cv/herdr-mobile-relay/internal/childenv"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowsInstallerPreservesConfiguration(t *testing.T) {
	if os.Getenv("HERDR_WINDOWS_INTEGRATION") != "1" {
		t.Skip("requires pinned Herdr on Windows CI")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	runtime := filepath.Join(dir, "relay-state")
	t.Setenv("PATH", filepath.Dir(os.Getenv("HERDR_WINDOWS_TEST_BIN"))+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APPDATA", filepath.Join(dir, "roaming"))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	run := func() {
		t.Helper()
		out, e := childenv.CommandContext(ctx, "pwsh.exe", "-NoProfile", "-NonInteractive", "-File", filepath.Join(root, "deploy", "windows-relay", "install.ps1"), "-RuntimeDirectory", runtime).CombinedOutput()
		if e != nil {
			t.Fatalf("installer: %v %s", e, out)
		}
	}
	run()
	info, err := os.Lstat(runtime)
	if err != nil || !privatepath.Private(runtime, info) {
		t.Fatal("installer did not protect configuration")
	}
	path := filepath.Join(runtime, "windows-relay.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if json.Unmarshal(data, &config) != nil || config["herdr_bin"] == "" {
		t.Fatal("invalid generated config")
	}
	config["port"] = 6389
	b, _ := json.Marshal(config)
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(runtime, "gateway-access.json")
	if err = os.WriteFile(marker, []byte("preserve existing identity"), 0600); err != nil {
		t.Fatal(err)
	}
	run()
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, b) {
		t.Fatal("reinstall overwrote configuration")
	}
	actual, _ = os.ReadFile(marker)
	if string(actual) != "preserve existing identity" {
		t.Fatal("reinstall overwrote identity")
	}
}
