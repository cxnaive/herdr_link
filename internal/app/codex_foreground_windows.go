package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"unsafe"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"github.com/0cv/herdr-mobile-relay/internal/pathidentity"
	"golang.org/x/sys/windows"
)

type codexWindowsProcess struct {
	pid, parent                  int
	birth, executable, cwd, home string
	argv                         []string
	started                      uint64
	session                      uint32
}

func codexProcessProofSupported() bool { return runtime.GOARCH == "amd64" }

func codexWindowsHandle(pid int, memory bool) (windows.Handle, string, error) {
	if pid <= 0 {
		return 0, "", errCodexProcessStart
	}
	rights := uint32(windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if memory {
		rights |= windows.PROCESS_QUERY_INFORMATION | windows.PROCESS_VM_READ
	}
	h, err := windows.OpenProcess(rights, false, uint32(pid))
	if err != nil {
		return 0, "", err
	}
	fail := func() (windows.Handle, string, error) { windows.CloseHandle(h); return 0, "", errCodexProcessStart }
	var token windows.Token
	if windows.OpenProcessToken(h, windows.TOKEN_QUERY, &token) != nil {
		return fail()
	}
	defer token.Close()
	owner, err := token.GetTokenUser()
	self, ownErr := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || ownErr != nil || !owner.User.Sid.Equals(self.User.Sid) {
		return fail()
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil || exited.HighDateTime != 0 || exited.LowDateTime != 0 {
		return fail()
	}
	birth := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	if birth == 0 {
		return fail()
	}
	return h, fmt.Sprintf("windows:%d:%d", pid, birth), nil
}

func codexProcessStart(pid int) (string, error) {
	h, birth, err := codexWindowsHandle(pid, false)
	if err == nil {
		windows.CloseHandle(h)
	}
	return birth, err
}

func codexReadMemory(h windows.Handle, address uintptr, target []byte) error {
	if address == 0 || len(target) == 0 || len(target) > 2*1024*1024 {
		return errCodexProcessStart
	}
	var n uintptr
	if err := windows.ReadProcessMemory(h, address, &target[0], uintptr(len(target)), &n); err != nil {
		return err
	}
	if n != uintptr(len(target)) {
		return errCodexProcessStart
	}
	return nil
}
func codexReadStruct[T any](h windows.Handle, address uintptr, v *T) error {
	return codexReadMemory(h, address, unsafe.Slice((*byte)(unsafe.Pointer(v)), int(unsafe.Sizeof(*v))))
}
func codexReadUnicode(h windows.Handle, s windows.NTUnicodeString) (string, error) {
	if s.Length == 0 {
		return "", nil
	}
	if s.Length%2 != 0 || s.Length > s.MaximumLength {
		return "", errCodexProcessStart
	}
	u := make([]uint16, int(s.Length)/2)
	if err := codexReadMemory(h, uintptr(unsafe.Pointer(s.Buffer)), unsafe.Slice((*byte)(unsafe.Pointer(&u[0])), int(s.Length))); err != nil {
		return "", err
	}
	return windows.UTF16ToString(u), nil
}

func readCodexWindowsProcess(pid int) (codexWindowsProcess, error) {
	p := codexWindowsProcess{pid: pid}
	if !codexProcessProofSupported() {
		return p, errCodexProcessStart
	}
	h, birth, err := codexWindowsHandle(pid, true)
	if err != nil {
		return p, err
	}
	defer windows.CloseHandle(h)
	p.birth = birth
	p.started, err = strconv.ParseUint(birth[strings.LastIndexByte(birth, ':')+1:], 10, 64)
	if err != nil || windows.ProcessIdToSessionId(uint32(pid), &p.session) != nil {
		return p, errCodexProcessStart
	}
	var wow uint16
	var native uint16
	if windows.IsWow64Process2(h, &wow, &native) != nil || wow != 0 || native != 0x8664 {
		return p, errCodexProcessStart
	}
	var basic windows.PROCESS_BASIC_INFORMATION
	if windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&basic), uint32(unsafe.Sizeof(basic)), nil) != nil || int(basic.UniqueProcessId) != pid {
		return p, errCodexProcessStart
	}
	p.parent = int(basic.InheritedFromUniqueProcessId)
	var peb windows.PEB
	if err = codexReadStruct(h, uintptr(unsafe.Pointer(basic.PebBaseAddress)), &peb); err != nil {
		return p, err
	}
	var parameters windows.RTL_USER_PROCESS_PARAMETERS
	if err = codexReadStruct(h, uintptr(unsafe.Pointer(peb.ProcessParameters)), &parameters); err != nil {
		return p, err
	}
	path := make([]uint16, 32768)
	n := uint32(len(path))
	if windows.QueryFullProcessImageName(h, 0, &path[0], &n) != nil {
		return p, errCodexProcessStart
	}
	p.executable = windows.UTF16ToString(path[:n])
	if p.cwd, err = codexReadUnicode(h, parameters.CurrentDirectory.DosPath); err != nil {
		return p, err
	}
	command, err := codexReadUnicode(h, parameters.CommandLine)
	if err != nil {
		return p, err
	}
	if p.argv, err = windows.DecomposeCommandLine(command); err != nil || len(p.argv) == 0 {
		return p, errCodexProcessStart
	}
	// Read only process parameters and the bounded environment block; retain
	// CODEX_HOME alone and never log other environment values.
	if parameters.EnvironmentSize > 2*1024*1024 || parameters.EnvironmentSize%2 != 0 {
		return p, errCodexProcessStart
	}
	if parameters.EnvironmentSize > 0 {
		env := make([]uint16, parameters.EnvironmentSize/2)
		if err = codexReadMemory(h, uintptr(parameters.Environment), unsafe.Slice((*byte)(unsafe.Pointer(&env[0])), int(parameters.EnvironmentSize))); err != nil {
			return p, err
		}
		for offset := 0; offset < len(env); {
			end := offset
			for end < len(env) && env[end] != 0 {
				end++
			}
			entry := windows.UTF16ToString(env[offset:end])
			key, value, ok := strings.Cut(entry, "=")
			if ok && strings.EqualFold(key, "CODEX_HOME") {
				if p.home != "" {
					return p, errCodexProcessStart
				}
				p.home = value
			}
			offset = end + 1
		}
		clear(env)
	}
	after, e := codexProcessStart(pid)
	if e != nil || after != birth {
		return p, errCodexProcessStart
	}
	return p, nil
}

func codexProcessHome(pid int, home string) (string, error) {
	p, err := readCodexWindowsProcess(pid)
	if err != nil {
		return "", err
	}
	root := p.home
	if root == "" {
		root = filepath.Join(home, ".codex")
	}
	if !filepath.IsAbs(root) {
		return "", errCodexProcessStart
	}
	return filepath.EvalSymlinks(root)
}

func codexWindowsProcessParents() (map[int]int, error) {
	h, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	p := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	parents := map[int]int{}
	for err = windows.Process32First(h, &p); err == nil; err = windows.Process32Next(h, &p) {
		parents[int(p.ProcessID)] = int(p.ParentProcessID)
		if len(parents) > 32768 {
			return nil, errCodexProcessStart
		}
	}
	if err != windows.ERROR_NO_MORE_FILES {
		return nil, err
	}
	return parents, nil
}

func codexRuntimeForegroundProcess(info *herdr.PaneProcessInfo, pane string) (herdr.PaneProcess, error) {
	return codexForegroundGroup(info, pane)
}
func codexForegroundGroup(info *herdr.PaneProcessInfo, pane string) (herdr.PaneProcess, error) {
	parents, err := codexWindowsProcessParents()
	if err != nil {
		return herdr.PaneProcess{}, err
	}
	return codexWindowsForegroundWith(info, pane, parents, readCodexWindowsProcess)
}
func codexWindowsForegroundWith(info *herdr.PaneProcessInfo, pane string, parents map[int]int, read func(int) (codexWindowsProcess, error)) (herdr.PaneProcess, error) {
	bad := func() (herdr.PaneProcess, error) { return herdr.PaneProcess{}, errCodexProcessStart }
	if info == nil || info.PaneID != pane || pane == "" || info.ShellPID <= 0 || len(info.ForegroundProcesses) != 1 || info.ForegroundProcessGroupID != info.ForegroundProcesses[0].PID || info.ForegroundProcessGroupID == info.ShellPID {
		return bad()
	}
	shell, err := read(info.ShellPID)
	if err != nil {
		return bad()
	}
	leader, err := read(info.ForegroundProcessGroupID)
	if err != nil || leader.parent != shell.pid || parents[leader.pid] != shell.pid || leader.started < shell.started || leader.session != shell.session {
		return bad()
	}
	selected := leader
	if strings.EqualFold(filepath.Base(leader.executable), "node.exe") {
		if len(leader.argv) < 2 || !strings.HasSuffix(strings.ReplaceAll(strings.ToLower(leader.argv[1]), "\\", "/"), "/@openai/codex/bin/codex.js") {
			return bad()
		}
		found := false
		for pid, parent := range parents {
			if parent == leader.pid {
				child, e := read(pid)
				if e != nil {
					return bad()
				}
				if !strings.EqualFold(filepath.Base(child.executable), "codex.exe") {
					continue
				}
				if found || child.parent != leader.pid || child.started < leader.started || child.session != leader.session || !pathidentity.SameDirectory(child.cwd, leader.cwd) {
					return bad()
				}
				selected = child
				found = true
			}
		}
		if !found {
			return bad()
		}
	}
	if !strings.EqualFold(filepath.Base(selected.executable), "codex.exe") || len(selected.argv) == 0 || !strings.EqualFold(filepath.Base(selected.argv[0]), "codex.exe") || !filepath.IsAbs(selected.cwd) {
		return bad()
	}
	// Normalize only the verified executable spelling for the common CLI parser.
	args := slices.Clone(selected.argv)
	args[0] = "codex"
	// Reuse the strict TUI option grammar without enabling standalone mode.
	if !codexNoDaemonArgs(append([]string{"codex", "--no-daemon"}, args[1:]...)) {
		return bad()
	}
	for _, p := range []codexWindowsProcess{shell, leader, selected} {
		after, e := read(p.pid)
		if e != nil || after.birth != p.birth || after.parent != p.parent || after.session != p.session || after.executable != p.executable || after.cwd != p.cwd || !slices.Equal(after.argv, p.argv) {
			return bad()
		}
	}
	return herdr.PaneProcess{PID: selected.pid, Name: "codex", Cwd: selected.cwd, Argv: args, Argv0: args[0]}, nil
}
