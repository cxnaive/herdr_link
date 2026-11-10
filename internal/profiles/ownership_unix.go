//go:build !windows

package profiles

import (
	"os"
	"syscall"
)

func ownershipFileSafe(_ string, info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 && info.Mode().IsRegular() && info.Mode().Perm() == 0600
}
func syncOwnershipDirectory(f *os.File) error { return f.Sync() }
