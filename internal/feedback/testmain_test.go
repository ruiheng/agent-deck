package feedback_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Setenv("AGENTDECK_TEST_USE_HOME", "1")
	os.Exit(m.Run())
}
