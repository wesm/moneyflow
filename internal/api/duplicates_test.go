package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestDuplicateProjectionIsBoundedReadOnlyPost(t *testing.T) {
	t.Parallel()

	server := newDuplicateAPITestServer(t, "/moneyflow/")
	body := DuplicateBody{
		Version: DuplicateSchemaVersion, ExpectedRevision: "1", Query: "v=1",
		GroupWindow: Window{Limit: 20}, RowWindow: Window{Limit: 20},
	}
	response := requestJSON(t, server, "/moneyflow/api/v1/duplicates", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))

	var projection DuplicateResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	assert.Equal(t, DuplicateSchemaVersion, projection.Version)
	assert.Equal(t, "1", projection.Revision)
	assert.Equal(t, "v=1", projection.CanonicalQuery)
	assert.Equal(t, 1, projection.TotalGroups)
	assert.Equal(t, 2, projection.TotalTransactions)
	require.Len(t, projection.Groups, 1)
	require.Len(t, projection.Groups[0].Rows, 2)
	assert.Equal(t, "-1234", projection.Groups[0].Rows[0].Amount.Minor)
	assert.Equal(t, "Example Merchant", projection.Groups[0].Rows[0].MatchingLabel)
	assert.Equal(t, app.IdentityTransaction, projection.Groups[0].Rows[0].Target.Kind)
	assert.NotEmpty(t, projection.Groups[0].Rows[0].Target.Identity)
	assert.NotContains(t, response.Body.String(), "provider-txn")
	assert.NotContains(t, response.Body.String(), "ProviderID")

	profilePath, err := ProfileAPIPath("/moneyflow/", testProfileID, "duplicates")
	require.NoError(t, err)
	profileResponse := requestJSON(t, server, profilePath, body)
	assert.Equal(t, http.StatusOK, profileResponse.Code, profileResponse.Body.String())
}

func TestDuplicateProjectionDoesNotRequireMutationSecurity(t *testing.T) {
	t.Parallel()

	server := newDuplicateAPITestServer(t, "/")
	response := requestJSON(t, server, "/api/v1/duplicates", DuplicateBody{
		Version: DuplicateSchemaVersion, ExpectedRevision: "1", Query: "v=1",
		GroupWindow: Window{Limit: 20}, RowWindow: Window{Limit: 20},
	})
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())

	deleteResponse := requestJSON(t, server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: "0", Query: "v=1",
		Action: app.ActionDeleteTransaction,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: "txn-1"},
		Window: Window{Limit: 20},
	})
	assert.Equal(t, http.StatusForbidden, deleteResponse.Code, deleteResponse.Body.String())
}

func TestDuplicateProjectionRejectsStaleRevisionAndInvalidWindows(t *testing.T) {
	t.Parallel()

	server := newDuplicateAPITestServer(t, "/")
	stale := requestJSON(t, server, "/api/v1/duplicates", DuplicateBody{
		Version: DuplicateSchemaVersion, ExpectedRevision: "0", Query: "v=1",
		GroupWindow: Window{Limit: 20}, RowWindow: Window{Limit: 20},
	})
	assert.Equal(t, http.StatusConflict, stale.Code, stale.Body.String())

	invalid := requestJSON(t, server, "/api/v1/duplicates", DuplicateBody{
		Version: DuplicateSchemaVersion, ExpectedRevision: "1", Query: "invalid=private",
		GroupWindow: Window{Limit: 20}, RowWindow: Window{Limit: 20},
	})
	assert.Equal(t, http.StatusBadRequest, invalid.Code, invalid.Body.String())
	assert.NotContains(t, invalid.Body.String(), "private")

	window := requestServer(t, server, http.MethodPost, "/api/v1/duplicates", strings.NewReader(
		`{"version":"1","expected_revision":"1","query":"v=1","group_window":{"offset":0,"limit":401},"row_window":{"offset":0,"limit":20}}`,
	))
	assert.Equal(t, http.StatusUnprocessableEntity, window.Code, window.Body.String())
}

func TestDuplicateProjectionRejectsAnOversizedRequestBody(t *testing.T) {
	t.Parallel()

	server := newDuplicateAPITestServer(t, "/")
	response := requestServer(
		t, server, http.MethodPost, "/api/v1/duplicates",
		bytes.NewReader(bytes.Repeat([]byte("x"), MaxViewBodyBytes+1)),
	)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code, response.Body.String())
}

func TestDuplicateToWireUsesAnEmptyGroupArray(t *testing.T) {
	t.Parallel()

	response := duplicateToWire(app.DuplicateProjection{}, "v=1")
	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"groups":[]`)
}

func newDuplicateAPITestServer(t testing.TB, basePath string) *Server {
	t.Helper()
	ctx := context.Background()
	first := apiTransaction(t)
	second := first.Clone()
	second.ID = "txn-2"
	second.ProviderID = "provider-txn-2"
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	committed, err := fixture.CommittedProfile([]domain.Transaction{first, second})
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	origin, err := ResolveOrigin("127.0.0.1:8080", basePath, "")
	require.NoError(t, err)
	security, err := NewMutationSecurity(origin, nil, nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
		BasePath: basePath, Version: "test", Origin: origin, Security: security,
	})
	require.NoError(t, err)
	return server
}
