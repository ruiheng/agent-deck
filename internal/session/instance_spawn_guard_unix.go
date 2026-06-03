//go:build !windows

package session

import (
	"os"
	"syscall"
)

func instanceSpawnLockPIDAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
