//go:build !windows

package config

import "path/filepath"

func defaultConfigHome() string            { return filepath.Join(homeDir(), ".config") }
func defaultCacheHome() string             { return filepath.Join(homeDir(), ".cache") }
func validatePlatformRuntime(string) error { return nil }
