package api

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// lockedBuffer collects log output written by background goroutines.
type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (buffer *lockedBuffer) Write(data []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *lockedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}

func newLoggedServer(t testing.TB, config Config) (*Server, *lockedBuffer) {
	t.Helper()
	output := &lockedBuffer{}
	config.ErrorLog = log.New(output, "", 0)
	if config.BasePath == "" {
		config.BasePath = "/"
	}
	config.Version = "test"
	server, err := New(config)
	require.NoError(t, err)
	return server, output
}

func TestCatalogFailureLogsItsCauseBehindAGenericProblem(t *testing.T) {
	t.Parallel()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	catalog := &apiCatalogFake{listErr: errors.New("catalog-private-detail")}
	server, logged := newLoggedServer(t, Config{
		Resolver: resolverForService(testProfileID, service),
		Catalog:  catalog, Evictor: &apiEvictorFake{},
	})

	response := requestServer(t, server, http.MethodGet, "/api/v1/profiles", nil)

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "catalog-private-detail")
	assert.Contains(t, logged.String(),
		"GET /api/v1/profiles: 500 internal_error: catalog-private-detail")
}

func TestProfileOpenFailureLogsItsCause(t *testing.T) {
	t.Parallel()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, logged := newLoggedServer(t, Config{
		Resolver: resolverForService(testProfileID, service),
	})
	path, err := ProfileAPIPath("/", otherProfileID, "health")
	require.NoError(t, err)

	response := requestServer(t, server, http.MethodGet, path, nil)

	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "resolver-private-detail")
	assert.Contains(t, logged.String(),
		"GET "+path+": the profile could not be opened: resolver-private-detail")
}

func TestBackgroundProviderWriteFailuresAreLogged(t *testing.T) {
	t.Parallel()
	unconfigured := newPersistentAPITestService(t, []domain.Transaction{apiTransaction(t)})
	server, logged := newLoggedServer(t, Config{
		Resolver: resolverForService(testProfileID, unconfigured),
	})

	server.startProviderWrite(otherProfileID)
	server.startProviderWrite(testProfileID)

	require.Eventually(t, func() bool {
		output := logged.String()
		return strings.Contains(output,
			"provider write for "+otherProfileID+" could not start: resolver-private-detail") &&
			strings.Contains(output, "provider write for "+testProfileID+
				" stopped: The requested operation is invalid.: provider refresh is not configured")
	}, 3*time.Second, 10*time.Millisecond, logged.String())
}

func TestNonProblemServerFailureIsLogged(t *testing.T) {
	t.Parallel()
	output := &bytes.Buffer{}
	handler := safeProblemResponses(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "plain failure", http.StatusInternalServerError)
		},
	), log.New(output, "", 0))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/example?secret=value", http.NoBody)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	assert.Equal(t, "GET /api/v1/example: 500 internal_error\n", output.String())
}
