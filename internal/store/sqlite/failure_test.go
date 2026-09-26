package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/store"
)

func TestFailureAtomicityStoreFullAppend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	profile := openSeededProfile(t, DefaultOptions)
	before, err := profile.Load(ctx)
	require.NoError(t, err)

	require.NoError(t, installFullTrigger(ctx, profile, "journal_operations"))
	_, err = profile.Append(ctx, before.Revision, draftHideOperation(
		"operation_full_append", before.Revision, before.Committed.Transactions[0].ID,
	))
	assertStoreCode(t, err, store.CodeStoreError)
	assertSafeStorageFailure(t, err)

	after, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	assert.Equal(t, before, after)
}

func TestFailureAtomicityStoreFullFold(t *testing.T) {
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

	require.NoError(t, installFullTrigger(ctx, profile, "merchants"))
	_, err = profile.Fold(ctx, revision, plan)
	assertStoreCode(t, err, store.CodeStoreError)
	assertSafeStorageFailure(t, err)

	after, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	assert.Equal(t, before, after)
}

func TestFailureAtomicityStoreBusy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	options := DefaultOptions
	options.MutationBusyTimeout = 10 * time.Millisecond
	profile := openSeededProfile(t, options)
	before, err := profile.Load(ctx)
	require.NoError(t, err)

	connection, err := profile.database.Conn(ctx)
	require.NoError(t, err)
	_, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE")
	require.NoError(t, err)
	_, err = profile.Append(ctx, before.Revision, draftHideOperation(
		"operation_busy_append", before.Revision, before.Committed.Transactions[0].ID,
	))
	assertStoreCode(t, err, store.CodeStoreBusy)
	assertSafeStorageFailure(t, err)
	_, err = connection.ExecContext(context.Background(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, connection.Close())

	after, loadErr := profile.Load(ctx)
	require.NoError(t, loadErr)
	assert.Equal(t, before, after)
}

func installFullTrigger(
	ctx context.Context,
	profile *profile,
	table string,
) error {
	var trigger string
	switch table {
	case "journal_operations":
		trigger = `
			CREATE TRIGGER force_store_full BEFORE INSERT ON journal_operations
			BEGIN
				INSERT INTO failure_padding(payload) VALUES(zeroblob(4194304));
			END`
	case "merchants":
		trigger = `
			CREATE TRIGGER force_store_full BEFORE UPDATE ON merchants
			BEGIN
				INSERT INTO failure_padding(payload) VALUES(zeroblob(4194304));
			END`
	default:
		return fmt.Errorf("unsupported failure table")
	}
	if _, err := profile.database.ExecContext(ctx, `
		CREATE TABLE failure_padding(payload BLOB) STRICT`); err != nil {
		return err
	}
	if _, err := profile.database.ExecContext(ctx, trigger); err != nil {
		return err
	}
	if _, err := profile.database.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	var pages int
	if err := profile.database.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return err
	}
	_, err := profile.database.ExecContext(ctx, fmt.Sprintf("PRAGMA max_page_count = %d", pages))
	return err
}

func assertSafeStorageFailure(t testing.TB, err error) {
	t.Helper()
	require.Error(t, err)
	for _, privateValue := range []string{
		"operation_full", "merchant", "transaction", "Example", "4194304", "moneyflow-v2.db",
	} {
		assert.NotContains(t, err.Error(), privateValue)
	}
	var failure *store.Error
	require.ErrorAs(t, err, &failure)
	assert.Nil(t, failure.CurrentRevision)
	assert.Nil(t, failure.ObservedRevision)
}
