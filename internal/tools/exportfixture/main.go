// Command exportfixture writes one fixed synthetic Parquet document for cross-language tests.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/exporter"
)

func main() {
	output := flag.String("output", "", "explicit output Parquet path")
	flag.Parse()
	if err := run(*output); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(output string) (resultErr error) {
	if output == "" || !filepath.IsAbs(output) {
		return fmt.Errorf("export fixture: output path must be absolute")
	}
	file, err := os.OpenFile( //nolint:gosec // explicit test-owned output is the command's contract.
		output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600,
	)
	if err != nil {
		return fmt.Errorf("export fixture: create output: %w", err)
	}
	failed := true
	defer func() {
		if closeErr := file.Close(); resultErr == nil && closeErr != nil {
			resultErr = fmt.Errorf("export fixture: close output: %w", closeErr)
		}
		if failed {
			_ = os.Remove(output)
		}
	}()
	document, err := fixtureDocument()
	if err != nil {
		return err
	}
	if err = exporter.WriteParquet(file, document); err != nil {
		return fmt.Errorf("export fixture: write: %w", err)
	}
	if err = file.Sync(); err != nil {
		return fmt.Errorf("export fixture: sync: %w", err)
	}
	failed = false
	return nil
}

func fixtureDocument() (app.ExportDocument, error) {
	date, err := domain.ParseDate("2026-08-19")
	if err != nil {
		return app.ExportDocument{}, fmt.Errorf("export fixture: date: %w", err)
	}
	exportedAt := time.Date(2026, 8, 19, 15, 30, 0, 123_000_000, time.UTC)
	return app.ExportDocument{
		Metadata: app.ExportMetadata{
			SchemaVersion: 2, AppVersion: "v2-test", ExportedAt: exportedAt,
			ProfileRevision: 42, JournalCursor: 2, ExcludedActiveOperations: 2,
			InactiveRedoOperations: 1, Scope: app.ExportScopeFull, TransactionCount: 1,
			EarliestDate: &date, LatestDate: &date, ProviderKinds: []string{"fixture"},
		},
		Rows: []app.ExportRow{{
			TransactionID: "txn-example", Provider: "fixture", ProviderTransactionID: "provider-example",
			Date: date, Amount: "-12.34", AmountMinor: -1234, Currency: "USD", Scale: 2,
			AccountID: "account-example", Account: "Example Account",
			MerchantID: "merchant-example", Merchant: "Example Merchant",
			CategoryID: "category-example", Category: "Example Category",
			GroupID: "group-example", Group: "Example Group", Notes: "Synthetic fixture",
			TransactionMetadataJSON: `{}`,
		}},
	}, nil
}
