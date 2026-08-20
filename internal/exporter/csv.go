package exporter

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"unicode"

	"github.com/wesm/moneyflow/internal/app"
)

var exportColumnNames = []string{
	"transaction_id", "provider", "provider_transaction_id", "date", "amount", "amount_minor",
	"currency", "scale", "account_id", "account", "merchant_id", "merchant", "category_id",
	"category", "group_id", "group", "notes", "hidden", "transaction_metadata_json",
}

func writeCSV(output io.Writer, document app.ExportDocument) error {
	return writeCSVContext(context.Background(), output, document)
}

func writeCSVContext(ctx context.Context, output io.Writer, document app.ExportDocument) error {
	output = contextWriter{ctx: ctx, writer: output}
	for _, entry := range metadataEntries(document.Metadata) {
		if _, err := fmt.Fprintf(output, "# %s: %s\n", entry.Key, sanitizeMetadataValue(entry.Value)); err != nil {
			return fmt.Errorf("write CSV metadata: %w", err)
		}
	}
	writer := csv.NewWriter(output)
	writer.UseCRLF = false
	if err := writer.Write(exportColumnNames); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, row := range document.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writer.Write(csvRecord(row)); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush CSV: %w", err)
	}
	return nil
}

func csvRecord(row app.ExportRow) []string {
	return []string{
		row.TransactionID, row.Provider, row.ProviderTransactionID, row.Date.String(), row.Amount,
		strconv.FormatInt(row.AmountMinor, 10), row.Currency, strconv.Itoa(int(row.Scale)), row.AccountID,
		guardFreeText(row.Account), row.MerchantID, guardFreeText(row.Merchant), row.CategoryID,
		guardFreeText(row.Category), row.GroupID, guardFreeText(row.Group), guardFreeText(row.Notes),
		strconv.FormatBool(row.Hidden), guardFreeText(row.TransactionMetadataJSON),
	}
}

func guardFreeText(value string) string {
	if needsFormulaGuard(value) {
		return "'" + value
	}
	return value
}

func needsFormulaGuard(value string) bool {
	for _, current := range value {
		switch current {
		case '=', '+', '-', '@', '\t', '\r':
			return true
		default:
			if !unicode.IsSpace(current) {
				return false
			}
		}
	}
	return false
}
