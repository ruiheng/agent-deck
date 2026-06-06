//go:build windows

package session

import (
	"fmt"
	"os"
	"os/exec"
)

func (r *SSHRunner) attachWithPTY(remoteCmd string) error {
	cmd := exec.Command("ssh", r.windowsAttachArgs(remoteCmd)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh attach failed: %w", err)
	}
	return nil
}
