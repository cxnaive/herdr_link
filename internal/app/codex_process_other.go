//go:build !linux && !windows

package app

func codexProcessStart(int) (string, error) { return "", errCodexProcessStart }
