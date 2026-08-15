// Package app coordinates renderer-neutral application state and analytics.
package app

import (
	"fmt"
	"sync"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
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
	mu                    sync.RWMutex
	interactions          sync.Mutex
	transactions          []domain.Transaction
	committedTransactions []domain.Transaction
	localPending          map[string]struct{}
	profile               store.Profile
	snapshot              *EffectiveSnapshot
	providerRuntime       *providerRuntimeState
	providerBound         bool
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
	service.mu.RLock()
	transactions := append([]domain.Transaction(nil), service.transactions...)
	committed := append([]domain.Transaction(nil), service.committedTransactions...)
	localPending := make(map[string]struct{}, len(service.localPending))
	for id := range service.localPending {
		localPending[id] = struct{}{}
	}
	persistent := service.profile != nil
	service.mu.RUnlock()
	result, err := analytics.Query(transactions, session.QuerySpec())
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("query service: %w", err)
	}
	if persistent {
		decorateLocalPending(&result, committed, transactions, session.QuerySpec(), localPending)
	}
	selectedTransactions := make(map[string]bool, len(session.SelectedTransactionIDs))
	for id := range session.SelectedTransactionIDs {
		selectedTransactions[id] = true
	}
	result.DetailRows = analytics.DecorateDetailRows(result.DetailRows, selectedTransactions)
	for index := range result.DetailRows {
		// Pending edits do not exist in this read-only slice. Provider pending state remains
		// available on Transaction.Pending without borrowing the Python edit marker.
		if !persistent {
			result.DetailRows[index].Flags.Pending = false
		}
	}
	for index := range result.AggregateRows {
		identity := AggregateIdentity(result.AggregateRows[index])
		_, result.AggregateRows[index].Flags.Selected = session.SelectedAggregateKeys[identity]
	}
	return result.Clone(), nil
}

func decorateLocalPending(
	result *domain.QueryResult,
	committed []domain.Transaction,
	effective []domain.Transaction,
	spec domain.QuerySpec,
	localPending map[string]struct{},
) {
	for index := range result.DetailRows {
		_, result.DetailRows[index].Flags.Pending = localPending[result.DetailRows[index].Transaction.ID]
	}
	if result.AggregateRows == nil {
		return
	}
	pendingAggregates := make(map[string]struct{})
	collectPendingAggregates(pendingAggregates, committed, spec, localPending)
	collectPendingAggregates(pendingAggregates, effective, spec, localPending)
	for index := range result.AggregateRows {
		identity := AggregateIdentity(result.AggregateRows[index])
		_, result.AggregateRows[index].Flags.Pending = pendingAggregates[identity]
	}
}

func collectPendingAggregates(
	destination map[string]struct{},
	transactions []domain.Transaction,
	spec domain.QuerySpec,
	localPending map[string]struct{},
) {
	marked := append([]domain.Transaction(nil), transactions...)
	for index := range marked {
		_, marked[index].Pending = localPending[marked[index].ID]
	}
	projection, err := analytics.QueryWithPending(marked, spec)
	if err != nil {
		return
	}
	for _, row := range projection.AggregateRows {
		if row.Flags.Pending {
			destination[AggregateIdentity(row)] = struct{}{}
		}
	}
}
