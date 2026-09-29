package app_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestTaxonomyPreviewMatchesStagedOperationsWithoutWriting(t *testing.T) {
	for _, test := range []struct {
		name     string
		action   app.ActionID
		input    app.EditInput
		affected int
	}{
		{"category create", app.ActionManageCategories, app.EditInput{Taxonomy: app.TaxonomyCreate, EntityID: "category_new", Label: "New Category", GroupID: "group_a"}, 0},
		{"category rename", app.ActionManageCategories, app.EditInput{Taxonomy: app.TaxonomyRename, EntityID: "category_a", Label: "Renamed"}, 1},
		{"category move", app.ActionManageCategories, app.EditInput{Taxonomy: app.TaxonomyMove, EntityID: "category_a", DestinationID: "group_b"}, 1},
		{"category merge", app.ActionManageCategories, app.EditInput{Taxonomy: app.TaxonomyMerge, EntityID: "category_a", DestinationID: "category_b"}, 1},
		{"category delete", app.ActionManageCategories, app.EditInput{Taxonomy: app.TaxonomyDelete, EntityID: "category_a", ReplacementID: domain.UncategorizedCategoryID}, 1},
		{"group create", app.ActionManageGroups, app.EditInput{Taxonomy: app.TaxonomyCreate, EntityID: "group_new", Label: "New Group"}, 0},
		{"group rename", app.ActionManageGroups, app.EditInput{Taxonomy: app.TaxonomyRename, EntityID: "group_a", Label: "Renamed"}, 1},
		{"group merge", app.ActionManageGroups, app.EditInput{Taxonomy: app.TaxonomyMerge, EntityID: "group_a", DestinationID: "group_b"}, 1},
		{"group delete", app.ActionManageGroups, app.EditInput{Taxonomy: app.TaxonomyDelete, EntityID: "group_a", ReplacementID: "group_b"}, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			profile := newMemoryProfile(t, 5)
			service, err := app.NewProfileService(t.Context(), profile)
			require.NoError(t, err)
			before := profile.snapshot.Clone()
			request := app.MutationRequest{Action: test.action, ExpectedRevision: 5,
				State: detailViewState(), Selection: app.EmptySelection(), Input: test.input,
				Window: app.WindowRequest{Limit: 1}}
			preview, err := service.PreviewMutation(t.Context(), request)
			require.NoError(t, err)
			assert.Equal(t, test.affected, preview.AffectedCount)
			assert.Len(t, preview.Rows, test.affected)
			assert.Equal(t, before, profile.snapshot.Clone())
			mutated, err := service.Mutate(t.Context(), request)
			require.NoError(t, err)
			assert.Equal(t, uint64(6), mutated.Revision)
			for _, row := range preview.Rows {
				assert.Equal(t, row.After, detailTransactionByID(t, mutated.Projection.DetailRows, string(row.TransactionID)))
			}
		})
	}
}

func TestTaxonomyPreviewBoundsEntityChangesAndIncludesHiddenMembers(t *testing.T) {
	profile := newMemoryProfile(t, 5)
	for index := range 101 {
		label := fmt.Sprintf("Extra %03d", index)
		profile.snapshot.Committed.Categories = append(profile.snapshot.Committed.Categories, domain.Category{
			ID: domain.EntityID(fmt.Sprintf("category_extra_%03d", index)), GroupID: "group_b",
			Label: label, CollisionKey: fmt.Sprintf("extra %03d", index),
		})
	}
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	preview, err := service.PreviewMutation(t.Context(), app.MutationRequest{
		Action: app.ActionManageGroups, ExpectedRevision: 5, State: app.DefaultViewState(),
		Input:  app.EditInput{Taxonomy: app.TaxonomyMerge, EntityID: "group_b", DestinationID: "group_a"},
		Window: app.WindowRequest{Offset: 1, Limit: 2},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, preview.AffectedCount, "hidden group member is affected even though the default view hides it")
	assert.Empty(t, preview.Rows, "transaction window is exhausted independently of the entity window")
	require.NotNil(t, preview.Taxonomy)
	assert.Equal(t, 103, preview.Taxonomy.AffectedCount)
	assert.Equal(t, domain.EntityID("group_b"), preview.Taxonomy.EntityID)
	require.Len(t, preview.Taxonomy.Changes, 2)
	assert.Equal(t, domain.EntityID("category_extra_000"), preview.Taxonomy.Changes[0].EntityID)
	assert.Equal(t, domain.EntityID("category_extra_001"), preview.Taxonomy.Changes[1].EntityID)
	for _, change := range preview.Taxonomy.Changes {
		require.NotNil(t, change.Before)
		assert.Equal(t, domain.EntityID("group_b"), change.Before.GroupID)
		assert.Equal(t, domain.EntityID("group_a"), change.After.GroupID)
	}
	assert.Empty(t, profile.snapshot.Journal)
	assert.Equal(t, uint64(5), profile.snapshot.Revision)
}
