package docker

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireSymlink(t *testing.T, oldname, newname string) {
	t.Helper()

	err := os.Symlink(oldname, newname)
	if err != nil && runtime.GOOS == "windows" && strings.Contains(strings.ToLower(err.Error()), "privilege") {
		t.Skipf("symlink creation requires Windows Developer Mode or elevated privileges: %v", err)
	}
	require.NoError(t, err)
}
