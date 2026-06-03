//go:build !windows

package session

import "os"

func replaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
