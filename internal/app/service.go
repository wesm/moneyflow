// Package app coordinates renderer-neutral application state and analytics.
package app

import (
	"context"
	"fmt"
	"strings"
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
	providerState         store.ProviderState
	profileKind           string
	amazonSettings        *store.AmazonSettings
	amazonMatcher         *AmazonMatchingService
}

// ConfigureAmazonMatching installs the shared cross-profile matcher used by both renderers.
func (service *Service) ConfigureAmazonMatching(matcher *AmazonMatchingService) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.amazonMatcher = matcher
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
	return service.QueryContext(context.Background(), session)
}

// QueryContext evaluates the current session and enriches search with matched Amazon products.
func (service *Service) QueryContext(ctx context.Context, session Session) (domain.QueryResult, error) {
	service.mu.RLock()
	transactions := append([]domain.Transaction(nil), service.transactions...)
	committed := append([]domain.Transaction(nil), service.committedTransactions...)
	localPending := make(map[string]struct{}, len(service.localPending))
	for id := range service.localPending {
		localPending[id] = struct{}{}
	}
	persistent := service.profile != nil
	matcher := service.amazonMatcher
	rawLabels := service.rawProviderMerchantLabelsLocked()
	service.mu.RUnlock()
	spec := session.QuerySpec()
	pendingSpec := spec
	pendingCommitted := committed
	result, err := analytics.Query(transactions, spec)
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("query service: %w", err)
	}
	if matcher != nil && spec.Search != "" {
		transactions, err = amazonProductSearch(ctx, transactions, spec, matcher, rawLabels)
		if err != nil {
			return domain.QueryResult{}, fmt.Errorf("query Amazon products: %w", err)
		}
		withoutSearch := spec
		withoutSearch.Search = ""
		result, err = analytics.Query(transactions, withoutSearch)
		if err != nil {
			return domain.QueryResult{}, fmt.Errorf("query Amazon product results: %w", err)
		}
		pendingSpec.Search = ""
		pendingCommitted = transactionsWithIDs(committed, transactionIDs(transactions))
	}
	if persistent {
		decorateLocalPending(&result, pendingCommitted, transactions, pendingSpec, localPending)
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

func transactionIDs(transactions []domain.Transaction) map[string]struct{} {
	ids := make(map[string]struct{}, len(transactions))
	for _, transaction := range transactions {
		ids[transaction.ID] = struct{}{}
	}
	return ids
}

func transactionsWithIDs(
	transactions []domain.Transaction,
	ids map[string]struct{},
) []domain.Transaction {
	filtered := make([]domain.Transaction, 0, len(ids))
	for _, transaction := range transactions {
		if _, ok := ids[transaction.ID]; ok {
			filtered = append(filtered, transaction)
		}
	}
	return filtered
}

func amazonProductSearch(
	ctx context.Context,
	transactions []domain.Transaction,
	spec domain.QuerySpec,
	matcher *AmazonMatchingService,
	rawLabels map[string]string,
) ([]domain.Transaction, error) {
	normal, err := analytics.Filter(transactions, spec)
	if err != nil {
		return nil, err
	}
	included := make(map[string]struct{}, len(normal))
	for _, transaction := range normal {
		included[transaction.ID] = struct{}{}
	}
	baseSpec := spec
	baseSpec.Search = ""
	base, err := analytics.Filter(transactions, baseSpec)
	if err != nil {
		return nil, err
	}
	candidates := make([]domain.Transaction, 0, len(base))
	inputs := make([]AmazonMatchInput, 0, len(base))
	for _, transaction := range base {
		if _, ok := included[transaction.ID]; ok {
			continue
		}
		candidates = append(candidates, transaction)
		inputs = append(inputs, AmazonMatchInput{
			Transaction: transaction, RawProviderMerchantLabel: rawLabels[transaction.Merchant.ID],
		})
	}
	matched, err := matcher.MatchBatch(ctx, inputs, 20)
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(spec.Search)
	for index, projection := range matched {
		if amazonProjectionProductMatches(projection, query) {
			transaction := candidates[index]
			included[transaction.ID] = struct{}{}
		}
	}
	result := make([]domain.Transaction, 0, len(included))
	for _, transaction := range base {
		if _, ok := included[transaction.ID]; ok {
			result = append(result, transaction)
		}
	}
	return result, nil
}

func amazonProjectionProductMatches(projection AmazonMatchProjection, loweredQuery string) bool {
	if !projection.Qualified {
		return false
	}
	for _, match := range projection.Result.Matches {
		for _, item := range match.Items {
			if strings.Contains(strings.ToLower(item.ProductName), loweredQuery) {
				return true
			}
		}
	}
	return false
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
