package app

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/wesm/moneyflow/internal/domain"
)

// MerchantEditPreview describes the exact edit scope before a destination is chosen.
type MerchantEditPreview struct {
	Revision             uint64
	AffectedTransactions int
	Transactions         []domain.Transaction
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
	return MerchantEditPreview{
		Revision: snapshot.Revision, AffectedTransactions: len(ids),
		Transactions: preview,
	}, nil
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
