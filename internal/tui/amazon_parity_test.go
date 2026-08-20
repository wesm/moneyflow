package tui_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/parity"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

type parityAmazonDirectory struct{}

func (parityAmazonDirectory) ListAmazonSources(context.Context) ([]app.AmazonSourceDescriptor, error) {
	return []app.AmazonSourceDescriptor{{ProfileID: "amazon-parity", Kind: "amazon"}}, nil
}

func amazonParityService(
	t testing.TB,
	scenario parity.FrameScenario,
	transactions []domain.Transaction,
) (*app.Service, string, bool) {
	t.Helper()
	switch scenario.ProfileKind {
	case "amazon":
		return importedAmazonParityService(t, transactions), "", true
	case "finance_with_amazon":
		service, err := app.NewService(transactions)
		require.NoError(t, err)
		matcher, err := app.NewAmazonMatchingService(
			parityAmazonDirectory{},
			func(context.Context, app.AmazonSourceDescriptor) (store.AmazonMatchSourceState, func() error, error) {
				return parityAmazonMatchSource(t), func() error { return nil }, nil
			},
		)
		require.NoError(t, err)
		service.ConfigureAmazonMatching(matcher)
		return service, "", true
	default:
		return nil, "", false
	}
}

func importedAmazonParityService(t testing.TB, transactions []domain.Transaction) *app.Service {
	t.Helper()
	require.Len(t, transactions, 1)
	transaction := transactions[0]
	row := amazon.Row{
		OrderID: transaction.Metadata["amazon_order_id"], ProductName: transaction.Merchant.Name,
		ASIN: transaction.Metadata["amazon_asin"], OrderDate: transaction.Date,
		Quantity: 1, AmountMinor: transaction.Amount.Minor, Currency: transaction.Amount.Currency,
		Scale: transaction.Amount.Scale, OrderStatus: transaction.Metadata["amazon_order_status"],
		ShipmentStatus:   transaction.Metadata["amazon_shipment_status"],
		RelativeFilename: "parity.csv", Record: 1,
	}
	fingerprints, err := amazon.Fingerprints(row)
	require.NoError(t, err)
	row.IdentityFingerprint = fingerprints.Identity
	row.FullFingerprint = fingerprints.Full

	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = app.ImportAmazonProfile(ctx, profile, app.AmazonImportRequest{
		Candidate: amazon.Candidate{
			Rows: []amazon.Row{row}, ObservedOrderIDs: []string{row.OrderID},
			FileCount: 1, LogicalRecordCount: 1, Digest: strings.Repeat("a", 64),
		},
		Settings:   amazon.Settings{Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale},
		ImportedAt: time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	return service
}

func parityAmazonMatchSource(t testing.TB) store.AmazonMatchSourceState {
	t.Helper()
	date, err := domain.ParseDate("2026-08-19")
	require.NoError(t, err)
	return store.AmazonMatchSourceState{
		Revision: 1,
		Settings: store.AmazonSettings{Currency: "USD", Scale: 2},
		Items: []store.AmazonOrderItem{{
			LocalTransactionID: "amazon-parity-item", SourceIdentity: "amazon-parity-source",
			OrderID: "order-example", ASIN: "ASIN-EXAMPLE", ProductName: "Example Headphones",
			OrderDate: date, Quantity: 1, AmountMinor: -1234, Currency: "USD", Scale: 2,
			OrderStatus: "Closed", ShipmentStatus: "Delivered",
		}},
	}
}
