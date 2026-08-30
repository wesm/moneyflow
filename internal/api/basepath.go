package api

import "github.com/wesm/moneyflow/internal/httpsecurity"

// NormalizeBasePath returns one leading and trailing slash for a safe mount path.
func NormalizeBasePath(input string) (string, error) {
	return httpsecurity.NormalizeBasePath(input)
}
