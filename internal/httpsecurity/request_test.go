package httpsecurity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalHostAndOptionalOrigin(t *testing.T) {
	origin, err := ResolveOrigin(
		"127.0.0.1:8081", "/mcp/", "https://moneyflow.example/mcp/",
	)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "https://moneyflow.example/mcp/", http.NoBody)
	assert.NoError(t, ValidateCanonicalHost(request, origin))
	assert.NoError(t, ValidateOptionalOrigin(request, origin))
	request.Header.Set("Origin", origin.Origin())
	assert.NoError(t, ValidateOptionalOrigin(request, origin))

	request.Host = "127.0.0.1:8081"
	assert.Error(t, ValidateCanonicalHost(request, origin))
	request.Host = origin.Canonical.Host
	request.Header["Origin"] = []string{origin.Origin(), origin.Origin()}
	assert.Error(t, ValidateOptionalOrigin(request, origin))
	request.Header.Set("Origin", "null")
	assert.Error(t, ValidateOptionalOrigin(request, origin))
}
