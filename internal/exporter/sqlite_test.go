package exporter

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestWriteSQLiteCreatesLosslessStandaloneDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.db")
	require.NoError(t, writeSQLite(context.Background(), path, testDocument(t)))
	database, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	var integrity string
	require.NoError(t, database.QueryRow("PRAGMA integrity_check").Scan(&integrity))
	assert.Equal(t, "ok", integrity)
	var metadataCount int
	require.NoError(t, database.QueryRow("SELECT count(*) FROM export_metadata").Scan(&metadataCount))
	assert.Equal(t, len(metadataEntries(testDocument(t).Metadata)), metadataCount)
	var revision string
	require.NoError(t, database.QueryRow(
		"SELECT value FROM export_metadata WHERE key = 'source_revision'",
	).Scan(&revision))
	assert.Equal(t, "42", revision)

	rows, err := database.Query(`
		SELECT transaction_id, amount, amount_minor, currency, scale, account, notes, hidden
		FROM transactions ORDER BY rowid`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()
	require.True(t, rows.Next())
	var id, amount, currency, account, notes string
	var minor int64
	var scale, hidden int
	require.NoError(t, rows.Scan(&id, &amount, &minor, &currency, &scale, &account, &notes, &hidden))
	assert.Equal(t, "txn-a", id)
	assert.Equal(t, "-12.34", amount)
	assert.Equal(t, int64(-1234), minor)
	assert.Equal(t, "USD", currency)
	assert.Equal(t, 2, scale)
	assert.Equal(t, "  =Formula Account", account)
	assert.Equal(t, "\tformula\nline two", notes)
	assert.Equal(t, 1, hidden)
}

func TestSQLiteSchemaIsStrictAndRejectsInvalidTypedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.db")
	require.NoError(t, writeSQLite(context.Background(), path, testDocument(t)))
	database, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	columns, err := database.Query("PRAGMA table_info(transactions)")
	require.NoError(t, err)
	types := map[string]string{}
	for columns.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		require.NoError(t, columns.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey))
		types[name] = dataType
	}
	require.NoError(t, columns.Close())
	assert.Equal(t, "INTEGER", types["amount_minor"])
	assert.Equal(t, "TEXT", types["amount"])
	for _, dataType := range types {
		assert.NotEqual(t, "REAL", dataType)
	}

	_, err = database.Exec(`
		INSERT INTO transactions (
			transaction_id, provider, provider_transaction_id, date, amount, amount_minor,
			currency, scale, account_id, account, merchant_id, merchant, category_id,
			category, group_id, "group", notes, hidden, transaction_metadata_json
		) VALUES ('bad', '', '', 'not-a-date', '1.00', 100, 'usd', 10, 'a', '', 'm', '', '', '', '', '', '', 2, '{}')`)
	assert.Error(t, err)

	var forbidden int
	require.NoError(t, database.QueryRow(`
		SELECT count(*) FROM sqlite_schema
		WHERE name IN ('profile', 'journal_operations', 'provider_sessions', 'provider_write_batches')`,
	).Scan(&forbidden))
	assert.Zero(t, forbidden)
}
