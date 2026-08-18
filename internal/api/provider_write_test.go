package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProviderWriteRoutesAreProtectedAndCredentialBlind(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	response := requestServer(t, fixture.server, http.MethodGet, "/api/v1/provider/write-status", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var status ProviderWriteStatusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Equal(t, ProviderWriteSchemaVersion, status.Version)
	assert.Equal(t, "1", status.Revision)
	assert.Equal(t, "1", status.Generation)
	assert.Empty(t, status.BatchVersion)
	assert.NotContains(t, response.Body.String(), "subscription-example")
	assert.NotContains(t, response.Body.String(), "Example Merchant")

	for _, endpoint := range []string{
		"pause", "resume", "reconcile", "reconcile/confirm",
	} {
		response = requestJSON(t, fixture.server, "/api/v1/provider/write/"+endpoint, map[string]any{})
		assert.Equal(t, http.StatusForbidden, response.Code, endpoint)
	}
}

func TestProviderCommitReturnsPreparedBatchAndStartsWebWorker(t *testing.T) {
	t.Parallel()

	fixture := newProviderAPIFixture(t, "/", 3)
	fixture.source.enableWrites()
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)
	view := projectPersistentViewForQuery(t, fixture.server, query)
	require.NotEmpty(t, view.DetailRows)

	mutatedResponse := requestProtectedJSON(t, fixture.server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: view.Revision,
		Query: query, Selection: view.Selection, Action: app.ActionToggleHidden,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: view.DetailRows[0].Identity},
		Input:  MutationInput{}, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, mutatedResponse.Code, mutatedResponse.Body.String())
	var mutated MutationResponse
	require.NoError(t, json.Unmarshal(mutatedResponse.Body.Bytes(), &mutated))

	committedResponse := requestProtectedJSON(t, fixture.server, "/api/v1/commit", CommitBody{
		Version: MutationSchemaVersion, ExpectedRevision: mutated.Revision,
		ReviewedRevision: mutated.Revision, Query: query,
		Selection: mutated.Projection.Selection, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, committedResponse.Code, committedResponse.Body.String())
	var committed MutationResponse
	require.NoError(t, json.Unmarshal(committedResponse.Body.Bytes(), &committed))
	require.NotNil(t, committed.ProviderWrite)
	assert.Equal(t, string(store.WritePhaseWriting), committed.ProviderWrite.Phase)
	assert.Equal(t, 1, committed.ProviderWrite.Total)
	assert.Equal(t, 1, committed.Pending.ActiveOperations)
	assert.NotContains(t, committedResponse.Body.String(), "transaction-example")

	require.Eventually(t, func() bool {
		response := requestServer(
			t, fixture.server, http.MethodGet, "/api/v1/provider/write-status", nil,
		)
		if response.Code != http.StatusOK {
			return false
		}
		var current ProviderWriteStatusResponse
		if json.Unmarshal(response.Body.Bytes(), &current) != nil {
			return false
		}
		return current.Phase == "" && current.BatchVersion == ""
	}, 3*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, fixture.source.writeCount())
}

func TestProviderWriteStatusActionsArePhaseSpecific(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status app.ProviderWriteStatus
		want   []string
	}{
		{name: "writing", status: app.ProviderWriteStatus{Phase: store.WritePhaseWriting, OwnerInstanceID: "owner"}, want: []string{"pause"}},
		{name: "paused", status: app.ProviderWriteStatus{Phase: store.WritePhasePaused}, want: []string{"resume", "reconcile"}},
		{name: "retryable", status: app.ProviderWriteStatus{Phase: store.WritePhaseAttentionRequired, AttentionClass: store.WriteAttentionRetryable}, want: []string{"resume", "reconcile"}},
		{name: "reconcile only", status: app.ProviderWriteStatus{Phase: store.WritePhaseAttentionRequired, AttentionClass: store.WriteAttentionReconcileOnly}, want: []string{"reconcile"}},
		{name: "confirmation", status: app.ProviderWriteStatus{Phase: store.WritePhaseReconcileConfirmationRequired}, want: []string{"confirm"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wire := providerWriteStatusToWire(7, 5, test.status)
			assert.Equal(t, test.want, wire.Actions)
		})
	}
}

func projectPersistentViewForQuery(t testing.TB, server *Server, query string) Projection {
	t.Helper()
	response := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: query, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	return projection
}

type apiProviderWriter struct{ source *apiProviderSource }

func (writer apiProviderWriter) ProbeIdentity(_ context.Context) (provider.ProfileIdentity, error) {
	writer.source.mu.Lock()
	defer writer.source.mu.Unlock()
	return writer.source.identity, nil
}

func (writer apiProviderWriter) UpdateTransaction(
	_ context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	writer.source.mu.Lock()
	writer.source.writes++
	writer.source.mu.Unlock()
	result := provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID}
	if update.MerchantName.Present {
		result.MerchantExternalID = provider.Some("merchant-example")
		result.MerchantLabel = provider.Some(update.MerchantName.Value)
	}
	if update.CategoryExternalID.Present {
		result.CategoryExternalID = provider.Some(update.CategoryExternalID.Value)
	}
	if update.Hidden.Present {
		result.Hidden = provider.Some(update.Hidden.Value)
	}
	return result, nil
}
