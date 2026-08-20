package sqlite

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

const editingPerformanceRows = 100_000

func TestColdProfilePerformance100K(t *testing.T) {
	skipEditingPerformance(t)
	ctx := context.Background()
	paths := createPerformanceProfile(t)

	started := time.Now()
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	_, err = app.Replay(snapshot)
	require.NoError(t, err)
	require.NoError(t, profile.Close())
	duration := time.Since(started)
	t.Logf("cold open, load, and replay: %s", duration)
	require.Less(t, duration, time.Second)
}

func TestBulkEditingPerformance100K(t *testing.T) {
	skipEditingPerformance(t)
	ctx := context.Background()
	paths := createPerformanceProfile(t)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	targets := transactionTargets(snapshot.Committed)
	revision := snapshot.Revision

	revision = requireEditingDuration(t, "append", func() (uint64, error) {
		return profile.Append(ctx, revision, performanceHideOperation("bulk_append", revision, targets))
	})
	revision = requireEditingDuration(t, "undo", func() (uint64, error) {
		return profile.MoveCursor(ctx, revision, -1)
	})
	revision = requireEditingDuration(t, "redo", func() (uint64, error) {
		return profile.MoveCursor(ctx, revision, 1)
	})
	revision = requireEditingDuration(t, "hide cancellation", func() (uint64, error) {
		return profile.CancelHide(ctx, revision, targets)
	})
	revision, err = profile.Append(
		ctx, revision, performanceHideOperation("bulk_fold", revision, targets),
	)
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	effective, err := app.Replay(loaded)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(effective, revision)
	require.NoError(t, err)
	_ = requireEditingDuration(t, "fold", func() (uint64, error) {
		return profile.Fold(ctx, revision, plan)
	})
}

func TestAmazonImport100KPerformance(t *testing.T) {
	skipEditingPerformance(t)
	ctx := context.Background()
	paths := createPerformanceProfile(t)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	transactions, err := snapshot.Committed.MaterializeTransactions()
	require.NoError(t, err)
	now := time.Date(2026, time.August, 20, 18, 0, 0, 0, time.UTC)

	started := time.Now()
	commit, err := profile.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-performance", StartedAt: now, ImportedAt: now,
		CandidateDigest: strings.Repeat("a", 64),
		ProposedCounts:  store.AmazonIDCounts{Sources: len(transactions)},
	}, func(_ store.AmazonImportState, proposed store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		items := make([]store.AmazonOrderItem, len(transactions))
		for index, transaction := range transactions {
			items[index] = store.AmazonOrderItem{
				LocalTransactionID: domain.EntityID(transaction.ID), SourceIdentity: proposed.SourceIdentities[index],
				OrderID: "order-" + fmt.Sprint(index/4), ASIN: "ASIN-" + fmt.Sprint(index),
				ProductName: transaction.Merchant.Name, OrderDate: transaction.Date,
				Quantity: 1, AmountMinor: transaction.Amount.Minor,
				Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale,
				OrderStatus: "Closed", ShipmentStatus: "Delivered",
				IdentityFingerprint: strings.Repeat("b", 64),
				FullFingerprint:     strings.Repeat("c", 64),
			}
		}
		return store.AmazonImportPlan{
			Committed: snapshot.Committed.Clone(), Journal: []domain.Operation{},
			KnownDrills: []domain.DrillIdentity{},
			Settings:    &store.AmazonSettings{Currency: "USD", Scale: 2, CreatedAt: now},
			Items:       items, History: store.AmazonImportHistory{
				FileCount: 1, LogicalRecordCount: len(items), InsertedCount: len(items),
			}, SemanticChange: true,
		}, nil
	})
	duration := time.Since(started)
	require.NoError(t, err)
	require.True(t, commit.SemanticChange)
	t.Logf("100k Amazon SQLite fold: %s", duration)
	require.Less(t, duration, 10*time.Second)
}

func BenchmarkColdProfile100K(b *testing.B) {
	ctx := context.Background()
	paths := createPerformanceProfile(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		profileStore, err := Open(ctx, paths, DefaultOptions)
		if err != nil {
			b.Fatal(err)
		}
		profile := profileStore.(*profile)
		snapshot, err := profile.Load(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if _, err = app.Replay(snapshot); err != nil {
			b.Fatal(err)
		}
		if err = profile.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBulk100K(b *testing.B) {
	benchmarkBulkAppend(b)
	benchmarkBulkCursor(b, "Undo", -1)
	benchmarkBulkCursor(b, "Redo", 1)
	benchmarkBulkCancel(b)
	benchmarkBulkFold(b)
}

func benchmarkBulkAppend(b *testing.B) {
	ctx, profile, targets, revision := openPerformanceProfile(b)
	defer func() { _ = profile.Close() }()
	b.Run("Append", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			operation := performanceHideOperation(fmt.Sprintf("bulk_append_%d", revision), revision, targets)
			var err error
			revision, err = profile.Append(ctx, revision, operation)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkBulkCursor(b *testing.B, name string, direction int) {
	ctx, profile, targets, revision := openPerformanceProfile(b)
	defer func() { _ = profile.Close() }()
	var err error
	revision, err = profile.Append(ctx, revision, performanceHideOperation("bulk_cursor", revision, targets))
	if err != nil {
		b.Fatal(err)
	}
	if direction > 0 {
		revision, err = profile.MoveCursor(ctx, revision, -1)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.Run(name, func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			b.StartTimer()
			revision, err = profile.MoveCursor(ctx, revision, direction)
			b.StopTimer()
			if err != nil {
				b.Fatal(err)
			}
			revision, err = profile.MoveCursor(ctx, revision, -direction)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkBulkCancel(b *testing.B) {
	ctx, profile, targets, revision := openPerformanceProfile(b)
	defer func() { _ = profile.Close() }()
	b.Run("CancelHide", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			var err error
			b.StopTimer()
			revision, err = profile.Append(ctx, revision, performanceHideOperation(
				fmt.Sprintf("bulk_cancel_%d", revision), revision, targets,
			))
			b.StartTimer()
			if err != nil {
				b.Fatal(err)
			}
			revision, err = profile.CancelHide(ctx, revision, targets)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkBulkFold(b *testing.B) {
	b.Run("Fold", func(b *testing.B) {
		b.ReportAllocs()
		for index := range b.N {
			b.StopTimer()
			ctx, profile, targets, revision := openPerformanceProfile(b)
			var err error
			revision, err = profile.Append(ctx, revision, performanceHideOperation(
				fmt.Sprintf("bulk_fold_%d", index), revision, targets,
			))
			if err != nil {
				b.Fatal(err)
			}
			snapshot, err := profile.Load(ctx)
			if err != nil {
				b.Fatal(err)
			}
			effective, err := app.Replay(snapshot)
			if err != nil {
				b.Fatal(err)
			}
			plan, err := app.BuildFoldPlan(effective, revision)
			if err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			_, err = profile.Fold(ctx, revision, plan)
			b.StopTimer()
			if err != nil {
				b.Fatal(err)
			}
			if err = profile.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func createPerformanceProfile(tb testing.TB) home.Paths {
	tb.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(tb.TempDir()+"/profile", nil, "")
	require.NoError(tb, err)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(tb, err)
	committed, err := fixture.CommittedProfile(fixture.Generate(20260814, editingPerformanceRows))
	require.NoError(tb, err)
	_, err = profileStore.CreateSeededProfile(ctx, committed)
	require.NoError(tb, err)
	require.NoError(tb, profileStore.Close())
	return paths
}

func openPerformanceProfile(tb testing.TB) (context.Context, *profile, []domain.EntityID, uint64) {
	tb.Helper()
	ctx := context.Background()
	paths := createPerformanceProfile(tb)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(tb, err)
	profile := profileStore.(*profile)
	snapshot, err := profile.Load(ctx)
	require.NoError(tb, err)
	return ctx, profile, transactionTargets(snapshot.Committed), snapshot.Revision
}

func transactionTargets(committed domain.CommittedProfile) []domain.EntityID {
	targets := make([]domain.EntityID, len(committed.Transactions))
	for index := range committed.Transactions {
		targets[index] = committed.Transactions[index].ID
	}
	return targets
}

func performanceHideOperation(
	id string, revision uint64, targets []domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: id, Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: revision,
		CreatedAt:       time.Date(2026, time.August, 14, 13, 0, 0, 0, time.UTC),
		Targets:         targets,
		HideToggle:      &domain.HideTogglePayload{},
	}
}

func requireEditingDuration(
	t *testing.T, name string, operation func() (uint64, error),
) uint64 {
	t.Helper()
	started := time.Now()
	revision, err := operation()
	require.NoError(t, err)
	duration := time.Since(started)
	t.Logf("%s: %s", name, duration)
	require.Less(t, duration, 15*time.Second, "%s exceeded regression budget", name)
	return revision
}

func skipEditingPerformance(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}
}
