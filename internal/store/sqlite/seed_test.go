package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/store"
)

func TestSeedCreatesRevisionOneProfileAndIsVisibleAcrossHandles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	first := firstStore.(*profile)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	committed := fixtureProfile(t)

	revision, err := first.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), revision)
	secondStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	second := secondStore.(*profile)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	loaded, err := second.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), loaded.Revision)
	assert.Equal(t, committed, loaded.Committed)
	assert.Zero(t, loaded.Cursor)
	assert.Empty(t, loaded.Journal)
	assert.NotEmpty(t, loaded.KnownDrills)
}

func TestSeedRefusesOverwriteWithoutChangingProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	committed := fixtureProfile(t)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)

	_, err = profile.CreateSeededProfile(ctx, domain.CommittedProfile{})
	assertStoreCode(t, err, store.CodeInvalidOperation)
	loaded, err := profile.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, committed, loaded.Committed)
	assert.Equal(t, uint64(1), loaded.Revision)
}

func TestSeedRefusesPartiallyPopulatedRevisionZeroProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.database.ExecContext(ctx,
		"INSERT INTO accounts(id, label, collision_key, retired) VALUES ('account_existing', 'Existing', 'existing', 0)")
	require.NoError(t, err)

	_, err = profile.CreateSeededProfile(ctx, fixtureProfile(t))
	assertStoreCode(t, err, store.CodeInvalidOperation)
	assertTableCount(t, profile, "accounts", 1)
	revision, err := profile.CurrentRevision(ctx)
	require.NoError(t, err)
	assert.Zero(t, revision)
}

func TestSeedRollsBackMidWriteConstraintFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.database.ExecContext(ctx,
		"CREATE TRIGGER reject_seed_merchants BEFORE INSERT ON merchants "+
			"BEGIN SELECT RAISE(ABORT, 'synthetic seed failure'); END")
	require.NoError(t, err)

	_, err = profile.CreateSeededProfile(ctx, fixtureProfile(t))
	assertStoreCode(t, err, store.CodeStoreError)
	assertTableCount(t, profile, "accounts", 0)
	assertTableCount(t, profile, "merchants", 0)
	revision, err := profile.CurrentRevision(ctx)
	require.NoError(t, err)
	assert.Zero(t, revision)
}

func TestSeedStoresMoneyAsSQLiteInteger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	var nonIntegers int
	require.NoError(t, profile.database.QueryRowContext(ctx,
		"SELECT count(*) FROM transactions WHERE typeof(amount_minor) <> 'integer'").
		Scan(&nonIntegers))
	assert.Zero(t, nonIntegers)
}

func fixtureProfile(t *testing.T) domain.CommittedProfile {
	t.Helper()
	transactions, err := fixture.Load(filepath.Join(
		"..", "..", "..", "testdata", "parity", "transactions.json",
	))
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	return committed
}

func assertTableCount(t *testing.T, profile *profile, table string, expected int) {
	t.Helper()
	var actual int
	require.NoError(t, profile.database.QueryRowContext(context.Background(),
		"SELECT count(*) FROM "+table).Scan(&actual))
	assert.Equal(t, expected, actual)
}
