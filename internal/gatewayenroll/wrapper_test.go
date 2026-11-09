package gatewayenroll

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstalledWrapperSupportsEnrollmentAndPreservesLegacyConfiguration(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("POSIX shell required")
	}
	bundle := t.TempDir()
	raw, err := os.ReadFile("../../deploy/native-relay/run.sh")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(bundle, "run.sh")
	os.WriteFile(wrapper, raw, 0700)
	os.WriteFile(filepath.Join(bundle, "herdr-mobile-relay"), []byte("#!/bin/sh\nprintf '%s|%s|%s' \"$1\" \"$HERDR_RELAY_ENV\" \"${HERDR_RELAY_TOKEN:-}\"\n"), 0700)
	runtime := filepath.Join(t.TempDir(), "private")
	os.Mkdir(runtime, 0700)
	envFile := filepath.Join(runtime, "relay.env")
	run := func(command string) (string, error) {
		c := exec.Command(sh, wrapper, command)
		c.Env = []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH"), "HERDR_RELAY_ENV=" + envFile}
		b, e := c.CombinedOutput()
		return string(b), e
	}
	if out, err := run("gateway-join"); err != nil || !strings.HasPrefix(out, "gateway-join|") {
		t.Fatal("fresh enrollment blocked", out, err)
	}
	if _, err := run("serve"); err == nil {
		t.Fatal("unconfigured wrapper started")
	}
	os.WriteFile(filepath.Join(runtime, LocalFilename), []byte("{}"), 0600) // The Go loader, not the wrapper, validates this file.
	if out, err := run("serve"); err != nil || !strings.HasPrefix(out, "serve|") {
		t.Fatal("enrolled wrapper requires obsolete env file", out, err)
	}
	key := strings.Repeat("k", 32)
	os.WriteFile(envFile, []byte("HERDR_RELAY_TOKEN="+key+"\nHERDR_GATEWAY_REGISTRATION_TOKEN=REPLACE_WITH_GATEWAY_REGISTRATION_SECRET\n"), 0600)
	if out, err := run("gateway-join"); err != nil || !strings.HasSuffix(out, key) {
		t.Fatal("existing identity was not loaded", out, err)
	}
	os.Remove(filepath.Join(runtime, LocalFilename))
	if _, err := run("serve"); err == nil {
		t.Fatal("legacy placeholder admitted")
	}
}
