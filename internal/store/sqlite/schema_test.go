package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestSchemaUsesStrictConstrainedTables(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })

	requiredTables := []string{
		"schema_metadata", "profile_state", "accounts", "merchants", "category_groups",
		"categories", "transactions", "external_identities", "known_drills",
		"ynab_transaction_splits",
		"journal_operations", "operation_payloads", "operation_targets",
		"provider_binding", "provider_refresh_state", "provider_operation_lease",
		"provider_label_allocations", "provider_identity_lineage", "provider_write_batches",
		"provider_write_batch_operations", "provider_write_items", "provider_write_results",
		"provider_last_write_summary",
		"amazon_profile_settings", "amazon_order_items", "amazon_import_history",
	}
	for _, table := range requiredTables {
		var strict int
		err := profile.database.QueryRowContext(context.Background(),
			"SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = ?", table).Scan(&strict)
		require.NoError(t, err, table)
		assert.Equal(t, 1, strict, table)
	}

	assertColumnType(t, profile.database, "transactions", "amount_minor", "INTEGER")
	assertColumnType(t, profile.database, "transactions", "scale", "INTEGER")
	assertColumnType(t, profile.database, "ynab_transaction_splits", "amount_milliunits", "INTEGER")
	assertColumnType(t, profile.database, "ynab_transaction_splits", "amount_minor", "INTEGER")
	var realMoneyColumns int
	require.NoError(t, profile.database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pragma_table_info('transactions')
		WHERE lower(name) LIKE '%amount%' AND upper(type) = 'REAL'`).Scan(&realMoneyColumns))
	assert.Zero(t, realMoneyColumns)

	for _, table := range []string{"categories", "transactions", "ynab_transaction_splits", "journal_operations", "operation_payloads", "operation_targets"} {
		var count int
		require.NoError(t, profile.database.QueryRowContext(context.Background(),
			"SELECT count(*) FROM pragma_foreign_key_list(?)", table).Scan(&count))
		assert.Positive(t, count, table)
	}
}

func TestCurrentSchemaInstallsProtectedSplitCategory(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	var version int
	require.NoError(t, profile.database.QueryRowContext(context.Background(),
		"SELECT schema_version FROM schema_metadata WHERE singleton = 1").Scan(&version))
	assert.Equal(t, CurrentSchemaVersion, version)
	loaded, err := profile.Load(context.Background())
	require.NoError(t, err)
	assert.Contains(t, loaded.Committed.Categories, domain.Category{
		ID: domain.SplitCategoryID, GroupID: domain.UncategorizedGroupID,
		Label: domain.SplitLabel, CollisionKey: domain.SplitCollisionKey, Protected: true,
	})
}

func TestSchemaInstallsProviderWriteObjects(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })

	rows, err := profile.database.QueryContext(context.Background(), `
		SELECT name FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY name`)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rows.Close()) })
	names := make([]string, 0)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())

	assert.Subset(t, names, []string{
		"provider_operation_lease",
		"provider_identity_lineage",
		"provider_write_batches",
		"provider_write_batch_operations",
		"provider_write_items",
		"provider_write_results",
		"provider_last_write_summary",
	})
	assert.NotContains(t, names, "provider_refresh_lease")

	var version int
	require.NoError(t, profile.database.QueryRowContext(context.Background(),
		"SELECT schema_version FROM schema_metadata WHERE singleton = 1").Scan(&version))
	assert.Equal(t, CurrentSchemaVersion, version)
	assert.Equal(t, CurrentSchemaVersion, version)
	assertColumnType(t, profile.database, "provider_write_items", "item_kind", "TEXT")
	assertColumnType(t, profile.database, "provider_write_results", "already_absent", "INTEGER")
}

func TestAmazonSchemaUsesStrictExactMoneyTables(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })

	for _, table := range []string{
		"amazon_profile_settings", "amazon_order_items", "amazon_import_history",
	} {
		var strict int
		err = profile.database.QueryRowContext(context.Background(),
			"SELECT strict FROM pragma_table_list WHERE schema = 'main' AND name = ?", table).
			Scan(&strict)
		require.NoError(t, err, table)
		assert.Equal(t, 1, strict, table)
	}
	for _, table := range []string{"amazon_profile_settings", "amazon_order_items"} {
		var realColumns int
		require.NoError(t, profile.database.QueryRowContext(context.Background(), `
			SELECT count(*) FROM pragma_table_info(?) WHERE upper(type) = 'REAL'`, table).
			Scan(&realColumns))
		assert.Zero(t, realColumns, table)
	}
}

func TestProviderWriteItemSchemaEnforcesUpdateDeleteUnion(t *testing.T) {
	t.Parallel()

	profile, prepared, _ := preparedWriteProfile(t)
	ctx := context.Background()
	_, err := profile.database.ExecContext(ctx,
		"DELETE FROM provider_write_items WHERE batch_id = ?", prepared.Batch.ID)
	require.NoError(t, err)

	insert := func(kind string, hidden any, expectation any, leader int) error {
		_, insertErr := profile.database.ExecContext(ctx, `
			INSERT INTO provider_write_items(
				item_id, batch_id, position, item_kind, transaction_id, transaction_external_id,
				requested_hidden, originating_operation_ids_json, expectation_kind,
				group_leader, item_state, attempt_count
			) VALUES ('union-item', ?, 0, ?, 'transaction-a', 'provider-a', ?, '["operation-a"]', ?, ?, 'pending', 0)`,
			prepared.Batch.ID, kind, hidden, expectation, leader)
		return insertErr
	}

	assert.Error(t, insert("delete", 1, nil, 0))
	assert.Error(t, insert("delete", nil, "new", 0))
	assert.Error(t, insert("delete", nil, nil, 1))
	assert.Error(t, insert("update", nil, nil, 0))
	require.NoError(t, insert("delete", nil, nil, 0))
}

func TestCategoryClearPersistsAndRejectsConflictingSQLFields(t *testing.T) {
	t.Parallel()
	p, prepared, _ := preparedWriteProfile(t)
	ctx := context.Background()
	_, err := p.database.ExecContext(ctx, `UPDATE provider_write_items SET
		clear_category = 1, requested_hidden = NULL WHERE batch_id = ?`, prepared.Batch.ID)
	require.NoError(t, err)
	state, err := p.ProviderWriteState(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, state.Items)
	item := state.Items[0]
	assert.True(t, item.ClearCategory)
	_, err = p.database.ExecContext(ctx, `UPDATE provider_write_items SET
		requested_category_external_id = 'category-a' WHERE item_id = ?`, item.ID)
	require.Error(t, err)
	connection, err := p.database.Conn(ctx)
	require.NoError(t, err)
	defer func() {
		if connection != nil {
			require.NoError(t, connection.Close())
		}
	}()
	result := store.WriteResult{ItemID: item.ID, Kind: store.WriteItemUpdate,
		TransactionExternalID: item.TransactionExternalID, CategoryCleared: true,
		RecordedAt: time.Unix(1, 0)}
	require.NoError(t, insertWriteResult(ctx, connection, result))
	results, err := loadWriteResults(ctx, connection, prepared.Batch.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].CategoryCleared)
	_, err = connection.ExecContext(ctx, `UPDATE provider_write_results SET
		category_external_id = 'category-a' WHERE item_id = ?`, item.ID)
	require.Error(t, err)
	var filename string
	require.NoError(t, connection.QueryRowContext(ctx,
		"SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&filename))
	require.NoError(t, connection.Close())
	connection = nil
	require.NoError(t, p.Close())
	reopened, err := sql.Open(driverName, dataSourceName(filename, DefaultOptions))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	items, err := loadWriteItems(ctx, reopened, prepared.Batch.ID)
	require.NoError(t, err)
	assert.True(t, items[0].ClearCategory)
	results, err = loadWriteResults(ctx, reopened, prepared.Batch.ID)
	require.NoError(t, err)
	assert.True(t, results[0].CategoryCleared)
}

func TestWriteRestrictionsRejectOrphansAndFollowEntityLifecycle(t *testing.T) {
	t.Parallel()
	p := openSeededProfile(t, DefaultOptions)
	ctx := context.Background()
	loaded, err := p.Load(ctx)
	require.NoError(t, err)
	transaction := loaded.Committed.Transactions[0]
	insert := func(kind string, id domain.EntityID) error {
		_, err := p.database.ExecContext(ctx, `INSERT INTO provider_write_restrictions
			(entity_type, entity_id, reason) VALUES (?, ?, 'transfer')`, kind, id)
		return err
	}
	require.NoError(t, insert("transaction", transaction.ID))
	require.NoError(t, insert("merchant", transaction.MerchantID))
	require.Error(t, insert("transaction", "missing"))
	_, err = p.database.ExecContext(ctx, `UPDATE provider_write_restrictions
		SET entity_id = 'missing' WHERE entity_type = 'merchant'`)
	require.Error(t, err)
	_, err = p.database.ExecContext(ctx, "DELETE FROM transactions WHERE id = ?", transaction.ID)
	require.NoError(t, err)
	_, err = p.database.ExecContext(ctx, "UPDATE merchants SET retired = 1 WHERE id = ?", transaction.MerchantID)
	require.NoError(t, err)
	var count int
	require.NoError(t, p.database.QueryRowContext(ctx, "SELECT count(*) FROM provider_write_restrictions").Scan(&count))
	assert.Zero(t, count)
	require.Error(t, insert("merchant", transaction.MerchantID))
}

func TestProviderSchemaEnforcesSingletonLeaseAndAllocationConstraints(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	ctx := context.Background()

	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, currency, scale, bound_at_unix_ms
		) VALUES (2, 'monarch', 'monarch', 'remote-a', 'USD', 2, 1)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, currency, scale, bound_at_unix_ms
		) VALUES (1, 'monarch', 'monarch', 'remote-a', 'usd', 2, 1)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_operation_lease(
			singleton, owner_id, renderer, operation_kind, expires_at_unix_ms
		) VALUES (1, 'owner-a', 'background', 'refresh', 1)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_label_allocations(
			entity_type, namespace, external_id, base_collision_key,
			display_label, provider_label, suffix_token, unsuffixed
		) VALUES ('transaction', 'monarch/transaction', 'external-a', 'example',
			'Example', 'Example', 'a1b2', 0)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_label_allocations(
			entity_type, namespace, external_id, base_collision_key,
			display_label, provider_label, suffix_token, unsuffixed
		) VALUES ('merchant', 'shared', 'external-a', 'example', 'Example', 'Example', '', 1)`)
	require.NoError(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_label_allocations(
			entity_type, namespace, external_id, base_collision_key,
			display_label, provider_label, suffix_token, unsuffixed)
		VALUES ('merchant', 'other', 'external-b', 'example', 'Example', 'Example', '', 1)`)
	assert.Error(t, err, "one collision key can have only one permanent unsuffixed owner")
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO provider_label_allocations(
			entity_type, namespace, external_id, base_collision_key,
			display_label, provider_label, suffix_token, unsuffixed
		) VALUES ('group', 'shared', 'external-a', 'group', 'Group', 'Group', '', 1)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		UPDATE provider_refresh_state SET generation = -1 WHERE singleton = 1`)
	assert.Error(t, err)
}

func TestSchemaEnforcesMoneySingletonCollisionAndJournalConstraints(t *testing.T) {
	t.Parallel()

	profileStore, err := Open(context.Background(), temporaryPaths(t), DefaultOptions)
	require.NoError(t, err)
	profile := profileStore.(*profile)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	ctx := context.Background()

	_, err = profile.database.ExecContext(ctx,
		"INSERT INTO profile_state(singleton, revision, journal_cursor) VALUES (2, 0, 0)")
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx,
		"UPDATE profile_state SET revision = -1 WHERE singleton = 1")
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx,
		"UPDATE profile_state SET journal_cursor = -1 WHERE singleton = 1")
	assert.Error(t, err)

	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO accounts(id, label, collision_key, retired)
		VALUES ('a', 'Account', 'account', 0);
		INSERT INTO merchants(id, label, collision_key, retired, protected)
		VALUES ('m', 'Merchant', 'merchant', 0, 0);
		INSERT INTO category_groups(id, label, collision_key, retired, protected)
		VALUES ('g', 'Group', 'group', 0, 0);
		INSERT INTO categories(id, group_id, label, collision_key, retired, protected)
		VALUES ('c', 'g', 'Category', 'category', 0, 0);
		INSERT INTO transactions(
			id, provider, provider_id, account_id, merchant_id, category_id, transaction_date,
			amount_minor, currency, scale, notes, hidden, pending, metadata_json
		) VALUES ('t', 'fixture', 'provider-t', 'a', 'm', 'c', '2026-08-14',
			1.5, 'USD', 2, '', 0, 0, '{}')`)
	assert.Error(t, err)

	_, err = profile.database.ExecContext(ctx,
		"INSERT INTO merchants(id, label, collision_key, retired, protected) VALUES ('m1','A','same',0,0),('m2','B','same',0,0)")
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx,
		"INSERT INTO merchants(id, label, collision_key, retired, protected) VALUES ('m1','A','same',0,0),('m2','B','same',1,0)")
	require.NoError(t, err)

	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('op', 0, 'hide_toggle', 1, 0, 0)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('op', 1, 'unknown', 1, 0, 0)`)
	assert.Error(t, err)
	_, err = profile.database.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('op', 1, 'transaction.hide-toggle', 0, 0, 0)`)
	assert.Error(t, err)
}

func assertColumnType(t *testing.T, database *sql.DB, table, column, expected string) {
	t.Helper()
	var actual string
	require.NoError(t, database.QueryRowContext(context.Background(),
		"SELECT type FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&actual))
	assert.Equal(t, expected, strings.ToUpper(actual))
}
