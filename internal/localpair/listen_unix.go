//go:build !windows

package localpair

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func listenPairing(runtime string) (net.Listener, error) {
	path := socketPath(runtime)
	dir := filepath.Dir(path)
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("TCP pairing directory must be a private directory (0700)")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("refusing to replace non-socket pairing path")
		}
		conn, dialErr := net.DialTimeout("unix", path, time.Second)
		if dialErr == nil {
			_ = conn.Close()
			return nil, errors.New("pairing socket already active")
		}
		// A private socket with no listening process can survive an unclean exit.
		if !errors.Is(dialErr, os.ErrNotExist) {
			var op *net.OpError
			if !errors.As(dialErr, &op) || !errors.Is(op.Err, syscall.ECONNREFUSED) {
				return nil, dialErr
			}
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		return nil, err
	}

	return listener, nil
}
