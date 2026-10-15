package sqlite

import (
	"context"
	"fmt"
	"os"
	"runtime"
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

// coldProfileLimit leaves room for GitHub's macOS runners, which took 1.06-1.56 s for a cold
// open that takes about 0.37 s on a developer laptop.
const coldProfileLimit = 2 * time.Second

// windowsFoldLimit leaves room for GitHub's Windows runners, where the fold has a long tail: 27 of
// 29 runs took 6.7-12.9 s, and two took 19.4 s and 119.1 s while the other steps in those jobs ran
// at their usual speed. It is about 1.5 times the slowest run. Other systems keep the 15 s limit.
const windowsFoldLimit = 3 * time.Minute

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
	require.Less(t, duration, coldProfileLimit)
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
	paths := temporaryPaths(t)
	profileStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	candidateCommitted, err := fixture.CommittedProfile(fixture.Generate(20260814, editingPerformanceRows))
	require.NoError(t, err)
	transactions, err := candidateCommitted.MaterializeTransactions()
	require.NoError(t, err)
	now := time.Date(2026, time.August, 20, 18, 0, 0, 0, time.UTC)

	started := time.Now()
	commit, err := profile.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-performance", StartedAt: now, ImportedAt: now,
		CandidateDigest: strings.Repeat("a", 64),
		ProposedCounts:  store.AmazonIDCounts{Sources: len(transactions)},
	}, func(_ store.AmazonImportState, proposed store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		items := make([]store.AmazonOrderItem, len(transactions))
		committed := candidateCommitted.Clone()
		for index, transaction := range transactions {
			committed.Transactions[index].Provider = "amazon"
			committed.Transactions[index].ProviderID = proposed.SourceIdentities[index]
			committed.Transactions[index].Amount.Currency = "USD"
			committed.Transactions[index].Amount.Scale = 2
			committed.ExternalIdentities = append(committed.ExternalIdentities, domain.ExternalIdentity{
				EntityType: domain.EntityKindTransaction, EntityID: domain.EntityID(transaction.ID),
				Namespace: "amazon/order-item", ExternalID: proposed.SourceIdentities[index],
			})
			items[index] = store.AmazonOrderItem{
				LocalTransactionID: domain.EntityID(transaction.ID), SourceIdentity: proposed.SourceIdentities[index],
				OrderID: "order-" + fmt.Sprint(index/4), ASIN: "ASIN-" + fmt.Sprint(index),
				ProductName: transaction.Merchant.Name, OrderDate: transaction.Date,
				Quantity: 1, AmountMinor: transaction.Amount.Minor,
				Currency: "USD", Scale: 2,
				OrderStatus: "Closed", ShipmentStatus: "Delivered",
				IdentityFingerprint: strings.Repeat("b", 64),
				FullFingerprint:     strings.Repeat("c", 64),
				LocalAccountID:      domain.EntityID(transaction.Account.ID),
				LocalMerchantID:     domain.EntityID(transaction.Merchant.ID),
				LocalCategoryID:     domain.EntityID(transaction.Category.ID),
				LocalNotes:          transaction.Notes, LocalHidden: transaction.Hidden,
			}
		}
		return store.AmazonImportPlan{
			Committed: committed, Journal: []domain.Operation{},
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
	limit := 15 * time.Second
	if name == "fold" && runtime.GOOS == "windows" {
		limit = windowsFoldLimit
	}
	require.Less(t, duration, limit, "%s exceeded regression budget", name)
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
