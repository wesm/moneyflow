package amazon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
)

const requiredHeader = "Order ID,Order Date,Product Name,Quantity,Total Owed,Order Status,Shipment Status,ASIN,Currency,Unit Price\n"

func TestParseNormalizesExactMoneyAndLogicalRecords(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "Retail.OrderHistory.1.csv", "\ufeff"+requiredHeader+
		`order-1,2026-08-19T14:15:16Z,"Example, Product",2,"1,234.50",Closed,Delivered,ASIN1,USD,617.25`+"\r\n")

	candidate, err := Parse(context.Background(), []SourceFile{file}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	require.NoError(t, err)
	require.Len(t, candidate.Rows, 1)
	row := candidate.Rows[0]
	assert.Equal(t, int64(-123450), row.AmountMinor)
	assert.Equal(t, int64(61725), *row.UnitPriceMinor)
	assert.Equal(t, int64(2), row.Quantity)
	assert.Equal(t, "2026-08-19", row.OrderDate.String())
	assert.Equal(t, 1, row.Record)
	assert.Equal(t, "Example, Product", row.ProductName)
	assert.Equal(t, []string{"order-1"}, candidate.ObservedOrderIDs)
}

func TestParseDefaultsBlankQuantityToOneAndMissingCurrencyToBinding(t *testing.T) {
	t.Parallel()
	header := "Order ID,Order Date,Product Name,Quantity,Total Owed,Order Status,Shipment Status\n"
	file := sourceCSV(t, "Retail.OrderHistory.old.csv", header+
		"order-2,2026-08-18,Example Product,,12.34,Closed,Delivered\n")

	candidate, err := Parse(context.Background(), []SourceFile{file}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	require.NoError(t, err)
	require.Len(t, candidate.Rows, 1)
	assert.Equal(t, int64(1), candidate.Rows[0].Quantity)
	assert.Equal(t, domain.Currency("USD"), candidate.Rows[0].Currency)
	assert.NotEmpty(t, candidate.Rows[0].ASINLessKey)
}

func TestParseCancelledGarbageStillObservesOrder(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "Retail.OrderHistory.cancelled.csv", requiredHeader+
		"order-cancelled,garbage,,,,Cancelled,,_ASINLESS_,EUR,garbage\n")

	candidate, err := Parse(context.Background(), []SourceFile{file}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	require.NoError(t, err)
	assert.Empty(t, candidate.Rows)
	assert.Equal(t, []string{"order-cancelled"}, candidate.ObservedOrderIDs)
	assert.Equal(t, 1, candidate.CancelledRecordCount)
}

func TestParseInvalidActiveRowReturnsActionableCoordinate(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "nested/Retail.OrderHistory.bad.csv", requiredHeader+
		"order-3,2026-08-18,Example Product,1,not-money,Closed,Delivered,ASIN3,USD,1.00\n")

	_, err := Parse(context.Background(), []SourceFile{file}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	require.Error(t, err)
	var parseError *Error
	require.ErrorAs(t, err, &parseError)
	assert.Equal(t, CodeInvalid, parseError.Code)
	assert.Equal(t, Coordinate{
		RelativeFilename: "nested/Retail.OrderHistory.bad.csv",
		Record:           1, Column: "Total Owed", Reason: "invalid_money",
	}, parseError.Coordinate)
}

func TestParseSkipsFullyBlankLogicalRecord(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "Retail.OrderHistory.blank.csv", requiredHeader+",,,,,,,,,\n"+
		"order-4,2026-08-18,Example Product,1,1.00,Closed,Delivered,ASIN4,USD,1.00\n")

	candidate, err := Parse(context.Background(), []SourceFile{file}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, candidate.BlankRecordCount)
	assert.Len(t, candidate.Rows, 1)
}

func TestParseRejectsInvalidCurrencyBindingBeforeReading(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "Retail.OrderHistory.currency.csv", requiredHeader)

	for _, currency := range []domain.Currency{"usd", "US1", "€"} {
		_, err := Parse(context.Background(), []SourceFile{file}, Settings{
			Currency: currency, Scale: 2,
		}, ProductionLimits, nil)
		assert.Error(t, err, currency)
		var parseError *Error
		require.ErrorAs(t, err, &parseError)
		assert.Equal(t, CodeInvalid, parseError.Code)
	}
}

func TestParseEnforcesAggregateFileBudgetForDirectSources(t *testing.T) {
	t.Parallel()
	first := sourceCSV(t, "Retail.OrderHistory.1.csv", requiredHeader)
	second := sourceCSV(t, "Retail.OrderHistory.2.csv", requiredHeader)
	limits := ProductionLimits
	limits.TotalBytes = int64(len(requiredHeader)*2 - 1)

	_, err := Parse(context.Background(), []SourceFile{first, second}, Settings{
		Currency: "USD", Scale: 2,
	}, limits, nil)
	var parseError *Error
	require.ErrorAs(t, err, &parseError)
	assert.Equal(t, CodeTooLarge, parseError.Code)
}

func TestParseRejectsOversizedDataColumnCount(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "Retail.OrderHistory.columns.csv", requiredHeader+
		"order-1,2026-08-19,Example Product,1,12.34,Closed,Delivered,ASIN1,USD,12.34,extra\n")
	limits := ProductionLimits
	limits.Columns = 10

	_, err := Parse(context.Background(), []SourceFile{file}, Settings{
		Currency: "USD", Scale: 2,
	}, limits, nil)
	var parseError *Error
	require.ErrorAs(t, err, &parseError)
	assert.Equal(t, CodeTooLarge, parseError.Code)
}

func TestParseReturnsContextCancellationFromCSVRead(t *testing.T) {
	t.Parallel()
	file := sourceCSV(t, "Retail.OrderHistory.cancel.csv", requiredHeader+
		"order-1,2026-08-19,Example Product,1,12.34,Closed,Delivered,ASIN1,USD,12.34\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Parse(ctx, []SourceFile{file}, Settings{Currency: "USD", Scale: 2}, ProductionLimits, nil)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestParseDeduplicatesExactOrderObservationAcrossFiles(t *testing.T) {
	row := "order-overlap,2026-08-19,Example Product,1,12.34,Closed,Delivered,ASIN1,USD,12.34\n"
	first := sourceCSV(t, "Retail.OrderHistory.1.csv", requiredHeader+row)
	second := sourceCSV(t, "Retail.OrderHistory.2.csv", requiredHeader+row)

	candidate, err := Parse(context.Background(), []SourceFile{second, first}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	require.NoError(t, err)
	require.Len(t, candidate.Rows, 1)
	assert.Equal(t, "Retail.OrderHistory.1.csv", candidate.Rows[0].RelativeFilename)
}

func TestParseRejectsConflictingOrderObservationAcrossFiles(t *testing.T) {
	first := sourceCSV(t, "Retail.OrderHistory.1.csv", requiredHeader+
		"order-overlap,2026-08-19,Example Product,1,12.34,Closed,Delivered,ASIN1,USD,12.34\n")
	second := sourceCSV(t, "Retail.OrderHistory.2.csv", requiredHeader+
		"order-overlap,2026-08-19,Example Product,1,13.34,Closed,Delivered,ASIN1,USD,13.34\n")

	_, err := Parse(context.Background(), []SourceFile{first, second}, Settings{
		Currency: "USD", Scale: 2,
	}, ProductionLimits, nil)
	var parseError *Error
	require.ErrorAs(t, err, &parseError)
	assert.Equal(t, Coordinate{
		RelativeFilename: "Retail.OrderHistory.2.csv", Record: 1,
		Reason: "overlapping_order_conflict",
	}, parseError.Coordinate)
}

func sourceCSV(t *testing.T, relativeName string, contents string) SourceFile {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.Base(relativeName))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return SourceFile{RelativeName: relativeName, Path: path}
}
