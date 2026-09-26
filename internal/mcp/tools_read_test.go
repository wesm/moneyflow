package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestOfficialClientReadToolsUseExactMoneyAndLiteralNotesSearch(t *testing.T) {
	date, err := domain.ParseDate("2026-08-29")
	require.NoError(t, err)
	service, err := app.NewService([]domain.Transaction{{
		ID: "transaction_a", ProviderID: "provider_a", Provider: "fixture",
		Account: domain.EntityRef{ID: "account_a", Name: "Example Account"}, Date: date,
		Merchant: domain.EntityRef{ID: "merchant_a", Name: "Example Merchant"},
		Category: domain.CategoryRef{ID: "category_a", Name: "Food", GroupID: "group_a", Group: "Living"},
		Amount:   domain.Money{Minor: -9007199254740993, Currency: "USD", Scale: 2},
		Notes:    "literal [memo]",
	}, {
		ID: "transaction_hidden", ProviderID: "provider_hidden", Provider: "fixture",
		Account: domain.EntityRef{ID: "account_a", Name: "Example Account"}, Date: date,
		Merchant: domain.EntityRef{ID: "merchant_b", Name: "Hidden Merchant"},
		Category: domain.CategoryRef{ID: "category_a", Name: "Food", GroupID: "group_a", Group: "Living"},
		Amount:   domain.Money{Minor: -100, Currency: "USD", Scale: 2}, Hidden: true,
	}})
	require.NoError(t, err)
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()

	result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "search_transactions", Arguments: map[string]any{"query": "[MEMO]", "limit": 10},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	structured, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1", structured["version"])
	rows := structured["transactions"].([]any)
	require.Len(t, rows, 1)
	amount := rows[0].(map[string]any)["money"].(map[string]any)
	assert.Equal(t, "-90071992547409.93", amount["amount"])
	assert.Equal(t, "-9007199254740993", amount["amount_minor"])
	text := result.Content[0].(*mcpsdk.TextContent).Text
	assert.JSONEq(t, text, mustJSON(t, structured))

	all, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "get_transactions"})
	require.NoError(t, err)
	assert.Equal(t, float64(2), all.StructuredContent.(map[string]any)["total"])
	visible, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "get_transactions", Arguments: map[string]any{"include_hidden": false},
	})
	require.NoError(t, err)
	assert.Equal(t, float64(1), visible.StructuredContent.(map[string]any)["total"])
	invalid, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "get_transactions", Arguments: map[string]any{"limit": 0},
	})
	require.NoError(t, err)
	assert.True(t, invalid.IsError)
	assert.Equal(t, "invalid_operation", invalid.StructuredContent.(map[string]any)["code"])
}

func TestOfficialClientResourcesMatchToolProjection(t *testing.T) {
	date, err := domain.ParseDate("2026-08-29")
	require.NoError(t, err)
	service, err := app.NewService([]domain.Transaction{{
		ID: "transaction_a", ProviderID: "provider_a", Provider: "fixture",
		Account: domain.EntityRef{ID: "account_a", Name: "Example Account"}, Date: date,
		Merchant: domain.EntityRef{ID: "merchant_a", Name: "Example Merchant"},
		Category: domain.CategoryRef{ID: "category_a", Name: "Food", GroupID: "group_a", Group: "Living"},
		Amount:   domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
	}})
	require.NoError(t, err)
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()

	tests := []struct {
		uri       string
		tool      string
		arguments map[string]any
	}{
		{uri: resourceAccount, tool: "get_account_info", arguments: map[string]any{"partition_limit": 1000}},
		{uri: resourceCategories, tool: "get_categories", arguments: map[string]any{"group_limit": 1000, "category_limit": 1000}},
		{uri: resourceMerchantsTop, tool: "get_merchants", arguments: map[string]any{"limit": 50}},
		{uri: resourceMonthly, tool: "get_spending_summary", arguments: map[string]any{"start_date": "2026-08-01", "end_date": "2026-08-31", "group_by": "category", "limit": 1000}},
		{uri: resourceRecent, tool: "get_transactions", arguments: map[string]any{"include_hidden": true, "limit": 50}},
	}
	for _, test := range tests {
		t.Run(test.uri, func(t *testing.T) {
			tool, callErr := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: test.tool, Arguments: test.arguments})
			require.NoError(t, callErr)
			assert.False(t, tool.IsError)
			resource, readErr := client.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{URI: test.uri})
			require.NoError(t, readErr)
			require.Len(t, resource.Contents, 1)
			assert.JSONEq(t, tool.Content[0].(*mcpsdk.TextContent).Text, resource.Contents[0].Text)
		})
	}
}

func TestReadToolBoundsAccountPartitionsAndTransactionMatches(t *testing.T) {
	date, err := domain.ParseDate("2026-08-29")
	require.NoError(t, err)
	service, err := app.NewService([]domain.Transaction{
		{ID: "a", ProviderID: "provider-a", Provider: "fixture", Date: date, Account: domain.EntityRef{ID: "account", Name: "Account"}, Merchant: domain.EntityRef{ID: "merchant", Name: "Merchant"}, Category: domain.CategoryRef{ID: "category", Name: "Category", GroupID: "group", Group: "Group"}, Amount: domain.Money{Minor: -1, Currency: "EUR", Scale: 2}},
		{ID: "b", ProviderID: "provider-b", Provider: "fixture", Date: date, Account: domain.EntityRef{ID: "account", Name: "Account"}, Merchant: domain.EntityRef{ID: "merchant", Name: "Merchant"}, Category: domain.CategoryRef{ID: "category", Name: "Category", GroupID: "group", Group: "Group"}, Amount: domain.Money{Minor: -1, Currency: "USD", Scale: 2}},
	})
	require.NoError(t, err)
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()

	account, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "get_account_info", Arguments: map[string]any{"partition_offset": 1, "partition_limit": 1},
	})
	require.NoError(t, err)
	structured := account.StructuredContent.(map[string]any)
	window := structured["partition_window"].(map[string]any)
	assert.Equal(t, float64(2), window["total"])
	assert.Equal(t, float64(1), window["returned"])
	assert.Len(t, structured["money_partitions"].([]any), 1)

	for _, arguments := range []map[string]any{
		{"transaction_id": "a", "match_limit": 21},
		{"transaction_id": "a", "item_limit": 101},
	} {
		result, callErr := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "get_transaction_details", Arguments: arguments})
		require.NoError(t, callErr)
		assert.True(t, result.IsError)
		assert.Equal(t, "invalid_operation", result.StructuredContent.(map[string]any)["code"])
	}
}

func TestCalendarDefaultsUseInjectedClockLocation(t *testing.T) {
	location := time.FixedZone("UTC-minus-7", -7*60*60)
	clock := func() time.Time { return time.Date(2026, 8, 31, 23, 30, 0, 0, location) }
	start, end, err := spendingDates(clock, "", "")
	require.NoError(t, err)
	assert.Equal(t, "2026-08-02", start.String())
	assert.Equal(t, "2026-08-31", end.String())

	monthStart, monthEnd, err := currentMonth(clock())
	require.NoError(t, err)
	assert.Equal(t, "2026-08-01", monthStart.String())
	assert.Equal(t, "2026-08-31", monthEnd.String())
}

func TestReviewDocumentsUseStableSnakeCaseWireTypes(t *testing.T) {
	date, err := domain.ParseDate("2026-08-29")
	require.NoError(t, err)
	document := ReviewDocument{
		Operations: reviewOperationDocuments([]app.ReviewOperation{{
			OperationID: "operation-a", Sequence: 99, Type: domain.OperationCategoryAssign,
			Active: true, AffectedCount: 2, Before: "Before", After: "After",
		}}),
		Targets: reviewTargetDocuments([]app.ReviewTarget{{
			TransactionID: "transaction-a", Date: date, Merchant: "Merchant", Category: "Category",
		}}),
	}
	encoded, err := json.Marshal(document)
	require.NoError(t, err)
	text := string(encoded)
	assert.Contains(t, text, `"operation_id":"operation-a"`)
	assert.Contains(t, text, `"affected_count":2`)
	assert.Contains(t, text, `"transaction_id":"transaction-a"`)
	assert.NotContains(t, text, "OperationID")
	assert.NotContains(t, text, "Sequence")
}

func connectReadTestServer(t *testing.T, service *app.Service) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A", ProfileRoot: t.TempDir(),
		Clock:  func() time.Time { return time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC) },
		Random: strings.NewReader(strings.Repeat("r", 128)), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	cleanup := func() {
		require.NoError(t, clientSession.Close())
		require.NoError(t, serverSession.Close())
		require.NoError(t, server.Close(context.Background()))
	}
	return clientSession, cleanup
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
