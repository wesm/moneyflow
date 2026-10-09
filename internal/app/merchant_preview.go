package app

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// MerchantEditPreview describes the exact edit scope before a destination is chosen.
type MerchantEditPreview struct {
	Revision             uint64
	AffectedTransactions int
	Transactions         []domain.Transaction
	// OutsideView counts affected transactions whose committed values the opening view hides.
	OutsideView int
	// OutsideCurrencies lists, sorted and unique, the currencies of those transactions.
	OutsideCurrencies []domain.Currency
}

// PreviewMerchantEdit resolves the same targets as an edit without appending an operation.
func (service *Service) PreviewMerchantEdit(ctx context.Context, request MutationRequest) (MerchantEditPreview, error) {
	snapshot, err := service.readSnapshot(ctx)
	if err != nil {
		return MerchantEditPreview{}, err
	}
	if request.Action != ActionEditMerchant {
		return MerchantEditPreview{}, newAppError(AppInvalidOperation, snapshot.Revision, errors.New("merchant preview requires a merchant edit"))
	}
	targets, err := ResolveTargets(snapshot, request)
	if err != nil {
		return MerchantEditPreview{}, mapAppError(err, snapshot.Revision)
	}
	ids := targets.TransactionIDs
	switch request.Input.Scope {
	case EditScopeEntity:
		source, sourceErr := sourceMerchantForEntityEdit(snapshot, targets)
		if sourceErr != nil {
			return MerchantEditPreview{}, newAppError(AppInvalidTarget, snapshot.Revision, sourceErr)
		}
		ids = affectedByOperation(snapshot.Effective, domain.Operation{
			Type: domain.OperationMerchantLabel, Targets: []domain.EntityID{source.ID},
		})
	case EditScopeTransactions:
	default:
		return MerchantEditPreview{}, newAppError(AppInvalidOperation, snapshot.Revision, errors.New("merchant edit scope is invalid"))
	}
	transactions, err := snapshot.Effective.MaterializeTransactions()
	if err != nil {
		return MerchantEditPreview{}, mapAppError(err, snapshot.Revision)
	}
	slices.Sort(ids)
	preview := make([]domain.Transaction, 0, min(3, len(ids)))
	for _, transaction := range transactions {
		if slices.Contains(ids[:min(3, len(ids))], domain.EntityID(transaction.ID)) {
			preview = append(preview, transaction.Clone())
		}
	}
	slices.SortFunc(preview, func(a, b domain.Transaction) int { return cmp.Compare(a.ID, b.ID) })
	result := MerchantEditPreview{
		Revision: snapshot.Revision, AffectedTransactions: len(ids),
		Transactions: preview,
	}
	if request.Input.Scope == EditScopeEntity {
		state := request.State.Current
		if err := service.countOutsideView(ctx, state, ids, transactions, &result); err != nil {
			return MerchantEditPreview{}, err
		}
	}
	return result, nil
}

// countOutsideView records which affected transactions the opening view hides, judged by
// committed values as the table shows them. Currencies come from the effective transactions.
func (service *Service) countOutsideView(
	ctx context.Context, state AnalyticalState, ids []domain.EntityID,
	effective []domain.Transaction, preview *MerchantEditPreview,
) error {
	inView, err := service.committedViewMembers(ctx, state)
	if err != nil {
		return err
	}
	affected := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		affected[string(id)] = struct{}{}
	}
	for _, transaction := range effective {
		if _, isAffected := affected[transaction.ID]; !isAffected {
			continue
		}
		if _, visible := inView[transaction.ID]; visible {
			continue
		}
		preview.OutsideView++
		preview.OutsideCurrencies = append(preview.OutsideCurrencies, transaction.Amount.Currency)
	}
	slices.Sort(preview.OutsideCurrencies)
	preview.OutsideCurrencies = slices.Compact(preview.OutsideCurrencies)
	return nil
}

// committedViewMembers returns the IDs of committed transactions that pass the view's filters.
// For Amazon profiles a search also matches product names, as it does in the table.
func (service *Service) committedViewMembers(
	ctx context.Context, state AnalyticalState,
) (map[string]struct{}, error) {
	service.mu.RLock()
	transactions := append([]domain.Transaction(nil), service.browseTransactionsLocked()...)
	matcher := service.amazonMatcher
	rawLabels := service.rawProviderMerchantLabelsLocked()
	service.mu.RUnlock()
	spec := analyticalQuerySpec(state)
	var members []domain.Transaction
	var err error
	if matcher != nil && spec.Search != "" {
		members, err = amazonProductSearch(ctx, transactions, spec, matcher, rawLabels)
	} else {
		members, err = analytics.Filter(transactions, spec)
	}
	if err != nil {
		return nil, newAppError(AppInvalidOperation, service.Revision(), err)
	}
	inView := make(map[string]struct{}, len(members))
	for _, transaction := range members {
		inView[transaction.ID] = struct{}{}
	}
	return inView, nil
}

func (service *Service) readSnapshot(ctx context.Context) (EffectiveSnapshot, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return EffectiveSnapshot{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return EffectiveSnapshot{}, mapAppError(err, service.Revision())
	}
	return snapshot, nil
}
