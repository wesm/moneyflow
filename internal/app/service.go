// Package app coordinates renderer-neutral application state and analytics.
package app

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

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
	return result.Clone(), nil
}
