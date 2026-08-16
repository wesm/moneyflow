//go:build !windows && !darwin

package home

import "os"

func rejectExtendedACLPath(string) error   { return nil }
func rejectExtendedACLFile(*os.File) error { return nil }
