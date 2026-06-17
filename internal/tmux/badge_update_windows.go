//go:build windows

package tmux

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/agentpaths"
)

const badgeUpdatesDirEnv = "AGENTDECK_BADGE_UPDATES_DIR"

func BadgeUpdatesDir() string {
	if v := strings.TrimSpace(os.Getenv(badgeUpdatesDirEnv)); v != "" {
		return v
	}
	dir, err := agentpaths.EffectiveDataPath("badge-updates", "badge-updates")
	if err != nil {
		return filepath.Join(os.TempDir(), "agent-deck", "badge-updates")
	}
	return dir
}

func WriteBadgeUpdate(tmuxSessionName, title string) error {
	if tmuxSessionName == "" {
		return fmt.Errorf("badge update: empty tmux session name")
	}
	return nil
}
