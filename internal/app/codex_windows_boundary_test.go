//go:build windows

package app

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0cv/herdr-mobile-relay/internal/herdr"
	"golang.org/x/sys/windows"
)

func TestWindowsCodexProcessParametersAndOwnedListener(t *testing.T) {
	p, err := readCodexWindowsProcess(os.Getpid())
	if err != nil || p.pid != os.Getpid() || p.parent <= 0 || p.birth == "" || len(p.argv) == 0 || p.cwd == "" {
		t.Fatal(p, err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	origin := "http://" + l.Addr().String()
	before, err := codexProcessListeners(os.Getpid())
	if err != nil || before[origin] == "" {
		t.Fatal(before, err)
	}
	l.Close()
	after, err := codexProcessListeners(os.Getpid())
	if err != nil || after[origin] != "" {
		t.Fatal("closed listener remained owned", after, err)
	}
}

func TestWindowsCodexForegroundRejectsForeignAndReusedProcesses(t *testing.T) {
	for _, variant := range []string{"valid", "direct", "foreign_parent", "reused_parent", "wrong_wrapper", "multiple_native", "different_directory", "different_session", "changed_birth", "daemon", "exec", "remote"} {
		t.Run(variant, func(t *testing.T) {
			cwd := t.TempDir()
			ps := map[int]codexWindowsProcess{
				10: {pid: 10, birth: "shell", started: 1, executable: `C:\pwsh.exe`, cwd: cwd, argv: []string{"pwsh.exe"}},
				20: {pid: 20, parent: 10, birth: "node", started: 2, executable: `C:\node.exe`, cwd: cwd, argv: []string{"node.exe", `C:\npm\@openai\codex\bin\codex.js`}},
				21: {pid: 21, parent: 20, birth: "native", started: 3, executable: `C:\codex.exe`, cwd: cwd, argv: []string{`C:\codex.exe`}},
			}
			parents := map[int]int{20: 10, 21: 20, 22: 21}
			info := &herdr.PaneProcessInfo{PaneID: "pane", ShellPID: 10, ForegroundProcessGroupID: 20, ForegroundProcesses: []herdr.PaneProcess{{PID: 20}}}
			modify := func(pid int, f func(*codexWindowsProcess)) { p := ps[pid]; f(&p); ps[pid] = p }
			switch variant {
			case "direct":
				modify(21, func(p *codexWindowsProcess) { p.parent = 10 })
				parents[21] = 10
				info.ForegroundProcessGroupID = 21
				info.ForegroundProcesses[0].PID = 21
			case "foreign_parent":
				modify(20, func(p *codexWindowsProcess) { p.parent = 99 })
			case "reused_parent":
				modify(10, func(p *codexWindowsProcess) { p.started = 100 })
			case "wrong_wrapper":
				modify(20, func(p *codexWindowsProcess) { p.argv[1] = `C:\custom\codex.js` })
			case "multiple_native":
				p := ps[21]
				p.pid = 23
				ps[23] = p
				parents[23] = 20
			case "different_directory":
				modify(21, func(p *codexWindowsProcess) { p.cwd = t.TempDir() })
			case "different_session":
				modify(21, func(p *codexWindowsProcess) { p.session = 7 })
			case "daemon":
				modify(21, func(p *codexWindowsProcess) { p.argv = append(p.argv, "app-server") })
			case "exec":
				modify(21, func(p *codexWindowsProcess) { p.argv = append(p.argv, "exec", "test") })
			case "remote":
				modify(21, func(p *codexWindowsProcess) { p.argv = append(p.argv, "--remote", "example") })
			}
			reads := map[int]int{}
			read := func(pid int) (codexWindowsProcess, error) {
				reads[pid]++
				p, ok := ps[pid]
				if !ok {
					return p, errCodexProcessStart
				}
				if variant == "changed_birth" && pid == 21 && reads[pid] > 1 {
					p.birth = "new"
				}
				return p, nil
			}
			p, err := codexWindowsForegroundWith(info, "pane", parents, read)
			if variant == "valid" || variant == "direct" {
				if err != nil || p.PID != 21 {
					t.Fatal(p, err)
				}
			} else if err == nil {
				t.Fatal("accepted unproven process", p)
			}
		})
	}
}

func TestWindowsCodexTCPTableRejectsUnownedAndWildcard(t *testing.T) {
	b := make([]byte, 8+160*3)
	binary.LittleEndian.PutUint32(b, 3)
	for i := 0; i < 3; i++ {
		r := b[8+i*160:]
		binary.LittleEndian.PutUint32(r, 2)
		r[4] = 127
		r[7] = 1
		binary.BigEndian.PutUint16(r[8:], uint16(1000+i))
		binary.LittleEndian.PutUint32(r[20:], 12)
		binary.LittleEndian.PutUint64(r[24:], uint64(50+i))
	}
	binary.LittleEndian.PutUint32(b[8+160+20:], 13)
	b[8+320+4] = 0
	b[8+320+7] = 0
	got, err := codexWindowsListeners(b, 12, "birth")
	if err != nil || len(got) != 1 || got["http://127.0.0.1:1000"] == "" {
		t.Fatal(got, err)
	}
	if _, err = codexWindowsListeners(b[:len(b)-1], 12, "birth"); err == nil {
		t.Fatal("truncated table accepted")
	}
}

func TestWindowsCodexLockHelper(t *testing.T) {
	path := os.Getenv("HERDR_TEST_CODEX_LOCK_FILE")
	if path == "" {
		t.Skip("child helper")
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	mode := os.Getenv("HERDR_TEST_CODEX_LOCK_MODE")
	if mode != "unlocked" {
		flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
		if mode != "shared" {
			flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
		}
		low, high := uint32(0xffffffff), uint32(0xffffffff)
		if mode == "partial" {
			low, high = 1, 0
		}
		var overlapped windows.Overlapped
		if err = windows.LockFileEx(h, flags, 0, low, high, &overlapped); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Println("LOCK_READY")
	bufio.NewReader(os.Stdin).ReadByte()
}

func TestWindowsCodexWriterRequiresExclusiveLiveOwner(t *testing.T) {
	for _, mode := range []string{"exclusive", "shared", "unlocked", "partial"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			locks := filepath.Join(dir, "thread-writer-locks")
			os.Mkdir(locks, 0700)
			path := filepath.Join(locks, reportID+".lock")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWindowsCodexLockHelper$")
			cmd.Env = append(os.Environ(), "HERDR_TEST_CODEX_LOCK_FILE="+path, "HERDR_TEST_CODEX_LOCK_MODE="+mode)
			in, _ := cmd.StdinPipe()
			out, _ := cmd.StdoutPipe()
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { in.Close(); cmd.Wait() })
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(out)
				for scanner.Scan() {
					if scanner.Text() == "LOCK_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("lock helper failed")
				}
			case <-time.After(5 * time.Second):
				cmd.Process.Kill()
				t.Fatal("lock helper timeout")
			}
			writers, err := codexProcessWriters(cmd.Process.Pid, filepath.Join(dir, "sessions"))
			if mode == "exclusive" {
				if err != nil || writers[reportID] == "" {
					// Only synthetic fixture data: identify which fail-closed boundary
					// rejected the owner without weakening the ownership assertion.
					birth, birthErr := codexProcessStart(cmd.Process.Pid)
					canonical, canonicalErr := filepath.EvalSymlinks(locks)
					identity, owned, ownerErr := codexWindowsWriter(path, cmd.Process.Pid)
					t.Logf("fixture pid=%d birth=%q birthErr=%v dir=%q canonical=%q canonicalErr=%v identity=%q owned=%v ownerErr=%v",
						cmd.Process.Pid, birth, birthErr, locks, canonical, canonicalErr, identity, owned, ownerErr)
					t.Fatal(writers, err)
				}
				var wg sync.WaitGroup
				for range 6 {
					wg.Go(func() {
						for range 8 {
							got, e := codexProcessWriters(cmd.Process.Pid, filepath.Join(dir, "sessions"))
							if e != nil || got[reportID] != writers[reportID] {
								t.Errorf("parallel readers revoked writer: %v", e)
								return
							}
						}
					})
				}
				wg.Wait()
			} else if err == nil && len(writers) > 0 {
				t.Fatal("nonexclusive/unlocked file trusted", writers)
			}
			if foreign, e := codexProcessWriters(os.Getppid(), filepath.Join(dir, "sessions")); e == nil && len(foreign) != 0 {
				t.Fatal("another process inherited lock", foreign)
			}
			in.Close()
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
			if _, owned, e := codexWindowsWriter(path, cmd.Process.Pid); e != nil || owned {
				t.Fatal("orphan lock file treated as live writer", e)
			}
		})
	}
}

func TestWindowsCodexLockDirectoryAcceptsAliasesButRejectsReparsePoints(t *testing.T) {
	root := t.TempDir()
	locks := filepath.Join(root, "thread-writer-locks")
	if err := os.Mkdir(locks, 0700); err != nil {
		t.Fatal(err)
	}
	long, err := filepath.EvalSymlinks(locks)
	if err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(locks)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]uint16, 32768)
	n, err := windows.GetShortPathName(name, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || n >= uint32(len(buffer)) {
		t.Fatal("short path lookup", n, err)
	}
	for _, alias := range []string{locks, long, strings.ToUpper(locks), windows.UTF16ToString(buffer[:n])} {
		got, err := codexWindowsLockDirectory(alias)
		if err != nil || got != long {
			t.Fatalf("same directory alias %q: got %q, %v", alias, got, err)
		}
	}
	file := filepath.Join(root, "regular-file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"relative", file, filepath.Join(root, "missing")} {
		if _, err := codexWindowsLockDirectory(invalid); err == nil {
			t.Fatalf("accepted invalid directory %q", invalid)
		}
	}
	// Directory junction creation does not require symlink privileges.
	junction := filepath.Join(root, "junction")
	output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, locks).CombinedOutput()
	if err != nil {
		t.Fatalf("create fixture junction: %v: %s", err, output)
	}
	t.Cleanup(func() { os.Remove(junction) })
	child := filepath.Join(locks, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{junction, filepath.Join(junction, "child")} {
		if _, err := codexWindowsLockDirectory(invalid); err == nil {
			t.Fatalf("accepted reparse directory %q", invalid)
		}
	}
}
