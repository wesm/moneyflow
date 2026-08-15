package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func TestEditingIdentityBoundarySurvivesRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := seedEditingProfile(ctx, t, filepath.Join(t.TempDir(), "profile"))

	profile, server := openEditingServer(ctx, t, paths)
	initial := projectPersistentView(t, server)
	catalog := editorCatalog(t, server, initial.Revision)
	source := findEditorChoiceByLabel(t, catalog.Merchants, "Example Housing")
	destination := findEditorChoiceByLabel(t, catalog.Merchants, "Example Grocer")
	sourceQuery := identityQuery(t, domain.DimensionMerchant, source.ID)

	drilled := projectQuery(t, server, sourceQuery)
	require.NotEmpty(t, drilled.DetailRows)
	sourceTransaction := drilled.DetailRows[0].Identity
	renamed := mutateHTTP(t, server, MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: drilled.Revision,
		Query: sourceQuery, Selection: drilled.Selection,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: sourceTransaction},
		Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Stable Merchant"},
		Window: Window{Limit: 200},
	})
	assert.Equal(t, sourceQuery, renamed.CanonicalQuery)
	assert.Contains(t, renamed.Projection.BreadcrumbText, "Stable Merchant")
	assert.NotZero(t, renamed.Projection.TotalRows)
	committed := commitHTTP(t, server, renamed)
	require.NoError(t, profile.Close())

	profile, server = openEditingServer(ctx, t, paths)
	afterRestart := projectQuery(t, server, sourceQuery)
	assert.Contains(t, afterRestart.BreadcrumbText, "Stable Merchant")
	assert.NotZero(t, afterRestart.TotalRows)

	merged := mutateHTTP(t, server, MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: committed.Revision,
		Query: sourceQuery, Selection: afterRestart.Selection,
		Action: app.ActionEditMerchant,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: sourceTransaction},
		Input: MutationInput{
			Scope: string(app.EditScopeEntity), Label: destination.Label,
			DestinationID: destination.ID,
		},
		Window: Window{Limit: 200},
	})
	_ = commitHTTP(t, server, merged)
	require.NoError(t, profile.Close())

	profile, server = openEditingServer(ctx, t, paths)
	t.Cleanup(func() { _ = profile.Close() })
	retired := projectQuery(t, server, sourceQuery)
	assert.Zero(t, retired.TotalRows)
	assert.Equal(t, sourceQuery, retired.CanonicalQuery)

	neverKnown := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query:  identityQuery(t, domain.DimensionMerchant, "merchant-never-observed"),
		Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusConflict, neverKnown.Code, neverKnown.Body.String())
}

func TestPendingOnlyIdentityIsInvalidAfterUndoTailTruncationAndRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := seedEditingProfile(ctx, t, filepath.Join(t.TempDir(), "profile"))
	profile, server := openEditingServer(ctx, t, paths)
	initial := projectPersistentView(t, server)
	createdID := "category-pending-only"
	created := mutateHTTP(t, server, MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
		Query: initial.CanonicalQuery, Selection: initial.Selection,
		Action: app.ActionManageCategories,
		Input: MutationInput{
			Taxonomy: string(app.TaxonomyCreate), EntityID: createdID,
			Label: "Pending Category", GroupID: string(domain.UncategorizedGroupID),
		},
		Window: Window{Limit: 200},
	})
	pendingQuery := identityQuery(t, domain.DimensionCategory, createdID)
	assert.Zero(t, projectQuery(t, server, pendingQuery).TotalRows)

	undone := requestRevisionAction(t, server, "/api/v1/undo", RevisionBody{
		Version: MutationSchemaVersion, ExpectedRevision: created.Revision,
		Query: created.CanonicalQuery, Selection: created.Projection.Selection,
		Window: Window{Limit: 200},
	})
	require.NotEmpty(t, undone.Projection.AggregateRows)
	_ = mutateHTTP(t, server, MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: undone.Revision,
		Query: undone.CanonicalQuery, Selection: undone.Projection.Selection,
		Action: app.ActionToggleHidden,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: undone.Projection.AggregateRows[0].Identity,
		},
		Window: Window{Limit: 200},
	})
	require.NoError(t, profile.Close())

	profile, server = openEditingServer(ctx, t, paths)
	t.Cleanup(func() { _ = profile.Close() })
	invalid := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: pendingQuery, Window: Window{Limit: 200},
	})
	assert.Equal(t, http.StatusConflict, invalid.Code, invalid.Body.String())
}

func TestConcurrentEditingRevisionCASAllowsExactlyOneWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	paths := seedEditingProfile(ctx, t, filepath.Join(t.TempDir(), "profile"))
	firstProfile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, firstProfile.Close()) }()
	secondProfile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	defer func() { require.NoError(t, secondProfile.Close()) }()
	first, err := app.NewProfileService(ctx, firstProfile)
	require.NoError(t, err)
	second, err := app.NewProfileService(ctx, secondProfile)
	require.NoError(t, err)

	projection, err := first.ProjectView(app.DefaultViewState(), app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, projection.AggregateRows)
	request := app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 1, State: app.DefaultViewState(),
		Selection: app.EmptySelection(),
		Target: &app.RowTarget{
			Kind: app.IdentityAggregate, Identity: projection.AggregateRows[0].Identity,
		},
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, service := range []*app.Service{first, second} {
		workers.Add(1)
		go func(service *app.Service) {
			defer workers.Done()
			<-start
			_, mutateErr := service.Mutate(ctx, request)
			results <- mutateErr
		}(service)
	}
	close(start)
	workers.Wait()
	close(results)

	successes, conflicts := 0, 0
	for result := range results {
		if result == nil {
			successes++
			continue
		}
		var failure *app.AppError
		require.ErrorAs(t, result, &failure)
		if failure.Code == app.AppRevisionConflict {
			conflicts++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
}

func seedEditingProfile(ctx context.Context, t testing.TB, root string) home.Paths {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	require.NoError(t, profile.Close())
	return paths
}

func openEditingServer(
	ctx context.Context,
	t testing.TB,
	paths home.Paths,
) (store.Profile, *Server) {
	t.Helper()
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	return profile, newAPITestServerForService(t, service)
}

func editorCatalog(t testing.TB, server *Server, revision string) EditorCatalogResponse {
	t.Helper()
	response := requestProtectedJSON(t, server, "/api/v1/editor-catalog", EditorCatalogBody{
		Version: MutationSchemaVersion, ExpectedRevision: revision,
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var catalog EditorCatalogResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &catalog))
	return catalog
}

func findEditorChoiceByLabel(t testing.TB, choices []EditorChoice, label string) EditorChoice {
	t.Helper()
	for _, choice := range choices {
		if choice.Label == label {
			return choice
		}
	}
	t.Fatalf("editor choice %q not found", label)
	return EditorChoice{}
}

func projectQuery(t testing.TB, server *Server, query string) Projection {
	t.Helper()
	response := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: query, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	return projection
}

func mutateHTTP(t testing.TB, server *Server, body MutationBody) MutationResponse {
	t.Helper()
	response := requestProtectedJSON(t, server, "/api/v1/mutations", body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var mutation MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &mutation))
	return mutation
}

func commitHTTP(t testing.TB, server *Server, mutation MutationResponse) MutationResponse {
	t.Helper()
	response := requestProtectedJSON(t, server, "/api/v1/commit", CommitBody{
		Version: MutationSchemaVersion, ExpectedRevision: mutation.Revision,
		ReviewedRevision: mutation.Revision, Query: mutation.CanonicalQuery,
		Selection: mutation.Projection.Selection, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var committed MutationResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &committed))
	return committed
}

func identityQuery(t testing.TB, dimension domain.Dimension, id string) string {
	t.Helper()
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: dimension, Key: id, Currency: "USD", Scale: 2,
	}}
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)
	return query
}
