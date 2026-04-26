//go:build !windows

package processutil

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// SetProcessGroup puts the command in its own process group so child processes
// can be terminated together.
func SetProcessGroup(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// TerminateProcessTree sends SIGTERM to the process group when available.
func TerminateProcessTree(proc *os.Process) error {
	if proc == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(proc.Pid)
	if err == nil {
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	return proc.Signal(syscall.SIGTERM)
}

// KillProcessTree sends SIGKILL to the process group when available.
func KillProcessTree(proc *os.Process) error {
	if proc == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(proc.Pid)
	if err == nil {
		return syscall.Kill(-pgid, syscall.SIGKILL)
	}
	return proc.Kill()
}

// CheckProcessRunning returns nil when the process still exists.
func CheckProcessRunning(proc *os.Process) error {
	if proc == nil {
		return fmt.Errorf("process not running")
	}
	return proc.Signal(syscall.Signal(0))
}
