package exporter

import (
	"context"
	"database/sql"
	"encoding/json"
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
    date TEXT NOT NULL CHECK (length(date) = 10 AND date = date(date)),
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
        CHECK (substr(transaction_metadata_json, 1, 1) = '{' AND substr(transaction_metadata_json, -1) = '}')
) STRICT;`

const insertExportRowsSQL = `INSERT INTO transactions (
    transaction_id, provider, provider_transaction_id, date, amount, amount_minor,
    currency, scale, account_id, account, merchant_id, merchant, category_id,
    category, group_id, "group", notes, hidden, transaction_metadata_json
) SELECT
    json_extract(value, '$.transaction_id'),
    json_extract(value, '$.provider'),
    json_extract(value, '$.provider_transaction_id'),
    json_extract(value, '$.date'),
    json_extract(value, '$.amount'),
    json_extract(value, '$.amount_minor'),
    json_extract(value, '$.currency'),
    json_extract(value, '$.scale'),
    json_extract(value, '$.account_id'),
    json_extract(value, '$.account'),
    json_extract(value, '$.merchant_id'),
    json_extract(value, '$.merchant'),
    json_extract(value, '$.category_id'),
    json_extract(value, '$.category'),
    json_extract(value, '$.group_id'),
    json_extract(value, '$.group'),
    json_extract(value, '$.notes'),
    json_extract(value, '$.hidden'),
    json_extract(value, '$.transaction_metadata_json')
FROM json_each(?)`

type sqliteJSONRow struct {
	TransactionID           string `json:"transaction_id"`
	Provider                string `json:"provider"`
	ProviderTransactionID   string `json:"provider_transaction_id"`
	Date                    string `json:"date"`
	Amount                  string `json:"amount"`
	AmountMinor             int64  `json:"amount_minor"`
	Currency                string `json:"currency"`
	Scale                   int    `json:"scale"`
	AccountID               string `json:"account_id"`
	Account                 string `json:"account"`
	MerchantID              string `json:"merchant_id"`
	Merchant                string `json:"merchant"`
	CategoryID              string `json:"category_id"`
	Category                string `json:"category"`
	GroupID                 string `json:"group_id"`
	Group                   string `json:"group"`
	Notes                   string `json:"notes"`
	Hidden                  bool   `json:"hidden"`
	TransactionMetadataJSON string `json:"transaction_metadata_json"`
}

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
	if _, err = database.ExecContext(ctx, `
		PRAGMA journal_mode=OFF;
		PRAGMA synchronous=OFF;
		PRAGMA temp_store=MEMORY;
		PRAGMA locking_mode=EXCLUSIVE;`); err != nil {
		return fmt.Errorf("configure SQLite export: %w", err)
	}
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite export: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err = transaction.ExecContext(ctx, exportSchemaSQL); err != nil {
		return fmt.Errorf("create SQLite export schema: %w", err)
	}
	metadataStatement, err := transaction.PrepareContext(
		ctx, "INSERT INTO export_metadata(key, value) VALUES (?, ?)",
	)
	if err != nil {
		return fmt.Errorf("prepare SQLite export metadata: %w", err)
	}
	defer func() { _ = metadataStatement.Close() }()
	for _, entry := range metadataEntries(document.Metadata) {
		if _, err = metadataStatement.ExecContext(ctx, entry.Key, entry.Value); err != nil {
			return fmt.Errorf("write SQLite export metadata: %w", err)
		}
	}
	rowsJSON, err := json.Marshal(sqliteJSONRows(document.Rows))
	if err != nil {
		return fmt.Errorf("encode SQLite export rows: %w", err)
	}
	if _, err = transaction.ExecContext(ctx, insertExportRowsSQL, string(rowsJSON)); err != nil {
		return fmt.Errorf("write SQLite export rows: %w", err)
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

func sqliteJSONRows(rows []app.ExportRow) []sqliteJSONRow {
	encoded := make([]sqliteJSONRow, len(rows))
	for index, row := range rows {
		encoded[index] = sqliteJSONRow{
			TransactionID: row.TransactionID, Provider: row.Provider,
			ProviderTransactionID: row.ProviderTransactionID, Date: row.Date.String(),
			Amount: row.Amount, AmountMinor: row.AmountMinor, Currency: row.Currency,
			Scale: int(row.Scale), AccountID: row.AccountID, Account: row.Account,
			MerchantID: row.MerchantID, Merchant: row.Merchant, CategoryID: row.CategoryID,
			Category: row.Category, GroupID: row.GroupID, Group: row.Group, Notes: row.Notes,
			Hidden: row.Hidden, TransactionMetadataJSON: row.TransactionMetadataJSON,
		}
	}
	return encoded
}
