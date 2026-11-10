// Package localipc connects to Herdr's platform-local API and terminal endpoints.
package localipc

import (
	"errors"
	"strings"
)

// Herdr 0.9.5 uses interprocess 2.4.4 GenericNamespaced: prefix the exact
// socket-path string, without cleaning, hashing, or changing slash direction.
func WindowsPipeName(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) || strings.HasPrefix(path, `\\`) {
		return "", errors.New("expected a local Herdr socket marker path")
	}
	return `\\.\pipe\` + path, nil
}
