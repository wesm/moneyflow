package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestComposedServerRoutesAPIBeforeSPA(t *testing.T) {
	t.Parallel()
	application, err := NewServer(ServerConfig{
		Service: serverTestService(t), BasePath: "/moneyflow/", Version: "test",
	})
	require.NoError(t, err)

	for path, expected := range map[string]int{
		"/moneyflow/":                     http.StatusOK,
		"/moneyflow/accounts":             http.StatusOK,
		"/moneyflow/api/v1/health":        http.StatusOK,
		"/moneyflow/api/v1/missing":       http.StatusNotFound,
		"/moneyflow/openapi.json":         http.StatusOK,
		"/moneyflow/openapi.yaml":         http.StatusOK,
		"/moneyflow/openapi-unrelated":    http.StatusOK,
		"/outside?private=do-not-log":     http.StatusNotFound,
		"/moneyflow/assets/missing.js":    http.StatusNotFound,
		"/moneyflow/apiary-extensionless": http.StatusOK,
	} {
		request := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		request.Header.Set("Accept", "text/html")
		response := httptest.NewRecorder()
		application.Handler().ServeHTTP(response, request)
		assert.Equal(t, expected, response.Code, path)
	}
}

func TestHTTPServerUsesBoundedTimeoutsAndNoRequestLogging(t *testing.T) {
	t.Parallel()
	application, err := NewServer(ServerConfig{Service: serverTestService(t), BasePath: "/"})
	require.NoError(t, err)
	var logs bytes.Buffer
	server := application.HTTPServer("127.0.0.1:0", &logs)
	assert.Equal(t, 5*time.Second, server.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, server.ReadTimeout)
	assert.Equal(t, 30*time.Second, server.WriteTimeout)
	assert.Equal(t, 60*time.Second, server.IdleTimeout)
	assert.Equal(t, 1<<20, server.MaxHeaderBytes)

	request := httptest.NewRequest(http.MethodGet, "/?private=do-not-log", http.NoBody)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, logs.String(), "do-not-log")
}

func serverTestService(t testing.TB) *app.Service {
	t.Helper()
	date, err := domain.ParseDate("2024-01-02")
	require.NoError(t, err)
	amount, err := domain.ParseMoney("-12.34", "USD", 2)
	require.NoError(t, err)
	transaction, err := domain.NewTransaction(domain.Transaction{
		ID: "txn-1", ProviderID: "provider-txn-1", Provider: "fixture", Date: date, Amount: amount,
		Account:  domain.EntityRef{ID: "account-1", Name: "Account Name"},
		Merchant: domain.EntityRef{ID: "merchant-1", Name: "Example Merchant"},
		Category: domain.CategoryRef{ID: "category-1", Name: "Example Category", Group: "Example Group"},
	})
	require.NoError(t, err)
	service, err := app.NewService([]domain.Transaction{transaction})
	require.NoError(t, err)
	return service
}
