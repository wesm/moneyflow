// Package app coordinates renderer-neutral application state and analytics.
package app

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// AggregateIdentity returns a stable identity for one dimension and money partition.
func AggregateIdentity(row domain.AggregateRow) string {
	return fmt.Sprintf(
		"%s:%d:%s:%d:%s",
		row.Dimension,
		len(row.Key),
		row.Key,
		row.Total.Scale,
		row.Total.Currency,
	)
}

// Service owns the immutable normalized transaction set used by every interface.
type Service struct {
	transactions []domain.Transaction
}

// NewService validates and defensively copies the normalized transaction set.
func NewService(transactions []domain.Transaction) (*Service, error) {
	owned := make([]domain.Transaction, len(transactions))
	seen := make(map[string]struct{}, len(transactions))
	for index, transaction := range transactions {
		validated, err := domain.NewTransaction(transaction)
		if err != nil {
			return nil, fmt.Errorf("new service: transaction %d: %w", index, err)
		}
		if _, exists := seen[validated.ID]; exists {
			return nil, fmt.Errorf("new service: duplicate transaction ID %q", validated.ID)
		}
		seen[validated.ID] = struct{}{}
		owned[index] = validated
	}
	return &Service{transactions: owned}, nil
}

// Query evaluates the current session without exposing the service's owned data.
func (service *Service) Query(session Session) (domain.QueryResult, error) {
	result, err := analytics.Query(service.transactions, session.QuerySpec())
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("query service: %w", err)
	}
	selectedTransactions := make(map[string]bool, len(session.SelectedTransactionIDs))
	for id := range session.SelectedTransactionIDs {
		selectedTransactions[id] = true
	}
	result.DetailRows = analytics.DecorateDetailRows(result.DetailRows, selectedTransactions)
	for index := range result.DetailRows {
		// Pending edits do not exist in this read-only slice. Provider pending state remains
		// available on Transaction.Pending without borrowing the Python edit marker.
		result.DetailRows[index].Flags.Pending = false
	}
	for index := range result.AggregateRows {
		identity := AggregateIdentity(result.AggregateRows[index])
		_, result.AggregateRows[index].Flags.Selected = session.SelectedAggregateKeys[identity]
	}
	return result.Clone(), nil
}
