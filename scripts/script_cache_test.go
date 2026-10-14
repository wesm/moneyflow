package scripts

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// trackScript reads a script that a test runs as a subprocess. Go's test cache records only the
// files the test process itself opens, so without this read an edited script could reuse a
// cached pass.
func trackScript(t *testing.T, path string) {
	t.Helper()
	_, err := os.ReadFile(path) //nolint:gosec // Fixed repository script.
	require.NoError(t, err)
}
