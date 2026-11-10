//go:build !windows

package privatepath

import "os"

func Private(_ string, info os.FileInfo) bool {
	return info != nil && Owned(info) && info.Mode().Perm()&0077 == 0
}
func Protect(path string, mode os.FileMode) error { return os.Chmod(path, mode) }
func SyncDir(f *os.File) error                    { return f.Sync() }
