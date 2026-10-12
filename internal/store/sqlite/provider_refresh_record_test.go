package sqlite

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFullProviderRefreshRecordIsAnAdvisoryPrivateFile(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	profileStore, err := Open(t.Context(), paths, DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profileStore.Close()) })
	assert.True(t, profileStore.LastFullProviderRefresh().IsZero(), "a missing record is unknown")

	at := time.Date(2026, time.August, 15, 12, 0, 0, 123_000_000, time.UTC)
	require.NoError(t, profileStore.RecordFullProviderRefresh(at))
	assert.Equal(t, at, profileStore.LastFullProviderRefresh())
	record := filepath.Join(paths.Root, "provider-refresh.json")
	info, err := os.Stat(record)
	require.NoError(t, err)
	if os.PathSeparator == '/' {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	require.NoError(t, os.WriteFile(record, []byte(`{"last_full_refresh":"yesterday"}`), 0o600))
	assert.True(t, profileStore.LastFullProviderRefresh().IsZero(), "a corrupt record is unknown")
}

func TestFullProviderRefreshRecordFailureNamesThePath(t *testing.T) {
	t.Parallel()

	paths := temporaryPaths(t)
	profileStore, err := Open(t.Context(), paths, DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profileStore.Close()) })
	record := filepath.Join(paths.Root, "provider-refresh.json")
	require.NoError(t, os.Mkdir(record, 0o700))

	err = profileStore.RecordFullProviderRefresh(time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), record)
}
