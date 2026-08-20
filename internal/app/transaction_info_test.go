package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestTransactionInfoReturnsAmazonSourceFactsWithoutASINLessDigest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	profile, err := sqlite.Open(ctx, home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	row := amazonIncomingRow("order-a", "", amazonDigest("info"), "orders.csv", 2)
	row.ASINLessKey = "amazon:asinless:private-digest"
	row.ProductName = "Example Product"
	row.Quantity = 3
	row.OrderStatus = "Closed"
	row.ShipmentStatus = "Delivered"
	unitPrice := int64(500)
	row.UnitPriceMinor = &unitPrice
	fingerprints, err := amazon.Fingerprints(row)
	require.NoError(t, err)
	row.IdentityFingerprint = fingerprints.Identity
	row.FullFingerprint = fingerprints.Full
	_, err = ImportAmazonProfile(ctx, profile, AmazonImportRequest{
		Candidate: amazon.Candidate{Rows: []amazon.Row{row}, ObservedOrderIDs: []string{"order-a"}, Digest: amazonDigest("c")},
		Settings:  amazon.Settings{Currency: "USD", Scale: 2}, ImportedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	service, err := NewProfileService(ctx, profile)
	require.NoError(t, err)
	state, err := profile.LoadAmazonMatchSource(ctx)
	require.NoError(t, err)

	info, err := service.TransactionInfo(ctx, TransactionInfoRequest{
		ExpectedRevision: service.Revision(), TransactionID: string(state.Items[0].LocalTransactionID),
		MatchLimit: 20, ItemLimit: 20,
	})
	require.NoError(t, err)
	require.NotNil(t, info.AmazonItem)
	assert.Empty(t, info.AmazonItem.ASIN)
	assert.NotContains(t, info.AmazonItem.ASIN, "asinless")
	assert.Equal(t, int64(3), info.AmazonItem.Quantity)
	assert.Equal(t, "Closed", info.AmazonItem.OrderStatus)
	assert.Equal(t, "Delivered", info.AmazonItem.ShipmentStatus)
	require.NotNil(t, info.AmazonItem.UnitPrice)
	assert.Equal(t, int64(500), info.AmazonItem.UnitPrice.Minor)
}

func TestTransactionInfoReturnsBoundedCrossProfileMatches(t *testing.T) {
	transaction := matchingFinanceTransaction(t, "finance", "Amazon", -1234)
	service, err := NewService([]domain.Transaction{transaction})
	require.NoError(t, err)
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"source": amazonSourceState(t, 1, "USD", 2, -1234)}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)

	info, err := service.TransactionInfo(context.Background(), TransactionInfoRequest{
		TransactionID: "finance", MatchLimit: 1, ItemLimit: 1,
	})
	require.NoError(t, err)
	assert.True(t, info.AmazonQualified)
	assert.Equal(t, 1, info.TotalMatches)
	require.Len(t, info.Matches, 1)
	assert.Equal(t, "Example Product", info.Matches[0].FirstProduct)
	assert.Len(t, info.Matches[0].Items, 1)
}

func TestAmazonColumnRequiresEveryDetailRowToQualify(t *testing.T) {
	first := matchingFinanceTransaction(t, "first", "Amazon", -1234)
	second := matchingFinanceTransaction(t, "second", "AMZN Marketplace", -1234)
	service, err := NewService([]domain.Transaction{first, second})
	require.NoError(t, err)
	configureMatchingSource(t, service, amazonSourceState(t, 1, "USD", 2, -1234))

	state := DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	projection, err := service.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.True(t, projection.AmazonMatchColumn)
	require.Len(t, projection.DetailRows, 2)
	for _, row := range projection.DetailRows {
		require.NotNil(t, row.AmazonMatch)
		assert.Equal(t, "Example Product", row.AmazonMatch.FirstProduct)
	}

	third := matchingFinanceTransaction(t, "third", "Example Merchant", -1234)
	mixed, err := NewService([]domain.Transaction{first, third})
	require.NoError(t, err)
	configureMatchingSource(t, mixed, amazonSourceState(t, 1, "USD", 2, -1234))
	mixedProjection, err := mixed.ProjectView(state, EmptySelection(), WindowRequest{})
	require.NoError(t, err)
	assert.False(t, mixedProjection.AmazonMatchColumn)
	for _, row := range mixedProjection.DetailRows {
		assert.Nil(t, row.AmazonMatch)
	}
}

func TestAmazonProductSearchCombinesWithExistingFilters(t *testing.T) {
	matching := matchingFinanceTransaction(t, "matching", "Amazon", -1234)
	other := matchingFinanceTransaction(t, "other", "Amazon", -9900)
	service, err := NewService([]domain.Transaction{matching, other})
	require.NoError(t, err)
	state := amazonSourceState(t, 1, "USD", 2, -1234)
	state.Items[0].ProductName = "Noise Cancelling Headphones"
	configureMatchingSource(t, service, state)

	session := NewSession()
	session.Mode = domain.ResultModeDetail
	session.Search = "HEADPHONES"
	result, err := service.QueryContext(context.Background(), session)
	require.NoError(t, err)
	require.Len(t, result.DetailRows, 1)
	assert.Equal(t, "matching", result.DetailRows[0].Transaction.ID)

	start, err := domain.ParseDate("2026-08-21")
	require.NoError(t, err)
	end, err := domain.ParseDate("2026-08-22")
	require.NoError(t, err)
	session.DateRange = &domain.DateRange{Start: start, End: end}
	filtered, err := service.QueryContext(context.Background(), session)
	require.NoError(t, err)
	assert.Empty(t, filtered.DetailRows)
}

func configureMatchingSource(t *testing.T, service *Service, state store.AmazonMatchSourceState) {
	t.Helper()
	directory := &fakeAmazonDirectory{sources: []AmazonSourceDescriptor{{ProfileID: "source", Kind: amazonProvider}}}
	loader := &fakeAmazonLoader{states: map[string]store.AmazonMatchSourceState{"source": state}}
	matcher, err := NewAmazonMatchingService(directory, loader.Load)
	require.NoError(t, err)
	service.ConfigureAmazonMatching(matcher)
}
