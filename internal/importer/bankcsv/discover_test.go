package bankcsv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverFiltersAndSortsButAcceptsAnExplicitFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "nested"), 0o700))
	for _, name := range []string{"nested/Chase-b.csv", "Chase-a.csv", "other.csv"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte(chaseHeader), 0o600))
	}
	mapping, err := Lookup("chase_credit")
	require.NoError(t, err)
	files, err := Discover(t.Context(), root, mapping, ProductionLimits)
	require.NoError(t, err)
	require.Len(t, files, 2)
	assert.Equal(t, "Chase-a.csv", files[0].RelativeName)
	assert.Equal(t, filepath.Join("nested", "Chase-b.csv"), files[1].RelativeName)
	files, err = Discover(t.Context(), filepath.Join(root, "other.csv"), mapping, ProductionLimits)
	require.NoError(t, err)
	assert.Len(t, files, 1)
	limits := ProductionLimits
	limits.Files = 1
	_, err = Discover(t.Context(), root, mapping, limits)
	require.Error(t, err)
}
