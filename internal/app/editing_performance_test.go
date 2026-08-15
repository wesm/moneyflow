package app_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
)

const editingReplayRows = 100_000

func TestBulkEditingPerformance100KReplay(t *testing.T) {
	if testing.Short() || os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke is skipped for short or instrumented test runs")
	}
	snapshot := editingPerformanceSnapshot(t)
	started := time.Now()
	effective, err := app.Replay(snapshot)
	require.NoError(t, err)
	_, err = app.BuildFoldPlan(effective, snapshot.Revision)
	require.NoError(t, err)
	duration := time.Since(started)
	t.Logf("100k replay and fold-plan build: %s", duration)
	require.Less(t, duration, time.Second)
}

func BenchmarkBulkReplay100K(b *testing.B) {
	snapshot := editingPerformanceSnapshot(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		effective, err := app.Replay(snapshot)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = app.BuildFoldPlan(effective, snapshot.Revision); err != nil {
			b.Fatal(err)
		}
	}
}

func editingPerformanceSnapshot(tb testing.TB) domain.ProfileSnapshot {
	tb.Helper()
	committed, err := fixture.CommittedProfile(fixture.Generate(20260814, editingReplayRows))
	require.NoError(tb, err)
	targets := make([]domain.EntityID, len(committed.Transactions))
	for index := range committed.Transactions {
		targets[index] = committed.Transactions[index].ID
	}
	operation := domain.Operation{
		ID: "bulk_replay", Sequence: 1, Type: domain.OperationTransactionHide,
		PayloadVersion: 1, CreatedRevision: 1,
		CreatedAt: time.Date(2026, time.August, 14, 13, 0, 0, 0, time.UTC),
		Targets:   targets, HideToggle: &domain.HideTogglePayload{},
	}
	snapshot := domain.ProfileSnapshot{
		Revision: 2, Cursor: 1, Committed: committed, Journal: []domain.Operation{operation},
	}
	require.NoError(tb, snapshot.Validate())
	return snapshot
}
