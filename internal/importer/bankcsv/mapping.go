// Package bankcsv parses bounded bank exports into exact source facts.
package bankcsv

import (
	"errors"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// Mapping describes column names, not an executable import plugin.
type Mapping struct {
	Name, Pattern, DefaultAccount                     string
	Date, DateLayout, Merchant, Amount, Credit, Debit string
	Category, Notes                                   string
	Metadata                                          []string
	Currency                                          domain.Currency
	Scale                                             uint8
}

// Lookup returns a built-in institution mapping.
func Lookup(name string) (Mapping, error) {
	if name != "chase_credit" {
		return Mapping{}, errors.New("unknown institution mapping")
	}
	return Mapping{Name: name, Pattern: "Chase*.csv", DefaultAccount: "Chase Credit Card",
		Date: "Transaction Date", DateLayout: "01/02/2006", Merchant: "Description", Amount: "Amount",
		Category: "Category", Notes: "Memo", Metadata: []string{"Post Date", "Type"}, Currency: "USD", Scale: 2}, nil
}

// Validate rejects ambiguous or incomplete column mappings.
func (m Mapping) Validate() error {
	if m.Name == "" || m.Pattern == "" || m.Date == "" || m.DateLayout == "" || m.Merchant == "" || m.Currency == "" || m.Scale > 9 {
		return errors.New("invalid mapping identity, date, or currency")
	}
	if (m.Amount != "" && (m.Credit != "" || m.Debit != "")) || (m.Amount == "" && (m.Credit == "" || m.Debit == "")) {
		return errors.New("mapping requires either amount or credit and debit columns")
	}
	seen := map[string]bool{}
	for _, column := range append([]string{m.Date, m.Merchant, m.Amount, m.Credit, m.Debit, m.Category, m.Notes}, m.Metadata...) {
		if column == "" {
			continue
		}
		if strings.TrimSpace(column) != column || seen[column] {
			return errors.New("invalid or repeated mapping column")
		}
		seen[column] = true
	}
	return nil
}

// Limits bounds discovery and parsing for one import run.
type Limits struct {
	Files, Records, Columns                                 int
	BytesPerFile, TotalBytes, BytesPerRecord, BytesPerField int64
}

// ProductionLimits defines the fixed application import budgets.
var ProductionLimits = Limits{Files: 256, Records: 1_000_000, Columns: 128,
	BytesPerFile: 64 << 20, TotalBytes: 512 << 20, BytesPerRecord: 1 << 20, BytesPerField: 16 << 10}

func (l Limits) validate() error {
	if l.Files < 1 || l.Records < 1 || l.Columns < 1 || l.BytesPerFile < 1 || l.TotalBytes < 1 || l.BytesPerRecord < 1 || l.BytesPerField < 1 {
		return errors.New("invalid CSV limits")
	}
	return nil
}
