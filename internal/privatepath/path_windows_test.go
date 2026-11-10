package privatepath

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsPrivateACLRejectsBroadReadAccess(t *testing.T) {
	dir := t.TempDir()
	if err := Protect(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(path)
	if !Private(path, info) {
		t.Fatal("inherited private ACL rejected")
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, _ := sd.DACL()
	if err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Lstat(path)
	if Private(path, info) {
		t.Fatal("world-readable credential accepted")
	}
	if err = Protect(path, 0600); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Lstat(path)
	if !Private(path, info) {
		t.Fatal("private ACL recovery failed")
	}
}
