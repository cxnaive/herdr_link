//go:build !linux && !darwin

package privatepath

import "os"

// FileInfo alone cannot prove Windows ownership or ACLs. Call Private with
// the path when checking credential files on Windows.
func Owned(os.FileInfo) bool { return false }
