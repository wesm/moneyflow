package mcp

import (
	"context"
	"fmt"
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
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestWriteRegistrationIsConditionalAndExact(t *testing.T) {
	service, closeProfile := writeTestService(t, 2)
	defer closeProfile()
	readOnly, closeReadOnly := connectWriteTestServer(t, service, false)
	tools, err := readOnly.ListTools(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, tools.Tools, 14)
	closeReadOnly()

	writable, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	tools, err = writable.ListTools(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"batch_update_category", "commit_changes", "confirm_reconcile",
		"confirm_refresh_deletions", "get_account_info", "get_amazon_order_details",
		"get_categories", "get_commit_status", "get_merchants", "get_reconcile_status",
		"get_refresh_status", "get_spending_summary", "get_transaction_details",
		"get_transactions", "get_uncategorized_transactions", "pause_commit",
		"redo_changes", "refresh_data", "resume_commit", "review_changes",
		"search_transactions", "stop_and_reconcile", "undo_changes",
		"update_transaction_category",
	}, toolNames(tools.Tools))
}

func TestCategoryToolDryRunStagesOneOperationAndUndoRedo(t *testing.T) {
	service, closeProfile := writeTestService(t, 2)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()

	dryRun := callWriteTool(t, client, "update_transaction_category", map[string]any{
		"expected_revision": "1", "transaction_id": "transaction_000",
		"category_label": "  CATEGORY B ", "dry_run": true,
	})
	assert.False(t, dryRun.IsError)
	dryDocument := dryRun.StructuredContent.(map[string]any)
	assert.Equal(t, "1", dryDocument["revision"])
	assert.Equal(t, true, dryDocument["dry_run"])
	assert.Equal(t, float64(1), dryDocument["affected_count"])
	assert.Equal(t, float64(0), dryDocument["pending"].(map[string]any)["active_operations"])
	assert.Equal(t, uint64(1), service.Revision())

	staged := callWriteTool(t, client, "update_transaction_category", map[string]any{
		"expected_revision": "1", "transaction_id": "transaction_000",
		"category_id": "category_b",
	})
	assert.False(t, staged.IsError)
	stagedDocument := staged.StructuredContent.(map[string]any)
	assert.Equal(t, "2", stagedDocument["revision"])
	assert.Equal(t, false, stagedDocument["dry_run"])
	assert.Equal(t, float64(1), stagedDocument["pending"].(map[string]any)["active_operations"])

	review := callWriteTool(t, client, "review_changes", map[string]any{
		"expected_revision": "2", "operation_limit": 20,
	})
	operations := review.StructuredContent.(map[string]any)["operations"].([]any)
	require.Len(t, operations, 1)
	assert.Equal(t, "category.assign", operations[0].(map[string]any)["type"])
	assert.Equal(t, float64(1), operations[0].(map[string]any)["affected_count"])

	undone := callWriteTool(t, client, "undo_changes", map[string]any{"expected_revision": "2"})
	assert.False(t, undone.IsError)
	undoDocument := undone.StructuredContent.(map[string]any)
	assert.Equal(t, "3", undoDocument["revision"])
	assert.Equal(t, float64(0), undoDocument["pending"].(map[string]any)["active_operations"])
	assert.Equal(t, float64(1), undoDocument["pending"].(map[string]any)["inactive_operations"])

	redone := callWriteTool(t, client, "redo_changes", map[string]any{"expected_revision": "3"})
	assert.False(t, redone.IsError)
	redoDocument := redone.StructuredContent.(map[string]any)
	assert.Equal(t, "4", redoDocument["revision"])
	assert.Equal(t, float64(1), redoDocument["pending"].(map[string]any)["active_operations"])
}

func TestCategoryToolBatchIsAllOrNothingAndBounded(t *testing.T) {
	service, closeProfile := writeTestService(t, 101)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	ids := make([]any, 100)
	for index := range ids {
		ids[index] = fmt.Sprintf("transaction_%03d", index)
	}

	duplicate := append(append([]any(nil), ids[:99]...), ids[0])
	failed := callWriteTool(t, client, "batch_update_category", map[string]any{
		"expected_revision": "1", "transaction_ids": duplicate, "category_id": "category_b",
	})
	assert.True(t, failed.IsError)
	assert.Equal(t, "invalid_operation", failed.StructuredContent.(map[string]any)["code"])
	assert.Equal(t, uint64(1), service.Revision())

	tooMany := append(append([]any(nil), ids...), "transaction_100")
	failed = callWriteTool(t, client, "batch_update_category", map[string]any{
		"expected_revision": "1", "transaction_ids": tooMany, "category_id": "category_b",
	})
	assert.True(t, failed.IsError)
	assert.Equal(t, uint64(1), service.Revision())

	missing := append([]any(nil), ids...)
	missing[99] = "transaction_missing"
	failed = callWriteTool(t, client, "batch_update_category", map[string]any{
		"expected_revision": "1", "transaction_ids": missing, "category_id": "category_b",
	})
	assert.True(t, failed.IsError)
	assert.Equal(t, "invalid_target", failed.StructuredContent.(map[string]any)["code"])
	assert.Equal(t, uint64(1), service.Revision())

	staged := callWriteTool(t, client, "batch_update_category", map[string]any{
		"expected_revision": "1", "transaction_ids": ids, "category_id": "category_b",
	})
	assert.False(t, staged.IsError)
	document := staged.StructuredContent.(map[string]any)
	assert.Equal(t, "2", document["revision"])
	assert.Equal(t, float64(100), document["affected_count"])
	assert.Equal(t, float64(1), document["pending"].(map[string]any)["active_operations"])

	stale := callWriteTool(t, client, "update_transaction_category", map[string]any{
		"expected_revision": "1", "transaction_id": "transaction_100", "category_id": "category_a",
	})
	assert.True(t, stale.IsError)
	assert.Equal(t, "revision_conflict", stale.StructuredContent.(map[string]any)["code"])
	assert.Equal(t, uint64(2), service.Revision())
}

func callWriteTool(
	t *testing.T,
	client *mcpsdk.ClientSession,
	name string,
	arguments map[string]any,
) *mcpsdk.CallToolResult {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	return result
}

func connectWriteTestServer(
	t *testing.T,
	service *app.Service,
	allowWrite bool,
) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A", ProfileRoot: t.TempDir(),
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("r", 128)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{AllowWrite: allowWrite})
	require.NoError(t, err)
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	return clientSession, func() {
		require.NoError(t, clientSession.Close())
		require.NoError(t, serverSession.Wait())
		require.NoError(t, server.Close(context.Background()))
	}
}

func writeTestService(t *testing.T, transactionCount int) (*app.Service, func()) {
	t.Helper()
	date, err := domain.ParseDate("2026-08-29")
	require.NoError(t, err)
	transactions := make([]domain.Transaction, transactionCount)
	for index := range transactions {
		category := domain.CategoryRef{ID: "category_a", Name: "Category A", GroupID: "group_a", Group: "Group A"}
		if index == transactionCount-1 {
			category = domain.CategoryRef{ID: "category_b", Name: "Category B", GroupID: "group_b", Group: "Group B"}
		}
		transactions[index] = domain.Transaction{
			ID: fmt.Sprintf("transaction_%03d", index), ProviderID: fmt.Sprintf("provider_%03d", index), Provider: "fixture",
			Account: domain.EntityRef{ID: "account_a", Name: "Example Account"}, Date: date,
			Merchant: domain.EntityRef{ID: "merchant_a", Name: "Example Merchant"}, Category: category,
			Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
		}
	}
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(context.Background(), committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	return service, func() { require.NoError(t, profile.Close()) }
}
