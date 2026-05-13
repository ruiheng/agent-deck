package update

import (
	"os"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/testutil"
)

func TestMain(m *testing.M) {
	cleanupTmux := testutil.IsolateTmuxSocket()
	defer cleanupTmux()

	cleanupHome := testutil.IsolateHome("agentdeck-update-home-")
	defer cleanupHome()

	os.Exit(m.Run())
}
