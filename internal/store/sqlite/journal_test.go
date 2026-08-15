package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAppendPersistsCanonicalOperationAndAdvancesRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	operation := draftHideOperation("operation_first", 1, "transaction_a")

	next, err := profile.Append(ctx, 1, operation)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), next)
	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Len(t, snapshot.Journal, 1)
	assert.Equal(t, int64(1), snapshot.Journal[0].Sequence)
	assert.Equal(t, operation.ID, snapshot.Journal[0].ID)
	assert.Equal(t, operation.Type, snapshot.Journal[0].Type)
	assert.Equal(t, operation.Targets, snapshot.Journal[0].Targets)
	assert.Equal(t, 1, snapshot.Cursor)
	assert.Equal(t, uint64(2), snapshot.Revision)
}

func TestAppendBehindHeadTruncatesInactiveTailAtomically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	first := draftHideOperation("operation_first", 1, "transaction_a")
	second := draftHideOperation("operation_second", 2, "transaction_b")
	third := draftHideOperation("operation_third", 4, "transaction_a")

	revision, err := profile.Append(ctx, 1, first)
	require.NoError(t, err)
	revision, err = profile.Append(ctx, revision, second)
	require.NoError(t, err)
	revision, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)
	revision, err = profile.Append(ctx, revision, third)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), revision)

	snapshot, err := profile.Load(ctx)
	require.NoError(t, err)
	require.Len(t, snapshot.Journal, 2)
	assert.Equal(t, []string{first.ID, third.ID}, []string{
		snapshot.Journal[0].ID, snapshot.Journal[1].ID,
	})
	assert.Equal(t, []int64{1, 2}, []int64{
		snapshot.Journal[0].Sequence, snapshot.Journal[1].Sequence,
	})
	assert.Equal(t, 2, snapshot.Cursor)
}

func TestAppendStaleRevisionLeavesTailAndSequenceUntouched(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	first := draftHideOperation("operation_first", 1, "transaction_a")
	revision, err := profile.Append(ctx, 1, first)
	require.NoError(t, err)
	before, err := profile.Load(ctx)
	require.NoError(t, err)

	_, err = profile.Append(ctx, 1, draftHideOperation("operation_stale", 1, "transaction_b"))
	assertRevisionConflict(t, err, 1, revision)
	after, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestMoveCursorUndoRedoAndBoundaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_first", 1, "transaction_a"),
	)
	require.NoError(t, err)

	revision, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), revision)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Zero(t, loaded.Cursor)
	assert.Len(t, loaded.Journal, 1)

	revision, err = profile.MoveCursor(ctx, revision, +1)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), revision)
	loaded, err = profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, loaded.Cursor)

	_, err = profile.MoveCursor(ctx, revision, +1)
	assertStoreCode(t, err, store.CodeInvalidOperation)
	_, err = profile.MoveCursor(ctx, revision, 0)
	assertStoreCode(t, err, store.CodeInvalidOperation)
	loaded, err = profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(4), loaded.Revision)
	assert.Equal(t, 1, loaded.Cursor)
}

func TestMoveCursorUsesActiveCountWhenSequencesHaveGaps(t *testing.T) {
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
		draftHideOperation("operation_second", revision, "transaction_b"),
	)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx,
		"UPDATE journal_operations SET sequence = 3 WHERE id = 'operation_second'")
	require.NoError(t, err)

	revision, err = profile.MoveCursor(ctx, revision, -1)
	require.NoError(t, err)
	_, err = profile.MoveCursor(ctx, revision, +1)
	require.NoError(t, err)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3}, []int64{
		loaded.Journal[0].Sequence,
		loaded.Journal[1].Sequence,
	})
	assert.Equal(t, 2, loaded.Cursor)
}

func TestMoveCursorChecksAuthoritativeRevisionBeforeBoundary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	revision, err := profile.Append(
		ctx,
		1,
		draftHideOperation("operation_first", 1, "transaction_a"),
	)
	require.NoError(t, err)

	_, err = profile.MoveCursor(ctx, 1, +1)
	assertRevisionConflict(t, err, 1, revision)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, revision, loaded.Revision)
	assert.Equal(t, 1, loaded.Cursor)
}

func TestJournalLimitRejectsAppendAtOperationCeilingWithoutChangingState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	_, err := profile.database.ExecContext(ctx, `
		WITH RECURSIVE numbers(value) AS (
			SELECT 1 UNION ALL SELECT value + 1 FROM numbers WHERE value < ?
		)
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		)
		SELECT printf('operation_limit_%05d', value), value,
			'transaction.hide-toggle', 1, 1, 1786712400000 FROM numbers`, maxJournalOperations)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO operation_payloads(operation_id, payload_version, payload_json)
		SELECT id, 1, '{}' FROM journal_operations WHERE id LIKE 'operation_limit_%'`)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO operation_targets(operation_id, ordinal, entity_id)
		SELECT id, 0, 'transaction_a' FROM journal_operations WHERE id LIKE 'operation_limit_%'`)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx,
		"UPDATE profile_state SET journal_cursor = ? WHERE singleton = 1", maxJournalOperations)
	require.NoError(t, err)

	before := journalCounts(t, profile)
	_, err = profile.Append(ctx, 1, draftHideOperation("operation_over_limit", 1, "transaction_b"))
	assertStoreCode(t, err, store.CodeJournalFull)
	assert.Equal(t, before, journalCounts(t, profile))

	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, maxJournalOperations, loaded.Cursor)
	assert.Len(t, loaded.Journal, maxJournalOperations)
	revision, err := profile.MoveCursor(ctx, 1, -1)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), revision)
}

func TestJournalLimitBoundaries(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateJournalCapacity(
		maxJournalOperations-1, maxJournalTargets-1, 1,
	))
	assert.ErrorIs(t, validateJournalCapacity(maxJournalOperations, 0, 1), errJournalFull)
	assert.ErrorIs(t, validateJournalCapacity(0, maxJournalTargets, 1), errJournalFull)
}

func TestJournalLimitRejectsOversizedTargetBatchBeforeMutation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	operation := draftHideOperation("operation_too_many_targets", 1, "transaction_a")
	operation.Targets = make([]domain.EntityID, maxJournalTargets+1)
	before := journalCounts(t, profile)

	_, err := profile.Append(ctx, 1, operation)
	assertStoreCode(t, err, store.CodeJournalFull)
	assert.Equal(t, before, journalCounts(t, profile))
}

func journalCounts(t *testing.T, profile *profile) string {
	t.Helper()
	var operations, targets, cursor int
	require.NoError(t, profile.database.QueryRow(`
		SELECT (SELECT count(*) FROM journal_operations),
			(SELECT count(*) FROM operation_targets), journal_cursor
		FROM profile_state WHERE singleton = 1`).Scan(&operations, &targets, &cursor))
	return fmt.Sprintf("%d/%d/%d", operations, targets, cursor)
}

func openSeededProfile(t *testing.T, options Options) *profile {
	t.Helper()
	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), options)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	return profile
}

func draftHideOperation(
	id string,
	createdRevision uint64,
	targets ...domain.EntityID,
) domain.Operation {
	return domain.Operation{
		ID: id, Type: domain.OperationTransactionHide, PayloadVersion: 1,
		CreatedRevision: createdRevision,
		CreatedAt:       time.Date(2026, time.August, 14, 13, 0, 0, 0, time.UTC),
		Targets:         targets,
		HideToggle:      &domain.HideTogglePayload{},
	}
}

func assertRevisionConflict(t *testing.T, err error, observed, current uint64) {
	t.Helper()
	assertStoreCode(t, err, store.CodeRevisionConflict)
	var failure *store.Error
	require.ErrorAs(t, err, &failure)
	require.NotNil(t, failure.ObservedRevision)
	require.NotNil(t, failure.CurrentRevision)
	assert.Equal(t, observed, *failure.ObservedRevision)
	assert.Equal(t, current, *failure.CurrentRevision)
}
