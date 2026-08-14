package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestServerHealthAndBasePath(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/moneyflow")
	response := requestServer(t, server, http.MethodGet, "/moneyflow/api/v1/health", nil)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, response.Header().Values("Set-Cookie"))
	assert.Contains(t, response.Header().Get("Content-Type"), "application/json")
	var body Health
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.NotContains(t, response.Body.String(), "$schema")
	assert.Equal(t, "/moneyflow/", body.BasePath)
	assert.Equal(t, APISchemaVersion, body.APISchemaVersion)
	assert.True(t, body.ReadOnly)
	assert.Equal(t, "fixture", body.DataStatus)

	outside := requestServer(t, server, http.MethodGet, "/api/v1/health", nil)
	assert.Equal(t, http.StatusNotFound, outside.Code)
}

func TestServerProjectsAndTransitionsCanonicalViews(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	view := ViewBody{Query: "hidden=1&v=1", Window: Window{Limit: 200}}
	response := requestJSON(t, server, "/api/v1/view", view)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	assert.Equal(t, "v=1", projection.CanonicalQuery)
	assert.NotEmpty(t, projection.Selection)
	assert.NotEmpty(t, projection.AggregateRows)

	transition := TransitionBody{
		Query: projection.CanonicalQuery, Selection: projection.Selection,
		Action: app.ActionCycleGrouping, Window: Window{Limit: 200},
	}
	response = requestJSON(t, server, "/api/v1/view/transition", transition)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	assert.Contains(t, projection.CanonicalQuery, "group=category")
}

func TestServerAppliesISODateFilters(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	response := requestJSON(t, server, "/api/v1/view/transition", TransitionBody{
		Query: "v=1", Action: app.ActionApplyFilters,
		Filters: &TransitionFilters{
			DateRange:  &TransitionDateRange{Start: "2024-01-01", End: "2024-01-31"},
			ShowHidden: true,
		},
		Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	require.NotNil(t, projection.Filters.DateRange)
	assert.Equal(t, "2024-01-01", projection.Filters.DateRange.From)
	assert.Equal(t, "2024-01-31", projection.Filters.DateRange.To)
}

func TestServerResetsInvalidHydrationSelectionWithWarning(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	response := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: "v=1", Selection: "invalid", Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	require.Len(t, projection.Warnings, 1)
	assert.Equal(t, string(app.SelectionReset), projection.Warnings[0].Code)
	assert.Equal(t, string(app.EmptySelection()), projection.Selection)
}

func TestServerRejectsMalformedBodiesWithSafeProblems(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "unknown field", body: `{"query":"v=1","window":{"offset":0,"limit":200},"private":"do-not-echo"}`, want: http.StatusUnprocessableEntity},
		{name: "trailing value", body: `{"query":"v=1","window":{"offset":0,"limit":200}} {}`, want: http.StatusBadRequest},
		{name: "invalid query", body: `{"query":"private=do-not-echo","window":{"offset":0,"limit":200}}`, want: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestServer(
				t,
				server,
				http.MethodPost,
				"/api/v1/view",
				strings.NewReader(test.body),
			)
			assert.Equal(t, test.want, response.Code, response.Body.String())
			assert.Contains(t, response.Header().Get("Content-Type"), "application/problem+json")
			assert.NotContains(t, response.Body.String(), "do-not-echo")
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.NotEmpty(t, problem.Code)
			assert.NotEmpty(t, problem.Detail)
		})
	}
}

func TestServerEnforcesBodyLimitAndMethods(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	oversized := bytes.Repeat([]byte("x"), MaxViewBodyBytes+1)
	response := requestServer(t, server, http.MethodPost, "/api/v1/view", bytes.NewReader(oversized))
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), strings.Repeat("x", 32))

	response = requestServer(t, server, http.MethodGet, "/api/v1/view", nil)
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
	response = requestServer(t, server, http.MethodHead, "/api/v1/health", nil)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Empty(t, response.Body.String())
}

func TestServerEnforcesBodyLimitWithoutContentLength(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/")
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/view",
		strings.NewReader(strings.Repeat("x", MaxViewBodyBytes+1)),
	)
	request.ContentLength = -1
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), strings.Repeat("x", 32))
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "request_too_large", problem.Code)
	assert.Equal(t, "The request body is too large.", problem.Detail)
}

func TestRecoveryReturnsSafeProblem(t *testing.T) {
	t.Parallel()

	handler := noStore(recoverAPI(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("do-not-echo")
	})))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.NotContains(t, response.Body.String(), "do-not-echo")
	assert.Contains(t, response.Body.String(), "internal_error")
}

func TestServerOpenAPIEndpointsAndMethods(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, "/nested/")
	for _, path := range []string{"/nested/openapi.json", "/nested/openapi.yaml"} {
		response := requestServer(t, server, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		assert.Contains(t, response.Header().Get("Content-Type"), "openapi")
	}
	response := requestServer(t, server, http.MethodGet, "/nested/docs", nil)
	assert.Equal(t, http.StatusNotFound, response.Code)
	response = requestServer(t, server, http.MethodGet, "/nested/openapi-3.0.json", nil)
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func newTestServer(t testing.TB, basePath string) *Server {
	t.Helper()
	service, err := app.NewService([]domain.Transaction{apiTransaction(t)})
	require.NoError(t, err)
	server, err := New(Config{Service: service, BasePath: basePath, Version: "test"})
	require.NoError(t, err)
	return server
}

func apiTransaction(t testing.TB) domain.Transaction {
	t.Helper()
	date, err := domain.ParseDate("2024-01-02")
	require.NoError(t, err)
	money, err := domain.ParseMoney("-12.34", "USD", 2)
	require.NoError(t, err)
	transaction, err := domain.NewTransaction(domain.Transaction{
		ID: "txn-1", ProviderID: "provider-txn-1", Provider: "fixture",
		Account: domain.EntityRef{ID: "account-card", Name: "Account Name"}, Date: date,
		Merchant: domain.EntityRef{ID: "merchant-example", Name: "Example Merchant"},
		Category: domain.CategoryRef{ID: "category-example", Name: "Example Category", Group: "Example Group"},
		Amount:   money,
	})
	require.NoError(t, err)
	return transaction
}

func requestJSON(t testing.TB, server *Server, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	return requestServer(t, server, http.MethodPost, path, bytes.NewReader(data))
}

func requestServer(
	t testing.TB,
	server *Server,
	method string,
	path string,
	body io.Reader,
) *httptest.ResponseRecorder {
	t.Helper()
	if body == nil {
		body = http.NoBody
	}
	request := httptest.NewRequest(method, path, body)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
