//go:build !windows

package pibridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func Instance(socketPath string) (string, error) {
	path, err := filepath.EvalSymlinks(socketPath)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&os.ModeSocket == 0 || int(stat.Uid) != os.Getuid() {
		return "", errors.New("invalid Herdr socket")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%d\n%d", path, stat.Dev, stat.Ino)))
	return hex.EncodeToString(sum[:]), nil
}

func owned(path string, socket bool) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	expected := os.FileMode(0700)
	validType := info.IsDir()
	if socket {
		expected = 0600
		validType = info.Mode()&os.ModeSocket != 0
	}
	if !ok || int(stat.Uid) != os.Getuid() || !validType || info.Mode().Perm() != expected {
		return nil, errors.New("unsafe bridge endpoint")
	}
	return info, nil
}
