package ynab

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestNormalizeRetainsParentAccountingAndSplitDetails(t *testing.T) {
	t.Parallel()
	plan := syntheticPlan()
	snapshot, err := Normalize(plan, time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, snapshot.Transactions, 3)
	assert.True(t, transactionByID(snapshot, "txn-uncleared").Pending)
	assert.True(t, transactionByID(snapshot, "txn-transfer").Hidden)
	splitParent := transactionByID(snapshot, "txn-split")
	assert.Equal(t, domain.SplitCategoryID, splitParent.SystemCategoryID)
	assert.Equal(t, int64(-3000), splitParent.Amount.Minor)
	require.Len(t, snapshot.Splits, 2)
	assert.Equal(t, "split-a", snapshot.Splits[0].ExternalID)
	assert.Equal(t, int64(-10000), snapshot.Splits[0].SourceAmount)
	assert.Equal(t, int64(-1000), snapshot.Splits[0].Amount.Minor)
}

func TestNormalizeRecordsTransferRestrictionsWithoutRestrictingOffBudget(t *testing.T) {
	plan := syntheticPlan()
	plan.Payees = append(plan.Payees, Payee{ID: "payee-transfer", Name: "Transfer Payee", Deleted: new(false), TransferAccountID: "account-transfer"})
	plan.Accounts[0].OnBudget = new(false)
	plan.Subtransactions[0].TransferAccountID = "account-transfer"
	snapshot, err := Normalize(plan, time.Now())
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.ImportWriteRestriction{
		{Kind: domain.EntityKindMerchant, ExternalID: "payee-transfer", Reason: "transfer"},
		{Kind: domain.EntityKindTransaction, ExternalID: "txn-transfer", Reason: "transfer"},
		{Kind: domain.EntityKindTransaction, ExternalID: "txn-split", Reason: "transfer"},
	}, snapshot.WriteRestrictions)
	assert.True(t, transactionByID(snapshot, "txn-uncleared").Hidden)
}

func TestNormalizeRejectsInvalidMemoSplitTotalAndMoney(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*PlanDocument){
		"memo":                 func(plan *PlanDocument) { plan.Transactions[0].Memo = strings.Repeat("é", 501) },
		"split total":          func(plan *PlanDocument) { (*plan.Subtransactions[0].Amount)-- },
		"money":                func(plan *PlanDocument) { plan.Transactions[0].Amount = new(int64(1)) },
		"missing amount":       func(plan *PlanDocument) { plan.Transactions[0].Amount = nil },
		"missing split amount": func(plan *PlanDocument) { plan.Subtransactions[0].Amount = nil },
		"deleted split":        func(plan *PlanDocument) { value := true; plan.Subtransactions[0].Deleted = &value },
		"deleted transaction":  func(plan *PlanDocument) { plan.Transactions[0].Deleted = new(true) },
	} {
		t.Run(name, func(t *testing.T) {
			plan := syntheticPlan()
			mutate(&plan)
			_, err := Normalize(plan, time.Now())
			code, ok := provider.CodeOf(err)
			assert.True(t, ok)
			assert.Equal(t, provider.CodeDataInvalid, code)
		})
	}
}

func TestNormalizeRejectsSplitAccumulatorOverflow(t *testing.T) {
	for _, amount := range []int64{math.MaxInt64, math.MinInt64} {
		plan := syntheticPlan()
		plan.CurrencyFormat.DecimalDigits = new(3)
		plan.Subtransactions[0].Amount = new(amount)
		plan.Subtransactions[1].Amount = new(amount)
		plan.Transactions[2].Amount = new(amount + amount)
		_, err := Normalize(plan, time.Now())
		code, ok := provider.CodeOf(err)
		require.True(t, ok)
		assert.Equal(t, provider.CodeDataInvalid, code)
	}
}

func TestNormalizeExplainsMissingCategoryWithoutReturningPartialData(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(map[bool]string{false: "transaction", true: "split"}[split], func(t *testing.T) {
			plan := syntheticPlan()
			if split {
				plan.Subtransactions[0].CategoryID = "category-not-returned"
			} else {
				plan.Transactions[0].CategoryID = "category-not-returned"
			}
			snapshot, err := Normalize(plan, time.Now())
			require.Error(t, err)
			assert.Empty(t, snapshot.Transactions)
			reason, ok := provider.DataInvalidReasonOf(err)
			require.True(t, ok)
			assert.Equal(t, provider.DataInvalidReason("category_reference"), reason)
			assert.Contains(t, provider.DataInvalidDetail(reason), "category")
			assert.NotContains(t, provider.DataInvalidDetail(reason), "category-not-returned")
		})
	}
}

func syntheticPlan() PlanDocument {
	no, yes := false, true
	return PlanDocument{
		ID: "plan-a", Name: "Example Budget",
		CurrencyFormat: CurrencyFormat{ISOCode: "USD", DecimalDigits: new(2)},
		Accounts: []Account{
			{ID: "account-budget", Name: "Budget Account", Type: "checking", OnBudget: &yes, Closed: &no, Deleted: &no},
			{ID: "account-transfer", Name: "Transfer Account", Type: "savings", OnBudget: &yes, Closed: &no, Deleted: &no},
		},
		Payees:         []Payee{{ID: "payee-a", Name: "Example Payee", Deleted: &no}},
		CategoryGroups: []CategoryGroup{{ID: "group-a", Name: "Example Group", Hidden: &no, Deleted: &no}},
		Categories:     []Category{{ID: "category-a", CategoryGroupID: "group-a", Name: "Example Category", Hidden: &no, Deleted: &no}},
		Transactions: []Transaction{
			{ID: "txn-uncleared", Date: "2026-08-28", Amount: new(int64(-12340)), Cleared: "uncleared", Approved: &yes, AccountID: "account-budget", PayeeID: "payee-a", CategoryID: "category-a", Deleted: &no},
			{ID: "txn-transfer", Date: "2026-08-29", Amount: new(int64(-20000)), Cleared: "cleared", Approved: &yes, AccountID: "account-budget", PayeeID: "payee-a", TransferAccountID: "account-transfer", Deleted: &no},
			{ID: "txn-split", Date: "2026-08-30", Amount: new(int64(-30000)), Cleared: "reconciled", Approved: &yes, AccountID: "account-budget", PayeeID: "payee-a", Deleted: &no},
		},
		Subtransactions: []Subtransaction{
			{ID: "split-b", TransactionID: "txn-split", Amount: new(int64(-20000)), CategoryID: "category-a", Deleted: &no},
			{ID: "split-a", TransactionID: "txn-split", Amount: new(int64(-10000)), PayeeID: "payee-a", Deleted: &no},
		},
		ServerKnowledge: 1,
	}
}

func transactionByID(snapshot domain.ImportSnapshot, externalID string) domain.ImportTransaction {
	for _, transaction := range snapshot.Transactions {
		if transaction.ExternalID == externalID {
			return transaction
		}
	}
	return domain.ImportTransaction{}
}
