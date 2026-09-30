package mcp

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestMCPCommitStatusReportsRetryableAttentionWithoutRevisionChange(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	defer closeProfile()
	source.deleteFailure = provider.NewWriteFailure(provider.WriteResponseIncomplete)
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	rows := callWriteTool(t, client, "get_transactions", nil).StructuredContent.(map[string]any)
	id := rows["transactions"].([]any)[0].(map[string]any)["id"]
	staged := callWriteTool(t, client, "delete_transactions", map[string]any{
		"expected_revision": rows["revision"], "transaction_ids": []any{id},
	})
	require.False(t, staged.IsError)
	revision := staged.StructuredContent.(map[string]any)["revision"]
	committed := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	require.False(t, committed.IsError)
	preparedRevision := committed.StructuredContent.(map[string]any)["revision"]
	var status map[string]any
	require.Eventually(t, func() bool {
		result := callWriteTool(t, client, "get_commit_status", nil)
		require.False(t, result.IsError)
		status = result.StructuredContent.(map[string]any)
		return status["write"].(map[string]any)["phase"] == "attention_required"
	}, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, preparedRevision, status["revision"])
	write := status["write"].(map[string]any)
	assert.Equal(t, "retryable", write["attention_class"])
	assert.Equal(t, "provider_write_response_incomplete", write["attention_reason"])
	assert.Equal(t, "writing", write["resume_target"])
	assert.Equal(t, float64(1), write["failed"])
	assert.Equal(t, 1, source.fetchCalls(), "status polling must not refresh provider data")
}

func TestMCPReconcileRequiresNonzeroRevisionAndBatchVersion(t *testing.T) {
	for _, tool := range []string{"stop_and_reconcile", "confirm_reconcile"} {
		t.Run(tool, func(t *testing.T) {
			service, source, closeProfile := providerTestService(t, 1, "monarch")
			defer closeProfile()
			client, cleanup := connectWriteTestServer(t, service, true)
			defer cleanup()
			rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 1})
			require.NoError(t, err)
			staged := callWriteTool(t, client, "delete_transactions", map[string]any{
				"expected_revision": strconv.FormatUint(rows.Revision, 10), "transaction_ids": []string{rows.Rows[0].ID},
			})
			require.False(t, staged.IsError)
			revision := staged.StructuredContent.(map[string]any)["revision"]
			committed := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
			require.False(t, committed.IsError)
			var status app.ProviderWriteStatus
			require.Eventually(t, func() bool {
				status, err = service.ProviderWriteStatus(t.Context())
				return err == nil && status.Phase == store.WritePhaseAttentionRequired
			}, 3*time.Second, 10*time.Millisecond)
			require.Equal(t, store.WriteAttentionReconcileOnly, status.AttentionClass)
			require.Equal(t, store.WriteAttentionRejected, status.AttentionReason)
			for _, statusTool := range []string{"get_commit_status", "get_account_info"} {
				result := callWriteTool(t, client, statusTool, map[string]any{})
				require.False(t, result.IsError)
				write := result.StructuredContent.(map[string]any)["write"].(map[string]any)
				assert.Equal(t, string(status.Phase), write["phase"])
				assert.Equal(t, strconv.FormatUint(status.Version, 10), write["version"])
				assert.Equal(t, "reconcile_only", write["attention_class"])
				assert.Equal(t, "provider_write_rejected", write["attention_reason"])
				assert.Equal(t, string(status.ResumeTarget), write["resume_target"])
			}

			// An empty remote snapshot requires confirmation, so both tools can be
			// exercised against real application/store state rather than a fake token.
			source.setTransactionCount(0)
			args := map[string]any{"expected_revision": strconv.FormatUint(service.Revision(), 10), "batch_version": strconv.FormatUint(status.Version, 10)}
			if tool == "confirm_reconcile" {
				started := callWriteTool(t, client, "stop_and_reconcile", args)
				require.False(t, started.IsError)
				attemptID := started.StructuredContent.(map[string]any)["attempt_id"]
				var attempt map[string]any
				require.Eventually(t, func() bool {
					result := callWriteTool(t, client, "get_reconcile_status", map[string]any{"attempt_id": attemptID})
					attempt = result.StructuredContent.(map[string]any)
					return !result.IsError && attempt["state"] == string(AttemptConfirmationRequired)
				}, 3*time.Second, 10*time.Millisecond)
				status, err = service.ProviderWriteStatus(t.Context())
				require.NoError(t, err)
				args = map[string]any{"expected_revision": strconv.FormatUint(service.Revision(), 10), "batch_version": strconv.FormatUint(status.Version, 10),
					"attempt_id": attemptID, "confirmation_token": attempt["confirmation_token"]}
			}
			beforeRevision, beforeFetches := service.Revision(), source.fetchCalls()
			for _, field := range []string{"expected_revision", "batch_version"} {
				original := args[field]
				args[field] = "0"
				result := callWriteTool(t, client, tool, args)
				require.True(t, result.IsError, "zero %s must be rejected before reconciliation starts", field)
				assert.Equal(t, "invalid_operation", result.StructuredContent.(map[string]any)["code"])
				args[field] = original
			}
			assert.Equal(t, beforeRevision, service.Revision())
			assert.Equal(t, beforeFetches, source.fetchCalls())
			after, err := service.ProviderWriteStatus(t.Context())
			require.NoError(t, err)
			assert.Equal(t, status, after)
			valid := callWriteTool(t, client, tool, args)
			require.False(t, valid.IsError, "%v", valid.StructuredContent)
			if tool == "confirm_reconcile" {
				assert.Equal(t, string(AttemptCompleted), valid.StructuredContent.(map[string]any)["state"])
				review, reviewErr := service.Review(t.Context(), service.Revision(), app.ReviewWindow{Limit: 1})
				require.NoError(t, reviewErr)
				assert.Zero(t, review.Pending.ActiveOperations)
			}
		})
	}
}
