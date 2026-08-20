package exporter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/app"
	// Register the pure-Go SQLite driver used only for standalone export files.
	_ "modernc.org/sqlite"
)

const exportSchemaSQL = `
CREATE TABLE export_metadata (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL
) STRICT;
CREATE TABLE transactions (
    transaction_id TEXT PRIMARY KEY NOT NULL CHECK (length(transaction_id) > 0),
    provider TEXT NOT NULL,
    provider_transaction_id TEXT NOT NULL,
    date TEXT NOT NULL CHECK (length(date) = 10 AND date = strftime('%Y-%m-%d', date)),
    amount TEXT NOT NULL,
    amount_minor INTEGER NOT NULL,
    currency TEXT NOT NULL CHECK (length(currency) = 3 AND currency NOT GLOB '*[^A-Z]*'),
    scale INTEGER NOT NULL CHECK (scale BETWEEN 0 AND 9),
    account_id TEXT NOT NULL CHECK (length(account_id) > 0),
    account TEXT NOT NULL,
    merchant_id TEXT NOT NULL CHECK (length(merchant_id) > 0),
    merchant TEXT NOT NULL,
    category_id TEXT NOT NULL,
    category TEXT NOT NULL,
    group_id TEXT NOT NULL,
    "group" TEXT NOT NULL,
    notes TEXT NOT NULL,
    hidden INTEGER NOT NULL CHECK (hidden IN (0, 1)),
    transaction_metadata_json TEXT NOT NULL
        CHECK (json_valid(transaction_metadata_json) AND json_type(transaction_metadata_json) = 'object')
) STRICT;`

const insertExportRowSQL = `INSERT INTO transactions (
    transaction_id, provider, provider_transaction_id, date, amount, amount_minor,
    currency, scale, account_id, account, merchant_id, merchant, category_id,
    category, group_id, "group", notes, hidden, transaction_metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func writeSQLite(ctx context.Context, path string, document app.ExportDocument) (resultErr error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open SQLite export: %w", err)
	}
	defer func() {
		if closeErr := database.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("close SQLite export: %w", closeErr)
		}
	}()
	database.SetMaxOpenConns(1)
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite export: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err = transaction.ExecContext(ctx, exportSchemaSQL); err != nil {
		return fmt.Errorf("create SQLite export schema: %w", err)
	}
	for _, entry := range metadataEntries(document.Metadata) {
		if _, err = transaction.ExecContext(
			ctx, "INSERT INTO export_metadata(key, value) VALUES (?, ?)", entry.Key, entry.Value,
		); err != nil {
			return fmt.Errorf("write SQLite export metadata: %w", err)
		}
	}
	for _, row := range document.Rows {
		if _, err = transaction.ExecContext(ctx, insertExportRowSQL,
			row.TransactionID, row.Provider, row.ProviderTransactionID, row.Date.String(), row.Amount,
			row.AmountMinor, row.Currency, int(row.Scale), row.AccountID, row.Account, row.MerchantID,
			row.Merchant, row.CategoryID, row.Category, row.GroupID, row.Group, row.Notes,
			row.Hidden, row.TransactionMetadataJSON,
		); err != nil {
			return fmt.Errorf("write SQLite export row: %w", err)
		}
	}
	if err = transaction.Commit(); err != nil {
		return fmt.Errorf("commit SQLite export: %w", err)
	}
	var integrity string
	if err = database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("check SQLite export: %w", err)
	}
	if integrity != "ok" {
		return errors.New("check SQLite export: integrity check failed")
	}
	return nil
}
