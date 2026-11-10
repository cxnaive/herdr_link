// Package gatewayaccess owns the private gateway computer registry. It contains
// registration credentials only, never Relay/phone encryption keys or messages.
package gatewayaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	bolt "go.etcd.io/bbolt"
)

var ErrDenied = errors.New("gateway access denied")
var ErrInvalid = errors.New("invalid gateway management request")

const maxComputers = 4096
const maxInvites = 128

type Computer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Mode      string `json:"mode"`
	Enabled   bool   `json:"enabled"`
	Revoked   bool   `json:"revoked"`
	Epoch     uint64 `json:"epoch"`
	CreatedAt int64  `json:"created_at"`
	LastSeen  int64  `json:"last_seen"`
	Token     string `json:"token,omitempty"`
}
type Invite struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Target          string `json:"target,omitempty"`
	ExpiresAt       int64  `json:"expires_at"`
	SecretHash      string `json:"secret_hash,omitempty"`
	UsedBy          string `json:"used_by,omitempty"`
	RequestID       string `json:"request_id,omitempty"`
	CredentialEpoch uint64 `json:"credential_epoch,omitempty"`
}
type Limits struct {
	MaxRelays          int `json:"max_relays"`
	MaxClients         int `json:"max_clients"`
	MaxClientsPerRelay int `json:"max_clients_per_relay"`
}
type Audit struct {
	Time   int64  `json:"time"`
	Action string `json:"action"`
	ID     string `json:"id,omitempty"`
}
type State struct {
	Schema      int                 `json:"schema"`
	AdminHash   string              `json:"admin_hash"`
	Certificate string              `json:"certificate"`
	PrivateKey  string              `json:"private_key"`
	Imported    bool                `json:"imported"`
	Computers   map[string]Computer `json:"computers"`
	Invites     map[string]Invite   `json:"invites"`
	Limits      *Limits             `json:"limits,omitempty"`
	Audit       []Audit             `json:"audit"`
}
type Store struct {
	mu          sync.RWMutex
	db          *bolt.DB
	state       State
	certificate tls.Certificate
	fingerprint string
}

var bucket = []byte("gateway-access-v1")
var stateKey = []byte("state")

func secret() string { return base64.RawURLEncoding.EncodeToString(randBytes(32)) }
func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}
func hash(value string) string { h := sha256.Sum256([]byte(value)); return hex.EncodeToString(h[:]) }
func equalHash(value, want string) bool {
	return subtle.ConstantTimeCompare([]byte(hash(value)), []byte(want)) == 1
}
func privateDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return ErrInvalid
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() || !privatepath.Private(dir, st) {
		return errors.New("gateway management directory must be private (0700), not a symlink")
	}
	return nil
}
func Init(dir string) (string, error) {
	if err := privateDir(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "access.db")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", errors.New("management state already exists or cannot be created")
	}
	f.Close()
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return "", err
	}
	defer db.Close()
	cert, key, err := newCertificate()
	if err != nil {
		return "", err
	}
	password := secret()
	state := State{Schema: 1, AdminHash: hash(password), Certificate: cert, PrivateKey: key, Computers: map[string]Computer{}, Invites: map[string]Invite{}}
	err = db.Update(func(tx *bolt.Tx) error {
		b, e := tx.CreateBucket(bucket)
		if e != nil {
			return e
		}
		data, e := json.Marshal(state)
		if e != nil {
			return e
		}
		return b.Put(stateKey, data)
	})
	if err != nil {
		return "", err
	}
	return password, nil
}
func Open(dir string) (*Store, error) {
	if err := privateDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "access.db")
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || !privatepath.Private(path, st) {
		return nil, errors.New("initialize a private gateway management state first")
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		if b == nil {
			return ErrInvalid
		}
		return json.Unmarshal(b.Get(stateKey), &s.state)
	})
	if err == nil && (s.state.Schema != 1 || s.state.Computers == nil || s.state.Invites == nil || len(s.state.Computers) > maxComputers || len(s.state.Invites) > maxInvites || !validAdminHash(s.state.AdminHash)) {
		err = ErrInvalid
	}
	if err == nil {
		s.certificate, err = tls.X509KeyPair([]byte(s.state.Certificate), []byte(s.state.PrivateKey))
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	pin := sha256.Sum256(s.certificate.Certificate[0])
	s.fingerprint = hex.EncodeToString(pin[:])
	return s, nil
}
func (s *Store) Close() error                 { return s.db.Close() }
func (s *Store) Certificate() tls.Certificate { return s.certificate }
func (s *Store) Fingerprint() string          { return s.fingerprint }
func (s *Store) Login(password string) bool {
	s.mu.RLock()
	encoded := s.state.AdminHash
	s.mu.RUnlock()
	return verifyAdminPassword(password, encoded)
}
func clone(v State) State {
	b, _ := json.Marshal(v)
	var copy State
	_ = json.Unmarshal(b, &copy)
	return copy
}
func (s *Store) change(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.state)
	if err := fn(&next); err != nil {
		return err
	}
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err = s.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(bucket).Put(stateKey, b) }); err != nil {
		return err
	}
	s.state = next
	return nil
}
func audit(v *State, action, id string, now time.Time) {
	v.Audit = append(v.Audit, Audit{now.UnixMilli(), action, id})
	if len(v.Audit) > 500 {
		v.Audit = v.Audit[len(v.Audit)-500:]
	}
}
func validName(name string) bool {
	return strings.TrimSpace(name) != "" && len([]rune(name)) <= 48 && len(name) <= 160 && !strings.ContainsFunc(name, unicode.IsControl)
}
func (s *Store) ImportLegacy(ids map[string]bool, now time.Time) error {
	return s.change(func(v *State) error {
		if v.Imported {
			return nil
		}
		if len(ids) > maxComputers {
			return ErrInvalid
		}
		for id, allowed := range ids {
			if !allowed {
				continue
			}
			if !gatewaywire.ValidRelayID(id) {
				return ErrInvalid
			}
			v.Computers[id] = Computer{ID: id, Name: "电脑 " + id[:6], Mode: "legacy", Enabled: true, Epoch: 1, CreatedAt: now.UnixMilli()}
		}
		v.Imported = true
		audit(v, "import_legacy", "", now)
		return nil
	})
}
func (s *Store) Snapshot() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v := clone(s.state)
	v.AdminHash = ""
	v.Certificate = ""
	v.PrivateKey = ""
	for id, c := range v.Computers {
		c.Token = ""
		v.Computers[id] = c
	}
	for id, i := range v.Invites {
		i.SecretHash = ""
		i.RequestID = ""
		v.Invites[id] = i
	}
	return v
}
func (s *Store) Limits(fallback Limits) Limits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.Limits == nil {
		return fallback
	}
	return *s.state.Limits
}
func (s *Store) SetLimits(l Limits, now time.Time) error {
	if l.MaxRelays < 1 || l.MaxRelays > 4096 || l.MaxClients < 1 || l.MaxClients > 4096 || l.MaxClientsPerRelay < 1 || l.MaxClientsPerRelay > 256 {
		return ErrInvalid
	}
	return s.change(func(v *State) error { v.Limits = &l; audit(v, "set_limits", "", now); return nil })
}
func (s *Store) Invite(name, target string, now time.Time) (Invite, string, error) {
	if !validName(name) {
		return Invite{}, "", ErrInvalid
	}
	var result Invite
	key := secret()
	err := s.change(func(v *State) error {
		for id, i := range v.Invites {
			if i.ExpiresAt <= now.UnixMilli() {
				delete(v.Invites, id)
			}
		}
		if len(v.Invites) >= maxInvites || len(v.Computers) >= maxComputers && target == "" {
			return ErrInvalid
		}
		if target != "" {
			if _, ok := v.Computers[target]; !ok {
				return ErrInvalid
			}
		}
		result = Invite{ID: base64.RawURLEncoding.EncodeToString(randBytes(18)), Name: strings.TrimSpace(name), Target: target, ExpiresAt: now.Add(10 * time.Minute).UnixMilli(), SecretHash: hash(key)}
		v.Invites[result.ID] = result
		audit(v, "create_invite", result.ID, now)
		return nil
	})
	result.SecretHash = ""
	return result, key, err
}
func (s *Store) CancelInvite(id string, now time.Time) error {
	return s.change(func(v *State) error {
		invite, ok := v.Invites[id]
		if !ok {
			return ErrInvalid
		}
		if invite.UsedBy != "" {
			c := v.Computers[invite.UsedBy]
			if c.Epoch == invite.CredentialEpoch {
				c.Token = ""
				c.Enabled = false
				c.Revoked = true
				c.Epoch++
				v.Computers[c.ID] = c
			}
		}
		delete(v.Invites, id)
		audit(v, "cancel_invite", id, now)
		return nil
	})
}

// Redeem binds recovery retries to one exact Relay and client request. A successful
// registration consumes the pending invitation, so it cannot enroll another PC.
func (s *Store) Redeem(id, key, relayID, requestID string, now time.Time) (string, error) {
	if !gatewaywire.ValidRelayID(relayID) || len(key) != 43 || len(requestID) != 43 {
		return "", ErrDenied
	}
	var token string
	err := s.change(func(v *State) error {
		i, ok := v.Invites[id]
		if !ok || i.ExpiresAt <= now.UnixMilli() || !equalHash(key, i.SecretHash) || i.Target != "" && i.Target != relayID {
			return ErrDenied
		}
		c, exists := v.Computers[relayID]
		if i.UsedBy != "" {
			if i.UsedBy != relayID || i.RequestID != requestID || i.CredentialEpoch != c.Epoch || !c.Enabled || c.Revoked || c.Token == "" {
				return ErrDenied
			}
			token = c.Token
			return nil
		}
		if exists && i.Target != relayID {
			return ErrDenied
		}
		if !exists && len(v.Computers) >= maxComputers {
			return ErrDenied
		}
		token = secret()
		c.ID = relayID
		c.Name = i.Name
		c.Mode = "managed"
		c.Enabled = true
		c.Revoked = false
		c.Epoch++
		c.Token = token
		if c.CreatedAt == 0 {
			c.CreatedAt = now.UnixMilli()
		}
		v.Computers[relayID] = c
		i.UsedBy = relayID
		i.RequestID = requestID
		i.CredentialEpoch = c.Epoch
		v.Invites[id] = i
		audit(v, "enroll", relayID, now)
		return nil
	})
	return token, err
}
func (s *Store) Verify(id, legacy string, nonce, proof []byte) (uint64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.state.Computers[id]
	if !ok || !c.Enabled || c.Revoked {
		return 0, false
	}
	token := c.Token
	if c.Mode == "legacy" {
		token = legacy
	}
	if token == "" || !gatewaywire.VerifyRegistrationProof(token, id, nonce, proof) {
		return 0, false
	}
	return c.Epoch, true
}
func (s *Store) Admitted(id string, epoch uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.state.Computers[id]
	return ok && c.Enabled && !c.Revoked && c.Epoch == epoch
}
func (s *Store) Connected(id string, epoch uint64, now time.Time) error {
	return s.change(func(v *State) error {
		c, ok := v.Computers[id]
		if !ok || c.Epoch != epoch || !c.Enabled || c.Revoked {
			return ErrDenied
		}
		c.LastSeen = now.UnixMilli()
		v.Computers[id] = c
		for iid, i := range v.Invites {
			if i.UsedBy == id {
				delete(v.Invites, iid)
			}
		}
		return nil
	})
}
func (s *Store) Update(id, action, name string, now time.Time) error {
	return s.change(func(v *State) error {
		c, ok := v.Computers[id]
		if !ok {
			return ErrInvalid
		}
		switch action {
		case "rename":
			if !validName(name) {
				return ErrInvalid
			}
			c.Name = strings.TrimSpace(name)
		case "disable":
			c.Enabled = false
			c.Epoch++
		case "enable":
			if c.Revoked {
				return ErrDenied
			}
			c.Enabled = true
			c.Epoch++
		case "revoke":
			c.Enabled = false
			c.Revoked = true
			c.Token = ""
			c.Epoch++
		default:
			return ErrInvalid
		}
		v.Computers[id] = c
		if action == "disable" || action == "revoke" {
			for iid, i := range v.Invites {
				if i.Target == id || i.UsedBy == id {
					delete(v.Invites, iid)
				}
			}
		}
		audit(v, action, id, now)
		return nil
	})
}

func (s *Store) Seen(id string, epoch uint64, now time.Time) error {
	return s.change(func(v *State) error {
		c, ok := v.Computers[id]
		if ok && c.Epoch == epoch {
			c.LastSeen = now.UnixMilli()
			v.Computers[id] = c
		}
		return nil
	})
}

// ResetAdmin requires exclusive access to the database, so it only succeeds
// while the Gateway is stopped. Computer credentials and certificate stay intact.
func ResetAdmin(dir string) (string, error) {
	store, err := Open(dir)
	if err != nil {
		return "", err
	}
	defer store.Close()
	key := secret()
	err = store.change(func(v *State) error { v.AdminHash = hash(key); audit(v, "reset_admin", "", time.Now()); return nil })
	if err != nil {
		return "", err
	}
	return key, nil
}
