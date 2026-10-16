package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
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

	// Each write logs its failure before it ends.
	server.workers.Wait()
	output := logged.String()
	assert.Contains(t, output,
		"provider write for "+otherProfileID+" could not start: resolver-private-detail")
	assert.Contains(t, output, "provider write for "+testProfileID+
		" stopped: The requested operation is invalid.: provider refresh is not configured")
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

// Every endpoint's server failure passes through one response filter, so a request its client
// abandoned is reported as canceled there, whichever mapper produced the failure.
func TestAbandonedRequestIsNotLoggedAsAServerFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		canceled   bool
		cause      error
		wantStatus int
		wantLog    string
	}{
		{
			name: "client canceled", canceled: true,
			cause: fmt.Errorf("refresh: %w", context.Canceled), wantStatus: statusClientClosedRequest,
		},
		{
			name: "internal cancellation while the client waits", canceled: false,
			cause: context.Canceled, wantStatus: http.StatusInternalServerError,
			wantLog: "POST /api/v1/example: 500 internal_error: context canceled\n",
		},
		{
			name: "real failure racing a disconnect", canceled: true,
			cause: errors.New("disk failure"), wantStatus: http.StatusInternalServerError,
			wantLog: "POST /api/v1/example: 500 internal_error: disk failure\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output := &bytes.Buffer{}
			handler := safeProblemResponses(http.HandlerFunc(
				func(response http.ResponseWriter, request *http.Request) {
					noteProblemCause(request.Context(), test.cause)
					writeProblem(response, newProblem(
						http.StatusInternalServerError, "internal_error", "The request could not be completed.",
					))
				},
			), log.New(output, "", 0))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.canceled {
				cancel()
			}
			request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/example", http.NoBody)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			require.Equal(t, test.wantStatus, response.Code, response.Body.String())
			assert.Equal(t, test.wantLog, output.String())
			if test.wantStatus == statusClientClosedRequest {
				assert.Contains(t, response.Body.String(), string(CodeRequestCanceled))
			}
		})
	}
}

func TestUnreadableProviderSessionIsLoggedOncePerCause(t *testing.T) {
	t.Parallel()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, logged := newLoggedServer(t, Config{Resolver: resolverForService(testProfileID, service)})
	ctx := context.WithValue(context.Background(), resolvedProfileContextKey{}, resolvedProfile{
		id: testProfileID, service: service,
	})
	unreadable := errors.New("inspect session: permission denied")

	server.logSessionCheck(ctx, unreadable)
	server.logSessionCheck(ctx, unreadable)
	server.logSessionCheck(ctx, nil)
	server.logSessionCheck(ctx, unreadable)

	line := "provider write status for " + testProfileID +
		": the saved provider session could not be inspected: inspect session: permission denied"
	assert.Equal(t, 2, strings.Count(logged.String(), line))
}

// A formatted wrapper repeats its storage error's safe text, and the storage error keeps its
// diagnostic out of Error(), so the log has to keep unwrapping past both.
func TestServerFailureLogKeepsADiagnosticBehindAFormattedStorageWrapper(t *testing.T) {
	t.Parallel()
	storage := store.NewError(store.CodeStoreError, errors.New("disk-private-diagnostic"))
	cases := map[string]error{
		"formatted wrapper": fmt.Errorf("save view: %w", storage),
		"joined errors": errors.Join(errors.New("first-diagnostic"),
			fmt.Errorf("save view: %w", storage)),
	}
	for name, cause := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			output := &bytes.Buffer{}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/example", http.NoBody)

			logServerFailure(log.New(output, "", 0), request,
				&Problem{Status: http.StatusInternalServerError, Code: "store_error"}, cause)

			assert.Contains(t, output.String(), "save view: store_error:")
			assert.Contains(t, output.String(), "disk-private-diagnostic")
			assert.Equal(t, 1, strings.Count(output.String(), "save view:"))
		})
	}
}
