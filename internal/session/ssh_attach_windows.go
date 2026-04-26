//go:build windows

package session

import "fmt"

func (r *SSHRunner) attachWithPTY(remoteCmd string) error {
	return fmt.Errorf("pty attach is not used on Windows")
}
