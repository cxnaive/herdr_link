package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Query open-file owners and test read exclusion without taking, releasing,
// deleting or writing a Codex lock. File existence alone never binds a thread.
func codexProcessWriters(pid int, root string) (map[string]string, error) {
	if pid <= 0 || pid == os.Getpid() || !filepath.IsAbs(root) {
		return nil, errCodexProcessStart
	}
	birth, err := codexProcessStart(pid)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(filepath.Dir(root), "thread-writer-locks")
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil || canonical != dir {
		return nil, errCodexProcessStart
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(4097)
	f.Close()
	if err != nil || len(entries) > 4096 {
		return nil, errCodexProcessStart
	}
	result := map[string]string{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".lock")
		if !strings.HasSuffix(entry.Name(), ".lock") || !codexBindingID.MatchString(id) {
			continue
		}
		identity, owned, e := codexWindowsWriter(filepath.Join(dir, entry.Name()), pid)
		if e != nil {
			return nil, e
		}
		if owned {
			result[strings.ToLower(id)] = identity
		}
		if len(result) > 64 {
			return nil, errCodexProcessStart
		}
	}
	after, e := codexProcessStart(pid)
	if e != nil || after != birth {
		return nil, errCodexProcessStart
	}
	return result, nil
}

func codexFileUsers(h windows.Handle) ([]uint64, error) {
	// This NT structure requires pointer alignment even on amd64.
	words := make([]uint64, 1025)
	var ios windows.IO_STATUS_BLOCK
	if err := windows.NtQueryInformationFile(h, &ios, (*byte)(unsafe.Pointer(&words[0])), uint32(len(words)*8), 47); err != nil {
		return nil, err
	}
	n := words[0]
	if n > 1024 || ios.Information < uintptr((n+1)*8) {
		return nil, errCodexProcessStart
	}
	users := slices.Clone(words[1 : n+1])
	// Concurrent Relay requests can open additional read-only inspection handles.
	// They cannot own the lock and must not change the external-owner snapshot.
	users = slices.DeleteFunc(users, func(pid uint64) bool { return pid == uint64(os.Getpid()) })
	slices.Sort(users)
	return slices.Compact(users), nil
}

func codexWindowsWriter(path string, pid int) (string, bool, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	users, err := codexFileUsers(h)
	if err != nil {
		return "", false, err
	}
	if !slices.Contains(users, uint64(pid)) {
		return "", false, nil
	}
	// Another process with an open handle makes lock ownership ambiguous.
	for _, user := range users {
		if user != uint64(pid) {
			return "", false, errCodexProcessStart
		}
	}
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		return "", false, errCodexProcessStart
	}
	opened, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	named, err := os.Lstat(path)
	if err != nil || !named.Mode().IsRegular() || !os.SameFile(opened, named) {
		return "", false, errCodexProcessStart
	}
	// Codex holds a whole-file exclusive lock, including bytes past EOF. Shared
	// locks, unlocked handles and a lock on just the first byte are insufficient.
	for _, offset := range []int64{0, 4096, 1 << 62} {
		if _, err = f.ReadAt(make([]byte, 1), offset); !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return "", false, errCodexProcessStart
		}
	}
	after, err := codexFileUsers(h)
	if err != nil || !slices.Equal(after, users) {
		return "", false, errCodexProcessStart
	}
	named, err = os.Lstat(path)
	if err != nil || !os.SameFile(opened, named) {
		return "", false, errCodexProcessStart
	}
	return fmt.Sprintf("%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), true, nil
}
