//go:build !windows

package session

import "os"

func createMultiRepoPathAliasPlatform(source, target string) error {
	return os.Symlink(source, target)
}
