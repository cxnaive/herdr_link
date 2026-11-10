//go:build !windows

package update

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
func terminateProcessGroup(pgid int, waitCh <-chan error) bool {
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	timer := time.NewTimer(processTermGrace)
	defer timer.Stop()
	waitCompleted := false
	select {
	case <-waitCh:
		waitCompleted = true
		if !processGroupAlive(pgid) {
			return true
		}
		<-timer.C
	case <-timer.C:
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)

	if !waitCompleted {
		select {
		case <-waitCh:
			waitCompleted = true
		case <-time.After(processWaitDelay):
		}
	}
	deadline := time.Now().Add(processTermGrace)
	for processGroupAlive(pgid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	return waitCompleted
}

func processGroupAlive(pgid int) bool {
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
