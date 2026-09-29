package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestCSVBrowserProjectionAndLocalCommit(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	mapping, err := bankcsv.Lookup("chase_credit")
	require.NoError(t, err)
	file, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount\n09/01/2026,Example Shop,-12.34\n"), mapping, "Card", "input.csv", bankcsv.ProductionLimits)
	require.NoError(t, err)
	_, err = app.ImportBankCSVProfile(t.Context(), profile, file, mapping.Name, false)
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	server := newAPITestServerForService(t, service)
	view := projectPersistentView(t, server)
	require.Equal(t, "csv", view.ProfileKind)
	require.Len(t, view.AggregateRows, 1)
	require.Equal(t, "Example Shop", view.AggregateRows[0].Label)
	response := requestProtectedJSON(t, server, "/api/v1/mutations", MutationBody{Version: MutationSchemaVersion, ExpectedRevision: view.Revision, Query: view.CanonicalQuery, Selection: view.Selection, Action: app.ActionEditMerchant, Target: &TransitionTarget{Kind: app.IdentityAggregate, Identity: view.AggregateRows[0].Identity}, Input: MutationInput{Scope: string(app.EditScopeEntity), Label: "Local Shop"}, Window: Window{Limit: 20}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var staged MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &staged))
	response = requestProtectedJSON(t, server, "/api/v1/commit", CommitBody{Version: MutationSchemaVersion, ExpectedRevision: staged.Revision, ReviewedRevision: staged.Revision, Query: staged.CanonicalQuery, Selection: staged.Projection.Selection, Window: Window{Limit: 20}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	_, err = service.ImportBankCSV(t.Context(), file, mapping.Name, true)
	require.NoError(t, err)
	view = projectPersistentView(t, server)
	require.Equal(t, "Local Shop", view.AggregateRows[0].Label)
	require.Zero(t, view.Pending.ActiveOperations)
}
