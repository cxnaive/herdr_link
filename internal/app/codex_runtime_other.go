//go:build !linux && !windows

package app

import "os"

func codexProcessListeners(int) (map[string]string, error) { return nil, errCodexProcessStart }

func codexSocketIdentity(info os.FileInfo) string { return info.Name() + info.ModTime().String() }
