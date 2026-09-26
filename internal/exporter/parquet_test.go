package exporter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteParquetRoundTripsTypedRowsAndMetadata(t *testing.T) {
	var output bytes.Buffer
	document := testDocument(t)
	require.NoError(t, WriteParquet(&output, document))

	file, err := parquet.OpenFile(bytes.NewReader(output.Bytes()), int64(output.Len()))
	require.NoError(t, err)
	assert.Equal(t, int64(2), file.NumRows())
	fields := file.Schema().Fields()
	require.Len(t, fields, len(exportColumnNames))
	for index, field := range fields {
		assert.Equal(t, exportColumnNames[index], field.Name())
	}
	assert.Equal(t, parquet.Int64, fields[5].Type().Kind())
	assert.Equal(t, parquet.ByteArray, fields[4].Type().Kind())
	require.NotNil(t, fields[3].Type().LogicalType())
	assert.NotNil(t, fields[3].Type().LogicalType().Date)
	for _, rowGroup := range file.Metadata().RowGroups {
		for _, column := range rowGroup.Columns {
			assert.Equal(t, format.Snappy, column.MetaData.Codec)
		}
	}
	for _, entry := range metadataEntries(document.Metadata) {
		value, ok := file.Lookup(entry.Key)
		assert.True(t, ok, entry.Key)
		assert.Equal(t, entry.Value, value, entry.Key)
	}

	reader := parquet.NewGenericReader[parquetRow](bytes.NewReader(output.Bytes()))
	defer func() { require.NoError(t, reader.Close()) }()
	rows := make([]parquetRow, 2)
	count, err := reader.Read(rows)
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, 2, count)
	assert.Equal(t, "txn-a", rows[0].TransactionID)
	assert.Equal(t, "-12.34", rows[0].Amount)
	assert.Equal(t, int64(-1234), rows[0].AmountMinor)
	assert.True(t, rows[0].Hidden)
}

func TestWriteParquetIsPhysicallyDeterministic(t *testing.T) {
	document := testDocument(t)
	var first, second bytes.Buffer
	require.NoError(t, WriteParquet(&first, document))
	require.NoError(t, WriteParquet(&second, document))
	assert.Equal(t, first.Bytes(), second.Bytes())
	digest := sha256.Sum256(first.Bytes())
	assert.Equal(t, "06dc4a59900139dc6c3ee3d36bbf0b280ba15ae1e492e0c81cef8e3b51363f7b", hex.EncodeToString(digest[:]))
}

func TestWriteFilePublishesParquet(t *testing.T) {
	result, err := WriteFile(t.Context(), testRequest(t, t.TempDir(), FormatParquet))
	require.NoError(t, err)
	assert.Equal(t, ".parquet", filepath.Ext(result.Path))
	assert.Positive(t, result.Size)
}
