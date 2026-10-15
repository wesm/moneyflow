package mcp

import (
	"strconv"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// A provider read that panics during a reconcile fails the attempt and gives the reconcile lease
// back, so a new reconcile starts at once.
func TestMCPReconcileStartsAgainAfterAProviderPanic(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	t.Cleanup(closeProfile)
	client, supervisor := connectSupervisedServer(t, service, time.Now)
	commitRejectedDeletion(t, client, service, supervisor)
	source.panicOnNextFetch()

	failed := awaitReconcileTool(t, client, service, supervisor)
	retried := awaitReconcileTool(t, client, service, supervisor)

	assert.Equal(t, string(AttemptFailed), failed["state"])
	assert.Equal(t, "mcp_internal_error", failed["code"])
	assert.Equal(t, string(AttemptCompleted), retried["state"], "%v", retried)
}

// A reconcile confirmation fold that panics gives the reconcile lease back as well.
func TestMCPReconcileStartsAgainAfterAConfirmationPanic(t *testing.T) {
	var folds *foldPanicProfile
	service, source, closeProfile := providerTestServiceOn(t, 1, "monarch",
		func(profile store.Profile) store.Profile {
			folds = &foldPanicProfile{Profile: profile}
			return folds
		})
	t.Cleanup(closeProfile)
	client, supervisor := connectSupervisedServer(t, service, time.Now)
	commitRejectedDeletion(t, client, service, supervisor)
	source.setTransactionCount(0)
	parked := awaitReconcileTool(t, client, service, supervisor)
	require.Equal(t, string(AttemptConfirmationRequired), parked["state"])
	folds.armed.Store(true)

	confirmed := callWriteTool(t, client, "confirm_reconcile",
		reconcileArguments(t, service, map[string]any{
			"attempt_id": parked["attempt_id"], "confirmation_token": parked["confirmation_token"],
		}))
	retried := awaitReconcileTool(t, client, service, supervisor)

	require.True(t, confirmed.IsError)
	assert.Equal(t, "mcp_internal_error", confirmed.StructuredContent.(map[string]any)["code"])
	assert.Equal(t, string(AttemptConfirmationRequired), retried["state"], "%v", retried)
}

// A parked reconcile fails at the service's confirmation deadline, and a new reconcile can then
// start.
func TestMCPReconcileStartsAgainAfterItsConfirmationExpires(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	t.Cleanup(closeProfile)
	client, supervisor, advance := connectClockedServer(t, service, source, 0)
	commitRejectedDeletion(t, client, service, supervisor)
	source.setTransactionCount(0)
	parked := awaitReconcileTool(t, client, service, supervisor)
	require.Equal(t, string(AttemptConfirmationRequired), parked["state"])

	advance(10 * time.Minute)
	expired := callRefreshTool(t, client, "get_reconcile_status",
		map[string]any{"attempt_id": parked["attempt_id"]})
	assert.Equal(t, string(AttemptFailed), expired["state"])
	assert.Equal(t, "provider_confirmation_invalid", expired["code"])
	retried := awaitReconcileTool(t, client, service, supervisor)

	assert.NotEqual(t, parked["attempt_id"], retried["attempt_id"])
	assert.Equal(t, string(AttemptConfirmationRequired), retried["state"], "%v", retried)
}

// commitRejectedDeletion commits a deletion that the provider rejects and waits until the work of
// supervisor ends. That leaves a write batch that only a reconcile can finish.
func commitRejectedDeletion(
	t *testing.T,
	client *mcpsdk.ClientSession,
	service *app.Service,
	supervisor *Supervisor,
) {
	t.Helper()
	revision := stageDeletion(t, client, service)
	callRefreshTool(t, client, "commit_changes",
		map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	supervisor.wait.Wait()
	status, err := service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
}

// reconcileArguments adds the current revision and write batch version to arguments.
func reconcileArguments(
	t *testing.T,
	service *app.Service,
	arguments map[string]any,
) map[string]any {
	t.Helper()
	status, err := service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	arguments["expected_revision"] = strconv.FormatUint(service.Revision(), 10)
	arguments["batch_version"] = strconv.FormatUint(status.Version, 10)
	return arguments
}

// awaitReconcileTool starts a reconcile and returns its status once the work of supervisor ends.
func awaitReconcileTool(
	t *testing.T,
	client *mcpsdk.ClientSession,
	service *app.Service,
	supervisor *Supervisor,
) map[string]any {
	t.Helper()
	started := callRefreshTool(t, client, "stop_and_reconcile",
		reconcileArguments(t, service, map[string]any{}))
	supervisor.wait.Wait()
	return callRefreshTool(t, client, "get_reconcile_status",
		map[string]any{"attempt_id": started["attempt_id"]})
}

func TestMCPCommitStatusReportsRetryableAttentionWithoutRevisionChange(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "monarch")
	t.Cleanup(closeProfile)
	source.deleteFailure = provider.NewWriteFailure(provider.WriteResponseIncomplete)
	client, supervisor := connectSupervisedServer(t, service, time.Now)
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
	supervisor.wait.Wait()
	result := callWriteTool(t, client, "get_commit_status", nil)
	require.False(t, result.IsError)
	status := result.StructuredContent.(map[string]any)
	write := status["write"].(map[string]any)
	require.Equal(t, "attention_required", write["phase"])
	assert.Equal(t, preparedRevision, status["revision"])
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
			t.Cleanup(closeProfile)
			client, supervisor := connectSupervisedServer(t, service, time.Now)
			rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 1})
			require.NoError(t, err)
			staged := callWriteTool(t, client, "delete_transactions", map[string]any{
				"expected_revision": strconv.FormatUint(rows.Revision, 10), "transaction_ids": []string{rows.Rows[0].ID},
			})
			require.False(t, staged.IsError)
			revision := staged.StructuredContent.(map[string]any)["revision"]
			committed := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
			require.False(t, committed.IsError)
			supervisor.wait.Wait()
			status, err := service.ProviderWriteStatus(t.Context())
			require.NoError(t, err)
			require.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
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
				supervisor.wait.Wait()
				result := callWriteTool(t, client, "get_reconcile_status", map[string]any{"attempt_id": attemptID})
				require.False(t, result.IsError)
				attempt := result.StructuredContent.(map[string]any)
				require.Equal(t, string(AttemptConfirmationRequired), attempt["state"])
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
