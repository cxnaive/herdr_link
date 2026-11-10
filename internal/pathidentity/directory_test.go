package pathidentity

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirectoryIdentityDoesNotMergeNeighbours(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	if !SameDirectory(a, a+string(filepath.Separator)) || SameDirectory(a, b) || SameDirectory("relative", "relative") {
		t.Fatal("incorrect directory identity")
	}
	if runtime.GOOS == "windows" && !SameDirectory(a, strings.ToUpper(a)) {
		t.Fatal("Windows case alias rejected")
	}
	f := filepath.Join(a, "file")
	os.WriteFile(f, []byte("x"), 0600)
	if SameDirectory(f, b) {
		t.Fatal("file treated as directory")
	}
}
