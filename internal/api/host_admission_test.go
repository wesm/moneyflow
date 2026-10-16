package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestOnlyHostAdmissionAcceptsAForeignMutationWithoutAToken(t *testing.T) {
	t.Parallel()
	for hostAdmission, want := range map[bool]int{true: http.StatusOK, false: http.StatusForbidden} {
		service := newPersistentAPITestService(t, []domain.Transaction{apiTransaction(t)})
		server, err := New(Config{
			Resolver: resolverForService(testProfileID, service), BasePath: "/embedded/",
			Version: "test", HostAdmission: hostAdmission,
		})
		require.NoError(t, err)
		prefix := "/embedded/api/v1/profiles/" + testProfileID + "/"
		viewed := requestJSON(t, server, prefix+"view",
			ViewBody{Query: "v=1", Window: Window{Limit: 200}})
		require.Equal(t, http.StatusOK, viewed.Code, viewed.Body.String())
		var initial Projection
		require.NoError(t, json.Unmarshal(viewed.Body.Bytes(), &initial))
		require.NotEmpty(t, initial.AggregateRows)
		body, err := json.Marshal(MutationBody{
			Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
			Query: initial.CanonicalQuery, Selection: initial.Selection, Action: app.ActionEditMerchant,
			Target: &TransitionTarget{
				Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity,
			},
			Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Example Shop"},
			Window: Window{Limit: 200},
		})
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, prefix+"mutations", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://attacker.example")
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		mutated := httptest.NewRecorder()

		server.Handler().ServeHTTP(mutated, request)

		assert.Equal(t, want, mutated.Code, "host admission %t: %s", hostAdmission, mutated.Body.String())
	}
}

func TestHostAdmissionHasNoBootstrapEndpoints(t *testing.T) {
	t.Parallel()
	service, err := app.NewService([]domain.Transaction{apiTransaction(t)})
	require.NoError(t, err)
	resolver := &testProfileResolver{services: map[string]*app.Service{testProfileID: service}}
	server, err := New(Config{
		Resolver: resolver, BasePath: "/embedded/", Version: "test", HostAdmission: true,
	})
	require.NoError(t, err)
	response := requestServer(t, server, http.MethodGet, "/embedded/api/v1/bootstrap", nil)
	assert.Equal(t, http.StatusNotFound, response.Code)
	response = requestServer(t, server, http.MethodGet,
		"/embedded/api/v1/profiles/"+testProfileID+"/bootstrap", nil)
	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Zero(t, resolver.acquires, "a route the host does not serve must not open a profile")
	for _, operation := range server.Operations() {
		assert.NotContains(t, []string{"bootstrap", "profileBootstrap"}, operation.ID)
	}
}

func TestAServerThatStoppedAdmittingAnswersEveryRequestBusy(t *testing.T) {
	t.Parallel()
	service, err := app.NewService([]domain.Transaction{apiTransaction(t)})
	require.NoError(t, err)
	resolver := &testProfileResolver{services: map[string]*app.Service{testProfileID: service}}
	server, err := New(Config{
		Resolver: resolver, BasePath: "/embedded/", Version: "test", HostAdmission: true,
	})
	require.NoError(t, err)
	prefix := "/embedded/api/v1/profiles/" + testProfileID + "/"
	requests := map[string]struct {
		send   func() *httptest.ResponseRecorder
		before int
	}{
		"a read": {before: http.StatusOK, send: func() *httptest.ResponseRecorder {
			return requestServer(t, server, http.MethodGet, prefix+"health", nil)
		}},
		"a view": {before: http.StatusOK, send: func() *httptest.ResponseRecorder {
			return requestJSON(t, server, prefix+"view",
				ViewBody{Query: "v=1", Window: Window{Limit: 200}})
		}},
		"a route no operation serves": {before: http.StatusNotFound,
			send: func() *httptest.ResponseRecorder {
				return requestServer(t, server, http.MethodGet, "/embedded/api/v1/unserved", nil)
			}},
	}
	for name, request := range requests {
		response := request.send()
		assert.Equal(t, request.before, response.Code, "%s: %s", name, response.Body.String())
	}
	acquires := resolver.acquires

	server.StopAdmitting()

	for name, request := range requests {
		response := request.send()
		assert.Equal(t, http.StatusServiceUnavailable, response.Code,
			"%s: %s", name, response.Body.String())
		var problem Problem
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem), name)
		assert.Equal(t, "profile_busy", problem.Code, name)
		assert.Contains(t, problem.Detail, "Wait a moment", name)
	}
	assert.Equal(t, acquires, resolver.acquires, "a refused request opens no profile")
}

func TestStandaloneServerStillRequiresTheMutationToken(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)
	response := requestSignedJSON(t, server, "/api/v1/mutations",
		MutationBody{Version: MutationSchemaVersion}, "")
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Contains(t, response.Body.String(), string(CodeInvalidToken))
}
