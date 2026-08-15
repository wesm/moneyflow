package api

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestMutationTypesUseVersionedExactBoundedWireShape(t *testing.T) {
	t.Parallel()

	body := MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: "18446744073709551615",
		Query: "v=1", Selection: string(app.EmptySelection()),
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: "transaction-a"},
		Input:  MutationInput{Scope: "transactions", Label: "Example Merchant"},
		Window: Window{Offset: 0, Limit: 200},
	}
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"version":"1"`)
	assert.Contains(t, string(encoded), `"expected_revision":"18446744073709551615"`)
	assert.NotContains(t, string(encoded), "operation_id")
	assert.NotContains(t, string(encoded), "payload")
	assert.NotContains(t, string(encoded), "provider")

	revision, err := parseRevision(body.ExpectedRevision)
	require.NoError(t, err)
	assert.Equal(t, uint64(math.MaxUint64), revision)
	for _, value := range []string{"", "-1", "1.0", "18446744073709551616"} {
		_, err = parseRevision(value)
		assert.Error(t, err, value)
	}
}

func TestMutationTypesReturnRevisionProjectionPendingAndSelectionDisposition(t *testing.T) {
	t.Parallel()

	response := MutationResponse{
		Version: MutationSchemaVersion, Revision: "7", CanonicalQuery: "v=1",
		Projection: Projection{Revision: "7"},
		Pending:    PendingSummary{ActiveOperations: 1, AffectedTransactions: 2},
		Selection:  SelectionDisposition{Kind: "cleared", Value: string(app.EmptySelection())},
	}
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	text := string(encoded)
	assert.Contains(t, text, `"revision":"7"`)
	assert.Contains(t, text, `"pending":`)
	assert.Contains(t, text, `"selection":{"kind":"cleared"`)
}

func TestMutationEndpointAppendsAndReturnsEffectiveProjection(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	initial := projectPersistentView(t, server)
	require.NotEmpty(t, initial.AggregateRows)
	body := MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity,
		},
		Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Renamed Merchant"},
		Window: Window{Limit: 200},
	}
	response := requestProtectedJSON(t, server, "/api/v1/mutations", body)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, "2", result.Revision)
	assert.Equal(t, result.Revision, result.Projection.Revision)
	assert.Equal(t, 1, result.Pending.ActiveOperations)
	assert.Equal(t, "preserved", result.Selection.Kind)
	require.NotEmpty(t, result.Projection.AggregateRows)
	assert.Equal(t, "Renamed Merchant", result.Projection.AggregateRows[0].Label)
}

func TestMutationEndpointsUndoRedoAndRejectStaleReviewedCommit(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	mutated := appendMerchantMutation(t, server, "Renamed Merchant")

	undone := requestRevisionAction(t, server, "/api/v1/undo", RevisionBody{
		Version: MutationSchemaVersion, ExpectedRevision: mutated.Revision,
		Query: mutated.CanonicalQuery, Selection: mutated.Projection.Selection,
		Window: Window{Limit: 200},
	})
	assert.Equal(t, "3", undone.Revision)
	assert.Zero(t, undone.Pending.ActiveOperations)
	assert.Equal(t, 1, undone.Pending.InactiveOperations)

	redone := requestRevisionAction(t, server, "/api/v1/redo", RevisionBody{
		Version: MutationSchemaVersion, ExpectedRevision: undone.Revision,
		Query: undone.CanonicalQuery, Selection: undone.Projection.Selection,
		Window: Window{Limit: 200},
	})
	assert.Equal(t, "4", redone.Revision)
	assert.Equal(t, 1, redone.Pending.ActiveOperations)

	stale := requestProtectedJSON(t, server, "/api/v1/commit", CommitBody{
		Version: MutationSchemaVersion, ExpectedRevision: redone.Revision,
		ReviewedRevision: undone.Revision, Query: redone.CanonicalQuery,
		Selection: redone.Projection.Selection, Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(stale.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeRevisionConflict), problem.Code)
	assert.Equal(t, redone.Revision, problem.CurrentRevision)

	committedResponse := requestProtectedJSON(t, server, "/api/v1/commit", CommitBody{
		Version: MutationSchemaVersion, ExpectedRevision: redone.Revision,
		ReviewedRevision: redone.Revision, Query: redone.CanonicalQuery,
		Selection: redone.Projection.Selection, Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusOK, committedResponse.Code, committedResponse.Body.String())
	var committed MutationResponse
	require.NoError(t, json.Unmarshal(committedResponse.Body.Bytes(), &committed))
	assert.Equal(t, "5", committed.Revision)
	assert.Zero(t, committed.Pending.ActiveOperations)
	assert.Zero(t, committed.Pending.InactiveOperations)
	require.NotEmpty(t, committed.Projection.AggregateRows)
	assert.Equal(t, "Renamed Merchant", committed.Projection.AggregateRows[0].Label)
}

func TestProjectionRevisionRefreshesAfterAnotherProfileServiceMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	firstProfile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, firstProfile.Close()) })
	committed, err := fixture.CommittedProfile([]domain.Transaction{apiTransaction(t)})
	require.NoError(t, err)
	_, err = firstProfile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	firstService, err := app.NewProfileService(ctx, firstProfile)
	require.NoError(t, err)
	server := newAPITestServerForService(t, firstService)
	initial := projectPersistentView(t, server)

	secondProfile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, secondProfile.Close()) })
	secondService, err := app.NewProfileService(ctx, secondProfile)
	require.NoError(t, err)
	_, err = secondService.Mutate(ctx, app.MutationRequest{
		Action: app.ActionEditMerchant, ExpectedRevision: 1, State: app.DefaultViewState(),
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity,
		},
		Input: app.EditInput{Scope: app.EditScopeEntity, Label: "External Rename"},
	})
	require.NoError(t, err)

	refreshed := projectPersistentView(t, server)
	assert.Equal(t, "2", refreshed.Revision)
	require.NotEmpty(t, refreshed.AggregateRows)
	assert.Equal(t, "External Rename", refreshed.AggregateRows[0].Label)
}

func TestMutationSecurityRejectsExpiredTokenBeforeProfileEvaluation(t *testing.T) {
	t.Parallel()

	clock := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	server := newPersistentAPITestServerWithClock(t, func() time.Time { return clock })
	initial := projectPersistentView(t, server)
	issued, err := server.security.Issue()
	require.NoError(t, err)
	clock = clock.Add(time.Hour)
	body := MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity,
		},
		Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Must Not Apply"},
		Window: Window{Limit: 200},
	}
	response := requestSignedJSON(t, server, "/api/v1/mutations", body, issued.Value)
	assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeTokenExpired), problem.Code)

	unchanged := projectPersistentView(t, server)
	assert.Equal(t, initial.Revision, unchanged.Revision)
	assert.Equal(t, "Example Merchant", unchanged.AggregateRows[0].Label)
}

func TestMutationRequestBodyLimitPreventsProfileEvaluation(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/mutations", bytes.NewReader(bytes.Repeat([]byte("x"), MaxViewBodyBytes+1)),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	issued, err := server.security.Issue()
	require.NoError(t, err)
	request.Header.Set(MutationTokenHeader, issued.Value)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code, response.Body.String())
	assert.Equal(t, "1", projectPersistentView(t, server).Revision)
}

func TestAllResultSelectionMutationReturnsOnlyOpaqueClearedSelection(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	initial := projectPersistentView(t, server)
	selectionResponse := requestJSON(t, server, "/api/v1/view/transition", TransitionBody{
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionToggleSelectAll, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, selectionResponse.Code, selectionResponse.Body.String())
	var selected Projection
	require.NoError(t, json.Unmarshal(selectionResponse.Body.Bytes(), &selected))
	assert.NotEqual(t, string(app.EmptySelection()), selected.Selection)
	assert.Greater(t, selected.SelectionCount, 0)

	body := MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: selected.Revision,
		Query: selected.CanonicalQuery, Selection: selected.Selection,
		Action: app.ActionEditMerchant,
		Input: MutationInput{
			Scope: string(app.EditScopeTransactions), Label: "Selected Rename",
			DestinationID: "merchant-selected",
		},
		Window: Window{Limit: 200},
	}
	response := requestProtectedJSON(t, server, "/api/v1/mutations", body)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var stale Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &stale))
	require.NotNil(t, stale.Selection)
	assert.Equal(t, "refreshed", stale.Selection.Kind)
	body.Selection = stale.Selection.Value
	response = requestProtectedJSON(t, server, "/api/v1/mutations", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, "cleared", result.Selection.Kind)
	assert.Equal(t, string(app.EmptySelection()), result.Selection.Value)
	assert.NotContains(t, response.Body.String(), `"ids"`)
	assert.Less(t, len(result.Selection.Value), 2048)
}

func TestMutationFailureNeverEchoesTargetLabelsOrQueryText(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	initial := projectPersistentView(t, server)
	response := requestProtectedJSON(t, server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query:     "q=private-query-text&search-at=aggregate%3Amerchant%3A_%3A0&v=1",
		Selection: initial.Selection,
		Action:    app.ActionEditMerchant,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: "private-target-identity",
		},
		Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Private Merchant Label"},
		Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "private-query-text")
	assert.NotContains(t, response.Body.String(), "private-target-identity")
	assert.NotContains(t, response.Body.String(), "Private Merchant Label")
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, string(CodeInvalidTarget), problem.Code)
	assert.Equal(t, initial.Revision, problem.CurrentRevision)
}

func appendMerchantMutation(t testing.TB, server *Server, label string) MutationResponse {
	t.Helper()
	initial := projectPersistentView(t, server)
	require.NotEmpty(t, initial.AggregateRows)
	response := requestProtectedJSON(t, server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity,
		},
		Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: label},
		Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	return result
}

func requestRevisionAction(
	t testing.TB,
	server *Server,
	path string,
	body RevisionBody,
) MutationResponse {
	t.Helper()
	response := requestProtectedJSON(t, server, path, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	return result
}

func newPersistentAPITestServer(t testing.TB) *Server {
	return newPersistentAPITestServerWithClock(t, nil)
}

func newPersistentAPITestServerWithClock(t testing.TB, now func() time.Time) *Server {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	committed, err := fixture.CommittedProfile([]domain.Transaction{apiTransaction(t)})
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	return newAPITestServerForServiceWithClock(t, service, now)
}

func newAPITestServerForService(t testing.TB, service *app.Service) *Server {
	return newAPITestServerForServiceWithClock(t, service, nil)
}

func newAPITestServerForServiceWithClock(
	t testing.TB,
	service *app.Service,
	now func() time.Time,
) *Server {
	t.Helper()
	origin, err := ResolveOrigin("127.0.0.1:8080", "/", "")
	require.NoError(t, err)
	security, err := NewMutationSecurity(origin, nil, now)
	require.NoError(t, err)
	server, err := New(Config{
		Service: service, BasePath: "/", Version: "test", Origin: origin, Security: security,
	})
	require.NoError(t, err)
	return server
}

func projectPersistentView(t testing.TB, server *Server) Projection {
	t.Helper()
	response := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: "v=1", Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	return projection
}

func requestProtectedJSON(
	t testing.TB,
	server *Server,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	issued, err := server.security.Issue()
	require.NoError(t, err)
	return requestSignedJSON(t, server, path, body, issued.Value)
}

func requestSignedJSON(
	t testing.TB,
	server *Server,
	path string,
	body any,
	token string,
) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(MutationTokenHeader, token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
