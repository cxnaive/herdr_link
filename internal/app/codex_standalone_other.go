//go:build !linux && !windows

package app

func codexProcessWriters(int, string) (map[string]string, error) { return nil, errCodexProcessStart }
