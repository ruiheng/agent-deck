//go:build !windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/logging"
)

func installCrashDumpSignalHandler(baseDir string) {
	usr1Chan := make(chan os.Signal, 1)
	signal.Notify(usr1Chan, syscall.SIGUSR1)
	go func() {
		for range usr1Chan {
			dumpPath := filepath.Join(baseDir, fmt.Sprintf("crash-dump-%d.jsonl", time.Now().Unix()))
			if err := logging.DumpRingBuffer(dumpPath); err != nil {
				logging.ForComponent(logging.CompUI).Error("crash_dump_failed",
					slog.String("error", err.Error()))
			} else {
				logging.ForComponent(logging.CompUI).Info("crash_dump_written",
					slog.String("path", dumpPath))
			}
		}
	}()
}

func drainStdinPlatform(fd int) {
	const (
		tcflshDarwin = 0x80047410
		tcflshLinux  = 0x540B
		tciflush     = 0
	)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tcflshDarwin, tciflush)
	if errno != 0 {
		_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), tcflshLinux, tciflush)
	}
}
