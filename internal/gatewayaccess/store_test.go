package gatewayaccess

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
)

func fixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	key, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dir, key
}
func identity(t *testing.T, key string) string {
	t.Helper()
	id, err := gatewaywire.DeriveRelayID(strings.Repeat(key, 32))
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func verify(t *testing.T, s *Store, id, token string) (uint64, bool) {
	t.Helper()
	nonce := []byte(strings.Repeat("n", 32))
	return s.Verify(id, token, nonce, gatewaywire.RegistrationProof(token, id, nonce))
}
func TestRegistryLegacyMigrationIndependentCredentialsAndNoDowngrade(t *testing.T) {
	s, dir, admin := fixture(t)
	now := time.Now()
	id := identity(t, "a")
	other := identity(t, "b")
	legacy := strings.Repeat("s", 32)
	if !s.Login(admin) || s.Login(strings.Repeat("a", 43)) {
		t.Fatal("admin authentication")
	}
	if err := s.ImportLegacy(map[string]bool{id: true}, now); err != nil {
		t.Fatal(err)
	}
	old, ok := verify(t, s, id, legacy)
	if !ok || old == 0 {
		t.Fatal("old computer lost")
	}
	if _, ok := verify(t, s, other, legacy); ok {
		t.Fatal("shared token bypasses imported list")
	}
	invite, code, err := s.Invite("电脑", id, now)
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Redeem(invite.ID, code, id, strings.Repeat("r", 43), now)
	if err != nil {
		t.Fatal(err)
	}
	epoch, ok := verify(t, s, id, token)
	if !ok || epoch == old || s.Admitted(id, old) {
		t.Fatal("stale registration lease")
	}
	if _, ok = verify(t, s, id, legacy); ok {
		t.Fatal("legacy bypass after migration")
	}
	if err = s.Connected(id, epoch, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Redeem(invite.ID, code, id, strings.Repeat("r", 43), now); err == nil {
		t.Fatal("consumed invite reused")
	}
	encoded, _ := json.Marshal(s.Snapshot())
	for _, secret := range []string{admin, token, code, s.state.PrivateKey} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("snapshot exposed secrets")
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Login(admin) || len(reopened.Snapshot().Computers) != 1 {
		t.Fatal("lost persistent registry")
	}
	if err = reopened.ImportLegacy(map[string]bool{id: true, other: true}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := verify(t, reopened, other, legacy); ok {
		t.Fatal("restart reimported stale environmental allowlist")
	}
}
func TestInvitesRetryBindingExpiryCancellationAndCredentialEpoch(t *testing.T) {
	s, _, _ := fixture(t)
	now := time.Now()
	id := identity(t, "a")
	other := identity(t, "b")
	request := strings.Repeat("r", 43)
	invite, key, _ := s.Invite("新电脑", "", now)
	if _, err := s.Redeem(invite.ID, strings.Repeat("x", 43), id, request, now); err == nil {
		t.Fatal("wrong invite accepted")
	}
	token, err := s.Redeem(invite.ID, key, id, request, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Redeem(invite.ID, key, id, request, now)
	if err != nil || again != token {
		t.Fatal("lost-response retry changed token")
	}
	for _, row := range []struct{ id, request string }{{other, request}, {id, strings.Repeat("q", 43)}} {
		if _, err = s.Redeem(invite.ID, key, row.id, row.request, now); err == nil {
			t.Fatal("different claimant recovered token")
		}
	}
	next, nextKey, _ := s.Invite("重新接入", id, now)
	nextToken, err := s.Redeem(next.ID, nextKey, id, request, now)
	if err != nil || nextToken == token {
		t.Fatal("rotation failed")
	}
	if _, err = s.Redeem(invite.ID, key, id, request, now); err == nil {
		t.Fatal("old pending request stole newer credential")
	}
	if err = s.CancelInvite(invite.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := verify(t, s, id, nextToken); !ok {
		t.Fatal("cancel old invitation revoked new credential")
	}
	if err = s.CancelInvite(next.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, ok := verify(t, s, id, nextToken); ok {
		t.Fatal("cancel pending invite left usable credential")
	}
	expired, k, _ := s.Invite("过期", "", now)
	if _, err = s.Redeem(expired.ID, k, other, request, now.Add(11*time.Minute)); err == nil {
		t.Fatal("expired invitation accepted")
	}
}
func TestDisabledRevokedAndLimitsAreDurable(t *testing.T) {
	s, _, _ := fixture(t)
	now := time.Now()
	id := identity(t, "a")
	invite, key, _ := s.Invite("电脑", "", now)
	token, _ := s.Redeem(invite.ID, key, id, strings.Repeat("r", 43), now)
	epoch, _ := verify(t, s, id, token)
	if err := s.Update(id, "disable", "", now); err != nil {
		t.Fatal(err)
	}
	if _, ok := verify(t, s, id, token); ok || s.Admitted(id, epoch) {
		t.Fatal("disabled computer allowed")
	}
	if err := s.Update(id, "enable", "", now); err != nil {
		t.Fatal(err)
	}
	if _, ok := verify(t, s, id, token); !ok {
		t.Fatal("enable cannot restore non-revoked credential")
	}
	s.Update(id, "revoke", "", now)
	if s.Update(id, "enable", "", now) == nil {
		t.Fatal("revoked credential restored without reenrollment")
	}
	if err := s.SetLimits(Limits{3, 12, 4}, now); err != nil {
		t.Fatal(err)
	}
	if s.Limits(Limits{}) != (Limits{3, 12, 4}) {
		t.Fatal("limits lost")
	}
	if s.SetLimits(Limits{-1, 99999, 0}, now) == nil {
		t.Fatal("unbounded admin limits accepted")
	}
}
func TestRegistryPrivatePermissionsSingleWriterAndFailClosedWrites(t *testing.T) {
	s, dir, _ := fixture(t)
	if _, err := Init(dir); err == nil {
		t.Fatal("overwrote registry")
	}
	if second, err := Open(dir); err == nil {
		second.Close()
		t.Fatal("two processes opened registry")
	}
	s.Close()
	before := len(s.Snapshot().Computers)
	if _, _, err := s.Invite("不能提交", "", time.Now()); err == nil {
		t.Fatal("closed DB accepted mutation")
	}
	if len(s.Snapshot().Computers) != before || len(s.Snapshot().Invites) != 0 {
		t.Fatal("failed disk write changed in-memory authority")
	}
	os.Chmod(filepath.Join(dir, "access.db"), 0644)
	if reopened, err := Open(dir); err == nil {
		reopened.Close()
		t.Fatal("public credential file accepted")
	}
}
func TestConcurrentInviteRedemptionHasOneWinner(t *testing.T) {
	s, _, _ := fixture(t)
	now := time.Now()
	invite, key, _ := s.Invite("一次", "", now)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for _, id := range []string{identity(t, "a"), identity(t, "b"), identity(t, "c")} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Redeem(invite.ID, key, id, strings.Repeat("r", 43), now); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatal(wins)
	}
}

func TestOfflineAdminResetPreservesComputerCredentials(t *testing.T) {
	s, dir, old := fixture(t)
	now := time.Now()
	id := identity(t, "a")
	i, k, _ := s.Invite("电脑", "", now)
	token, _ := s.Redeem(i.ID, k, id, strings.Repeat("r", 43), now)
	pin := s.Fingerprint()
	if _, err := ResetAdmin(dir); err == nil {
		t.Fatal("reset live registry")
	}
	s.Close()
	key, err := ResetAdmin(dir)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.Login(key) || reopened.Login(old) || reopened.Fingerprint() != pin {
		t.Fatal("admin reset changed enrollment identity")
	}
	if _, ok := verify(t, reopened, id, token); !ok {
		t.Fatal("admin reset discarded computer credential")
	}
}
