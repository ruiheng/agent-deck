//go:build !windows

package session

import (
	"os"
	"syscall"
)

func lockHermesConfigFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func unlockHermesConfigFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
