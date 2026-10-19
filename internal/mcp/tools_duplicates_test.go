package mcp

import (
	"context"
	"fmt"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func duplicateTestTransaction(t *testing.T, id, date, merchant string, minor int64, hidden bool) domain.Transaction {
	t.Helper()
	parsed, err := domain.ParseDate(date)
	require.NoError(t, err)
	return domain.Transaction{
		ID: id, ProviderID: "provider_" + id, Provider: "fixture",
		Account: domain.EntityRef{ID: "account_a", Name: "Example Account"}, Date: parsed,
		Merchant: domain.EntityRef{ID: "merchant_" + merchant, Name: merchant},
		Category: domain.CategoryRef{ID: "category_a", Name: "Food", GroupID: "group_a", Group: "Living"},
		Amount:   domain.Money{Minor: minor, Currency: "USD", Scale: 2}, Hidden: hidden,
	}
}

// connectDuplicateTestServer serves a saved profile seeded with transactions, with edits allowed.
func connectDuplicateTestServer(t *testing.T, transactions []domain.Transaction) *mcpsdk.ClientSession {
	t.Helper()
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.CreateSeededProfile(context.Background(), committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	client, stop := connectWriteTestServer(t, service, true)
	t.Cleanup(stop)
	return client
}

func callFindDuplicates(t *testing.T, client *mcpsdk.ClientSession, arguments map[string]any) map[string]any {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "find_duplicates", Arguments: arguments})
	require.NoError(t, err)
	structured, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok)
	return structured
}

func duplicateGroupIDs(document map[string]any) [][]string {
	groups := document["groups"].([]any)
	result := make([][]string, 0, len(groups))
	for _, group := range groups {
		ids := []string{}
		for _, row := range group.(map[string]any)["transactions"].([]any) {
			ids = append(ids, row.(map[string]any)["id"].(string))
		}
		result = append(result, ids)
	}
	return result
}

func TestFindDuplicatesReturnsMatchingGroupsForTheView(t *testing.T) {
	client := connectDuplicateTestServer(t, []domain.Transaction{
		duplicateTestTransaction(t, "coffee_a", "2026-08-20", "Example Coffee", -450, false),
		duplicateTestTransaction(t, "coffee_b", "2026-08-20", "Example Coffee", -450, false),
		duplicateTestTransaction(t, "coffee_other_amount", "2026-08-20", "Example Coffee", -500, false),
		duplicateTestTransaction(t, "grocer_a", "2026-08-02", "Example Grocer", -4210, true),
		duplicateTestTransaction(t, "grocer_b", "2026-08-02", "Example Grocer", -4210, false),
	})

	all := callFindDuplicates(t, client, nil)
	assert.Equal(t, "ok", all["status"])
	assert.Equal(t, float64(2), all["total_groups"])
	assert.Equal(t, float64(4), all["total_transactions"])
	assert.Equal(t, [][]string{{"coffee_a", "coffee_b"}, {"grocer_a", "grocer_b"}}, duplicateGroupIDs(all))
	first := all["groups"].([]any)[0].(map[string]any)
	assert.Equal(t, float64(1), first["number"])
	assert.Equal(t, "Example Coffee", first["matching_label"])
	row := first["transactions"].([]any)[0].(map[string]any)
	assert.Equal(t, "-4.50", row["money"].(map[string]any)["amount"])
	assert.Equal(t, "Example Account", row["account"].(map[string]any)["label"])

	dated := callFindDuplicates(t, client, map[string]any{"start_date": "2026-08-01", "end_date": "2026-08-10"})
	assert.Equal(t, [][]string{{"grocer_a", "grocer_b"}}, duplicateGroupIDs(dated))

	second := callFindDuplicates(t, client, map[string]any{"offset": 1, "limit": 1})
	assert.Equal(t, [][]string{{"grocer_a", "grocer_b"}}, duplicateGroupIDs(second))
	assert.Equal(t, float64(2), second["groups"].([]any)[0].(map[string]any)["number"])
	assert.Equal(t, map[string]any{
		"total": float64(2), "offset": float64(1), "limit": float64(1), "returned": float64(1),
	}, second["group_window"])

	for _, arguments := range []map[string]any{
		{"start_date": "2026-08-01"}, {"limit": 0}, {"limit": 401}, {"offset": -1},
	} {
		invalid := callFindDuplicates(t, client, arguments)
		assert.Equal(t, "invalid_operation", invalid["code"], arguments)
	}

	// Groups come from committed transactions; a staged hide marks the row until the commit.
	hidden, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "toggle_transactions_hidden", Arguments: map[string]any{
			"expected_revision": all["revision"], "transaction_ids": []string{"coffee_b"},
		},
	})
	require.NoError(t, err)
	require.False(t, hidden.IsError, hidden.StructuredContent)
	staged := callFindDuplicates(t, client, nil)
	rows := staged["groups"].([]any)[0].(map[string]any)["transactions"].([]any)
	assert.Equal(t, "coffee_b", rows[1].(map[string]any)["id"])
	assert.Equal(t, false, rows[1].(map[string]any)["hidden"])
	assert.Equal(t, true, rows[1].(map[string]any)["pending_edit"])
	assert.Equal(t, false, rows[0].(map[string]any)["pending_edit"])
}

func TestFindDuplicatesReturnsOnlyWholeGroups(t *testing.T) {
	transactions := []domain.Transaction{
		duplicateTestTransaction(t, "recent_a", "2026-08-20", "Example Coffee", -450, false),
		duplicateTestTransaction(t, "recent_b", "2026-08-20", "Example Coffee", -450, false),
	}
	large := app.MaxWindowLimit - 1
	for index := range large {
		transactions = append(transactions, duplicateTestTransaction(
			t, fmt.Sprintf("large_%03d", index), "2026-08-02", "Example Grocer", -100, false,
		))
	}
	client := connectDuplicateTestServer(t, transactions)

	// Both groups do not fit in one response, so the partial second group is left for the next call.
	page := callFindDuplicates(t, client, nil)
	assert.Equal(t, [][]string{{"recent_a", "recent_b"}}, duplicateGroupIDs(page))
	assert.Equal(t, float64(1), page["group_window"].(map[string]any)["returned"])

	next := callFindDuplicates(t, client, map[string]any{"offset": 1})
	groups := duplicateGroupIDs(next)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0], large)
}

func TestFindDuplicatesRejectsAGroupTooLargeToReturn(t *testing.T) {
	transactions := []domain.Transaction{}
	for index := range app.MaxWindowLimit + 1 {
		transactions = append(transactions, duplicateTestTransaction(
			t, fmt.Sprintf("large_%03d", index), "2026-08-02", "Example Grocer", -100, false,
		))
	}
	client := connectDuplicateTestServer(t, transactions)

	result := callFindDuplicates(t, client, nil)
	assert.Equal(t, "invalid_operation", result["code"])
	assert.Contains(t, result["detail"], "more than 400 transactions")
}

func TestFindDuplicatesCoversEveryCurrency(t *testing.T) {
	euro := duplicateTestTransaction(t, "euro_a", "2026-08-20", "Example Cafe", -300, false)
	euro.Amount.Currency = "EUR"
	euroCopy := euro
	euroCopy.ID, euroCopy.ProviderID = "euro_b", "provider_euro_b"
	client := connectDuplicateTestServer(t, []domain.Transaction{
		duplicateTestTransaction(t, "dollar_a", "2026-08-20", "Example Cafe", -300, false),
		duplicateTestTransaction(t, "dollar_b", "2026-08-20", "Example Cafe", -300, false),
		euro, euroCopy,
	})

	document := callFindDuplicates(t, client, nil)
	assert.Equal(t, [][]string{{"euro_a", "euro_b"}, {"dollar_a", "dollar_b"}}, duplicateGroupIDs(document))
}

func TestFindDuplicatesLeavesOutTransfersUnlessAsked(t *testing.T) {
	transfer := duplicateTestTransaction(t, "transfer_a", "2026-08-20", "Example Bank", -10000, false)
	transfer.Category = domain.CategoryRef{ID: "category_transfer", Name: "Transfer", GroupID: "group_transfers", Group: "Transfers"}
	transferCopy := transfer
	transferCopy.ID, transferCopy.ProviderID = "transfer_b", "provider_transfer_b"
	client := connectDuplicateTestServer(t, []domain.Transaction{transfer, transferCopy})

	assert.Empty(t, duplicateGroupIDs(callFindDuplicates(t, client, nil)))
	included := callFindDuplicates(t, client, map[string]any{"include_transfers": true})
	assert.Equal(t, [][]string{{"transfer_a", "transfer_b"}}, duplicateGroupIDs(included))
}

func TestFindDuplicatesReturnsAGroupThatFillsTheResponse(t *testing.T) {
	transactions := []domain.Transaction{}
	for index := range app.MaxWindowLimit {
		transactions = append(transactions, duplicateTestTransaction(
			t, fmt.Sprintf("full_%03d", index), "2026-08-20", "Example Coffee", -450, false,
		))
	}
	transactions = append(transactions,
		duplicateTestTransaction(t, "older_a", "2026-08-02", "Example Grocer", -100, false),
		duplicateTestTransaction(t, "older_b", "2026-08-02", "Example Grocer", -100, false),
	)
	client := connectDuplicateTestServer(t, transactions)

	page := callFindDuplicates(t, client, nil)
	groups := duplicateGroupIDs(page)
	require.Len(t, groups, 1, page)
	assert.Len(t, groups[0], app.MaxWindowLimit)

	next := callFindDuplicates(t, client, map[string]any{"offset": 1})
	assert.Equal(t, [][]string{{"older_a", "older_b"}}, duplicateGroupIDs(next))
}
