package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPathsReportsUnformattedGoFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	formatted := filepath.Join(directory, "formatted.go")
	unformatted := filepath.Join(directory, "unformatted.go")
	require.NoError(t, os.WriteFile(formatted, []byte("package sample\n"), 0o600))
	require.NoError(t, os.WriteFile(unformatted, []byte("package sample\n\nfunc f( ){ }\n"), 0o600))

	files, err := checkPaths([]string{directory})
	require.NoError(t, err)
	assert.Equal(t, []string{unformatted}, files)
}
