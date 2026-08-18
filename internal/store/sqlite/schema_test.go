package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		"journal_operations", "operation_payloads", "operation_targets",
		"provider_binding", "provider_refresh_state", "provider_operation_lease",
		"provider_label_allocations", "provider_identity_lineage", "provider_write_batches",
		"provider_write_batch_operations", "provider_write_items", "provider_write_results",
		"provider_last_write_summary",
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
	var realMoneyColumns int
	require.NoError(t, profile.database.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pragma_table_info('transactions')
		WHERE lower(name) LIKE '%amount%' AND upper(type) = 'REAL'`).Scan(&realMoneyColumns))
	assert.Zero(t, realMoneyColumns)

	for _, table := range []string{"categories", "transactions", "journal_operations", "operation_payloads", "operation_targets"} {
		var count int
		require.NoError(t, profile.database.QueryRowContext(context.Background(),
			"SELECT count(*) FROM pragma_foreign_key_list(?)", table).Scan(&count))
		assert.Positive(t, count, table)
	}
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
	assert.Equal(t, 5, version)
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
