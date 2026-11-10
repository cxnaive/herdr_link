//go:build !windows

package herdr

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func startCommand(cmd *exec.Cmd) (func(), error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return func() {}, cmd.Start()
}

func terminateProcessGroup(pgid int, waitCh <-chan error) {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	timer := time.NewTimer(termGrace)
	defer timer.Stop()
	leaderDone := false
	select {
	case <-waitCh:
		leaderDone = true
		if !GroupAlive(pgid) {
			return
		}
		<-timer.C
	case <-timer.C:
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)

	if !leaderDone {
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for GroupAlive(pgid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

func GroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
