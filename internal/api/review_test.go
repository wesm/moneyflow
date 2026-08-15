package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestReviewTypesExposeBoundedSummariesWithoutJournalPayloads(t *testing.T) {
	t.Parallel()

	projection := app.ReviewProjection{
		Revision: 9,
		Pending:  app.PendingSummary{ActiveOperations: 1, AffectedTransactions: 2},
		ActiveOperations: []app.ReviewOperation{{
			OperationID: "operation-a", Sequence: 42, Type: domain.OperationMerchantLabel,
			Active: true, AffectedCount: 2, Before: "Before", After: "After",
		}},
	}
	wire := reviewToWire(projection)
	encoded, err := json.Marshal(wire)
	require.NoError(t, err)
	text := string(encoded)
	assert.Contains(t, text, `"revision":"9"`)
	assert.Contains(t, text, `"operation_id":"operation-a"`)
	assert.NotContains(t, text, "sequence")
	assert.NotContains(t, text, "payload")
	assert.NotContains(t, text, "transaction_ids")
}

func TestReviewTypesSeparateSummaryAndTargetRequests(t *testing.T) {
	t.Parallel()

	summary := ReviewBody{Version: ReviewSchemaVersion, ExpectedRevision: "9"}
	targets := ReviewTargetsBody{
		Version: ReviewSchemaVersion, ExpectedRevision: "9", OperationID: "operation-a",
		Window: Window{Offset: 400, Limit: app.MaxReviewTargetLimit},
	}
	encoded, err := json.Marshal(summary)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "operation_id")
	assert.Equal(t, app.MaxReviewTargetLimit, targets.Window.Limit)
}

func TestReviewEndpointsReturnSeparateSummariesAndBoundedTargets(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	mutated := appendMerchantMutation(t, server, "Renamed Merchant")
	summaryResponse := requestProtectedJSON(t, server, "/api/v1/review", ReviewBody{
		Version: ReviewSchemaVersion, ExpectedRevision: mutated.Revision,
	})
	require.Equal(t, http.StatusOK, summaryResponse.Code, summaryResponse.Body.String())
	var summary ReviewResponse
	require.NoError(t, json.Unmarshal(summaryResponse.Body.Bytes(), &summary))
	assert.Equal(t, mutated.Revision, summary.Revision)
	require.Len(t, summary.ActiveOperations, 1)
	assert.Empty(t, summary.InactiveOperations)
	assert.Empty(t, summary.Targets)
	assert.NotContains(t, summaryResponse.Body.String(), "sequence")
	assert.NotContains(t, summaryResponse.Body.String(), "payload")

	operationID := summary.ActiveOperations[0].OperationID
	targetResponse := requestProtectedJSON(t, server, "/api/v1/review/targets", ReviewTargetsBody{
		Version: ReviewSchemaVersion, ExpectedRevision: mutated.Revision,
		OperationID: operationID, Window: Window{Limit: app.MaxReviewTargetLimit},
	})
	require.Equal(t, http.StatusOK, targetResponse.Code, targetResponse.Body.String())
	var targets ReviewResponse
	require.NoError(t, json.Unmarshal(targetResponse.Body.Bytes(), &targets))
	assert.Equal(t, operationID, targets.OperationID)
	assert.LessOrEqual(t, len(targets.Targets), app.MaxReviewTargetLimit)
	require.Len(t, targets.Targets, 1)
	assert.Equal(t, "Renamed Merchant", targets.Targets[0].Merchant)
	assert.NotContains(t, targetResponse.Body.String(), "transaction_id")

	oversized := requestProtectedJSON(t, server, "/api/v1/review/targets", ReviewTargetsBody{
		Version: ReviewSchemaVersion, ExpectedRevision: mutated.Revision,
		OperationID: operationID, Window: Window{Limit: app.MaxReviewTargetLimit + 1},
	})
	assert.Equal(t, http.StatusUnprocessableEntity, oversized.Code, oversized.Body.String())
}

func TestReviewEndpointRejectsStaleRevisionWithoutChangingJournal(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	mutated := appendMerchantMutation(t, server, "Renamed Merchant")
	undone := requestRevisionAction(t, server, "/api/v1/undo", RevisionBody{
		Version: MutationSchemaVersion, ExpectedRevision: mutated.Revision,
		Query: mutated.CanonicalQuery, Selection: mutated.Projection.Selection,
		Window: Window{Limit: 200},
	})
	response := requestProtectedJSON(t, server, "/api/v1/review", ReviewBody{
		Version: ReviewSchemaVersion, ExpectedRevision: mutated.Revision,
	})
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeRevisionConflict), problem.Code)
	assert.Equal(t, undone.Revision, problem.CurrentRevision)

	current := requestProtectedJSON(t, server, "/api/v1/review", ReviewBody{
		Version: ReviewSchemaVersion, ExpectedRevision: undone.Revision,
	})
	require.Equal(t, http.StatusOK, current.Code, current.Body.String())
	var review ReviewResponse
	require.NoError(t, json.Unmarshal(current.Body.Bytes(), &review))
	assert.Zero(t, review.Pending.ActiveOperations)
	assert.Equal(t, 1, review.Pending.InactiveOperations)
}
