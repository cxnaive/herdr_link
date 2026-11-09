//go:build linux || darwin

// Package privatepath checks ownership before using private credential files.
package privatepath

import (
	"os"
	"syscall"
)

func Owned(info os.FileInfo) bool {
	if info == nil {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Geteuid())
}
