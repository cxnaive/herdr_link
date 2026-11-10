package config

import (
	"errors"
	"github.com/0cv/herdr-mobile-relay/internal/privatepath"
	"os"
	"path/filepath"
)

func defaultConfigHome() string {
	return envOr("APPDATA", filepath.Join(homeDir(), "AppData", "Roaming"))
}
func defaultCacheHome() string {
	return filepath.Join(envOr("LOCALAPPDATA", filepath.Join(homeDir(), "AppData", "Local")), "Cache")
}
func validatePlatformRuntime(path string) error {
	// Bootstrap only under a pre-created private directory; do not silently trust
	// chmod's POSIX-looking mode bits for Windows credentials.
	if !filepath.IsAbs(path) {
		return errors.New("Windows Relay runtime directory must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || !privatepath.Private(path, info) {
		return errors.New("Windows Relay runtime needs a private ACL; run install.ps1 first")
	}
	return nil
}
