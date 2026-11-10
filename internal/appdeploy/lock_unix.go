//go:build !windows

package appdeploy

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

func lockFile(filename string) (*os.File, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errDeployLocked
		}
		return nil, fmt.Errorf("lock app deployment: %w", err)
	}
	return file, nil
}
