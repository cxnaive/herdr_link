package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var codexTCPTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

func codexProcessListeners(pid int) (map[string]string, error) {
	birth, err := codexProcessStart(pid)
	if err != nil {
		return nil, err
	}
	var size uint32
	// OWNER_MODULE_LISTENER includes kernel endpoint creation timestamps, so
	// closing and reusing a port in the same TUI revokes the previous lease.
	r, _, _ := codexTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 6, 0)
	if r != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 8 || size > 16*1024*1024 {
		return nil, errCodexProcessStart
	}
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, size)
		r, _, _ = codexTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, 6, 0)
		if r == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) && size <= 16*1024*1024 {
			continue
		}
		if r != 0 {
			return nil, errCodexProcessStart
		}
		result, e := codexWindowsListeners(buf, pid, birth)
		if e != nil {
			return nil, e
		}
		after, e := codexProcessStart(pid)
		if e != nil || after != birth {
			return nil, errCodexProcessStart
		}
		return result, nil
	}
	return nil, errCodexProcessStart
}
func codexWindowsListeners(buf []byte, pid int, birth string) (map[string]string, error) {
	if len(buf) < 8 {
		return nil, errCodexProcessStart
	}
	count := int(binary.LittleEndian.Uint32(buf))
	if count > (len(buf)-8)/160 {
		return nil, errCodexProcessStart
	}
	result := map[string]string{}
	for i := 0; i < count; i++ {
		row := buf[8+i*160 : 8+(i+1)*160]
		if binary.LittleEndian.Uint32(row) != 2 || int(binary.LittleEndian.Uint32(row[20:])) != pid || row[4] != 127 || row[5] != 0 || row[6] != 0 || row[7] != 1 {
			continue
		}
		port := binary.BigEndian.Uint16(row[8:])
		created := binary.LittleEndian.Uint64(row[24:])
		if port == 0 || created == 0 {
			return nil, errCodexProcessStart
		}
		origin := fmt.Sprintf("http://127.0.0.1:%d", port)
		if result[origin] != "" {
			return nil, errCodexProcessStart
		}
		result[origin] = fmt.Sprintf("%s:%d:%d", birth, port, created)
	}
	return result, nil
}
func codexSocketIdentity(info os.FileInfo) string {
	return fmt.Sprintf("%s:%d", info.Name(), info.ModTime().UnixNano())
}
