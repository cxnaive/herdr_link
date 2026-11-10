package pibridge

import (
	"errors"
	"net"
	"os"
)

func Instance(string) (string, error) {
	return "", errors.New("Pi local command bridge is not supported on Windows")
}
func owned(string, bool) (os.FileInfo, error) {
	return nil, errors.New("Pi local command bridge is not supported on Windows")
}
func checkPeer(*net.UnixConn, int) error {
	return errors.New("Pi local command bridge is not supported on Windows")
}
