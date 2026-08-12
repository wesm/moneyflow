package domain

import (
	"errors"
	"fmt"
)

// EntityRef identifies a normalized account or merchant.
type EntityRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CategoryRef identifies a normalized category and its group.
type CategoryRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

// Transaction is a normalized provider transaction.
type Transaction struct {
	ID         string            `json:"id"`
	ProviderID string            `json:"provider_id"`
	Provider   string            `json:"provider"`
	Account    EntityRef         `json:"account"`
	Date       Date              `json:"date"`
	Merchant   EntityRef         `json:"merchant"`
	Category   CategoryRef       `json:"category"`
	Amount     Money             `json:"amount"`
	Notes      string            `json:"notes,omitempty"`
	Hidden     bool              `json:"hidden"`
	Pending    bool              `json:"pending"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// NewTransaction validates and defensively copies a transaction.
func NewTransaction(transaction Transaction) (Transaction, error) {
	required := []struct {
		name  string
		value string
	}{
		{"id", transaction.ID},
		{"provider_id", transaction.ProviderID},
		{"provider", transaction.Provider},
		{"account.id", transaction.Account.ID},
		{"account.name", transaction.Account.Name},
		{"merchant.id", transaction.Merchant.ID},
		{"merchant.name", transaction.Merchant.Name},
		{"category.id", transaction.Category.ID},
		{"category.name", transaction.Category.Name},
		{"category.group", transaction.Category.Group},
	}
	for _, field := range required {
		if field.value == "" {
			return Transaction{}, fmt.Errorf("new transaction: %s is empty", field.name)
		}
	}
	if transaction.Date.Year() == 0 {
		return Transaction{}, errors.New("new transaction: date is invalid")
	}
	if !validCurrency(transaction.Amount.Currency) {
		return Transaction{}, errors.New("new transaction: currency must be a three-letter uppercase code")
	}
	return transaction.Clone(), nil
}

// Clone returns a transaction whose mutable metadata is independent.
func (transaction Transaction) Clone() Transaction {
	transaction.Metadata = cloneStringMap(transaction.Metadata)
	return transaction
}

func validCurrency(currency Currency) bool {
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

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
