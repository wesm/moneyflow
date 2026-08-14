package app_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestCategoryManagerBuildsEveryPendingOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     app.EditInput
		wantType  domain.OperationType
		wantApply func(*testing.T, domain.CommittedProfile)
	}{
		{
			name: "create",
			input: app.EditInput{
				Taxonomy: app.TaxonomyCreate, EntityID: "category_new",
				Label: "New Category", GroupID: "group_a",
			},
			wantType: domain.OperationCategoryCreate,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "New Category", categoryByID(t, profile, "category_new").Label)
			},
		},
		{
			name: "rename",
			input: app.EditInput{
				Taxonomy: app.TaxonomyRename, EntityID: "category_a", Label: "Renamed Category",
			},
			wantType: domain.OperationCategoryLabel,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "Renamed Category", categoryByID(t, profile, "category_a").Label)
			},
		},
		{
			name: "move",
			input: app.EditInput{
				Taxonomy: app.TaxonomyMove, EntityID: "category_a", DestinationID: "group_b",
			},
			wantType: domain.OperationCategoryMove,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, domain.EntityID("group_b"), categoryByID(t, profile, "category_a").GroupID)
			},
		},
		{
			name: "merge",
			input: app.EditInput{
				Taxonomy: app.TaxonomyMerge, EntityID: "category_a", DestinationID: "category_b",
			},
			wantType: domain.OperationCategoryMerge,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, categoryByID(t, profile, "category_a").Retired)
				assert.Equal(t, domain.EntityID("category_b"), transactionByID(t, profile, "transaction_a").CategoryID)
			},
		},
		{
			name: "delete",
			input: app.EditInput{
				Taxonomy: app.TaxonomyDelete, EntityID: "category_a",
				ReplacementID: domain.UncategorizedCategoryID,
			},
			wantType: domain.OperationCategoryDelete,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, categoryByID(t, profile, "category_a").Retired)
				assert.Equal(t, domain.UncategorizedCategoryID, transactionByID(t, profile, "transaction_a").CategoryID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			effective := effectiveForMutation(t, 5)
			plan, err := app.BuildTaxonomyOperation(effective, app.MutationRequest{
				Action: app.ActionManageCategories, ExpectedRevision: 5,
				State: app.DefaultViewState(), Selection: app.EmptySelection(), Input: test.input,
			}, operationMetadata("category_"+test.name))
			require.NoError(t, err)
			assert.Equal(t, test.wantType, plan.Operation.Type)
			assert.Equal(t, []domain.EntityID{test.input.EntityID}, plan.Operation.Targets)
			assert.Equal(t, app.SelectionPreserved, plan.SelectionDisposition)

			applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
			require.NoError(t, err)
			test.wantApply(t, applied)
		})
	}
}

func TestCategoryManagerRejectsCollisionMissingReplacementRetiredIDAndSentinel(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	tests := []app.EditInput{
		{Taxonomy: app.TaxonomyRename, EntityID: "category_a", Label: "Category B"},
		{Taxonomy: app.TaxonomyDelete, EntityID: "category_a"},
		{
			Taxonomy: app.TaxonomyCreate, EntityID: "category_retired",
			Label: "Fresh Label", GroupID: "group_a",
		},
		{
			Taxonomy: app.TaxonomyRename, EntityID: domain.UncategorizedCategoryID,
			Label: "Changed",
		},
	}
	effective.Effective.Categories = append(effective.Effective.Categories, domain.Category{
		ID: "category_retired", GroupID: "group_a", Label: "Retired",
		CollisionKey: "retired", Retired: true,
	})

	for index, input := range tests {
		_, err := app.BuildTaxonomyOperation(effective, app.MutationRequest{
			Action: app.ActionManageCategories, ExpectedRevision: 5,
			State: app.DefaultViewState(), Selection: app.EmptySelection(), Input: input,
		}, operationMetadata("category_reject_"+string(rune('a'+index))))
		failure := requireMutationCode(t, err, app.MutationInvalidOperation)
		if index == 0 {
			assert.ErrorContains(t, errors.Unwrap(failure), "explicit merge")
		}
	}
}

func TestGroupManagerBuildsEveryPendingOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     app.EditInput
		wantType  domain.OperationType
		wantApply func(*testing.T, domain.CommittedProfile)
	}{
		{
			name: "create",
			input: app.EditInput{
				Taxonomy: app.TaxonomyCreate, EntityID: "group_new", Label: "New Group",
			},
			wantType: domain.OperationGroupCreate,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "New Group", groupByID(t, profile, "group_new").Label)
			},
		},
		{
			name: "rename",
			input: app.EditInput{
				Taxonomy: app.TaxonomyRename, EntityID: "group_a", Label: "Renamed Group",
			},
			wantType: domain.OperationGroupLabel,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.Equal(t, "Renamed Group", groupByID(t, profile, "group_a").Label)
			},
		},
		{
			name: "merge",
			input: app.EditInput{
				Taxonomy: app.TaxonomyMerge, EntityID: "group_a", DestinationID: "group_b",
			},
			wantType: domain.OperationGroupMerge,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, groupByID(t, profile, "group_a").Retired)
				assert.Equal(t, domain.EntityID("group_b"), categoryByID(t, profile, "category_a").GroupID)
			},
		},
		{
			name: "delete",
			input: app.EditInput{
				Taxonomy: app.TaxonomyDelete, EntityID: "group_a",
				ReplacementID: domain.UncategorizedGroupID,
			},
			wantType: domain.OperationGroupDelete,
			wantApply: func(t *testing.T, profile domain.CommittedProfile) {
				assert.True(t, groupByID(t, profile, "group_a").Retired)
				assert.Equal(t, domain.UncategorizedGroupID, categoryByID(t, profile, "category_a").GroupID)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			effective := effectiveForMutation(t, 5)
			plan, err := app.BuildTaxonomyOperation(effective, app.MutationRequest{
				Action: app.ActionManageGroups, ExpectedRevision: 5,
				State: app.DefaultViewState(), Selection: app.EmptySelection(), Input: test.input,
			}, operationMetadata("group_"+test.name))
			require.NoError(t, err)
			assert.Equal(t, test.wantType, plan.Operation.Type)
			assert.Equal(t, []domain.EntityID{test.input.EntityID}, plan.Operation.Targets)

			applied, err := app.ApplyOperation(effective.Effective, storedDraft(plan.Operation, 1))
			require.NoError(t, err)
			test.wantApply(t, applied)
		})
	}
}

func TestGroupManagerRejectsCollisionMissingReplacementRetiredIDAndSentinel(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	effective.Effective.Groups = append(effective.Effective.Groups, domain.CategoryGroup{
		ID: "group_retired", Label: "Retired", CollisionKey: "retired", Retired: true,
	})
	tests := []app.EditInput{
		{Taxonomy: app.TaxonomyRename, EntityID: "group_a", Label: "Group B"},
		{Taxonomy: app.TaxonomyDelete, EntityID: "group_a"},
		{Taxonomy: app.TaxonomyCreate, EntityID: "group_retired", Label: "Fresh Label"},
		{Taxonomy: app.TaxonomyMerge, EntityID: domain.UncategorizedGroupID, DestinationID: "group_b"},
	}
	for index, input := range tests {
		_, err := app.BuildTaxonomyOperation(effective, app.MutationRequest{
			Action: app.ActionManageGroups, ExpectedRevision: 5,
			State: app.DefaultViewState(), Selection: app.EmptySelection(), Input: input,
		}, operationMetadata("group_reject_"+string(rune('a'+index))))
		failure := requireMutationCode(t, err, app.MutationInvalidOperation)
		if index == 0 {
			assert.ErrorContains(t, errors.Unwrap(failure), "explicit merge")
		}
	}
}

func TestTaxonomyLabelsAreTrimmedBeforeNoOpComparisonAndPersistence(t *testing.T) {
	t.Parallel()

	effective := effectiveForMutation(t, 5)
	_, err := app.BuildTaxonomyOperation(effective, app.MutationRequest{
		Action: app.ActionManageCategories, ExpectedRevision: 5,
		State: app.DefaultViewState(), Selection: app.EmptySelection(), Input: app.EditInput{
			Taxonomy: app.TaxonomyRename, EntityID: "category_a", Label: "  Category A  ",
		},
	}, operationMetadata("category_trim_noop"))
	assertMutationCode(t, err, app.MutationInvalidOperation)

	plan, err := app.BuildTaxonomyOperation(effective, app.MutationRequest{
		Action: app.ActionManageGroups, ExpectedRevision: 5,
		State: app.DefaultViewState(), Selection: app.EmptySelection(), Input: app.EditInput{
			Taxonomy: app.TaxonomyCreate, EntityID: "group_trimmed", Label: "  Trimmed Group  ",
		},
	}, operationMetadata("group_trimmed"))
	require.NoError(t, err)
	assert.Equal(t, "Trimmed Group", plan.Operation.Create.Label)
}
