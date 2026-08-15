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
		Service: service, BasePath: origin.BasePath, Version: "test",
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
	require.NoError(t, security.Verify(body.MutationToken))
}

func newTestServerWithConfig(t testing.TB, config Config) *Server {
	t.Helper()
	server, err := New(config)
	require.NoError(t, err)
	return server
}
