//go:build windows

package processutil

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// SetProcessGroup is a no-op on Windows. Tree termination is handled with
// taskkill against the root PID.
func SetProcessGroup(cmd *exec.Cmd) {
}

// TerminateProcessTree asks taskkill to stop the process tree without /F first.
func TerminateProcessTree(proc *os.Process) error {
	return taskkill(proc, false)
}

// KillProcessTree force-kills the process tree.
func KillProcessTree(proc *os.Process) error {
	return taskkill(proc, true)
}

func taskkill(proc *os.Process, force bool) error {
	if proc == nil {
		return nil
	}
	args := []string{"/PID", strconv.Itoa(proc.Pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	cmd := exec.Command("taskkill", args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(output))
	if strings.Contains(strings.ToLower(msg), "not found") ||
		strings.Contains(strings.ToLower(msg), "no running instance") {
		return nil
	}
	return fmt.Errorf("taskkill failed: %w: %s", err, msg)
}

// CheckProcessRunning returns nil when tasklist can still see the PID.
func CheckProcessRunning(proc *os.Process) error {
	if proc == nil {
		return fmt.Errorf("process not running")
	}
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(proc.Pid), "/FO", "CSV", "/NH")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tasklist failed: %w", err)
	}
	lower := strings.ToLower(string(output))
	if strings.Contains(lower, "no tasks are running") || strings.Contains(lower, "info: no tasks are running") {
		return fmt.Errorf("process not running")
	}
	if !strings.Contains(string(output), strconv.Itoa(proc.Pid)) {
		return fmt.Errorf("process not running")
	}
	return nil
}
