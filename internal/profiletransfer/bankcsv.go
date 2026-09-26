package profiletransfer

import (
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/store"
)

type csvSettings struct {
	Mapping   string          `json:"mapping"`
	Currency  domain.Currency `json:"currency"`
	Scale     uint8           `json:"scale"`
	CreatedAt time.Time       `json:"created_at"`
}

type csvFile struct {
	Key         string    `json:"file_key"`
	AccountKey  string    `json:"account_key"`
	Digest      string    `json:"digest"`
	CompletedAt time.Time `json:"completed_at"`
	Imported    int       `json:"imported"`
	Duplicates  int       `json:"duplicates"`
	Skipped     int       `json:"skipped"`
}

type csvFileRow struct {
	FileKey   string `json:"file_key"`
	SourceKey string `json:"source_key"`
}

type csvRow struct {
	SourceKey          string            `json:"source_key"`
	AccountKey         string            `json:"account_key"`
	Date               domain.Date       `json:"date"`
	Money              money             `json:"money"`
	Merchant           string            `json:"merchant"`
	Category           string            `json:"category"`
	Notes              string            `json:"notes"`
	Metadata           map[string]string `json:"metadata"`
	Occurrence         int               `json:"occurrence"`
	LocalTransactionID domain.EntityID   `json:"local_transaction_id"`
	MerchantOverride   bool              `json:"merchant_override"`
	CategoryOverride   bool              `json:"category_override"`
	Disposition        string            `json:"disposition"`
}

func csvRowFrom(row store.CSVRow) csvRow {
	return csvRow{row.SourceKey, row.AccountKey, row.Date, moneyFrom(row.Amount), row.Merchant, row.Category, row.Notes, row.Metadata, row.Occurrence, row.LocalTransactionID, row.MerchantOverride, row.CategoryOverride, row.Disposition}
}

func (row csvRow) record() (store.CSVRow, error) {
	amount, err := row.Money.parse()
	return store.CSVRow{Row: bankcsv.Row{SourceKey: row.SourceKey, AccountKey: row.AccountKey, Date: row.Date, Amount: amount, Merchant: row.Merchant, Category: row.Category, Notes: row.Notes, Metadata: row.Metadata, Occurrence: row.Occurrence}, LocalTransactionID: row.LocalTransactionID, MerchantOverride: row.MerchantOverride, CategoryOverride: row.CategoryOverride, Disposition: row.Disposition}, err
}

func writeCSV(state store.CSVState, write func(string, any) error) error {
	if state.Settings != nil {
		if err := write("csv_settings", csvSettings(*state.Settings)); err != nil {
			return err
		}
	}
	if err := writeValues(state.Files, "csv_file", func(v store.CSVFile) string { return v.Key }, func(v store.CSVFile) any { return csvFile(v) }, write); err != nil {
		return err
	}
	if err := writeValues(state.Rows, "csv_row", func(v store.CSVRow) string { return v.SourceKey }, func(v store.CSVRow) any { return csvRowFrom(v) }, write); err != nil {
		return err
	}
	return writeValues(state.Membership, "csv_file_row", func(v store.CSVFileRow) string { return v.FileKey + "\x00" + v.SourceKey }, func(v store.CSVFileRow) any { return csvFileRow(v) }, write)
}
