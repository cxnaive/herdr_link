package processjob

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowsJobCancellationKillsDescendant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.txt")
	code := `$c=Start-Process -PassThru -WindowStyle Hidden ping.exe -ArgumentList '-t','127.0.0.1'; [IO.File]::WriteAllText('` + strings.ReplaceAll(path, "'", "''") + `',[string]$c.Id); Start-Sleep 60`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", code)
	cleanup, err := Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	var child int
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		b, e := os.ReadFile(path)
		if e == nil {
			child, _ = strconv.Atoi(string(b))
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if child == 0 {
		Terminate(cmd.Process.Pid, wait)
		t.Fatal("helper did not start a child")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(child))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if !Terminate(cmd.Process.Pid, wait) {
		t.Fatal("parent wait timed out")
	}
	state, err := windows.WaitForSingleObject(handle, 3000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatal("child survived job cancellation", state, err)
	}
}
