package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

const (
	amazonMatchingPerformanceRows      = 100_000
	amazonMatchingPerformanceQualified = 64
)

func TestAmazonMatching100KPerformance(t *testing.T) {
	requireAmazonPerformanceEnvironment(t)
	service, matcher, loader, qualified := amazonPerformanceService(t)
	for _, transaction := range qualified[:4] {
		projection, err := matcher.Match(context.Background(), transaction, "", 1)
		require.NoError(t, err)
		require.True(t, projection.Qualified)
		require.NotEmpty(t, projection.Result.Matches)
	}

	state := DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	start := time.Now()
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	duration := time.Since(start)
	require.NoError(t, err)
	require.Len(t, projection.DetailRows, DefaultWindowLimit)
	assert.Less(t, duration, time.Second, "100k Amazon bounded projection took %s", duration)
	assertAmazonSourceLoadedOnce(t, matcher, loader)
}

func TestAmazonSearch100KPerformance(t *testing.T) {
	requireAmazonPerformanceEnvironment(t)
	service, matcher, loader, qualified := amazonPerformanceService(t)
	session := NewSession()
	session.Mode = domain.ResultModeDetail
	session.Search = "unique performance product"

	coldStart := time.Now()
	projection, err := matcher.Match(context.Background(), qualified[0], "", 1)
	require.NoError(t, err)
	require.NotEmpty(t, projection.Result.Matches)
	t.Logf("100k Amazon cold source load and index: %s", time.Since(coldStart))

	start := time.Now()
	result, err := service.QueryContext(context.Background(), session)
	duration := time.Since(start)
	require.NoError(t, err)
	require.Len(t, result.DetailRows, amazonMatchingPerformanceQualified)
	assert.Less(t, duration, time.Second, "100k Amazon product search took %s", duration)
	assertAmazonSourceLoadedOnce(t, matcher, loader)
}

// assertAmazonSourceLoadedOnce proves that matching many qualified rows probes the
// persistent source revision repeatedly but materializes its 100k items only once.
func assertAmazonSourceLoadedOnce(t *testing.T, matcher *AmazonMatchingService, loader *sqliteAmazonLoader) {
	t.Helper()
	assert.Equal(t, 1, matcher.CacheBuilds(), "source index should be built once per revision")
	assert.Equal(t, int64(1), loader.materializations.Load(), "source items should load once per revision")
	assert.GreaterOrEqual(t, loader.probes.Load(), int64(2), "later batches should only probe the revision")
}

// sqliteAmazonLoader mirrors the production catalog loader against one SQLite profile.
type sqliteAmazonLoader struct {
	paths            home.Paths
	probes           atomic.Int64
	materializations atomic.Int64
}

func (loader *sqliteAmazonLoader) Load(
	ctx context.Context,
	_ AmazonSourceDescriptor,
	knownRevision uint64,
) (*store.AmazonMatchSourceState, func() error, error) {
	loader.probes.Add(1)
	currentRevision, err := sqlite.ProbeRevision(ctx, loader.paths, sqlite.DefaultOptions)
	if err != nil {
		return nil, nil, err
	}
	if knownRevision != 0 && currentRevision == knownRevision {
		return nil, func() error { return nil }, nil
	}
	loader.materializations.Add(1)
	profile, err := sqlite.Open(ctx, loader.paths, sqlite.DefaultOptions)
	if err != nil {
		return nil, nil, err
	}
	state, err := profile.LoadAmazonMatchSource(ctx)
	if err != nil {
		_ = profile.Close()
		return nil, nil, err
	}
	return &state, profile.Close, nil
}

func amazonPerformanceService(
	t testing.TB,
) (*Service, *AmazonMatchingService, *sqliteAmazonLoader, []domain.Transaction) {
	t.Helper()
	transactions := fixture.Generate(20260820, amazonMatchingPerformanceRows)
	qualified := make([]domain.Transaction, 0, amazonMatchingPerformanceQualified)
	for index := 0; index < amazonMatchingPerformanceQualified; index++ {
		position := index * (amazonMatchingPerformanceRows / amazonMatchingPerformanceQualified)
		transactions[position].Merchant.Name = "Amazon Marketplace"
		transactions[position].Category.Group = "Expenses"
		transactions[position].Amount.Currency = "USD"
		transactions[position].Amount.Scale = 2
		if transactions[position].Amount.Minor >= 0 {
			transactions[position].Amount.Minor = -transactions[position].Amount.Minor - 1
		}
		qualified = append(qualified, transactions[position])
	}
	service, err := NewService(transactions)
	require.NoError(t, err)

	paths := amazonPerformanceProfile(t, transactions, qualified)
	loader := &sqliteAmazonLoader{paths: paths}
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "amazon-performance", Kind: amazonProvider}}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)
	return service, matcher, loader, qualified
}

// amazonPerformanceProfile installs a SQLite profile holding 100k order items, one of which
// matches each qualified finance transaction exactly.
func amazonPerformanceProfile(
	t testing.TB,
	transactions []domain.Transaction,
	qualified []domain.Transaction,
) home.Paths {
	t.Helper()
	money := qualified[0].Amount
	rows := make([]amazon.Row, len(transactions))
	observed := make([]string, 0, len(transactions)/4+len(qualified))
	for index, transaction := range transactions {
		row := amazon.Row{
			OrderID: "order-" + strconv.Itoa(index/4), ASIN: "ASIN-" + strconv.Itoa(index),
			ProductName: "Catalog item " + strconv.Itoa(index), OrderDate: transaction.Date,
			Quantity: 1, AmountMinor: -1000 - int64(index%9000), Currency: money.Currency,
			Scale: money.Scale, OrderStatus: "Closed", ShipmentStatus: "Delivered",
			RelativeFilename: "orders.csv", Record: index + 2,
		}
		if index%4 == 0 {
			observed = append(observed, row.OrderID)
		}
		rows[index] = row
	}
	for index, transaction := range qualified {
		row := &rows[index]
		row.OrderID = "order-performance-" + strconv.Itoa(index)
		row.ProductName = "Unique Performance Product " + strconv.Itoa(index)
		row.OrderDate = transaction.Date
		row.AmountMinor = transaction.Amount.Minor
		observed = append(observed, row.OrderID)
	}
	for index := range rows {
		fingerprints, err := amazon.Fingerprints(rows[index])
		require.NoError(t, err)
		rows[index].IdentityFingerprint = fingerprints.Identity
		rows[index].FullFingerprint = fingerprints.Full
	}

	ctx := context.Background()
	root := t.TempDir()
	paths := home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = ImportAmazonProfile(ctx, profile, AmazonImportRequest{
		Candidate: amazon.Candidate{
			Rows: rows, ObservedOrderIDs: observed, FileCount: 1,
			LogicalRecordCount: len(rows), Digest: strings.Repeat("a", 64),
		},
		Settings:   amazon.Settings{Currency: money.Currency, Scale: money.Scale},
		ImportedAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.NoError(t, profile.Close())
	return paths
}

func requireAmazonPerformanceEnvironment(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("performance smoke is not part of short tests")
	}
	if os.Getenv("MONEYFLOW_SKIP_PERF") == "1" {
		t.Skip("performance smoke explicitly skipped for instrumented race job")
	}
}
