package amazon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverDirectoryReturnsCanonicalAmazonFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0o700))
	writeSource(t, filepath.Join(root, "nested", "Retail.OrderHistory.2.csv"), "two")
	writeSource(t, filepath.Join(root, "Retail.OrderHistory.1.csv"), "one")
	writeSource(t, filepath.Join(root, "ignored.csv"), "ignored")

	files, err := DiscoverDirectory(context.Background(), root, ProductionLimits)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "Retail.OrderHistory.1.csv", files[0].RelativeName)
	assert.Equal(t, "nested/Retail.OrderHistory.2.csv", files[1].RelativeName)
}

func TestDiscoverDirectoryRejectsDuplicateContents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "Retail.OrderHistory.1.csv"), "same")
	writeSource(t, filepath.Join(root, "Retail.OrderHistory.2.csv"), "same")

	_, err := DiscoverDirectory(context.Background(), root, ProductionLimits)
	assert.ErrorIs(t, err, ErrInvalid)
}

func TestDiscoverDirectoryEnforcesFileLimitBeforeReturning(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, filepath.Join(root, "Retail.OrderHistory.1.csv"), "one")
	writeSource(t, filepath.Join(root, "Retail.OrderHistory.2.csv"), "two")

	limits := ProductionLimits
	limits.Files = 1
	_, err := DiscoverDirectory(context.Background(), root, limits)
	assert.ErrorIs(t, err, ErrTooLarge)
}

func writeSource(t *testing.T, path string, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}
