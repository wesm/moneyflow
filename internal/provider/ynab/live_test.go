package ynab

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
)

func TestLiveYNABReadOnlySnapshot(t *testing.T) {
	if os.Getenv("MONEYFLOW_YNAB_LIVE") != "1" {
		t.Skip("set MONEYFLOW_YNAB_LIVE=1 to enable the read-only YNAB characterization")
	}
	token := os.Getenv("MONEYFLOW_YNAB_TOKEN")
	if token == "" {
		t.Fatal("MONEYFLOW_YNAB_TOKEN is required when live characterization is enabled")
	}
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	require.NotEmpty(t, paths.Root)

	client, err := NewClient(ClientOptions{}, token)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	plans, err := client.ListPlans(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, plans)
	selectedID := os.Getenv("MONEYFLOW_YNAB_PLAN_ID")
	if len(plans) > 1 && selectedID == "" {
		t.Fatal("MONEYFLOW_YNAB_PLAN_ID is required when the token can access multiple budgets")
	}
	if selectedID == "" {
		selectedID = plans[0].ID
	}
	selected := false
	for _, plan := range plans {
		if plan.ID == selectedID {
			selected = true
			break
		}
	}
	if !selected {
		t.Fatal("MONEYFLOW_YNAB_PLAN_ID does not identify a budget visible to the token")
	}

	firstPlan, err := client.FetchPlan(ctx, selectedID)
	require.NoError(t, err)
	secondPlan, err := client.FetchPlan(ctx, selectedID)
	require.NoError(t, err)
	if firstPlan.ID != secondPlan.ID || firstPlan.ID != selectedID {
		t.Fatal("YNAB budget identity changed across complete reads")
	}
	first, err := Normalize(firstPlan, time.Now().UTC())
	require.NoError(t, err)
	second, err := Normalize(secondPlan, time.Now().UTC())
	require.NoError(t, err)
	require.NoError(t, first.Validate())
	require.NoError(t, second.Validate())
	assertLiveYNABMoney(t, firstPlan)
	assertLiveYNABAccountCoverage(t, firstPlan, first)
	assertLiveYNABTransactionCoverage(t, firstPlan, first)
	assertLiveYNABSplitSums(t, firstPlan, first)
	if !sameExternalTransactionIDs(first, second) {
		t.Fatalf(
			"YNAB transaction identities changed across immediate reads: first=%d second=%d",
			len(first.Transactions), len(second.Transactions),
		)
	}
	t.Logf(
		"YNAB live read: plans=%d accounts=%d transactions=%d splits=%d",
		len(plans), len(first.Accounts), len(first.Transactions), len(first.Splits),
	)
}

func assertLiveYNABMoney(t testing.TB, plan PlanDocument) {
	t.Helper()
	if !domain.IsValidCurrency(domain.Currency(plan.CurrencyFormat.ISOCode)) ||
		plan.CurrencyFormat.DecimalDigits < 0 || plan.CurrencyFormat.DecimalDigits > 9 {
		t.Fatal("YNAB returned an invalid currency or minor-unit scale")
	}
}

func assertLiveYNABAccountCoverage(
	t testing.TB,
	plan PlanDocument,
	snapshot domain.ImportSnapshot,
) {
	t.Helper()
	imported := make(map[string]struct{}, len(snapshot.Accounts))
	for _, account := range snapshot.Accounts {
		imported[account.ExternalID] = struct{}{}
	}
	for _, account := range plan.Accounts {
		if account.Deleted == nil || *account.Deleted {
			continue
		}
		if _, ok := imported[account.ID]; !ok {
			t.Fatal("a nondeleted YNAB account was absent from the normalized snapshot")
		}
	}
}

func assertLiveYNABTransactionCoverage(
	t testing.TB,
	plan PlanDocument,
	snapshot domain.ImportSnapshot,
) {
	t.Helper()
	imported := make(map[string]domain.ImportTransaction, len(snapshot.Transactions))
	for _, transaction := range snapshot.Transactions {
		imported[transaction.ExternalID] = transaction
	}
	for _, transaction := range plan.Transactions {
		if transaction.Deleted == nil || *transaction.Deleted {
			continue
		}
		normalized, ok := imported[transaction.ID]
		if !ok {
			t.Fatal("a nondeleted YNAB transaction was absent from the normalized snapshot")
		}
		if transaction.Cleared == "uncleared" && !normalized.Pending {
			t.Fatal("an uncleared YNAB transaction lost its pending state")
		}
	}
}

func assertLiveYNABSplitSums(
	t testing.TB,
	plan PlanDocument,
	snapshot domain.ImportSnapshot,
) {
	t.Helper()
	parentAmounts := make(map[string]int64, len(plan.Transactions))
	for _, transaction := range plan.Transactions {
		parentAmounts[transaction.ID] = transaction.Amount
	}
	sums := make(map[string]int64)
	for _, split := range snapshot.Splits {
		sums[split.ParentTransactionExternalID] += split.SourceAmount
	}
	for parentID, sum := range sums {
		if sum != parentAmounts[parentID] {
			t.Fatal("normalized YNAB split amounts do not equal their parent")
		}
	}
}

func sameExternalTransactionIDs(left, right domain.ImportSnapshot) bool {
	if len(left.Transactions) != len(right.Transactions) {
		return false
	}
	identities := make(map[string]struct{}, len(left.Transactions))
	for _, transaction := range left.Transactions {
		identities[transaction.ExternalID] = struct{}{}
	}
	for _, transaction := range right.Transactions {
		if _, ok := identities[transaction.ExternalID]; !ok {
			return false
		}
	}
	return true
}
