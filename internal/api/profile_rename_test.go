package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type apiRenamerFake struct {
	calls *[]string
	err   error
}

func (renamer *apiRenamerFake) Rename(
	_ context.Context,
	profileID, displayName string,
) (profilecatalog.Entry, error) {
	*renamer.calls = append(*renamer.calls, "rename "+profileID+" to "+displayName)
	if renamer.err != nil {
		return profilecatalog.Entry{}, renamer.err
	}
	return profilecatalog.Entry{
		Key: profileID, ID: profileID, DisplayName: displayName, ProviderKind: "monarch",
		Status: profilecatalog.StatusReady,
	}, nil
}

func newRenameServer(t *testing.T, calls *[]string, renameErr error) *Server {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), BasePath: "/embedded/",
		Version: "test", HostAdmission: true, Catalog: &apiCatalogFake{},
		Evictor: &apiEvictorFake{calls: calls}, Renamer: &apiRenamerFake{calls: calls, err: renameErr},
	})
	require.NoError(t, err)
	return server
}

func TestRenameProfileAnswersWithTheRenamedProfile(t *testing.T) {
	t.Parallel()
	var calls []string
	server := newRenameServer(t, &calls, nil)
	response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/rename",
		ProfileRenameBody{Version: ProfileCatalogSchemaVersion, DisplayName: "Household"})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.JSONEq(t, `{"version":"1","profile":{"key":"`+testProfileID+`","id":"`+testProfileID+
		`","display_name":"Household","provider_kind":"monarch","status":"ready"}}`,
		response.Body.String())
	// The renamer closes the profile itself; the API's evictor would also stop its refreshes.
	assert.Equal(t, []string{"rename " + testProfileID + " to Household"}, calls)
}

func TestRenameProfileReportsWhyTheCatalogRefused(t *testing.T) {
	t.Parallel()
	for code, status := range map[profilecatalog.Code]int{
		profilecatalog.CodeProfileNameConflict: http.StatusConflict,
		profilecatalog.CodeProfileNotFound:     http.StatusNotFound,
		profilecatalog.CodeProfileBusy:         http.StatusServiceUnavailable,
		profilecatalog.CodeProfileInvalid:      http.StatusUnprocessableEntity,
	} {
		var calls []string
		server := newRenameServer(t, &calls, &profilecatalog.Error{Code: code})
		response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/rename",
			ProfileRenameBody{Version: ProfileCatalogSchemaVersion, DisplayName: "Household"})
		assert.Equal(t, status, response.Code, code)
		assert.Contains(t, response.Body.String(), string(code))
	}
}

func TestRenameProfileTakesOnlyAProfileID(t *testing.T) {
	t.Parallel()
	var calls []string
	server := newRenameServer(t, &calls, nil)
	response := requestJSON(t, server, "/embedded/api/v1/profiles/Monarch/rename",
		ProfileRenameBody{Version: ProfileCatalogSchemaVersion, DisplayName: "Household"})
	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Empty(t, calls)
}

func TestRenameProfileRefusesAnUnsupportedVersion(t *testing.T) {
	t.Parallel()
	var calls []string
	server := newRenameServer(t, &calls, nil)
	response := requestJSON(t, server, "/embedded/api/v1/profiles/"+testProfileID+"/rename",
		ProfileRenameBody{Version: "2", DisplayName: "Household"})
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code)
	assert.Contains(t, response.Body.String(), string(CodeProfileInvalid))
	assert.Empty(t, calls)
}

func TestRenameProfileIsAStageOperationOnlyWithARenamer(t *testing.T) {
	t.Parallel()
	var calls []string
	var found []Operation
	for _, operation := range newRenameServer(t, &calls, nil).Operations() {
		if operation.ID == "renameProfile" {
			found = append(found, operation)
		}
	}
	assert.Equal(t, []Operation{{
		ID: "renameProfile", Method: http.MethodPost,
		Path: "/embedded/api/v1/profiles/{profile_id}/rename", Class: access.Stage,
	}}, found)
	for _, operation := range newTestServer(t, "/").Operations() {
		assert.NotEqual(t, "renameProfile", operation.ID)
	}
}
