package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0cv/herdr-mobile-relay/internal/gatewayenroll"
	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
)

func TestManagedGatewayCredentialsOverrideOnlyRegistrationNotIdentity(t *testing.T) {
	isolateLoadEnvironment(t)
	runtime := filepath.Join(t.TempDir(), "private")
	os.Mkdir(runtime, 0700)
	t.Setenv("HERDR_RELAY_ENV", filepath.Join(runtime, "relay.env"))
	key := strings.Repeat("k", 32)
	id, _ := gatewaywire.DeriveRelayID(key)
	access := gatewayenroll.LocalAccess{Schema: 1, RelayKey: key, RelayID: id, Endpoint: "tcp://127.0.0.1:8377", RegistrationToken: gatewayenroll.RandomSecret()}
	raw, _ := json.Marshal(access)
	path := filepath.Join(runtime, gatewayenroll.LocalFilename)
	os.WriteFile(path, raw, 0600)
	t.Setenv("HERDR_RELAY_TOKEN", key)
	t.Setenv("HERDR_GATEWAY_REGISTRATION_TOKEN", "obsolete")
	t.Setenv("HERDR_GATEWAY_URL", "tcp://127.0.0.1:9000")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != key || cfg.GatewayRegistrationToken != access.RegistrationToken || cfg.GatewayURL != access.Endpoint || len(cfg.GatewayURLs) != 1 {
		t.Fatal("managed registration not selected")
	}
	t.Setenv("HERDR_RELAY_TOKEN", strings.Repeat("other", 7))
	if _, err = Load(); err == nil {
		t.Fatal("identity collision not rejected")
	}
	t.Setenv("HERDR_RELAY_TOKEN", "")
	cfg, err = Load()
	if err != nil || cfg.Token != key {
		t.Fatal("fresh enrolled Relay cannot start", err)
	}
	os.WriteFile(path, []byte("{broken"), 0600)
	if _, err = Load(); err == nil {
		t.Fatal("corrupt credentials silently fell back to legacy")
	}
}
