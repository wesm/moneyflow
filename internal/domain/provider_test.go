package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportSnapshotCloneOwnsItsSlices(t *testing.T) {
	t.Parallel()

	snapshot := validImportSnapshot(t)
	clone := snapshot.Clone()
	clone.Accounts[0].Label = "Changed Account"
	clone.Transactions[0].Notes = "Changed notes"

	assert.Equal(t, "Example Account", snapshot.Accounts[0].Label)
	assert.Equal(t, "", snapshot.Transactions[0].Notes)
}

func TestImportSnapshotValidatesPostedAndPendingRows(t *testing.T) {
	t.Parallel()

	snapshot := validImportSnapshot(t)
	pending := snapshot.Transactions[0]
	pending.ExternalID = "transaction_pending"
	pending.Pending = true
	snapshot.Transactions = append(snapshot.Transactions, pending)

	require.NoError(t, snapshot.Validate())
	assert.False(t, snapshot.Transactions[0].Pending)
	assert.True(t, snapshot.Transactions[1].Pending)
}

func TestImportSnapshotRejectsDuplicateExternalIdentities(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*ImportSnapshot){
		"account": func(snapshot *ImportSnapshot) {
			snapshot.Accounts = append(snapshot.Accounts, snapshot.Accounts[0])
		},
		"merchant": func(snapshot *ImportSnapshot) {
			snapshot.Merchants = append(snapshot.Merchants, snapshot.Merchants[0])
		},
		"group": func(snapshot *ImportSnapshot) {
			snapshot.Groups = append(snapshot.Groups, snapshot.Groups[0])
		},
		"category": func(snapshot *ImportSnapshot) {
			snapshot.Categories = append(snapshot.Categories, snapshot.Categories[0])
		},
		"transaction": func(snapshot *ImportSnapshot) {
			snapshot.Transactions = append(snapshot.Transactions, snapshot.Transactions[0])
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := validImportSnapshot(t)
			mutate(&snapshot)
			assert.ErrorContains(t, snapshot.Validate(), "duplicate external identity")
		})
	}
}

func TestImportSnapshotRejectsInvalidRecords(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*ImportSnapshot){
		"zero observation": func(snapshot *ImportSnapshot) { snapshot.ObservedAt = time.Time{} },
		"wrong entity kind": func(snapshot *ImportSnapshot) {
			snapshot.Accounts[0].Kind = EntityKindMerchant
		},
		"missing account": func(snapshot *ImportSnapshot) {
			snapshot.Transactions[0].AccountExternalID = ""
		},
		"missing merchant": func(snapshot *ImportSnapshot) {
			snapshot.Transactions[0].MerchantExternalID = ""
		},
		"missing category": func(snapshot *ImportSnapshot) {
			snapshot.Transactions[0].CategoryExternalID = ""
		},
		"invalid money": func(snapshot *ImportSnapshot) {
			snapshot.Transactions[0].Amount.Currency = "usd"
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snapshot := validImportSnapshot(t)
			mutate(&snapshot)
			assert.Error(t, snapshot.Validate())
		})
	}
}

func validImportSnapshot(t *testing.T) ImportSnapshot {
	t.Helper()
	amount, err := ParseMoney("-12.34", "USD", 2)
	require.NoError(t, err)
	date, err := ParseDate("2026-08-15")
	require.NoError(t, err)
	return ImportSnapshot{
		Accounts: []ImportEntity{{
			Kind: EntityKindAccount, ExternalID: "account_a", Label: "Example Account",
		}},
		Merchants: []ImportEntity{{
			Kind: EntityKindMerchant, ExternalID: "merchant_a", Label: "Example Merchant",
		}},
		Groups: []ImportEntity{{
			Kind: EntityKindGroup, ExternalID: "group_a", Label: "Example Group",
		}},
		Categories: []ImportEntity{{
			Kind: EntityKindCategory, ExternalID: "category_a", Label: "Example Category",
			ParentExternalID: "group_a",
		}},
		Transactions: []ImportTransaction{{
			ExternalID:         "transaction_a",
			AccountExternalID:  "account_a",
			MerchantExternalID: "merchant_a",
			CategoryExternalID: "category_a",
			Date:               date,
			Amount:             amount,
		}},
		ObservedAt: time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
	}
}
