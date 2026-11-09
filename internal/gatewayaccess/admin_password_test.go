package gatewayaccess

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCustomAdminPasswordPreservesRegistryAndSurvivesReopen(t *testing.T) {
	s, dir, old := fixture(t)
	now := time.Now()
	id := identity(t, "a")
	i, key, err := s.Invite("existing computer", "", now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Redeem(i.ID, key, id, strings.Repeat("r", 43), now)
	if err != nil {
		t.Fatal(err)
	}
	pin := s.Fingerprint()
	const password = "test-only-admin-password"
	if err := SetAdminPassword(dir, password); err == nil {
		t.Fatal("changed live database")
	}
	s.Close()
	if err := SetAdminPassword(dir, password); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Login(password) || s.Login(old) || s.Login("wrong-password") || s.Fingerprint() != pin {
		t.Fatal("password or gateway identity incorrect")
	}
	if _, ok := verify(t, s, id, token); !ok {
		t.Fatal("computer credentials changed")
	}
	firstHash := s.state.AdminHash
	snapshot, _ := json.Marshal(s.Snapshot())
	if strings.Contains(string(snapshot), firstHash) || strings.Contains(string(snapshot), password) {
		t.Fatal("admin verifier exposed by snapshot")
	}
	s.Close()
	data, err := os.ReadFile(filepath.Join(dir, "access.db"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), password) {
		t.Fatal("plaintext password persisted")
	}
	if err := SetAdminPassword(dir, password); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == s.state.AdminHash || !s.Login(password) {
		t.Fatal("salt not renewed")
	}
	s.Close()
	random, err := ResetAdmin(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.Login(random) || s.Login(password) {
		t.Fatal("random-key reset failed")
	}
}

func TestCustomAdminPasswordRejectsInvalidInputsAndVerifiers(t *testing.T) {
	s, dir, old := fixture(t)
	s.Close()
	for _, password := range []string{"", "short", strings.Repeat("a", 129), "test-password\n", "test\x00password", "test\xffpassword"} {
		if err := SetAdminPassword(dir, password); err == nil {
			t.Fatal("accepted invalid password")
		}
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.Login(old) {
		t.Fatal("rejected change modified credential")
	}
	for _, encoded := range []string{"", strings.Repeat("g", 64), adminPasswordPrefix + "00:00", "pbkdf2-sha256:1:" + strings.Repeat("0", 32) + ":" + strings.Repeat("0", 64)} {
		if validAdminHash(encoded) || verifyAdminPassword("test-password", encoded) {
			t.Fatal("accepted malformed verifier")
		}
	}
}
