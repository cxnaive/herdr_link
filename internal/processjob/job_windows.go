// Package processjob bounds helper processes and all their descendants on Windows.
package processjob

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var jobs sync.Map

// Start suspends the first thread until the process belongs to a kill-on-close
// job. Children cannot escape between process creation and job assignment.
func Start(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}
	if err = cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}
	pid := cmd.Process.Pid
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err == nil {
		err = resumeInitialThread(uint32(pid))
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		windows.CloseHandle(job)
		return nil, err
	}
	jobs.Store(pid, job)
	return func() { jobs.Delete(pid); _ = windows.CloseHandle(job) }, nil
}

func resumeInitialThread(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return err
	}
	return errors.New("cannot resume helper's initial thread")
}

func Terminate(pid int, wait <-chan error) bool {
	if value, ok := jobs.Load(pid); ok {
		_ = windows.TerminateJobObject(value.(windows.Handle), 1)
	}
	select {
	case <-wait:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

func Alive(pid int) bool {
	value, ok := jobs.Load(pid)
	if !ok {
		return false
	}
	var info struct {
		UserTime, KernelTime, PeriodUserTime, PeriodKernelTime           int64
		PageFaults, TotalProcesses, ActiveProcesses, TerminatedProcesses uint32
	}
	if windows.QueryInformationJobObject(value.(windows.Handle), windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil) != nil {
		return true
	}
	return info.ActiveProcesses != 0
}
