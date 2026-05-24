//go:build windows

package tmux

import "fmt"

func WriteBadgeUpdate(tmuxSessionName, title string) error {
	if tmuxSessionName == "" {
		return fmt.Errorf("badge update: empty tmux session name")
	}
	return nil
}
