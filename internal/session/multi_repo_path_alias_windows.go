//go:build windows

package session

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func createMultiRepoPathAliasPlatform(source, target string) error {
	if err := os.Symlink(source, target); err == nil {
		return nil
	}

	info, statErr := os.Stat(source)
	if statErr != nil || !info.IsDir() {
		return os.Symlink(source, target)
	}

	const script = `$ErrorActionPreference = 'Stop'; New-Item -ItemType Junction -Path $env:AGENT_DECK_JUNCTION_TARGET -Target $env:AGENT_DECK_JUNCTION_SOURCE | Out-Null`
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(),
		"AGENT_DECK_JUNCTION_TARGET="+target,
		"AGENT_DECK_JUNCTION_SOURCE="+source,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("junction creation failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
