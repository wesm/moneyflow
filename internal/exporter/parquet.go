package exporter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/snappy"
	"github.com/wesm/moneyflow/internal/app"
)

const parquetRowsPerGroup int64 = 8192

type parquetRow struct {
	TransactionID           string `parquet:"transaction_id"`
	Provider                string `parquet:"provider"`
	ProviderTransactionID   string `parquet:"provider_transaction_id"`
	Date                    int32  `parquet:"date,date"`
	Amount                  string `parquet:"amount"`
	AmountMinor             int64  `parquet:"amount_minor"`
	Currency                string `parquet:"currency"`
	Scale                   int32  `parquet:"scale"`
	AccountID               string `parquet:"account_id"`
	Account                 string `parquet:"account"`
	MerchantID              string `parquet:"merchant_id"`
	Merchant                string `parquet:"merchant"`
	CategoryID              string `parquet:"category_id"`
	Category                string `parquet:"category"`
	GroupID                 string `parquet:"group_id"`
	Group                   string `parquet:"group"`
	Notes                   string `parquet:"notes"`
	Hidden                  bool   `parquet:"hidden"`
	TransactionMetadataJSON string `parquet:"transaction_metadata_json"`
}

// WriteParquet writes one detached export document without opening a profile.
// Callers remain responsible for private staging, syncing, and publication.
func WriteParquet(output io.Writer, document app.ExportDocument) (resultErr error) {
	return writeParquetContext(context.Background(), output, document)
}

func writeParquetContext(
	ctx context.Context,
	output io.Writer,
	document app.ExportDocument,
) (resultErr error) {
	writer := parquet.NewGenericWriter[parquetRow](
		contextWriter{ctx: ctx, writer: output},
		parquet.Compression(&snappy.Codec{}),
		parquet.MaxRowsPerRowGroup(parquetRowsPerGroup),
	)
	defer func() {
		if closeErr := writer.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close Parquet export: %w", closeErr))
		}
	}()
	for _, entry := range metadataEntries(document.Metadata) {
		writer.SetKeyValueMetadata(entry.Key, entry.Value)
	}
	rows := make([]parquetRow, len(document.Rows))
	for index, row := range document.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows[index], resultErr = makeParquetRow(row)
		if resultErr != nil {
			return resultErr
		}
	}
	if _, err := writer.Write(rows); err != nil {
		return fmt.Errorf("write Parquet rows: %w", err)
	}
	return nil
}

func makeParquetRow(row app.ExportRow) (parquetRow, error) {
	date := time.Date(row.Date.Year(), row.Date.Month(), row.Date.Day(), 0, 0, 0, 0, time.UTC)
	days := date.Unix() / int64((24*time.Hour)/time.Second)
	if days < math.MinInt32 || days > math.MaxInt32 {
		return parquetRow{}, errors.New("encode Parquet date: value is outside DATE range")
	}
	return parquetRow{
		TransactionID: row.TransactionID, Provider: row.Provider,
		ProviderTransactionID: row.ProviderTransactionID,
		Date:                  int32(days),
		Amount:                row.Amount, AmountMinor: row.AmountMinor, Currency: row.Currency,
		Scale: int32(row.Scale), AccountID: row.AccountID, Account: row.Account,
		MerchantID: row.MerchantID, Merchant: row.Merchant, CategoryID: row.CategoryID,
		Category: row.Category, GroupID: row.GroupID, Group: row.Group, Notes: row.Notes,
		Hidden: row.Hidden, TransactionMetadataJSON: row.TransactionMetadataJSON,
	}, nil
}
