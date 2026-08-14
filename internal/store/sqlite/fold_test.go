package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestFoldMatchesEffectiveSnapshotAndClearsCompleteJournal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(ctx, 1, draftFoldMerchantLabelOperation(1))
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftCategoryAssignOperation(revision, "category-dining", "txn-001"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_hide_active", revision, "txn-001"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_redo", revision, "txn-002"),
	)
	require.NoError(t, err)
	revision, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)

	before, err := profile.Load(ctx)
	require.NoError(t, err)
	effective, err := app.Replay(before)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(effective, revision)
	require.NoError(t, err)

	next, err := profile.Fold(ctx, revision, plan)
	require.NoError(t, err)
	assert.Equal(t, revision+1, next)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, effective.Effective, after.Committed)
	assert.Empty(t, after.Journal)
	assert.Zero(t, after.Cursor)
	assert.Equal(t, next, after.Revision)
	assert.Equal(t, plan.KnownDrills, after.KnownDrills)
	assert.True(t, transactionRecord(t, after.Committed, "txn-001").Hidden)
}

func TestFoldPersistsMerchantAndTaxonomyMergesMovesAndCreation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	sourceGroup := categoryRecord(t, loaded.Committed, "category-groceries").GroupID
	revision := uint64(1)
	for _, operation := range []domain.Operation{
		draftFoldMerchantMergeOperation(revision, "merchant-cafe", "merchant-grocer"),
		draftGroupCreateOperation(revision+1, "group_new"),
		draftCategoryMoveOperation(revision+2, "category-groceries", "group_new"),
		draftCategoryLabelOperation(revision+3, "category-groceries"),
		draftGroupMergeOperation(revision+4, sourceGroup, "group_new"),
	} {
		revision, err = profile.Append(ctx, revision, operation)
		require.NoError(t, err)
	}

	before, err := profile.Load(ctx)
	require.NoError(t, err)
	effective, err := app.Replay(before)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(effective, revision)
	require.NoError(t, err)
	_, err = profile.Fold(ctx, revision, plan)
	require.NoError(t, err)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, effective.Effective, after.Committed)
	merchant := merchantRecord(t, after.Committed, "merchant-cafe")
	assert.True(t, merchant.Retired)
	group := groupRecord(t, after.Committed, sourceGroup)
	assert.True(t, group.Retired)
	assert.Equal(t, domain.EntityID("group_new"), categoryRecord(
		t, after.Committed, "category-groceries",
	).GroupID)
}

func TestFoldPersistsCreatedThenRetiredTaxonomyAndKnownIdentity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	groupID := categoryRecord(t, loaded.Committed, "category-groceries").GroupID
	revision, err := profile.Append(
		ctx,
		1,
		draftCategoryCreateOperation(1, groupID, "txn-001"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftCategoryMergeOperation(revision, "category_new", "category-dining"),
	)
	require.NoError(t, err)

	before, err := profile.Load(ctx)
	require.NoError(t, err)
	effective, err := app.Replay(before)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(effective, revision)
	require.NoError(t, err)
	_, err = profile.Fold(ctx, revision, plan)
	require.NoError(t, err)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	created := categoryRecord(t, after.Committed, "category_new")
	assert.True(t, created.Retired)
	require.NotNil(t, created.MergeDestination)
	assert.Equal(t, domain.EntityID("category-dining"), *created.MergeDestination)
	assert.True(t, hasKnownDrill(after.KnownDrills, domain.DrillIdentity{
		Dimension: domain.DimensionCategory, Currency: "USD", Scale: 2, Key: "category_new",
	}))
}

func TestFoldRejectsStaleRevisionAndChangedActivePrefixAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(ctx, 1, draftFoldMerchantLabelOperation(1))
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	effective, err := app.Replay(before)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(effective, revision)
	require.NoError(t, err)

	_, err = profile.Fold(ctx, 1, plan)
	assertRevisionConflict(t, err, 1, revision)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)

	plan.ActiveOperationIDs[0] = "operation_other"
	_, err = profile.Fold(ctx, revision, plan)
	assertStoreCode(t, err, store.CodeInvalidOperation)
	after, err = profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestFoldConstraintFailureRollsBackCommittedRowsJournalCursorAndRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(ctx, 1, draftFoldMerchantLabelOperation(1))
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftCategoryAssignOperation(revision, "category-dining", "txn-001"),
	)
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)
	effective, err := app.Replay(before)
	require.NoError(t, err)
	plan, err := app.BuildFoldPlan(effective, revision)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		CREATE TRIGGER fail_fold_transaction_update
		BEFORE UPDATE ON transactions
		BEGIN
			SELECT RAISE(ABORT, 'injected fold failure');
		END`)
	require.NoError(t, err)

	_, err = profile.Fold(ctx, revision, plan)
	assertStoreCode(t, err, store.CodeStoreError)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestKnownDrillCommitBoundarySurvivesRestart(t *testing.T) {
	t.Parallel()

	t.Run("abandoned pending identity becomes invalid", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		paths := temporaryPaths(t)
		opened, err := Open(ctx, paths, DefaultOptions)
		require.NoError(t, err)
		handle := opened.(*profile)
		_, err = handle.CreateSeededProfile(ctx, fixtureProfile(t))
		require.NoError(t, err)
		revision, err := handle.Append(ctx, 1, draftGroupCreateOperation(1, "group_pending"))
		require.NoError(t, err)
		loaded, err := handle.Load(ctx)
		require.NoError(t, err)
		effective, err := app.Replay(loaded)
		require.NoError(t, err)
		identity := domain.DrillIdentity{
			Dimension: domain.DimensionGroup, Currency: "USD", Scale: 2, Key: "group_pending",
		}
		assert.Equal(t, app.DrillEmpty, app.ClassifyKnownDrill(effective, identity))

		revision, err = handle.MoveCursor(ctx, revision, -1)
		require.NoError(t, err)
		_, err = handle.Append(
			ctx,
			revision,
			draftHideOperation("operation_truncate", revision, "txn-001"),
		)
		require.NoError(t, err)
		require.NoError(t, handle.Close())

		reopenedStore, err := Open(ctx, paths, DefaultOptions)
		require.NoError(t, err)
		reopened := reopenedStore.(*profile)
		t.Cleanup(func() { require.NoError(t, reopened.Close()) })
		restarted, err := reopened.Load(ctx)
		require.NoError(t, err)
		replayed, err := app.Replay(restarted)
		require.NoError(t, err)
		assert.Equal(t, app.DrillInvalid, app.ClassifyKnownDrill(replayed, identity))
	})

	t.Run("committed then retired identity remains empty", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		paths := temporaryPaths(t)
		opened, err := Open(ctx, paths, DefaultOptions)
		require.NoError(t, err)
		handle := opened.(*profile)
		_, err = handle.CreateSeededProfile(ctx, fixtureProfile(t))
		require.NoError(t, err)
		loaded, err := handle.Load(ctx)
		require.NoError(t, err)
		destination := categoryRecord(t, loaded.Committed, "category-groceries").GroupID
		revision, err := handle.Append(ctx, 1, draftGroupCreateOperation(1, "group_committed"))
		require.NoError(t, err)
		merge := draftGroupMergeOperation(revision, "group_committed", destination)
		merge.ID = "operation_group_retire"
		revision, err = handle.Append(ctx, revision, merge)
		require.NoError(t, err)
		loaded, err = handle.Load(ctx)
		require.NoError(t, err)
		effective, err := app.Replay(loaded)
		require.NoError(t, err)
		plan, err := app.BuildFoldPlan(effective, revision)
		require.NoError(t, err)
		_, err = handle.Fold(ctx, revision, plan)
		require.NoError(t, err)
		require.NoError(t, handle.Close())

		reopenedStore, err := Open(ctx, paths, DefaultOptions)
		require.NoError(t, err)
		reopened := reopenedStore.(*profile)
		t.Cleanup(func() { require.NoError(t, reopened.Close()) })
		restarted, err := reopened.Load(ctx)
		require.NoError(t, err)
		replayed, err := app.Replay(restarted)
		require.NoError(t, err)
		identity := domain.DrillIdentity{
			Dimension: domain.DimensionGroup, Currency: "USD", Scale: 2, Key: "group_committed",
		}
		assert.Equal(t, app.DrillEmpty, app.ClassifyKnownDrill(replayed, identity))
		assert.True(t, hasKnownDrill(restarted.KnownDrills, identity))
	})
}

func draftCategoryAssignOperation(
	createdRevision uint64,
	destination domain.EntityID,
	targets ...domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: "operation_category_assign", Type: domain.OperationCategoryAssign, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(), Targets: targets,
		Reassign: &domain.ReassignPayload{DestinationID: destination},
	}
}

func draftFoldMerchantMergeOperation(
	createdRevision uint64,
	source, destination domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: "operation_merchant_merge", Type: domain.OperationMerchantMerge, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{source},
		Merge:   &domain.MergePayload{SourceID: source, DestinationID: destination},
	}
}

func draftGroupCreateOperation(createdRevision uint64, id domain.EntityID) domain.Operation {
	return domain.Operation{
		ID: "operation_group_create", Type: domain.OperationGroupCreate, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{id},
		Create: &domain.CreatePayload{
			EntityType: string(domain.EntityKindGroup), EntityID: id,
			Label: "New Group", CollisionKey: "new group",
		},
	}
}

func draftCategoryMoveOperation(
	createdRevision uint64,
	categoryID, groupID domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: "operation_category_move", Type: domain.OperationCategoryMove, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{categoryID},
		Move: &domain.MovePayload{
			EntityID: categoryID, DestinationID: groupID,
		},
	}
}

func draftCategoryLabelOperation(createdRevision uint64, id domain.EntityID) domain.Operation {
	return domain.Operation{
		ID: "operation_category_label", Type: domain.OperationCategoryLabel, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{id},
		Label: &domain.LabelPayload{
			EntityID: id, Label: "Groceries Renamed", CollisionKey: "groceries renamed",
		},
	}
}

func draftGroupMergeOperation(
	createdRevision uint64,
	source, destination domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: "operation_group_merge", Type: domain.OperationGroupMerge, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{source},
		Merge:   &domain.MergePayload{SourceID: source, DestinationID: destination},
	}
}

func draftFoldMerchantLabelOperation(createdRevision uint64) domain.Operation {
	return domain.Operation{
		ID: "operation_merchant_label", Type: domain.OperationMerchantLabel, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{"merchant-grocer"},
		Label: &domain.LabelPayload{
			EntityID: "merchant-grocer", Label: "Example Grocer Renamed",
			CollisionKey: "example grocer renamed",
		},
	}
}

func draftCategoryCreateOperation(
	createdRevision uint64,
	groupID domain.EntityID,
	targets ...domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: "operation_category_create", Type: domain.OperationCategoryCreate, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(), Targets: targets,
		Create: &domain.CreatePayload{
			EntityType: string(domain.EntityKindCategory), EntityID: "category_new",
			Label: "New Category", CollisionKey: "new category", ParentID: groupID,
		},
	}
}

func draftCategoryMergeOperation(
	createdRevision uint64,
	source, destination domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: "operation_category_merge", Type: domain.OperationCategoryMerge, PayloadVersion: 1,
		CreatedRevision: createdRevision, CreatedAt: foldOperationTime(),
		Targets: []domain.EntityID{source},
		Merge:   &domain.MergePayload{SourceID: source, DestinationID: destination},
	}
}

func foldOperationTime() time.Time {
	return time.Date(2026, time.August, 14, 15, 0, 0, 0, time.UTC)
}

func categoryRecord(
	t *testing.T,
	profile domain.CommittedProfile,
	id domain.EntityID,
) domain.Category {
	t.Helper()
	for _, category := range profile.Categories {
		if category.ID == id {
			return category
		}
	}
	t.Fatalf("category %q not found", id)
	return domain.Category{}
}

func merchantRecord(
	t *testing.T,
	profile domain.CommittedProfile,
	id domain.EntityID,
) domain.Merchant {
	t.Helper()
	for _, merchant := range profile.Merchants {
		if merchant.ID == id {
			return merchant
		}
	}
	t.Fatalf("merchant %q not found", id)
	return domain.Merchant{}
}

func groupRecord(
	t *testing.T,
	profile domain.CommittedProfile,
	id domain.EntityID,
) domain.CategoryGroup {
	t.Helper()
	for _, group := range profile.Groups {
		if group.ID == id {
			return group
		}
	}
	t.Fatalf("group %q not found", id)
	return domain.CategoryGroup{}
}

func transactionRecord(
	t *testing.T,
	profile domain.CommittedProfile,
	id domain.EntityID,
) domain.TransactionRecord {
	t.Helper()
	for _, transaction := range profile.Transactions {
		if transaction.ID == id {
			return transaction
		}
	}
	t.Fatalf("transaction %q not found", id)
	return domain.TransactionRecord{}
}

func hasKnownDrill(values []domain.DrillIdentity, want domain.DrillIdentity) bool {
	wantKey, err := want.CanonicalKey()
	if err != nil {
		return false
	}
	for _, value := range values {
		key, keyErr := value.CanonicalKey()
		if keyErr == nil && key == wantKey {
			return true
		}
	}
	return false
}
