package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
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
		"URL decode, query, 200-row/chart projection, and JSON encoding took %s", duration)
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
