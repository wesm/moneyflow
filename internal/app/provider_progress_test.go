package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

// progressStepSource replays per-partition progress updates and reads provider status after each.
type progressStepSource struct {
	identity provider.ProfileIdentity
	snapshot domain.ImportSnapshot
	steps    []provider.Progress
	observe  func(provider.Progress)
}

func (source *progressStepSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return source, "session-a", nil
}

func (source *progressStepSource) Changed(provider.SessionFingerprint) (bool, error) {
	return false, nil
}

func (source *progressStepSource) FetchSnapshot(
	_ context.Context,
	_ provider.FetchRequest,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	for _, step := range source.steps {
		progress(step)
		source.observe(step)
	}
	return provider.SnapshotResult{Identity: source.identity, Snapshot: source.snapshot.Clone()}, nil
}

type observedProgress struct {
	Fetched   int
	Total     int
	Verifying bool
	Percent   int
}

// Monarch reads visible transactions, then hidden ones, and then reads both again to confirm
// nothing changed. Status reports one count per read, so the hidden partition continues the
// visible count instead of starting over, and the percentage never moves backward.
func TestProviderStatusCountsOneReadAcrossPartitionsAndPasses(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	read := func(partition string, pass, fetched, total int) provider.Progress {
		return provider.Progress{
			Partition: partition, Fetched: fetched, Total: total, Attempt: 1, Pass: pass, Passes: 2,
		}
	}
	source := &progressStepSource{
		identity: monarchTestIdentity(),
		snapshot: providerSnapshot(t, now, 2),
		steps: []provider.Progress{
			read("visible", 1, 1000, 3000),
			read("visible", 1, 3000, 3000),
			read("hidden", 1, 500, 1500),
			read("hidden", 1, 1500, 1500),
			read("visible", 2, 1000, 3000),
			read("visible", 2, 3000, 3000),
			read("hidden", 2, 1500, 1500),
			{Partition: "visible", Fetched: 10, Total: 3000, Attempt: 2, Pass: 1, Passes: 2},
		},
	}
	var observed []observedProgress
	source.observe = func(provider.Progress) {
		status, err := service.ProviderStatus(ctx)
		require.NoError(t, err)
		observed = append(observed, observedProgress{
			Fetched: status.Fetched, Total: status.Total,
			Verifying: status.Verifying, Percent: status.Percent,
		})
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	_, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)

	assert.Equal(t, []observedProgress{
		{Fetched: 1000, Total: 3000, Percent: 16},
		{Fetched: 3000, Total: 3000, Percent: 50},
		// The hidden total arrives with its first page; the count continues and the bar holds.
		{Fetched: 3500, Total: 4500, Percent: 50},
		{Fetched: 4500, Total: 4500, Percent: 50},
		// The confirming read counts against the first read's complete total.
		{Fetched: 1000, Total: 4500, Verifying: true, Percent: 61},
		{Fetched: 3000, Total: 4500, Verifying: true, Percent: 83},
		{Fetched: 4500, Total: 4500, Verifying: true, Percent: 100},
		// A new attempt reads changed data from the start, so its count starts over.
		{Fetched: 10, Total: 3000, Percent: 0},
	}, observed)
}

// A provider that reads once, such as YNAB, reports its whole read as one complete pass.
func TestProviderStatusCountsASingleReadProvider(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 16, 9, 0, 0, 0, time.UTC)
	source := &progressStepSource{
		identity: monarchTestIdentity(),
		snapshot: providerSnapshot(t, now, 2),
		steps:    []provider.Progress{{Partition: "all", Fetched: 2, Total: 2, Attempt: 1}},
	}
	var observed []observedProgress
	source.observe = func(provider.Progress) {
		status, err := service.ProviderStatus(ctx)
		require.NoError(t, err)
		observed = append(observed, observedProgress{
			Fetched: status.Fetched, Total: status.Total,
			Verifying: status.Verifying, Percent: status.Percent,
		})
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	_, err := service.RefreshProvider(ctx, manualProviderRefresh())
	require.NoError(t, err)

	assert.Equal(t, []observedProgress{{Fetched: 2, Total: 2, Percent: 100}}, observed)
}
