package gatewayenroll

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/gatewaywire"
	"github.com/0cv/herdr-mobile-relay/internal/nativeconn"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	bolt "go.etcd.io/bbolt"
)

const LocalFilename = "gateway-access.json"

type LocalAccess struct {
	Schema            int    `json:"schema"`
	Endpoint          string `json:"endpoint"`
	RelayID           string `json:"relay_id"`
	RelayKey          string `json:"relay_key"`
	RegistrationToken string `json:"registration_token"`
}
type pendingJoin struct {
	CodeID    string `json:"code_id"`
	Endpoint  string `json:"endpoint"`
	Pin       string `json:"pin"`
	RequestID string `json:"request_id"`
	RelayKey  string `json:"relay_key"`
}

func privateFile(path string) (*os.File, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || !privatepath.Private(path, st) {
		return nil, errors.New("gateway credential file must be a private regular file (0600)")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(st, actual) {
		f.Close()
		return nil, ErrEnrollment
	}
	return f, nil
}
func readJSONFile(path string, out any) error {
	f, err := privateFile(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 16384))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return errors.New("invalid local gateway credentials")
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return ErrEnrollment
	}
	return nil
}
func LoadLocal(runtime string) (LocalAccess, error) {
	var record LocalAccess
	if err := readJSONFile(filepath.Join(runtime, LocalFilename), &record); err != nil {
		return record, err
	}
	id, err := gatewaywire.DeriveRelayID(record.RelayKey)
	if err != nil || len(record.RelayKey) != 32 || record.Schema != 1 || record.RelayID != id || len(record.RegistrationToken) != 43 {
		return LocalAccess{}, errors.New("invalid local gateway credentials")
	}
	if _, err = nativeconn.Endpoint(record.Endpoint); err != nil {
		return LocalAccess{}, errors.New("invalid saved gateway endpoint")
	}
	return record, nil
}
func writePrivate(path string, value any) error {
	if st, err := os.Lstat(path); err == nil {
		if !st.Mode().IsRegular() || !privatepath.Private(path, st) {
			return ErrEnrollment
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gateway-save-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = privatepath.Protect(f.Name(), 0600); err == nil {
		_, err = f.Write(append(data, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return privatepath.SyncDir(dir)
}
func Join(ctx context.Context, raw, runtime, configuredKey string) (LocalAccess, error) {
	code, err := Parse(raw, time.Now())
	if err != nil {
		return LocalAccess{}, err
	}
	if !filepath.IsAbs(runtime) {
		return LocalAccess{}, errors.New("Relay runtime directory must be absolute")
	}
	if err = os.MkdirAll(runtime, 0700); err != nil {
		return LocalAccess{}, err
	}
	st, err := os.Lstat(runtime)
	if err != nil || !st.IsDir() || !privatepath.Private(runtime, st) {
		return LocalAccess{}, errors.New("Relay runtime directory must be private (0700)")
	}
	lockPath := filepath.Join(runtime, ".gateway-join.lock")
	if st, err := os.Lstat(lockPath); err == nil {
		if !st.Mode().IsRegular() || !privatepath.Private(lockPath, st) {
			return LocalAccess{}, ErrEnrollment
		}
	} else if !os.IsNotExist(err) {
		return LocalAccess{}, err
	}
	lock, err := bolt.Open(lockPath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return LocalAccess{}, errors.New("another Gateway enrollment is running")
	}
	defer lock.Close()
	existing, loadErr := LoadLocal(runtime)
	if loadErr != nil && !os.IsNotExist(loadErr) {
		return LocalAccess{}, loadErr
	}
	key := configuredKey
	if key == "" && os.IsNotExist(loadErr) {
		key, err = configuredIdentity(runtime)
		if err != nil {
			return LocalAccess{}, err
		}
	}
	if loadErr == nil {
		if key != "" && key != existing.RelayKey {
			return LocalAccess{}, errors.New("existing Relay identity differs; refusing to replace pairing identity")
		}
		key = existing.RelayKey
	}
	pendingPath := filepath.Join(runtime, "gateway-join-pending.json")
	var pending pendingJoin
	pendingErr := readJSONFile(pendingPath, &pending)
	if pendingErr != nil && !os.IsNotExist(pendingErr) {
		return LocalAccess{}, pendingErr
	}
	if pendingErr == nil && pending.CodeID == code.ID {
		if pending.Endpoint != code.Endpoint || pending.Pin != code.Pin || key != "" && key != pending.RelayKey {
			return LocalAccess{}, ErrEnrollment
		}
		key = pending.RelayKey
	} else {
		if key == "" {
			if pendingErr == nil {
				key = pending.RelayKey
			} else {
				key = RandomSecret()[:32]
			}
		}
		pending = pendingJoin{CodeID: code.ID, Endpoint: code.Endpoint, Pin: code.Pin, RequestID: RandomSecret(), RelayKey: key}
	}
	if len(key) != 32 {
		return LocalAccess{}, errors.New("Relay identity must be exactly 32 bytes; existing key is not changed")
	}
	id, err := gatewaywire.DeriveRelayID(key)
	if err != nil {
		return LocalAccess{}, err
	}
	// Persist identity and recovery request BEFORE exchanging the one-time code.
	if err = writePrivate(pendingPath, pending); err != nil {
		return LocalAccess{}, err
	}
	token, err := Redeem(ctx, code, id, pending.RequestID)
	if err != nil {
		return LocalAccess{}, err
	}
	record := LocalAccess{Schema: 1, Endpoint: code.Endpoint, RelayID: id, RelayKey: key, RegistrationToken: token}
	if err = writePrivate(filepath.Join(runtime, LocalFilename), record); err != nil {
		return LocalAccess{}, err
	}
	_ = os.Remove(pendingPath)
	return record, nil
}

// Read only the installer's literal identity assignment; never evaluate a shell
// config. Existing device data without a recoverable identity is not a new install.
func configuredIdentity(runtime string) (string, error) {
	path := filepath.Join(runtime, "relay.env")
	f, err := privateFile(path)
	if os.IsNotExist(err) {
		for _, name := range []string{"device-auth", "relay.pid"} {
			if _, err := os.Lstat(filepath.Join(runtime, name)); err == nil {
				return "", errors.New("existing Relay identity is not loaded; run gateway-join through the installed run.sh wrapper")
			} else if !os.IsNotExist(err) {
				return "", err
			}
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, statErr := f.Stat()
	if statErr != nil || info.Size() > 64*1024 {
		return "", errors.New("Relay configuration exceeds the safe identity read limit; use the installed wrapper")
	}
	scanner := bufio.NewScanner(io.LimitReader(f, 64*1024+1))
	key := ""
	literal := regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != "HERDR_RELAY_TOKEN" {
			continue
		}
		if key != "" {
			return "", ErrEnrollment
		}
		value = strings.TrimSpace(value)
		if len(value) == 34 && (value[0] == '\'' && value[33] == '\'' || value[0] == '"' && value[33] == '"') {
			value = value[1:33]
		}
		if !literal.MatchString(value) {
			return "", errors.New("Relay identity is not a simple literal; use the installed run.sh wrapper")
		}
		key = value
	}
	if scanner.Err() != nil || key == "" {
		return "", errors.New("existing Relay configuration has no readable identity; refusing to create another")
	}
	return key, nil
}
