//go:build !windows

package home

import "os"

func publishPrivateNoReplace(source string, destination string) (bool, error) {
	if err := os.Link(source, destination); err != nil {
		return false, err
	}
	return true, os.Remove(source)
}
