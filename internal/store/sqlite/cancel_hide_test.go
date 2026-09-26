package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestCancelHidePartiallyReplacesBatchWithoutCreatingUndoUnit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_batch", 1, "transaction_a", "transaction_b"),
	)
	require.NoError(t, err)

	revision, err = profile.CancelHide(ctx, revision, []domain.EntityID{"transaction_a"})
	require.NoError(t, err)
	assert.Equal(t, uint64(3), revision)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Journal, 1)
	assert.Equal(t, "operation_batch", loaded.Journal[0].ID)
	assert.Equal(t, int64(1), loaded.Journal[0].Sequence)
	assert.Equal(t, uint64(1), loaded.Journal[0].CreatedRevision)
	assert.Equal(t, uint16(1), loaded.Journal[0].PayloadVersion)
	assert.NotNil(t, loaded.Journal[0].HideToggle)
	assert.Equal(t, []domain.EntityID{"transaction_b"}, loaded.Journal[0].Targets)
	assert.Equal(t, 1, loaded.Cursor)
}

func TestCancelHideRemovesCompleteOperationAndUndoReachesPrecedingEdit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(ctx, 1, draftMerchantLabelOperation(1))
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_hide", revision, "transaction_a"),
	)
	require.NoError(t, err)

	revision, err = profile.CancelHide(ctx, revision, []domain.EntityID{"transaction_a"})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Journal, 1)
	assert.Equal(t, "operation_label", loaded.Journal[0].ID)
	assert.Equal(t, 1, loaded.Cursor)

	_, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)
	loaded, err = profile.Load(ctx)
	require.NoError(t, err)
	assert.Zero(t, loaded.Cursor)
}

func TestCancelHideRemovesTargetFromEveryOriginAndAdjustsCursorPerEmptyOperation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_first", 1, "transaction_a"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_second", revision, "transaction_a", "transaction_b"),
	)
	require.NoError(t, err)

	_, err = profile.CancelHide(ctx, revision, []domain.EntityID{"transaction_a"})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Journal, 1)
	assert.Equal(t, "operation_second", loaded.Journal[0].ID)
	assert.Equal(t, []domain.EntityID{"transaction_b"}, loaded.Journal[0].Targets)
	assert.Equal(t, 1, loaded.Cursor)
}

func TestCancelHideTruncatesRedoBeforeCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_active", 1, "transaction_a"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_redo", revision, "transaction_b"),
	)
	require.NoError(t, err)
	revision, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)

	_, err = profile.CancelHide(ctx, revision, []domain.EntityID{"transaction_a"})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Empty(t, loaded.Journal)
	assert.Zero(t, loaded.Cursor)
}

func TestCancelHideUsesActiveCountWithSequenceHoles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_ten", 1, "transaction_a"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_twenty", revision, "transaction_b"),
	)
	require.NoError(t, err)
	revision, err = profile.Append(
		ctx,
		revision,
		draftHideOperation("operation_thirty", revision, "transaction_a"),
	)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		UPDATE journal_operations SET sequence = CASE id
			WHEN 'operation_ten' THEN 10
			WHEN 'operation_twenty' THEN 20
			WHEN 'operation_thirty' THEN 30
		END`)
	require.NoError(t, err)

	revision, err = profile.CancelHide(ctx, revision, []domain.EntityID{"transaction_b"})
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{10, 30}, operationSequences(loaded.Journal))
	assert.Equal(t, 2, loaded.Cursor)

	_, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)
	loaded, err = profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.Cursor)
	assert.Equal(t, int64(30), loaded.Journal[1].Sequence)
}

func TestCancelHideStaleCrossHandleChangesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	first := firstStore.(*profile)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	_, err = first.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	secondStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	second := secondStore.(*profile)
	t.Cleanup(func() { require.NoError(t, second.Close()) })

	revision, err := first.Append(
		ctx,
		1,
		draftHideOperation("operation_hide", 1, "transaction_a"),
	)
	require.NoError(t, err)
	before, err := first.Load(ctx)
	require.NoError(t, err)

	_, err = second.CancelHide(ctx, 1, []domain.EntityID{"transaction_a"})
	assertRevisionConflict(t, err, 1, revision)
	after, err := first.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestCancelHideRejectsPartialCoverageAndNoncanonicalTargetsAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_hide", 1, "transaction_a"),
	)
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)

	for _, targets := range [][]domain.EntityID{
		{"transaction_a", "transaction_b"},
		{"transaction_b", "transaction_a"},
		{"transaction_a", "transaction_a"},
		{},
	} {
		_, err = profile.CancelHide(ctx, revision, targets)
		assertStoreCode(t, err, store.CodeInvalidOperation)
		after, loadErr := profile.Load(ctx)
		require.NoError(t, loadErr)
		assert.Equal(t, before, after)
	}
}

func draftMerchantLabelOperation(createdRevision uint64) domain.Operation {
	return domain.Operation{
		ID: "operation_label", Type: domain.OperationMerchantLabel, PayloadVersion: 1,
		CreatedRevision: createdRevision,
		CreatedAt:       draftHideOperation("unused", createdRevision, "unused").CreatedAt,
		Targets:         []domain.EntityID{"merchant_a"},
		Label: &domain.LabelPayload{
			EntityID: "merchant_a", Label: "Merchant Renamed", CollisionKey: "merchant renamed",
		},
	}
}

func operationSequences(operations []domain.Operation) []int64 {
	sequences := make([]int64, len(operations))
	for index, operation := range operations {
		sequences[index] = operation.Sequence
	}
	return sequences
}
