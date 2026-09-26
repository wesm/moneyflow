//go:build !windows

package home

import "os"

func openLockFile(root *os.Root, _ string, filename string) (*os.File, error) {
	return root.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
}
