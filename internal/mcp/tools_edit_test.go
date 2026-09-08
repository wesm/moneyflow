package mcp

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestMCPMerchantReassignmentRenameAndExplicitMerge(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	args := map[string]any{"expected_revision": "1", "transaction_ids": []string{"transaction_000"}, "new_merchant_label": "New Merchant", "dry_run": true}
	preview := callWriteTool(t, client, "reassign_transactions_merchant", args)
	require.False(t, preview.IsError, "%v", preview.StructuredContent)
	assert.Equal(t, uint64(1), service.Revision())
	args["dry_run"] = false
	staged := callWriteTool(t, client, "reassign_transactions_merchant", args)
	require.False(t, staged.IsError, "%v", staged.StructuredContent)
	doc := staged.StructuredContent.(map[string]any)
	change := doc["changes"].([]any)[0].(map[string]any)
	destination := change["after"].(map[string]any)["merchant"].(map[string]any)["id"]
	assert.NotEqual(t, "merchant_a", destination)
	assert.Equal(t, float64(1), doc["affected_count"])
	existing := callWriteTool(t, client, "reassign_transactions_merchant", map[string]any{"expected_revision": "2", "transaction_ids": []string{"transaction_001"}, "merchant_id": destination, "dry_run": true})
	require.False(t, existing.IsError, "%v", existing.StructuredContent)
	assert.Equal(t, destination, existing.StructuredContent.(map[string]any)["changes"].([]any)[0].(map[string]any)["after"].(map[string]any)["merchant"].(map[string]any)["id"])
	assert.Equal(t, uint64(2), service.Revision())

	renamed := callWriteTool(t, client, "rename_merchant", map[string]any{"expected_revision": "2", "merchant_id": "merchant_a", "new_label": "Renamed Merchant"})
	require.False(t, renamed.IsError, "%v", renamed.StructuredContent)
	assert.Equal(t, float64(2), renamed.StructuredContent.(map[string]any)["affected_count"])
	for _, row := range renamed.StructuredContent.(map[string]any)["changes"].([]any) {
		merchant := row.(map[string]any)["after"].(map[string]any)["merchant"].(map[string]any)
		assert.Equal(t, "merchant_a", merchant["id"])
		assert.Equal(t, "Renamed Merchant", merchant["label"])
	}
	mergeArgs := map[string]any{"expected_revision": "3", "merchant_id": "merchant_a", "new_label": "New Merchant"}
	refused := callWriteTool(t, client, "rename_merchant", mergeArgs)
	require.True(t, refused.IsError)
	assert.Equal(t, uint64(3), service.Revision())
	mergeArgs["merge_destination_id"] = destination
	merged := callWriteTool(t, client, "rename_merchant", mergeArgs)
	require.False(t, merged.IsError, "%v", merged.StructuredContent)
	for _, row := range merged.StructuredContent.(map[string]any)["changes"].([]any) {
		assert.Equal(t, destination, row.(map[string]any)["after"].(map[string]any)["merchant"].(map[string]any)["id"])
	}
	undone := callWriteTool(t, client, "undo_changes", map[string]any{"expected_revision": "4"})
	require.False(t, undone.IsError)
	redone := callWriteTool(t, client, "redo_changes", map[string]any{"expected_revision": "5"})
	require.False(t, redone.IsError)
	committed := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": "6", "reviewed_revision": "6"})
	require.False(t, committed.IsError)
	assert.Equal(t, true, committed.StructuredContent.(map[string]any)["completed"])
}

func TestMCPWholeMerchantPreviewIsBoundedButEditIsComplete(t *testing.T) {
	service, closeProfile := writeTestService(t, 101)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	result := callWriteTool(t, client, "rename_merchant", map[string]any{"expected_revision": "1", "merchant_id": "merchant_a", "new_label": "Renamed"})
	require.False(t, result.IsError, "%v", result.StructuredContent)
	doc := result.StructuredContent.(map[string]any)
	assert.Equal(t, float64(101), doc["affected_count"])
	assert.Len(t, doc["changes"], 100)
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 101})
	require.NoError(t, err)
	for _, row := range rows.Rows {
		assert.Equal(t, "Renamed", row.Merchant.Name)
	}
}

func TestMCPHideCancellationRemovesEveryPendingToggleEffect(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	for index, ids := range [][]string{{"transaction_000"}, {"transaction_000", "transaction_001"}} {
		result := callWriteTool(t, client, "toggle_transactions_hidden", map[string]any{"expected_revision": strconv.Itoa(index + 1), "transaction_ids": ids})
		require.False(t, result.IsError)
	}
	// transaction_000 has two active toggles: canceling both leaves it visible.
	for _, dry := range []bool{true, false} {
		result := callWriteTool(t, client, "toggle_transactions_hidden", map[string]any{"expected_revision": "3", "transaction_ids": []string{"transaction_000"}, "dry_run": dry})
		require.False(t, result.IsError)
		change := result.StructuredContent.(map[string]any)["changes"].([]any)[0].(map[string]any)
		assert.Equal(t, false, change["before"].(map[string]any)["hidden"])
		assert.Equal(t, false, change["after"].(map[string]any)["hidden"])
	}
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Filter: app.TransactionFilter{IncludeHidden: true}, Limit: 3})
	require.NoError(t, err)
	for _, row := range rows.Rows {
		assert.Equal(t, row.ID == "transaction_001", row.Hidden)
	}
}

func TestMCPYNABRejectsUnsupportedHideAndTransferEdits(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "ynab")
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 1})
	require.NoError(t, err)
	id := rows.Rows[0].ID
	for _, dry := range []bool{true, false} {
		result := callWriteTool(t, client, "toggle_transactions_hidden", map[string]any{"expected_revision": strconv.FormatUint(rows.Revision, 10), "transaction_ids": []string{id}, "dry_run": dry})
		require.True(t, result.IsError)
		assert.Equal(t, "provider_write_unsupported", result.StructuredContent.(map[string]any)["code"])
		assert.Equal(t, rows.Revision, service.Revision())
	}
	source.mu.Lock()
	source.snapshot.WriteRestrictions = []domain.ImportWriteRestriction{{Kind: domain.EntityKindTransaction, ExternalID: source.snapshot.Transactions[0].ExternalID, Reason: "transfer"}}
	source.mu.Unlock()
	_, err = service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	revision := service.Revision()
	for _, name := range []string{"delete_transactions", "reassign_transactions_merchant"} {
		args := map[string]any{"expected_revision": strconv.FormatUint(revision, 10), "transaction_ids": []string{id}}
		if name == "reassign_transactions_merchant" {
			args["new_merchant_label"] = "New Merchant"
		}
		result := callWriteTool(t, client, name, args)
		require.True(t, result.IsError)
		assert.Equal(t, "provider_write_unsupported", result.StructuredContent.(map[string]any)["code"])
		assert.Equal(t, revision, service.Revision())
	}
}

func TestMCPProviderDeletionFailureRetainsDurableIntent(t *testing.T) {
	for _, kind := range []string{"monarch", "ynab"} {
		t.Run(kind, func(t *testing.T) {
			service, _, closeProfile := providerTestService(t, 1, kind)
			defer closeProfile()
			client, cleanup := connectWriteTestServer(t, service, true)
			defer cleanup()
			rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 1})
			require.NoError(t, err)
			staged := callWriteTool(t, client, "delete_transactions", map[string]any{"expected_revision": strconv.FormatUint(rows.Revision, 10), "transaction_ids": []string{rows.Rows[0].ID}})
			require.False(t, staged.IsError)
			revision := staged.StructuredContent.(map[string]any)["revision"]
			committed := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
			require.False(t, committed.IsError)
			require.Eventually(t, func() bool {
				status, statusErr := service.ProviderWriteStatus(t.Context())
				return statusErr == nil && status.Phase == store.WritePhaseAttentionRequired
			}, 3*time.Second, 10*time.Millisecond)
			review := callWriteTool(t, client, "review_changes", map[string]any{"expected_revision": strconv.FormatUint(service.Revision(), 10)})
			require.False(t, review.IsError)
			assert.Equal(t, float64(1), review.StructuredContent.(map[string]any)["pending"].(map[string]any)["active_operations"])
		})
	}
}

func TestMCPHideCancellationAndDeletionPreview(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	for index, dry := range []bool{true, false, true, false} {
		revision := "1"
		if index >= 2 {
			revision = "2"
		}
		result := callWriteTool(t, client, "toggle_transactions_hidden", map[string]any{"expected_revision": revision, "transaction_ids": []string{"transaction_000", "transaction_001"}, "dry_run": dry})
		require.False(t, result.IsError, "%v", result.StructuredContent)
		for _, value := range result.StructuredContent.(map[string]any)["changes"].([]any) {
			assert.Equal(t, index < 2, value.(map[string]any)["after"].(map[string]any)["hidden"])
		}
	}
	review := callWriteTool(t, client, "review_changes", map[string]any{"expected_revision": "3"})
	require.False(t, review.IsError)
	assert.Equal(t, float64(0), review.StructuredContent.(map[string]any)["pending"].(map[string]any)["active_operations"])
	for _, dry := range []bool{true, false} {
		result := callWriteTool(t, client, "delete_transactions", map[string]any{"expected_revision": "3", "transaction_ids": []string{"transaction_000", "transaction_001"}, "dry_run": dry})
		require.False(t, result.IsError, "%v", result.StructuredContent)
		for _, value := range result.StructuredContent.(map[string]any)["changes"].([]any) {
			assert.Nil(t, value.(map[string]any)["after"])
		}
		if dry {
			assert.Equal(t, uint64(3), service.Revision())
		}
	}
	rows := callWriteTool(t, client, "get_transactions", nil)
	require.False(t, rows.IsError)
	assert.Equal(t, float64(1), rows.StructuredContent.(map[string]any)["total"])
	require.False(t, callWriteTool(t, client, "undo_changes", map[string]any{"expected_revision": "4"}).IsError)
	rows = callWriteTool(t, client, "get_transactions", nil)
	assert.Equal(t, float64(3), rows.StructuredContent.(map[string]any)["total"])
}

func TestMCPEditToolsRejectWholeInvalidBatchAndStaleRevision(t *testing.T) {
	for _, name := range []string{"reassign_transactions_merchant", "toggle_transactions_hidden", "delete_transactions"} {
		t.Run(name, func(t *testing.T) {
			service, closeProfile := writeTestService(t, 101)
			defer closeProfile()
			client, cleanup := connectWriteTestServer(t, service, true)
			defer cleanup()
			tooMany := make([]string, 101)
			for index := range tooMany {
				tooMany[index] = "transaction_" + strconv.Itoa(index)
			}
			for _, ids := range [][]string{nil, {"transaction_000", "missing"}, {"transaction_000", "transaction_000"}, tooMany} {
				args := map[string]any{"expected_revision": "1", "transaction_ids": ids}
				if name == "reassign_transactions_merchant" {
					args["new_merchant_label"] = "New Merchant"
				}
				result := callWriteTool(t, client, name, args)
				assert.True(t, result.IsError)
				assert.Equal(t, uint64(1), service.Revision())
			}
			args := map[string]any{"expected_revision": "2", "transaction_ids": []string{"transaction_000"}}
			if name == "reassign_transactions_merchant" {
				args["new_merchant_label"] = "New Merchant"
			}
			result := callWriteTool(t, client, name, args)
			require.True(t, result.IsError)
			assert.Equal(t, "revision_conflict", result.StructuredContent.(map[string]any)["code"])
		})
	}
}
