package amazon

import (
	"context"
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

func sourceCSV(t *testing.T, relativeName string, contents string) SourceFile {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.Base(relativeName))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	return SourceFile{RelativeName: relativeName, Path: path}
}
