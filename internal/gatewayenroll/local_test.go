package gatewayenroll

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
)

func TestCredentialFilePrivateAtomicAndIdentityValidation(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	key := strings.Repeat("k", 32)
	id, _ := gatewaywire.DeriveRelayID(key)
	record := LocalAccess{1, "tcp://127.0.0.1:8377", id, key, RandomSecret()}
	path := filepath.Join(dir, LocalFilename)
	if err := writePrivate(path, record); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLocal(dir)
	if err != nil || got != record {
		t.Fatal("credential round trip", err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("public credential file")
	}
	record.RelayID = "wrong"
	writePrivate(path, record)
	if _, err = LoadLocal(dir); err == nil {
		t.Fatal("wrong identity accepted")
	}
	os.Chmod(path, 0644)
	if _, err = LoadLocal(dir); err == nil {
		t.Fatal("public file read")
	}
	if err = writePrivate(path, record); err == nil {
		t.Fatal("public file overwritten")
	}
	os.Remove(path)
	target := filepath.Join(dir, "target")
	os.WriteFile(target, []byte("untouched"), 0600)
	os.Symlink(target, path)
	if _, err = LoadLocal(dir); err == nil {
		t.Fatal("symlink followed")
	}
	if err = writePrivate(path, record); err == nil {
		t.Fatal("symlink overwritten")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "untouched" {
		t.Fatal("target changed")
	}
}
func TestJoinFailurePreservesOldConfigurationAndRecoveryIdentity(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	key := strings.Repeat("k", 32)
	id, _ := gatewaywire.DeriveRelayID(key)
	record := LocalAccess{1, "tcp://127.0.0.1:8377", id, key, RandomSecret()}
	writePrivate(filepath.Join(dir, LocalFilename), record)
	before, _ := os.ReadFile(filepath.Join(dir, LocalFilename))
	code := Code{Endpoint: "tcp://127.0.0.1:1", ID: "abcdefghijklmnopqrstuvwx", Secret: RandomSecret(), Pin: strings.Repeat("1", 64), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Join(ctx, Encode(code), dir, key); err == nil {
		t.Fatal("cancelled join succeeded")
	}
	after, _ := os.ReadFile(filepath.Join(dir, LocalFilename))
	if string(after) != string(before) {
		t.Fatal("failed enrollment overwrote working credential")
	}
	var pending pendingJoin
	if err := readJSONFile(filepath.Join(dir, "gateway-join-pending.json"), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.RelayKey != key || pending.CodeID != code.ID {
		t.Fatal("pending identity lost")
	}
	if _, err := Join(ctx, Encode(code), dir, strings.Repeat("x", 32)); err == nil {
		t.Fatal("replaced existing identity")
	}
	next := pending
	Join(ctx, Encode(code), dir, key)
	readJSONFile(filepath.Join(dir, "gateway-join-pending.json"), &next)
	if pending != next {
		t.Fatal("retries use new request IDs")
	}
	private, _ := json.Marshal(pending)
	if strings.Contains(string(private), code.Secret) {
		t.Fatal("pending recovery stores enrollment secret")
	}
}

func TestConfiguredIdentityNeverCreatesNewKeyForExistingDeviceData(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	if key, err := configuredIdentity(dir); err != nil || key != "" {
		t.Fatal("fresh directory not accepted")
	}
	os.Mkdir(filepath.Join(dir, "device-auth"), 0700)
	if _, err := configuredIdentity(dir); err == nil {
		t.Fatal("existing phone pairing would get another identity")
	}
	key := strings.Repeat("k", 32)
	path := filepath.Join(dir, "relay.env")
	os.WriteFile(path, []byte("# config\nHERDR_RELAY_TOKEN='"+key+"'\n"), 0600)
	if got, err := configuredIdentity(dir); err != nil || got != key {
		t.Fatal("literal key not preserved", err)
	}
	for _, raw := range []string{"HERDR_RELAY_TOKEN=$(printf secret)\n", "HERDR_RELAY_TOKEN=" + key + "\nHERDR_RELAY_TOKEN=" + key, "NOT_A_KEY=other"} {
		os.WriteFile(path, []byte(raw), 0600)
		if _, err := configuredIdentity(dir); err == nil {
			t.Fatal("ambiguous or executable config evaluated")
		}
	}
}
