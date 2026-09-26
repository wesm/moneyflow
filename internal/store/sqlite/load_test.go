package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestLoadEmptyCurrentSchemaIncludesProtectedSentinels(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profileStore.Close()) })
	snapshot, err := profileStore.Load(context.Background())
	require.NoError(t, err)
	assert.Zero(t, snapshot.Revision)
	assert.Contains(t, snapshot.Committed.Groups, domain.CategoryGroup{
		ID: domain.UncategorizedGroupID, Label: "Uncategorized",
		CollisionKey: "uncategorized", Protected: true,
	})
	assert.Contains(t, snapshot.Committed.Categories, domain.Category{
		ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
		Label: "Uncategorized", CollisionKey: "uncategorized", Protected: true,
	})
}

func TestLoadSurvivesRestartAndReturnsDefensiveCopies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	paths := temporaryPaths(t)
	firstStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	committed := fixtureProfile(t)
	_, err = firstStore.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	require.NoError(t, firstStore.Close())

	secondStore, err := Open(ctx, paths, DefaultOptions)
	require.NoError(t, err)
	second := secondStore.(*profile)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	firstLoad, err := second.Load(ctx)
	require.NoError(t, err)
	firstLoad.Committed.Merchants[0].Label = "Changed"
	firstLoad.Committed.Transactions[0].Metadata["changed"] = "yes"
	firstLoad.KnownDrills[0].Key = "changed"
	secondLoad, err := second.Load(ctx)
	require.NoError(t, err)
	assert.Equal(t, committed, secondLoad.Committed)
	assert.NotEqual(t, "changed", secondLoad.KnownDrills[0].Key)
}

func TestLoadRejectsInvalidStoredMetadataShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx,
		"UPDATE transactions SET metadata_json = '[]' "+
			"WHERE id = (SELECT min(id) FROM transactions)")
	require.NoError(t, err)

	_, err = profile.Load(ctx)
	assertStoreCode(t, err, store.CodeStoreCorrupt)
}

func TestLoadRejectsJournalHeaderWithoutPayloadEvenWhenInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	profileStore, err := Open(ctx, temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	_, err = profile.CreateSeededProfile(ctx, fixtureProfile(t))
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('operation_missing_payload', 1, 'transaction.hide-toggle', 1, 1, 1)`)
	require.NoError(t, err)

	_, err = profile.Load(ctx)
	assertStoreCode(t, err, store.CodeStoreCorrupt)
}
