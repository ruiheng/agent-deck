//go:build !windows

package tmux

import (
	"errors"
	"syscall"
	"time"
)

// softKillProcessGroup is the process-group analogue of softKillProcess.
// It sends SIGTERM to the entire group (-pgid), polls every 5ms up to grace
// for the group to drain, and escalates to SIGKILL if any process in the
// group is still alive at the deadline. Returns true iff SIGKILL was
// ultimately used. An empty group (ESRCH on initial SIGTERM) is treated as
// already-dead and returns false without escalation.
func softKillProcessGroup(pgid int, grace time.Duration) bool {
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return false
		}
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return true
	}

	const pollInterval = 5 * time.Millisecond
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if err := syscall.Kill(-pgid, 0); err != nil && errors.Is(err, syscall.ESRCH) {
			return false
		}
	}

	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	return true
}
