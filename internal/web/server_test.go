package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

var mutationTokenMetaPattern = regexp.MustCompile(
	`<meta name="moneyflow-mutation-token" content="([^"]+)"[^>]*>`,
)

func TestComposedServerRoutesAPIBeforeSPA(t *testing.T) {
	t.Parallel()
	application, err := NewServer(ServerConfig{
		Resolver: serverTestResolver{service: serverTestService(t)}, BasePath: "/moneyflow/", Version: "test",
	})
	require.NoError(t, err)

	for path, expected := range map[string]int{
		"/moneyflow/": http.StatusOK,
		"/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/": http.StatusOK,
		"/moneyflow/accounts":                              http.StatusNotFound,
		"/moneyflow/api/v1/health":                         http.StatusNotFound,
		"/moneyflow/api/v1/missing":                        http.StatusNotFound,
		"/moneyflow/openapi.json":                          http.StatusOK,
		"/moneyflow/openapi.yaml":                          http.StatusOK,
		"/moneyflow/openapi-unrelated":                     http.StatusNotFound,
		"/outside?private=do-not-log":                      http.StatusNotFound,
		"/moneyflow/assets/missing.js":                     http.StatusNotFound,
		"/moneyflow/apiary-extensionless":                  http.StatusNotFound,
	} {
		request := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		request.Header.Set("Accept", "text/html")
		response := httptest.NewRecorder()
		application.Handler().ServeHTTP(response, request)
		assert.Equal(t, expected, response.Code, path)
	}
}

func TestComposedSingleProfileServerRejectsAnotherCanonicalProfileID(t *testing.T) {
	t.Parallel()
	application, err := NewServer(ServerConfig{
		Resolver: serverTestResolver{service: serverTestService(t), onlyID: fixedServerTestProfileID}, BasePath: "/moneyflow/", Version: "test",
	})
	require.NoError(t, err)
	path, err := api.ProfileAPIPath(
		"/moneyflow/", "profile_baaaaaaaaaaaaaaaaaaaaaaaaa", "health",
	)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestHTTPServerUsesBoundedTimeoutsAndNoRequestLogging(t *testing.T) {
	t.Parallel()
	application, err := NewServer(ServerConfig{Resolver: serverTestResolver{service: serverTestService(t)}, BasePath: "/"})
	require.NoError(t, err)
	var logs bytes.Buffer
	server := application.HTTPServer("127.0.0.1:0", &logs)
	assert.Equal(t, 5*time.Second, server.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, server.ReadTimeout)
	assert.Equal(t, api.ProviderRefreshTimeout+time.Minute, server.WriteTimeout)
	assert.Equal(t, 60*time.Second, server.IdleTimeout)
	assert.Equal(t, 1<<20, server.MaxHeaderBytes)

	request := httptest.NewRequest(http.MethodGet, "/?private=do-not-log", http.NoBody)
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, logs.String(), "do-not-log")
}

func TestComposedServerSharesCanonicalMutationSecurityWithHTMLAndAPI(t *testing.T) {
	t.Parallel()
	origin, err := api.ResolveOrigin(
		"127.0.0.1:8080", "/moneyflow", "https://moneyflow.example/moneyflow",
	)
	require.NoError(t, err)
	security, err := api.NewMutationSecurity(
		origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), nil,
	)
	require.NoError(t, err)
	application, err := NewServer(ServerConfig{
		Resolver: serverTestResolver{service: serverTestService(t)}, BasePath: origin.BasePath, Version: "test",
		Origin: origin, Security: security, WarnNonCanonical: true,
	})
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodGet, "/moneyflow/", http.NoBody)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Accept", "text/html")
	response := httptest.NewRecorder()
	application.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "This listener is read-only")
	match := mutationTokenMetaPattern.FindStringSubmatch(response.Body.String())
	require.Len(t, match, 2)
	require.NoError(t, security.Verify(match[1], api.CatalogMutationScope))

	bootstrap := httptest.NewRequest(
		http.MethodGet, "/moneyflow/api/v1/bootstrap", http.NoBody,
	)
	bootstrapResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(bootstrapResponse, bootstrap)
	assert.Equal(t, http.StatusOK, bootstrapResponse.Code, bootstrapResponse.Body.String())
	assert.Equal(t, "no-store", bootstrapResponse.Header().Get("Cache-Control"))
}

const fixedServerTestProfileID = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"

type serverTestResolver struct {
	service *app.Service
	onlyID  string
}

func (resolver serverTestResolver) Acquire(_ context.Context, profileID string) (api.ProfileLease, error) {
	if resolver.onlyID != "" && profileID != resolver.onlyID {
		return nil, errors.New("profile was not found")
	}
	return serverTestLease{service: resolver.service}, nil
}

type serverTestLease struct{ service *app.Service }

func (lease serverTestLease) Service() *app.Service { return lease.service }
func (serverTestLease) ProfileRoot() string         { return "" }
func (serverTestLease) Temporary() bool             { return false }
func (serverTestLease) Release() error              { return nil }

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
		Category: domain.CategoryRef{ID: "category-1", Name: "Example Category", GroupID: "group-1", Group: "Example Group"},
	})
	require.NoError(t, err)
	service, err := app.NewService([]domain.Transaction{transaction})
	require.NoError(t, err)
	return service
}
