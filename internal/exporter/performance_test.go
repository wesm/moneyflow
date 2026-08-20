package exporter

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/wesm/moneyflow/internal/app"
)

func BenchmarkWriteCSV100K(b *testing.B) {
	benchmarkWriter100K(b, func(document app.ExportDocument, path string) error {
		file, err := os.Create(path) //nolint:gosec // benchmark-owned temporary path.
		if err != nil {
			return err
		}
		if err = writeCSV(file, document); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
}

func BenchmarkWriteSQLite100K(b *testing.B) {
	benchmarkWriter100K(b, func(document app.ExportDocument, path string) error {
		file, err := os.OpenFile( //nolint:gosec // benchmark-owned temporary path.
			path, os.O_CREATE|os.O_EXCL, 0o600,
		)
		if err != nil {
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		return writeSQLite(context.Background(), path, document)
	})
}

func BenchmarkWriteParquet100K(b *testing.B) {
	benchmarkWriter100K(b, func(document app.ExportDocument, path string) error {
		file, err := os.Create(path) //nolint:gosec // benchmark-owned temporary path.
		if err != nil {
			return err
		}
		if err = WriteParquet(file, document); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	})
}

func benchmarkWriter100K(
	b *testing.B,
	write func(app.ExportDocument, string) error,
) {
	b.Helper()
	document := benchmarkDocument100K(b)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		path := filepath.Join(b.TempDir(), "export-"+strconv.Itoa(iteration))
		if err := write(document, path); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkDocument100K(b *testing.B) app.ExportDocument {
	b.Helper()
	document := testDocument(b)
	base := document.Rows[0]
	document.Rows = make([]app.ExportRow, 100_000)
	for index := range document.Rows {
		row := base
		row.TransactionID = "txn-" + strconv.Itoa(index)
		row.ProviderTransactionID = "provider-" + strconv.Itoa(index)
		document.Rows[index] = row
	}
	document.Metadata.TransactionCount = len(document.Rows)
	return document
}
