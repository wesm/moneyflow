package moneyflow

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/home"
)

const testBasePath = "/embedded/moneyflow/"

// testRoot returns a data directory that does not exist yet, inside the test's private
// temporary directory.
func testRoot(t *testing.T) string {
	t.Helper()
	// The catalog stores its root in canonical form: no symlinks and, on Windows, long names
	// instead of 8.3 short names.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return filepath.Join(dir, "data")
}

// openTestInstance opens an instance with test seams and closes it when the test ends.
func openTestInstance(t *testing.T, root string, testSeams seams) *Instance {
	t.Helper()
	return openInstance(t, Options{Root: root, APIBasePath: testBasePath}, testSeams)
}

// openInstance opens an instance with options and test seams, and closes it when the test ends.
func openInstance(t *testing.T, options Options, testSeams seams) *Instance {
	t.Helper()
	if testSeams.inspectSession == nil {
		testSeams.inspectSession = assembly.InspectSession
	}
	if testSeams.schedulerTick == 0 {
		testSeams.schedulerTick = time.Hour
	}
	instance, err := open(t.Context(), options, testSeams)
	require.NoError(t, err)
	t.Cleanup(func() {
		if closeErr := instance.Close(); closeErr != nil && !errors.Is(closeErr, ErrClosed) {
			t.Errorf("close instance: %v", closeErr)
		}
	})
	return instance
}

// serveAPI sends one request to the instance's API and returns the recorded response.
func serveAPI(
	t *testing.T,
	instance *Instance,
	method, path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, testBasePath+path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	instance.API().ServeHTTP(response, request)
	return response
}

func TestOpenRejectsInvalidOptions(t *testing.T) {
	root := testRoot(t)
	for name, options := range map[string]Options{
		"relative root": {Root: "data", APIBasePath: testBasePath},
		"negative SQLite option": {
			Root: root, APIBasePath: testBasePath, SQLite: SQLiteOptions{MaxOpenConnections: -1},
		},
		"base path without slashes": {Root: root, APIBasePath: "embedded"},
	} {
		t.Run(name, func(t *testing.T) {
			instance, err := Open(t.Context(), options)

			require.Error(t, err)
			assert.Nil(t, instance)
		})
	}
}

func TestOpenStopsWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := Open(ctx, Options{Root: testRoot(t), APIBasePath: testBasePath})

	require.ErrorIs(t, err, context.Canceled)
}

func TestOpenCreatesAPrivateRoot(t *testing.T) {
	root := testRoot(t)

	openTestInstance(t, root, seams{})

	info, err := os.Stat(root)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	}
}

func TestCloseReturnsErrClosedTheSecondTime(t *testing.T) {
	instance := openTestInstance(t, testRoot(t), seams{})

	require.NoError(t, instance.Close())
	require.ErrorIs(t, instance.Close(), ErrClosed)
}

func TestAPICreatesAProfileWithoutAMutationToken(t *testing.T) {
	instance := openTestInstance(t, testRoot(t), seams{})

	response := serveAPI(t, instance, http.MethodPost, "api/v1/profiles",
		`{"version":"1","display_name":"Example Shop","provider_kind":"local"}`)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"display_name":"Example Shop"`)
	listed := serveAPI(t, instance, http.MethodGet, "api/v1/profiles", "")
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	assert.Contains(t, listed.Body.String(), `"display_name":"Example Shop"`)
}

func TestOperationsAreServeMuxPatternsWithClasses(t *testing.T) {
	instance := openTestInstance(t, testRoot(t), seams{})
	mux := http.NewServeMux()
	classes := map[string]Class{}

	for _, operation := range instance.Operations() {
		require.True(t, strings.HasPrefix(operation.Path, testBasePath), operation.Path)
		require.True(t, operation.Class.Valid(), operation.ID)
		classes[operation.Method+" "+operation.Path] = operation.Class
		mux.Handle(operation.Method+" "+operation.Path, http.NotFoundHandler())
	}

	_, pattern := mux.Handler(httptest.NewRequest(http.MethodPost,
		testBasePath+"api/v1/profiles/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/remove", nil))
	assert.Equal(t, ClassCommit, classes[pattern])
	_, pattern = mux.Handler(httptest.NewRequest(http.MethodGet, testBasePath+"api/v1/profiles", nil))
	assert.Equal(t, ClassRead, classes[pattern])
}

func TestCloseReleasesAnAmazonImportAttempt(t *testing.T) {
	root := testRoot(t)
	instance := openTestInstance(t, root, seams{})
	orders := addProfile(t, instance, "Example Orders", "amazon")
	started := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+orders+"/amazon-import/start",
		`{"version":"1","currency":"USD","scale":2}`)
	require.Equal(t, http.StatusOK, started.Code, started.Body.String())
	profileRoot := filepath.Join(root, "profiles", orders)
	_, err := home.TryLockExisting(profileRoot, home.LockProfile, home.LockExclusive)
	require.ErrorIs(t, err, home.ErrLockBusy, "the attempt holds the profile")

	require.NoError(t, instance.Close())

	lock, err := home.TryLockExisting(profileRoot, home.LockProfile, home.LockExclusive)
	require.NoError(t, err, "Close released the attempt's hold on the profile")
	require.NoError(t, lock.Release())
}
