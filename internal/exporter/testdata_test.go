package exporter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func testDocument(t testing.TB) app.ExportDocument {
	t.Helper()
	first, err := domain.ParseDate("2026-08-18")
	require.NoError(t, err)
	second, err := domain.ParseDate("2026-08-17")
	require.NoError(t, err)
	return app.ExportDocument{
		Metadata: app.ExportMetadata{
			SchemaVersion: 2, AppVersion: "v2.0.0-test",
			ExportedAt:      time.Date(2026, 8, 19, 14, 15, 16, 123_000_000, time.UTC),
			ProfileRevision: 42, JournalCursor: 3, ExcludedActiveOperations: 3,
			InactiveRedoOperations: 2, Scope: app.ExportScopeFull,
			TransactionCount: 2, EarliestDate: &second, LatestDate: &first,
			ProviderKinds: []string{"local", "monarch"},
		},
		Rows: []app.ExportRow{
			{
				TransactionID: "txn-a", Provider: "monarch", ProviderTransactionID: "provider-a",
				Date: first, Amount: "-12.34", AmountMinor: -1234, Currency: "USD", Scale: 2,
				AccountID: "account-a", Account: "  =Formula Account", MerchantID: "merchant-a",
				Merchant: "Café, Example", CategoryID: "category-a", Category: "Food",
				GroupID: "group-a", Group: "Expenses", Notes: "\tformula\nline two", Hidden: true,
				TransactionMetadataJSON: `{"reference":"@unsafe"}`,
			},
			{
				TransactionID: "txn-b", Provider: "local", Date: second,
				Amount: "100.00", AmountMinor: 10000, Currency: "USD", Scale: 2,
				AccountID: "account-b", Account: "Cash", MerchantID: "merchant-b",
				Merchant: "Example", CategoryID: "", Category: "", GroupID: "", Group: "",
				Notes: "", Hidden: false, TransactionMetadataJSON: `{}`,
			},
		},
	}
}
