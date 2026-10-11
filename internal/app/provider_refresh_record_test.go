package app_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// refreshRecordProfile fails to write the advisory full-refresh record while failing is set.
type refreshRecordProfile struct {
	store.Profile
	failing *atomic.Bool
}

func (profile refreshRecordProfile) RecordFullProviderRefresh(at time.Time) error {
	if profile.failing.Load() {
		return errors.New("record full provider refresh in example/provider-refresh.json: disk full")
	}
	return profile.Profile.RecordFullProviderRefresh(at)
}

func TestFullRefreshRecordFailureCommitsTheRefreshAndWarns(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	handle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handle.Close()) })
	failing := &atomic.Bool{}
	failing.Store(true)
	service, err := app.NewProfileService(ctx, refreshRecordProfile{Profile: handle, failing: failing})
	require.NoError(t, err)
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: monarchTestIdentity(), snapshot: providerSnapshot(t, now, 2), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	result, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err, "the committed refresh stands")
	assert.Equal(t, now, result.Status.LastSuccess)
	assert.Equal(t, 2, result.Status.Summary.ImportedTransactions)
	for _, warning := range []string{result.Status.Warning, providerStatusWarning(t, service)} {
		assert.Contains(t, warning, "example/provider-refresh.json: disk full")
		assert.Contains(t, warning, "next check will download complete history")
	}
	status, err := service.ProviderStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, app.ProviderRefreshFull, status.RefreshDue)

	failing.Store(false)
	result, err = service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)
	assert.Empty(t, result.Status.Warning)
	assert.Empty(t, providerStatusWarning(t, service), "a recorded full refresh clears the warning")
}

func providerStatusWarning(t *testing.T, service *app.Service) string {
	t.Helper()
	status, err := service.ProviderStatus(t.Context())
	require.NoError(t, err)
	return status.Warning
}
