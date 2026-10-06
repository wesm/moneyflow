// Package uitest contains synthetic scenario data and provider controls for developer tools.
// Production application entry points must not import this package.
package uitest

import (
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// Fixture returns a small, independently reviewable profile spanning date and visibility boundaries.
func Fixture() domain.ImportSnapshot {
	snapshot := domain.ImportSnapshot{
		ObservedAt: time.Date(2026, time.October, 15, 12, 0, 0, 0, time.UTC),
		Accounts: []domain.ImportEntity{
			{Kind: domain.EntityKindAccount, ExternalID: "account-main", Label: "Example Checking"},
			{Kind: domain.EntityKindAccount, ExternalID: "account-other", Label: "Example Savings"},
		},
		Merchants: []domain.ImportEntity{
			{Kind: domain.EntityKindMerchant, ExternalID: "merchant-shop", Label: "Example Shop"},
			{Kind: domain.EntityKindMerchant, ExternalID: "merchant-shop-plus", Label: "Example Shop Plus"},
			{Kind: domain.EntityKindMerchant, ExternalID: "merchant-destination", Label: "Destination Shop"},
		},
		Groups: []domain.ImportEntity{{Kind: domain.EntityKindGroup, ExternalID: "group-expenses", Label: "Example Expenses"}},
		Categories: []domain.ImportEntity{
			{Kind: domain.EntityKindCategory, ExternalID: "category-home", ParentExternalID: "group-expenses", Label: "Home"},
			{Kind: domain.EntityKindCategory, ExternalID: "category-health", ParentExternalID: "group-expenses", Label: "Health"},
			{Kind: domain.EntityKindCategory, ExternalID: "category-health-extra", ParentExternalID: "group-expenses", Label: "Health Extras"},
		},
	}
	for _, row := range []struct {
		id, date, account, merchant string
		minor                       int64
		hidden                      bool
	}{
		{"current-a", "2026-09-10", "account-main", "merchant-shop", -1200, false},
		{"current-b", "2026-09-20", "account-main", "merchant-shop", -2300, false},
		{"older", "2025-09-10", "account-main", "merchant-shop", -4100, false},
		{"hidden", "2026-09-15", "account-main", "merchant-shop", -900, true},
		{"other-month", "2026-08-10", "account-main", "merchant-shop", -600, false},
		{"other-account", "2026-09-11", "account-other", "merchant-shop-plus", -700, false},
		{"destination", "2026-09-12", "account-main", "merchant-destination", -300, false},
	} {
		date, err := domain.ParseDate(row.date)
		if err != nil {
			panic(err)
		}
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID: row.id, AccountExternalID: row.account, MerchantExternalID: row.merchant,
			CategoryExternalID: "category-home", Date: date,
			Amount: domain.Money{Minor: row.minor, Currency: "USD", Scale: 2}, Hidden: row.hidden,
		})
	}
	return snapshot
}
