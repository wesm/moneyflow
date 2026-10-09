package api

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	assert.False(t, body.ReadOnly)
	assert.Equal(t, "profile", body.DataStatus)
	assert.Equal(t, "0", body.Revision)
	assert.Zero(t, body.Pending.ActiveOperations)

	outside := requestServer(t, server, http.MethodGet, "/api/v1/health", nil)
	assert.Equal(t, http.StatusNotFound, outside.Code)
}

func TestSafeProblemResponsesStreamsSuccessImmediately(t *testing.T) {
	recorder := httptest.NewRecorder()
	streamedInsideHandler := false
	handler := safeProblemResponses(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/octet-stream")
		response.WriteHeader(http.StatusOK)
		_, err := response.Write(bytes.Repeat([]byte("x"), 2<<20))
		require.NoError(t, err)
		streamedInsideHandler = recorder.Body.Len() == 2<<20
		unwrapper, ok := response.(interface{ Unwrap() http.ResponseWriter })
		require.True(t, ok)
		assert.Same(t, recorder, unwrapper.Unwrap())
	}))
	handler.ServeHTTP(recorder, httptest.NewRequest(
		http.MethodPost, "/api/v1/profiles/profile-a/export", http.NoBody,
	))
	assert.True(t, streamedInsideHandler)
	assert.Equal(t, 2<<20, recorder.Body.Len())
}

func TestSafeProblemResponsesBuffersOrdinarySuccessUntilHandlerReturns(t *testing.T) {
	recorder := httptest.NewRecorder()
	visibleInsideHandler := false
	handler := safeProblemResponses(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, err := response.Write([]byte("complete"))
		require.NoError(t, err)
		visibleInsideHandler = recorder.Body.Len() > 0
	}))
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))
	assert.False(t, visibleInsideHandler)
	assert.Equal(t, "complete", recorder.Body.String())
}

func TestServerReturnsCompleteLargeSelectionProjection(t *testing.T) {
	transactions := make([]domain.Transaction, 2500)
	ids := make([]domain.EntityID, len(transactions))
	for index := range transactions {
		transactions[index] = apiTransaction(t)
		transactions[index].ID = fmt.Sprintf("transaction-%04d-%s", index, strings.Repeat("x", 260))
		transactions[index].ProviderID = transactions[index].ID
		ids[index] = domain.EntityID(transactions[index].ID)
	}
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	selection, err := app.NewExplicitTransactionSelection(ids, 0)
	require.NoError(t, err)
	server, err := New(Config{Resolver: resolverForService(testProfileID, service)})
	require.NoError(t, err)
	response := requestJSON(t, server, "/api/v1/profiles/"+testProfileID+"/view", ViewBody{
		Query: "mode=detail&v=1", Selection: string(selection), Window: Window{Limit: 400},
	})
	require.Equal(t, http.StatusOK, response.Code)
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	assert.Equal(t, string(selection), projection.Selection)
	assert.Equal(t, 2500, projection.SelectionCount)
	assert.Len(t, projection.DetailRows, 400)
	assert.Greater(t, response.Body.Len(), 1<<20)
}

func TestSafeProblemResponsesRejectsOversizedSuccess(t *testing.T) {
	for _, extra := range []string{"", "x"} {
		t.Run(fmt.Sprintf("extra_bytes_%d", len(extra)), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			payload := strings.Repeat("x", 8<<20)
			handler := safeProblemResponses(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "text/plain")
				response.Header().Set("Content-Length", fmt.Sprint(len(payload)+len(extra)))
				_, _ = io.WriteString(response, payload)
				_, _ = io.WriteString(response, extra)
			}))
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))
			if extra == "" {
				require.Equal(t, http.StatusOK, recorder.Code)
				assert.True(t, payload == recorder.Body.String(), "response must contain the complete body")
				return
			}
			require.Equal(t, http.StatusInternalServerError, recorder.Code)
			assert.Equal(t, "application/problem+json", recorder.Header().Get("Content-Type"))
			assert.Empty(t, recorder.Header().Get("Content-Length"))
			var problem Problem
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &problem))
			assert.Equal(t, "response_too_large", problem.Code)
		})
	}
}

func TestServerRejectsOversizedProjection(t *testing.T) {
	transactions := make([]domain.Transaction, 400)
	for index := range transactions {
		transactions[index] = apiTransaction(t)
		transactions[index].ID = fmt.Sprintf("transaction-%04d", index)
		transactions[index].ProviderID = transactions[index].ID
		transactions[index].Account.Name = strings.Repeat("Account ", 3000)
	}
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	server, err := New(Config{Resolver: resolverForService(testProfileID, service)})
	require.NoError(t, err)
	response := requestJSON(t, server, "/api/v1/profiles/"+testProfileID+"/view", ViewBody{
		Query: "mode=detail&v=1", Window: Window{Limit: 400},
	})
	require.Equal(t, http.StatusInternalServerError, response.Code)
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "response_too_large", problem.Code)
	assert.NotContains(t, response.Body.String(), "Account ")
}

func TestOrdinarySuccessCanStillRecoverPanicWithoutPartialResponse(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := recoverAPI(safeProblemResponses(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte("private partial response"))
			panic("private panic")
		},
	)))
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/health", http.NoBody))
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "private")
	assert.Contains(t, recorder.Body.String(), "internal_error")
}

func TestSafeProblemResponsesStillSanitizesMaliciousError(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := safeProblemResponses(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
		_, _ = response.Write(bytes.Repeat([]byte("private-merchant-name"), 100_000))
	}))
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/failure", http.NoBody))
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "private-merchant-name")
	assert.Less(t, recorder.Body.Len(), 1024)
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
	assert.Equal(t, "0", projection.Revision)
	assert.Zero(t, projection.Pending.ActiveOperations)
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
			DateRange:  &DateRange{Start: "2024-01-01", End: "2024-01-31"},
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

func TestServerProtectsEveryPersistentEndpoint(t *testing.T) {
	t.Parallel()

	server := newPersistentAPITestServer(t)
	for _, path := range []string{
		"/api/v1/mutations", "/api/v1/undo", "/api/v1/redo", "/api/v1/commit",
		"/api/v1/review", "/api/v1/review/targets",
		"/api/v1/editor-catalog",
		"/api/v1/provider/refresh", "/api/v1/provider/refresh/confirm",
		"/api/v1/provider/write/pause", "/api/v1/provider/write/resume",
		"/api/v1/provider/write/reconcile", "/api/v1/provider/write/reconcile/confirm",
	} {
		t.Run(path, func(t *testing.T) {
			response := requestServer(t, server, http.MethodPost, path, strings.NewReader(`{}`))
			assert.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, string(CodeInvalidOrigin), problem.Code)
		})
	}
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
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: basePath, Version: "test",
	})
	require.NoError(t, err)
	return server
}

func TestServerProjectsAggregateInOutNet(t *testing.T) {
	t.Parallel()
	one := apiTransaction(t)
	two := one.Clone()
	two.ID, two.ProviderID = "refund", "provider-refund"
	two.Amount.Minor = 1234
	service, err := app.NewService([]domain.Transaction{one, two})
	require.NoError(t, err)
	server, err := New(Config{Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID})
	require.NoError(t, err)
	response := requestJSON(t, server, "/api/v1/view", ViewBody{Query: "v=1", Window: Window{Limit: 200}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection struct {
		Rows []struct {
			In  Money `json:"in"`
			Out Money `json:"out"`
			Net Money `json:"total"`
		} `json:"aggregate_rows"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	require.Len(t, projection.Rows, 1)
	assert.Equal(t, Money{Minor: "1234", Currency: "USD", Scale: 2, Decimal: "12.34", Display: "+12.34"}, projection.Rows[0].In)
	assert.Equal(t, Money{Minor: "-1234", Currency: "USD", Scale: 2, Decimal: "-12.34", Display: "-12.34"}, projection.Rows[0].Out)
	assert.Equal(t, "0", projection.Rows[0].Net.Minor)
}

func resolverForService(profileID string, service *app.Service) ProfileResolver {
	return &testProfileResolver{services: map[string]*app.Service{profileID: service}}
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
		Category: domain.CategoryRef{ID: "category-example", Name: "Example Category", GroupID: "group-example", Group: "Example Group"},
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

func apiTransactionWith(
	t testing.TB,
	id string,
	edit func(*domain.Transaction),
) domain.Transaction {
	t.Helper()
	transaction := apiTransaction(t).Clone()
	transaction.ID, transaction.ProviderID = id, "provider-"+id
	edit(&transaction)
	return transaction
}

func projectState(t testing.TB, server *Server, state app.ViewState) Projection {
	t.Helper()
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)
	return projectPersistentViewForQuery(t, server, query)
}

func TestServerProjectionListsCurrenciesAndSummarizesSelectionAcrossCurrencies(t *testing.T) {
	t.Parallel()
	usdLarge := apiTransactionWith(t, "usd-large", func(*domain.Transaction) {})
	usdSmall := apiTransactionWith(t, "usd-small", func(transaction *domain.Transaction) {
		transaction.Amount.Minor = -500
	})
	euro := apiTransactionWith(t, "eur", func(transaction *domain.Transaction) {
		transaction.Amount = domain.Money{Minor: -700, Currency: "EUR", Scale: 2}
		transaction.Date = mustDate(t, "2025-06-07")
	})
	service, err := app.NewService([]domain.Transaction{usdLarge, usdSmall, euro})
	require.NoError(t, err)
	server := newAPITestServerForService(t, service)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail

	projection := projectState(t, server, state)

	assert.Equal(t, []CurrencyCount{
		{Currency: "USD", Scale: 2, Count: 2, First: "2024-01-02", Latest: "2024-01-02"},
		{Currency: "EUR", Scale: 2, Count: 1, First: "2025-06-07", Latest: "2025-06-07"},
	}, projection.Currencies)
	assert.Empty(t, projection.SelectionSummary, "nothing is selected")
	assert.Nil(t, projection.View.Currency)

	response := requestJSON(t, server, "/api/v1/view/transition", TransitionBody{
		Query: projection.CanonicalQuery, Selection: projection.Selection,
		Action: app.ActionToggleSelectAll, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var selected Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &selected))
	assert.Equal(t, 3, selected.SelectionCount)
	require.Len(t, selected.SelectionSummary, 2)
	byCurrency := map[string]Statistics{}
	for _, stats := range selected.SelectionSummary {
		byCurrency[stats.Currency] = stats
	}
	assert.Equal(t, 2, byCurrency["USD"].Count)
	assert.Equal(t, "-17.34", byCurrency["USD"].Net.Decimal)
	assert.Equal(t, 1, byCurrency["EUR"].Count)
	assert.Equal(t, "-7.00", byCurrency["EUR"].Net.Decimal)
}

func TestServerTimeRowsCarrySavingsRateInCurrencyScopedViews(t *testing.T) {
	t.Parallel()
	income := apiTransactionWith(t, "income", func(transaction *domain.Transaction) {
		transaction.Amount.Minor = 100000
		transaction.Date = mustDate(t, "2024-03-05")
	})
	spending := apiTransactionWith(t, "spending", func(transaction *domain.Transaction) {
		transaction.Amount.Minor = -40000
		transaction.Date = mustDate(t, "2024-03-20")
	})
	idleMonth := apiTransactionWith(t, "idle-month", func(transaction *domain.Transaction) {
		transaction.Amount.Minor = -10000
		transaction.Date = mustDate(t, "2024-04-01")
	})
	service, err := app.NewService([]domain.Transaction{income, spending, idleMonth})
	require.NoError(t, err)
	server := newAPITestServerForService(t, service)
	state := app.DefaultViewState()
	state.Current.Dimension = domain.DimensionTime
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}

	projection := projectState(t, server, state)

	assert.Equal(t, &CurrencyScope{Currency: "USD", Scale: 2}, projection.View.Currency)
	rates := map[string]*int{}
	for _, row := range projection.AggregateRows {
		rates[row.Label] = row.SavingsRateTenths
	}
	require.Contains(t, rates, "Mar 2024")
	require.NotNil(t, rates["Mar 2024"])
	assert.Equal(t, 600, *rates["Mar 2024"], "net 600.00 of 1,000.00 income is 60.0%")
	require.Contains(t, rates, "Apr 2024")
	assert.Nil(t, rates["Apr 2024"], "a month with no income has no rate")

	state.Current.Dimension = domain.DimensionMerchant
	for _, row := range projectState(t, server, state).AggregateRows {
		assert.Nil(t, row.SavingsRateTenths, "only Time rows carry a savings rate")
	}
}

func TestServerDetailRowsCarryStagedValuesForPendingEdits(t *testing.T) {
	t.Parallel()
	server := newPersistentAPITestServer(t)
	appendMerchantMutation(t, server, "Renamed Merchant")
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail

	projection := projectState(t, server, state)

	require.Len(t, projection.DetailRows, 1)
	row := projection.DetailRows[0]
	assert.Equal(t, "Example Merchant", row.Merchant, "the row stays committed")
	require.NotNil(t, row.Staged)
	require.NotNil(t, row.Staged.Merchant)
	assert.Equal(t, "Renamed Merchant", *row.Staged.Merchant)
	assert.Nil(t, row.Staged.Category)
	assert.Nil(t, row.Staged.Group)
	assert.Nil(t, row.Staged.Hidden)
	assert.False(t, row.Staged.Deleted)
}

func TestServerDetailRowsCarryStagedCategoryWithItsGroup(t *testing.T) {
	t.Parallel()
	edited := apiTransactionWith(t, "edited", func(*domain.Transaction) {})
	other := apiTransactionWith(t, "other", func(transaction *domain.Transaction) {
		transaction.Amount.Minor = -500
		transaction.Category = domain.CategoryRef{
			ID: "category-other", Name: "Other Category", GroupID: "group-other", Group: "Other Group",
		}
	})
	server := newPersistentAPITestServerFor(t, []domain.Transaction{edited, other}, nil)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	initial := projectState(t, server, state)
	var target DetailRow
	for _, row := range initial.DetailRows {
		if row.Amount.Minor == "-1234" {
			target = row
		}
	}
	require.NotEmpty(t, target.Identity)
	response := requestProtectedJSON(t, server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionEditCategory,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: target.Identity},
		Input: MutationInput{
			Scope: string(app.EditScopeTransactions), DestinationID: "category-other",
		},
		Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	projection := projectState(t, server, state)

	var row DetailRow
	for _, candidate := range projection.DetailRows {
		if candidate.Identity == target.Identity {
			row = candidate
		}
	}
	assert.Equal(t, "Example Category", row.Category, "the row stays committed")
	require.NotNil(t, row.Staged)
	require.NotNil(t, row.Staged.Category)
	assert.Equal(t, "Other Category", *row.Staged.Category)
	require.NotNil(t, row.Staged.Group)
	assert.Equal(t, "Other Group", *row.Staged.Group)
	assert.Nil(t, row.Staged.Merchant)
	for _, candidate := range projection.DetailRows {
		if candidate.Identity != target.Identity {
			assert.Nil(t, candidate.Staged)
		}
	}
}

func mustDate(t testing.TB, value string) domain.Date {
	t.Helper()
	date, err := domain.ParseDate(value)
	require.NoError(t, err)
	return date
}

func directNavigationServer(t testing.TB) *Server {
	t.Helper()
	groceries := apiTransactionWith(t, "groceries", func(*domain.Transaction) {})
	rent := apiTransactionWith(t, "rent", func(transaction *domain.Transaction) {
		transaction.Merchant = domain.EntityRef{ID: "merchant-landlord", Name: "Example Landlord"}
		transaction.Category = domain.CategoryRef{
			ID: "category-rent", Name: "Example Rent", GroupID: "group-example", Group: "Example Group",
		}
		transaction.Amount.Minor = -90000
		transaction.Date = mustDate(t, "2024-03-10")
	})
	euro := apiTransactionWith(t, "eur", func(transaction *domain.Transaction) {
		transaction.Amount = domain.Money{Minor: -700, Currency: "EUR", Scale: 2}
	})
	service, err := app.NewService([]domain.Transaction{groceries, rent, euro})
	require.NoError(t, err)
	return newAPITestServerForService(t, service)
}

func transitionFrom(
	t testing.TB, server *Server, query string, body TransitionBody,
) *httptest.ResponseRecorder {
	t.Helper()
	body.Query = query
	body.Window = Window{Limit: 200}
	return requestJSON(t, server, "/api/v1/view/transition", body)
}

func decodeProjection(t testing.TB, response *httptest.ResponseRecorder) Projection {
	t.Helper()
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	return projection
}

func TestServerAppliesDirectNavigationActions(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	category, count := "category", "count"

	grouped := decodeProjection(t, transitionFrom(t, server, "v=1", TransitionBody{
		Action: app.ActionSetGrouping, Dimension: &category,
	}))
	assert.Equal(t, "category", grouped.View.Grouping)

	sorted := decodeProjection(t, transitionFrom(t, server, "v=1", TransitionBody{
		Action: app.ActionSetSort, SortField: &count,
	}))
	assert.Equal(t, "count", sorted.View.SortField)

	scoped := decodeProjection(t, transitionFrom(t, server, "v=1", TransitionBody{
		Action: app.ActionSetCurrency, Currency: &CurrencyScope{Currency: "EUR", Scale: 2},
	}))
	assert.Equal(t, &CurrencyScope{Currency: "EUR", Scale: 2}, scoped.View.Currency)

	start, end := "2024-03-01", "2024-03-31"
	period := decodeProjection(t, transitionFrom(t, server, "v=1", TransitionBody{
		Action: app.ActionSetPeriod, Period: &TransitionPeriod{Start: &start, End: &end},
	}))
	require.NotNil(t, period.Filters.DateRange)
	assert.Equal(t, ActiveDateRange{From: start, To: end}, *period.Filters.DateRange)

	all := decodeProjection(t, transitionFrom(t, server, period.CanonicalQuery, TransitionBody{
		Action: app.ActionSetPeriod, Period: &TransitionPeriod{},
	}))
	assert.Nil(t, all.Filters.DateRange, "an empty period shows all time")
}

func TestServerSetsSubGroupingBelowADrill(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	top := decodeProjection(t, transitionFrom(t, server, "v=1", TransitionBody{
		Action: app.ActionDrill,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: firstAggregateIdentity(t, server),
		},
	}))
	category := "category"

	nested := decodeProjection(t, transitionFrom(t, server, top.CanonicalQuery, TransitionBody{
		Action: app.ActionSetSubGrouping, Dimension: &category,
	}))
	assert.Equal(t, "category", nested.View.SubGrouping)
}

func TestServerRejectsMismatchedNavigationArguments(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	category, timeDimension, date := "category", "time", "date"
	start, malformed := "2024-03-01", "0000-01-01"
	currency := &CurrencyScope{Currency: "USD", Scale: 2}
	tests := []struct {
		name string
		body TransitionBody
		code string
	}{
		{"dimension on cycle", TransitionBody{
			Action: app.ActionCycleGrouping, Dimension: &category,
		}, "invalid_web_request"},
		{"grouping without dimension", TransitionBody{Action: app.ActionSetGrouping},
			"invalid_web_request"},
		{"currency with grouping", TransitionBody{
			Action: app.ActionSetGrouping, Dimension: &category, Currency: currency,
		}, "invalid_web_request"},
		{"set-currency without a currency", TransitionBody{Action: app.ActionSetCurrency},
			"invalid_web_request"},
		{"sort field on currency", TransitionBody{
			Action: app.ActionSetCurrency, Currency: currency, SortField: &category,
		}, "invalid_web_request"},
		{"period on sort", TransitionBody{
			Action: app.ActionSetSort, SortField: &category, Period: &TransitionPeriod{},
		}, "invalid_web_request"},
		{"period missing", TransitionBody{Action: app.ActionSetPeriod}, "invalid_web_request"},
		{"sub-grouping without a dimension", TransitionBody{Action: app.ActionSetSubGrouping},
			"invalid_web_request"},
		{"sub-grouping without a drill", TransitionBody{
			Action: app.ActionSetSubGrouping, Dimension: &timeDimension,
		}, "invalid_web_request"},
		{"detail sort on an aggregate view", TransitionBody{
			Action: app.ActionSetSort, SortField: &date,
		}, "invalid_web_request"},
		{"currency with an invalid code", TransitionBody{
			Action: app.ActionSetCurrency, Currency: &CurrencyScope{Currency: "usd", Scale: 2},
		}, "invalid_web_request"},
		{"period with only an end", TransitionBody{
			Action: app.ActionSetPeriod, Period: &TransitionPeriod{End: &start},
		}, "invalid_view_state"},
		{"period with one bound", TransitionBody{
			Action: app.ActionSetPeriod, Period: &TransitionPeriod{Start: &start},
		}, "invalid_view_state"},
		{"period with unparsable date", TransitionBody{
			Action: app.ActionSetPeriod, Period: &TransitionPeriod{Start: &start, End: &malformed},
		}, "invalid_view_state"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := transitionFrom(t, server, "v=1", test.body)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, test.code, problem.Code)
		})
	}
}

func firstAggregateIdentity(t testing.TB, server *Server) string {
	t.Helper()
	response := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: "v=1", Window: Window{Limit: 200},
	})
	projection := decodeProjection(t, response)
	require.NotEmpty(t, projection.AggregateRows)
	return projection.AggregateRows[0].Identity
}

func TestServerRejectsUnknownDimensionAndSortFieldInTheSchema(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	bogus := "bogus"
	for name, body := range map[string]TransitionBody{
		"dimension":  {Action: app.ActionSetGrouping, Dimension: &bogus},
		"sort field": {Action: app.ActionSetSort, SortField: &bogus},
	} {
		t.Run(name, func(t *testing.T) {
			response := transitionFrom(t, server, "v=1", body)
			assert.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
		})
	}
}

func TestServerReturnsToABreadcrumbLevel(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	category := "category"
	top := decodeProjection(t, transitionFrom(t, server, "v=1", TransitionBody{
		Action: app.ActionDrill,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: firstAggregateIdentity(t, server),
		},
	}))
	nested := decodeProjection(t, transitionFrom(t, server, top.CanonicalQuery, TransitionBody{
		Action: app.ActionSetSubGrouping, Dimension: &category,
	}))
	require.NotEmpty(t, nested.AggregateRows)
	deeper := decodeProjection(t, transitionFrom(t, server, nested.CanonicalQuery, TransitionBody{
		Action: app.ActionDrill,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: nested.AggregateRows[0].Identity,
		},
	}))
	require.Len(t, deeper.Breadcrumbs, 2)

	for depth, want := range map[int][]string{0: {"Merchants"}, 1: {"Example Landlord"}} {
		backed := decodeProjection(t, transitionFrom(t, server, deeper.CanonicalQuery, TransitionBody{
			Action: app.ActionBackTo, Depth: &depth,
		}))
		var labels []string
		for _, breadcrumb := range backed.Breadcrumbs {
			labels = append(labels, breadcrumb.Label)
		}
		assert.Equal(t, want, labels, "depth %d", depth)
	}

	deepest := 3
	for name, body := range map[string]TransitionBody{
		"missing depth": {Action: app.ActionBackTo},
		"too deep":      {Action: app.ActionBackTo, Depth: &deepest},
	} {
		response := transitionFrom(t, server, deeper.CanonicalQuery, body)
		require.Equal(t, http.StatusBadRequest, response.Code, name+": "+response.Body.String())
	}
	level := 2
	response := transitionFrom(t, server, deeper.CanonicalQuery, TransitionBody{
		Action: app.ActionBackTo, Depth: &level,
	})
	assert.Equal(t, http.StatusConflict, response.Code, "already at that level")
}

func TestServerRejectsANegativeBreadcrumbDepthInTheSchema(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	response := requestServer(
		t, server, http.MethodPost, "/api/v1/view/transition",
		strings.NewReader(
			`{"query":"v=1","action":"view.back-to","depth":-1,"window":{"offset":0,"limit":200}}`,
		),
	)
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code, response.Body.String())
}

func TestServerResetsAStaleSelectionOnATransition(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	response := transitionFrom(t, server, "v=1", TransitionBody{
		Selection: "invalid",
		Action:    app.ActionDrill,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: firstAggregateIdentity(t, server),
		},
	})

	drilled := decodeProjection(t, response)
	require.Len(t, drilled.Warnings, 1)
	assert.Equal(t, string(app.SelectionReset), drilled.Warnings[0].Code)
	assert.Equal(t, string(app.EmptySelection()), drilled.Selection)
	assert.NotEmpty(t, drilled.DetailRows, "the drill still happened")
}

func TestServerKeepsOtherTransitionProblemsWithAStaleSelection(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	response := transitionFrom(t, server, "v=1", TransitionBody{
		Selection: "invalid", Action: app.ActionBack,
	})

	assert.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var problem Problem
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
	assert.Equal(t, "no_change", problem.Code)
}
