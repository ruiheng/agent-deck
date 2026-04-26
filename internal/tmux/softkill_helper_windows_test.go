//go:build windows

package tmux

import "os"

func runSoftkillHelper(role string) {
	os.Exit(2)
}
