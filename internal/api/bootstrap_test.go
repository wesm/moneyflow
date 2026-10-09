package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestBootstrapReturnsFreshNoStoreTokenAndStringRevision(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	origin := mustOriginConfig(t, "https://moneyflow.example/moneyflow")
	security, err := NewMutationSecurity(origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), func() time.Time { return now })
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server := newTestServerWithConfig(t, Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: origin.BasePath, Version: "test",
		Origin: origin, Security: security,
	})
	response := requestServer(t, server, http.MethodGet, "/moneyflow/api/v1/bootstrap", nil)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.Empty(t, response.Header().Values("Set-Cookie"))
	var body Bootstrap
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, "0", body.Revision)
	assert.Equal(t, "/moneyflow/", body.BasePath)
	assert.Equal(t, "https://moneyflow.example/moneyflow/", body.CanonicalURL)
	assert.Equal(t, now.Add(time.Hour).Format(time.RFC3339), body.TokenExpiresAt)
	require.NoError(t, security.Verify(body.MutationToken, CatalogMutationScope))
}

func TestProfileBootstrapIssuesOnlyTheRequestedProfileScope(t *testing.T) {
	t.Parallel()
	origin := mustOriginConfig(t, "https://moneyflow.example/moneyflow")
	security, err := NewMutationSecurity(
		origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)),
		func() time.Time { return time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC) },
	)
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server := newTestServerWithConfig(t, Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: origin.BasePath, Version: "test",
		Origin: origin, Security: security,
	})
	path, err := ProfileAPIPath(origin.BasePath, testProfileID, "bootstrap")
	require.NoError(t, err)
	response := requestServer(t, server, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body Bootstrap
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	require.NoError(t, security.Verify(body.MutationToken, testProfileID))
	assert.ErrorIs(
		t, security.Verify(body.MutationToken, otherProfileID), ErrInvalidMutationToken,
	)
	assert.ErrorIs(
		t, security.Verify(body.MutationToken, CatalogMutationScope), ErrInvalidMutationToken,
	)
}

func TestProfileBootstrapListsTheProfileCurrenciesMostUsedFirst(t *testing.T) {
	t.Parallel()
	origin := mustOriginConfig(t, "https://moneyflow.example/moneyflow")
	security, err := NewMutationSecurity(origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), nil)
	require.NoError(t, err)
	euro := apiTransactionWith(t, "eur", func(transaction *domain.Transaction) {
		transaction.Amount = domain.Money{Minor: -700, Currency: "EUR", Scale: 2}
	})
	dollars := []domain.Transaction{
		apiTransactionWith(t, "usd-1", func(*domain.Transaction) {}),
		apiTransactionWith(t, "usd-2", func(*domain.Transaction) {}),
	}
	service, err := app.NewService(append([]domain.Transaction{euro}, dollars...))
	require.NoError(t, err)
	server := newTestServerWithConfig(t, Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: origin.BasePath, Version: "test", Origin: origin, Security: security,
	})
	path, err := ProfileAPIPath(origin.BasePath, testProfileID, "bootstrap")
	require.NoError(t, err)

	response := requestServer(t, server, http.MethodGet, path, nil)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body ProfileBootstrap
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, []CurrencyCount{
		{Currency: "USD", Scale: 2, Count: 2, First: "2024-01-02", Latest: "2024-01-02"},
		{Currency: "EUR", Scale: 2, Count: 1, First: "2024-01-02", Latest: "2024-01-02"},
	}, body.Currencies)
	assert.Equal(t, "0", body.Revision)
	require.NoError(t, security.Verify(body.MutationToken, testProfileID))
}

func TestProfileBootstrapListsNoCurrenciesForAnEmptyProfile(t *testing.T) {
	t.Parallel()
	origin := mustOriginConfig(t, "https://moneyflow.example/moneyflow")
	security, err := NewMutationSecurity(origin, bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)), nil)
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server := newTestServerWithConfig(t, Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: origin.BasePath, Version: "test", Origin: origin, Security: security,
	})
	path, err := ProfileAPIPath(origin.BasePath, testProfileID, "bootstrap")
	require.NoError(t, err)

	response := requestServer(t, server, http.MethodGet, path, nil)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"currencies":[]`)
}

func TestProfileBootstrapRejectsEncodedAndInvalidProfileRoutes(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, "/moneyflow/")
	for _, path := range []string{
		"/moneyflow/api/v1/profiles/legacy/bootstrap",
		"/moneyflow/api/v1/profiles/" + testProfileID + "%2fother/bootstrap",
		"/moneyflow/api/v1/profiles/" + testProfileID + "/../bootstrap",
	} {
		response := requestServer(t, server, http.MethodGet, path, nil)
		assert.Equal(t, http.StatusNotFound, response.Code, path)
	}
}

func newTestServerWithConfig(t testing.TB, config Config) *Server {
	t.Helper()
	server, err := New(config)
	require.NoError(t, err)
	return server
}
