package pathidentity

import (
	"os"
	"path/filepath"
	"runtime"
)

// SameDirectory accepts Windows aliases only when both resolve to the same
// directory object. A matching basename or case-folded prefix is not identity.
func SameDirectory(a, b string) bool {
	if !filepath.IsAbs(a) || !filepath.IsAbs(b) {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	if runtime.GOOS != "windows" {
		return false
	}
	x, err := os.Stat(a)
	if err != nil || !x.IsDir() {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && y.IsDir() && os.SameFile(x, y)
}
