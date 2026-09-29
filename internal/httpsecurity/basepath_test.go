package httpsecurity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeBasePathAndResolveOrigin(t *testing.T) {
	path, err := NormalizeBasePath("/moneyflow/mcp")
	require.NoError(t, err)
	assert.Equal(t, "/moneyflow/mcp/", path)
	origin, err := ResolveOrigin(
		"127.0.0.1:8081", path, "https://moneyflow.example/moneyflow/mcp/",
	)
	require.NoError(t, err)
	assert.Equal(t, "https://moneyflow.example", origin.Origin())
	assert.Equal(t, "/moneyflow/mcp/", origin.BasePath)

	_, err = ResolveOrigin("127.0.0.1:8081", path, "https://moneyflow.example/other/")
	assert.Error(t, err)
}

func TestValidateListenLoopbackPolicy(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8081", "[::1]:8081", "localhost:8081"} {
		_, err := ValidateListen(address, true)
		assert.NoError(t, err, address)
	}
	for _, address := range []string{"0.0.0.0:8081", "192.0.2.1:8081", "moneyflow.example:8081"} {
		_, err := ValidateListen(address, true)
		assert.Error(t, err, address)
	}
}
