package main

import (
	"os"
	"runtime"
	"strings"
)

func testAwareHomeDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" &&
		(runtime.GOOS != "windows" || os.Getenv("AGENTDECK_TEST_USE_HOME") == "1") {
		return home, nil
	}
	return os.UserHomeDir()
}
