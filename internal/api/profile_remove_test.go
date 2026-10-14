package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type apiRemoverFake struct {
	calls *[]string
	err   error
}

func (remover *apiRemoverFake) Remove(_ context.Context, profileID string) error {
	*remover.calls = append(*remover.calls, "remove "+profileID)
	return remover.err
}

func newRemovalServer(t *testing.T, calls *[]string, removeErr error) *Server {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), BasePath: "/embedded/",
		Version: "test", HostAdmission: true, Catalog: &apiCatalogFake{},
		Evictor: &apiEvictorFake{calls: calls}, Remover: &apiRemoverFake{calls: calls, err: removeErr},
	})
	require.NoError(t, err)
	return server
}

func TestRemoveProfileEvictsBeforeRemoving(t *testing.T) {
	t.Parallel()
	var calls []string
	server := newRemovalServer(t, &calls, nil)
	response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/remove",
		ProfileRemoveBody{Version: ProfileCatalogSchemaVersion})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.JSONEq(t, `{"version":"1"}`, response.Body.String())
	assert.Equal(t, []string{"evict", "remove " + testProfileID}, calls)
}

func TestRemoveProfileReportsABusyProfile(t *testing.T) {
	t.Parallel()
	var calls []string
	server := newRemovalServer(t, &calls, &profilecatalog.Error{Code: profilecatalog.CodeProfileBusy})
	response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/remove",
		ProfileRemoveBody{Version: ProfileCatalogSchemaVersion})
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "profile_busy")
}

func TestRemoveProfileExistsOnlyWithARemover(t *testing.T) {
	t.Parallel()
	var calls []string
	ids := func(server *Server) []string {
		var found []string
		for _, operation := range server.Operations() {
			found = append(found, operation.ID)
		}
		return found
	}
	assert.Contains(t, ids(newRemovalServer(t, &calls, nil)), "removeProfile")
	assert.NotContains(t, ids(newTestServer(t, "/")), "removeProfile")
}

func TestRemoveProfileIsACommitOperation(t *testing.T) {
	t.Parallel()
	var calls []string
	var found []Operation
	for _, operation := range newRemovalServer(t, &calls, nil).Operations() {
		if operation.ID == "removeProfile" {
			found = append(found, operation)
		}
	}
	assert.Equal(t, []Operation{{
		ID: "removeProfile", Method: http.MethodPost,
		Path: "/embedded/api/v1/profiles/{profile_id}/remove", Class: access.Commit,
	}}, found)
}

func TestRemoveProfileRefusesAnUnsupportedVersion(t *testing.T) {
	t.Parallel()
	var calls []string
	server := newRemovalServer(t, &calls, nil)
	response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/remove",
		ProfileRemoveBody{Version: "2"})
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code)
	assert.Contains(t, response.Body.String(), string(CodeProfileInvalid))
	assert.Empty(t, calls)
}

type apiRefusingEvictor struct{ err error }

func (evictor apiRefusingEvictor) Evict(context.Context, string) error { return evictor.err }

func TestRemoveProfileKeepsTheProfileWhenEvictionFails(t *testing.T) {
	t.Parallel()
	var calls []string
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), BasePath: "/embedded/",
		Version: "test", HostAdmission: true, Catalog: &apiCatalogFake{},
		Evictor: apiRefusingEvictor{err: &profilecatalog.Error{Code: profilecatalog.CodeProfileBusy}},
		Remover: &apiRemoverFake{calls: &calls},
	})
	require.NoError(t, err)
	response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/remove",
		ProfileRemoveBody{Version: ProfileCatalogSchemaVersion})
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "profile_busy")
	assert.Empty(t, calls)
}

func TestRemoveProfileDoesNotNeedTheProfileToOpen(t *testing.T) {
	t.Parallel()
	var calls []string
	server, err := New(Config{
		Resolver: &testProfileResolver{failure: errors.New("profile cannot be opened")},
		BasePath: "/embedded/", Version: "test", HostAdmission: true, Catalog: &apiCatalogFake{},
		Evictor: &apiEvictorFake{calls: &calls}, Remover: &apiRemoverFake{calls: &calls},
	})
	require.NoError(t, err)
	prefix := "/embedded/api/v1/profiles/" + testProfileID + "/"

	health := requestServer(t, server, http.MethodGet, prefix+"health", nil)
	require.Equal(t, http.StatusInternalServerError, health.Code, "the resolver must refuse it")
	removed := requestJSON(t, server, prefix+"remove",
		ProfileRemoveBody{Version: ProfileCatalogSchemaVersion})

	require.Equal(t, http.StatusOK, removed.Code, removed.Body.String())
	assert.Equal(t, []string{"evict", "remove " + testProfileID}, calls)
}

func TestRemoveProfileNeedsCatalogAuthorityWithoutHostAdmission(t *testing.T) {
	t.Parallel()
	var calls []string
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), BasePath: "/", Version: "test",
		Catalog: &apiCatalogFake{}, Evictor: &apiEvictorFake{calls: &calls},
		Remover: &apiRemoverFake{calls: &calls},
	})
	require.NoError(t, err)
	path, err := ProfileAPIPath("/", testProfileID, "remove")
	require.NoError(t, err)
	body := ProfileRemoveBody{Version: ProfileCatalogSchemaVersion}

	unsigned := requestSignedJSON(t, server, path, body, "")
	assert.Equal(t, http.StatusForbidden, unsigned.Code)
	wrongScope := requestScopedMutation(t, server, testProfileID, path, body)
	assert.Equal(t, http.StatusForbidden, wrongScope.Code)
	assert.Empty(t, calls)

	response := requestScopedMutation(t, server, CatalogMutationScope, path, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, []string{"evict", "remove " + testProfileID}, calls)
}

func TestNewRefusesARemoverWithoutAnEvictor(t *testing.T) {
	t.Parallel()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	_, err = New(Config{
		Resolver: resolverForService(testProfileID, service), Remover: &apiRemoverFake{},
	})
	require.EqualError(t, err, "new API server: profile removal requires an evictor")
}
