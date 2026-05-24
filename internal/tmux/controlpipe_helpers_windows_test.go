//go:build windows

package tmux

import (
	"fmt"
	"os"
	"testing"
)

func createTestSession(t *testing.T, suffix string) string {
	t.Helper()
	t.Skip("psmux does not provide reliable Unix tmux control-mode behavior for this test")
	return ""
}

func createTestSessionStrict(t *testing.T, suffix string) string {
	t.Helper()
	t.Skip("psmux does not provide reliable Unix tmux control-mode behavior for this test")
	return ""
}

func runOrphanControlClientHelper(sessionName string) {
	fmt.Fprintln(os.Stderr, "ORPHAN_CONTROL_CLIENT_HELPER is unsupported on Windows")
	os.Exit(2)
}
