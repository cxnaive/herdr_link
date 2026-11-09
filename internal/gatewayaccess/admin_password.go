package gatewayaccess

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"
)

const adminPasswordPrefix = "pbkdf2-sha256:600000:"

func validAdminPassword(password string) bool {
	return len(password) >= 8 && len(password) <= 128 && utf8.ValidString(password) && !strings.ContainsAny(password, "\r\n\x00")
}

func adminPasswordParts(encoded string) (salt, key []byte, ok bool) {
	if !strings.HasPrefix(encoded, adminPasswordPrefix) {
		return nil, nil, false
	}
	parts := strings.Split(strings.TrimPrefix(encoded, adminPasswordPrefix), ":")
	if len(parts) != 2 {
		return nil, nil, false
	}
	salt, saltErr := hex.DecodeString(parts[0])
	key, keyErr := hex.DecodeString(parts[1])
	return salt, key, saltErr == nil && keyErr == nil && len(salt) == 16 && len(key) == 32
}

func validAdminHash(encoded string) bool {
	if len(encoded) == 64 {
		_, err := hex.DecodeString(encoded)
		return err == nil
	}
	_, _, ok := adminPasswordParts(encoded)
	return ok
}

func verifyAdminPassword(password, encoded string) bool {
	if len(encoded) == 64 {
		return len(password) == 43 && equalHash(password, encoded)
	}
	if !validAdminPassword(password) {
		return false
	}
	salt, want, ok := adminPasswordParts(encoded)
	if !ok {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return err == nil && subtle.ConstantTimeCompare(key, want) == 1
}

// SetAdminPassword requires the gateway to be stopped. Only the administrator
// credential changes; enrolled computers and the TLS identity stay intact.
func SetAdminPassword(dir, password string) error {
	if !validAdminPassword(password) {
		return ErrInvalid
	}
	store, err := Open(dir)
	if err != nil {
		return err
	}
	defer store.Close()
	salt := randBytes(16)
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return err
	}
	encoded := adminPasswordPrefix + hex.EncodeToString(salt) + ":" + hex.EncodeToString(key)
	return store.change(func(v *State) error {
		v.AdminHash = encoded
		audit(v, "reset_admin", "", time.Now())
		return nil
	})
}
