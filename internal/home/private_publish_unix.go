//go:build !windows

package home

import "os"

func publishPrivateNoReplace(source string, destination string) error {
	if err := os.Link(source, destination); err != nil {
		return err
	}
	return os.Remove(source)
}
