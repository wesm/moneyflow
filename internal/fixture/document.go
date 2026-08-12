// Package fixture loads deterministic synthetic transaction data.
package fixture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wesm/moneyflow/internal/domain"
)

const supportedSchemaVersion = 1

type document struct {
	SchemaVersion int                   `json:"schema_version"`
	Currencies    []currencyDocument    `json:"currencies"`
	Transactions  []transactionDocument `json:"transactions"`
}

type currencyDocument struct {
	Code  domain.Currency `json:"code"`
	Scale uint8           `json:"scale"`
}

type transactionDocument struct {
	ID         string             `json:"id"`
	ProviderID string             `json:"provider_id"`
	Provider   string             `json:"provider"`
	Account    domain.EntityRef   `json:"account"`
	Date       string             `json:"date"`
	Merchant   domain.EntityRef   `json:"merchant"`
	Category   domain.CategoryRef `json:"category"`
	Amount     string             `json:"amount"`
	Currency   domain.Currency    `json:"currency"`
	Hidden     bool               `json:"hidden"`
	Pending    bool               `json:"pending"`
	Notes      string             `json:"notes,omitempty"`
	Metadata   map[string]string  `json:"metadata,omitempty"`
}

// Load validates a fixture document and returns normalized transactions.
func Load(path string) ([]domain.Transaction, error) {
	// The command or test chooses the fixture path; fixture data is never an executable resource.
	data, err := os.ReadFile(path) //nolint:gosec // loading an explicit caller-selected path is the API.
	if err != nil {
		return nil, fmt.Errorf("load fixture: read: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var input document
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("load fixture: decode: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("load fixture: trailing JSON value")
	}
	if input.SchemaVersion != supportedSchemaVersion {
		return nil, fmt.Errorf("load fixture: unsupported schema version %d", input.SchemaVersion)
	}

	scales := make(map[domain.Currency]uint8, len(input.Currencies))
	for index, currency := range input.Currencies {
		if _, exists := scales[currency.Code]; exists {
			return nil, fmt.Errorf("load fixture: currencies[%d]: duplicate code", index)
		}
		if !validCurrency(currency.Code) {
			return nil, fmt.Errorf("load fixture: currencies[%d].code: invalid", index)
		}
		scales[currency.Code] = currency.Scale
	}

	seen := make(map[string]struct{}, len(input.Transactions))
	transactions := make([]domain.Transaction, 0, len(input.Transactions))
	for index, raw := range input.Transactions {
		if _, exists := seen[raw.ID]; exists {
			return nil, fmt.Errorf("load fixture: transactions[%d].id: duplicate", index)
		}
		seen[raw.ID] = struct{}{}
		scale, exists := scales[raw.Currency]
		if !exists {
			return nil, fmt.Errorf("load fixture: transactions[%d].currency: undeclared", index)
		}
		date, err := domain.ParseDate(raw.Date)
		if err != nil {
			return nil, fmt.Errorf("load fixture: transactions[%d].date: %w", index, err)
		}
		amount, err := domain.ParseMoney(raw.Amount, raw.Currency, scale)
		if err != nil {
			return nil, fmt.Errorf("load fixture: transactions[%d].amount: %w", index, err)
		}
		transaction, err := domain.NewTransaction(domain.Transaction{
			ID: raw.ID, ProviderID: raw.ProviderID, Provider: raw.Provider,
			Account: raw.Account, Date: date, Merchant: raw.Merchant, Category: raw.Category,
			Amount: amount, Notes: raw.Notes, Hidden: raw.Hidden, Pending: raw.Pending,
			Metadata: raw.Metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("load fixture: transactions[%d]: %w", index, err)
		}
		transactions = append(transactions, transaction)
	}
	return transactions, nil
}

func validCurrency(currency domain.Currency) bool {
	if len(currency) != 3 {
		return false
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}
