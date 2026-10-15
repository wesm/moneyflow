package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const performanceTransactionCount = 100_000

var performanceProjectionBytes []byte

func TestProjectionPerformance100K(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}

	service, err := app.NewService(fixture.Generate(20260813, performanceTransactionCount))
	require.NoError(t, err)
	_, err = projectAndEncode100K(service)
	require.NoError(t, err)

	start := time.Now()
	encoded, err := projectAndEncode100K(service)
	duration := time.Since(start)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	require.Less(t, duration, time.Second,
		"URL decode, query, 200-row projection, and JSON encoding took %s", duration)
}

// TestSelectionTogglePerformance100K keeps a Space toggle at the cost of about one view
// projection. The toggle has to evaluate the view to check its target and project the new
// selection, but evaluating it again for each step made a toggle cost three projections.
// After Select all, the selection names all rows of the view. Resolving that name in each step
// evaluated the view again, so such a toggle cost about two projections of the same view and
// selection. It now reads the rows the toggle evaluated and must cost less than one and a half.
func TestSelectionTogglePerformance100K(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}

	service, err := app.NewService(fixture.Generate(20260813, performanceTransactionCount))
	require.NoError(t, err)
	state, _, err := DecodeViewQuery("mode=detail&v=1")
	require.NoError(t, err)
	window := app.WindowRequest{Limit: app.DefaultWindowLimit}
	view, err := service.ProjectView(state, app.EmptySelection(), window)
	require.NoError(t, err)
	// Every tenth generated transaction is a transfer, which the default view hides.
	require.Equal(t, performanceTransactionCount*9/10, view.TotalRows)
	target := app.RowTarget{Kind: app.IdentityTransaction, Identity: view.DetailRows[0].Identity}
	toggle := app.TransitionRequest{Action: app.ActionToggleSelection, Target: &target}

	// Alternating the two operations exposes both to the same host load, so load that changes
	// during the test cannot skew their ratio. Each keeps its fastest run.
	fastestPair := func(first, second func() error) (time.Duration, time.Duration) {
		timed := func(run func() error) time.Duration {
			// Collecting first keeps one run's garbage from being collected inside the next.
			runtime.GC()
			start := time.Now()
			require.NoError(t, run())
			return time.Since(start)
		}
		bestFirst, bestSecond := time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)
		for range 7 {
			bestFirst = min(bestFirst, timed(first))
			bestSecond = min(bestSecond, timed(second))
		}
		return bestFirst, bestSecond
	}
	selection := app.EmptySelection()
	projected, toggled := fastestPair(func() error {
		_, err := service.ProjectView(state, app.EmptySelection(), window)
		return err
	}, func() error {
		_, next, projection, err := service.TransitionView(state, selection, toggle, window)
		if err == nil && projection.SelectionCount > 1 {
			err = fmt.Errorf("selection count %d", projection.SelectionCount)
		}
		selection = next
		return err
	})
	require.Less(t, toggled, 2*projected,
		"a 100k-row toggle took %s; projecting the same view took %s", toggled, projected)

	_, all, _, err := service.TransitionView(
		state, app.EmptySelection(), app.TransitionRequest{Action: app.ActionToggleSelectAll}, window,
	)
	require.NoError(t, err)
	selection = all
	projectedAll, toggledAfterAll := fastestPair(func() error {
		_, err := service.ProjectView(state, all, window)
		return err
	}, func() error {
		_, next, projection, err := service.TransitionView(state, selection, toggle, window)
		if err == nil && projection.SelectionCount < view.TotalRows-1 {
			err = fmt.Errorf("selection count %d", projection.SelectionCount)
		}
		selection = next
		return err
	})
	require.Less(t, toggledAfterAll, projectedAll*3/2,
		"a 100k-row toggle after Select all took %s; projecting the same view and selection "+
			"took %s", toggledAfterAll, projectedAll)
}

func BenchmarkProjection100K(b *testing.B) {
	service, err := app.NewService(fixture.Generate(20260813, performanceTransactionCount))
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		performanceProjectionBytes, err = projectAndEncode100K(service)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestDuplicateProjectionPerformance100K(t *testing.T) {
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}

	service := duplicatePerformanceService(t, duplicatePerformanceTransactions(performanceTransactionCount))
	var err error
	_, err = projectAndEncodeDuplicates100K(service)
	require.NoError(t, err)

	start := time.Now()
	encoded, err := projectAndEncodeDuplicates100K(service)
	duration := time.Since(start)
	require.NoError(t, err)
	require.Less(t, duration, time.Second, "100k duplicate projection took %s", duration)
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	assert.Equal(t, "0b3e040e7e1c82592cc877657556d3eab73e2f5de1adef1561c9ba3b8048f742", digest)
}

func projectAndEncode100K(service *app.Service) ([]byte, error) {
	state, canonical, err := DecodeViewQuery("v=1")
	if err != nil {
		return nil, err
	}
	projection, err := service.ProjectView(
		state,
		app.EmptySelection(),
		app.WindowRequest{Limit: app.DefaultWindowLimit},
	)
	if err != nil {
		return nil, err
	}
	return json.Marshal(projectionToWire(projection, canonical, nil))
}

func projectAndEncodeDuplicates100K(service *app.Service) ([]byte, error) {
	state := app.DefaultViewState()
	state.Current.ShowTransfers = true
	projection, err := service.ProjectDuplicates(
		context.Background(), service.Revision(), state, app.EmptySelection(),
		app.DuplicateWindowRequest{GroupLimit: 200, RowLimit: 200},
	)
	if err != nil {
		return nil, err
	}
	if projection.TotalGroups != 50_000 || projection.TotalTransactions != 100_000 ||
		projection.GroupWindow.Count != 200 || projection.RowWindow.Count != 200 ||
		len(projection.Groups) != 100 {
		return nil, fmt.Errorf(
			"unexpected duplicate bounds: groups=%d transactions=%d group-window=%d row-window=%d projected-groups=%d",
			projection.TotalGroups, projection.TotalTransactions, projection.GroupWindow.Count,
			projection.RowWindow.Count, len(projection.Groups),
		)
	}
	return json.Marshal(duplicateToWire(projection, "v=1"))
}

func duplicatePerformanceTransactions(count int) []domain.Transaction {
	base := fixture.Generate(20260818, 1)[0]
	transactions := make([]domain.Transaction, count)
	for index := 0; index < count; index++ {
		group := index / 2
		transaction := base.Clone()
		transaction.ID = fmt.Sprintf("duplicate-%06d", index)
		transaction.ProviderID = fmt.Sprintf("provider-duplicate-%06d", index)
		transaction.Amount.Minor = -int64(group + 1)
		transactions[index] = transaction
	}
	return transactions
}

func duplicatePerformanceService(
	t testing.TB,
	transactions []domain.Transaction,
) *app.Service {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	return service
}
