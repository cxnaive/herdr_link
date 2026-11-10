package privatepath

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

func CurrentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

func Private(path string, info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	flags, err := windows.GetFileAttributes(name)
	if err != nil || flags&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return false
	}
	sid, err := CurrentSID()
	if err != nil {
		return false
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || (!owner.Equals(sid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid)) {
		return false
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return false
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if windows.GetAce(acl, i, &ace) != nil {
			return false
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return false
		}
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Mask != 0 && !trustee.Equals(sid) && !trustee.IsWellKnown(windows.WinLocalSystemSid) && !trustee.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			return false
		}
	}
	return true
}

// Protect uses a protected inheritable DACL, not chmod (which only toggles the
// Windows read-only bit). All secret-bearing children inherit the same boundary.
func Protect(path string, _ os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to protect a reparse path")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	flags, err := windows.GetFileAttributes(name)
	if err != nil {
		return err
	}
	if flags&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("refusing to protect a reparse path")
	}
	sid, err := CurrentSID()
	if err != nil {
		return err
	}
	inherit := ""
	if info.IsDir() {
		inherit = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;" + inherit + ";FA;;;" + sid.String() + ")(A;" + inherit + ";FA;;;SY)(A;" + inherit + ";FA;;;BA)")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// Files are flushed before rename. Windows does not support fsync on directory handles.
func SyncDir(*os.File) error { return nil }
