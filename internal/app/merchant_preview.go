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
	// SourceMerchantID is the merchant every affected transaction has now, with pending edits
	// applied. Whole-merchant scope always names it. Chosen transactions name it only when
	// they share one merchant.
	SourceMerchantID domain.EntityID
	// OutsideView counts affected transactions whose committed values the opening view hides.
	OutsideView int
	// OutsideCurrencies lists, sorted by currency then scale and unique, the money partitions of
	// those transactions.
	OutsideCurrencies []domain.CurrencyScope
}

// PreviewMerchantEdit resolves the same targets as an edit without appending an operation.
// It holds the interaction lock throughout, so the counts and the revision describe one state.
func (service *Service) PreviewMerchantEdit(ctx context.Context, request MutationRequest) (MerchantEditPreview, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	request.now = service.clock()
	snapshot, err := service.readSnapshotLocked(ctx)
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
	ids, sourceID, err := previewScope(snapshot, request, targets)
	if err != nil {
		return MerchantEditPreview{}, err
	}
	transactions, err := snapshot.Effective.MaterializeTransactions()
	if err != nil {
		return MerchantEditPreview{}, mapAppError(err, snapshot.Revision)
	}
	slices.Sort(ids)
	preview, sharedMerchantID := previewSample(transactions, ids)
	result := MerchantEditPreview{
		Revision: snapshot.Revision, AffectedTransactions: len(ids),
		Transactions: preview, SourceMerchantID: cmp.Or(sourceID, sharedMerchantID),
	}
	if request.Input.Scope == EditScopeEntity {
		state := request.State.Current
		if err := service.countOutsideView(ctx, state, ids, transactions, &result); err != nil {
			return MerchantEditPreview{}, err
		}
	}
	return result, nil
}

// previewScope returns the IDs a merchant edit affects and, for whole-merchant scope, the
// source merchant, which chosen-transaction scope leaves empty.
func previewScope(
	snapshot EffectiveSnapshot, request MutationRequest, targets ResolvedTargets,
) ([]domain.EntityID, domain.EntityID, error) {
	switch request.Input.Scope {
	case EditScopeEntity:
		source, err := sourceMerchantForEntityEdit(snapshot, targets)
		if err != nil {
			return nil, "", newAppError(AppInvalidTarget, snapshot.Revision, err)
		}
		ids := affectedByOperation(snapshot.Effective, domain.Operation{
			Type: domain.OperationMerchantLabel, Targets: []domain.EntityID{source.ID},
		})
		return ids, source.ID, nil
	case EditScopeTransactions:
		return targets.TransactionIDs, "", nil
	default:
		return nil, "", newAppError(
			AppInvalidOperation, snapshot.Revision, errors.New("merchant edit scope is invalid"),
		)
	}
}

// merchantTally finds the one merchant a run of transactions shares.
type merchantTally struct {
	id    domain.EntityID
	mixed bool
}

func (tally *merchantTally) add(id domain.EntityID) {
	if tally.id == "" {
		tally.id = id
	}
	tally.mixed = tally.mixed || id != tally.id
}

func (tally *merchantTally) shared() domain.EntityID {
	if tally.mixed {
		return ""
	}
	return tally.id
}

func entityIDSet(ids []domain.EntityID) map[domain.EntityID]struct{} {
	set := make(map[domain.EntityID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// previewSample returns the first three affected transactions by ID and the merchant that all
// affected transactions share, empty when they differ. It reads the effective transactions in
// one pass, so the merchant covers every target and not only the sampled rows. ids must be sorted.
func previewSample(
	transactions []domain.Transaction, ids []domain.EntityID,
) ([]domain.Transaction, domain.EntityID) {
	sampled := entityIDSet(ids[:min(3, len(ids))])
	affected := entityIDSet(ids)
	var sample []domain.Transaction
	var tally merchantTally
	for _, transaction := range transactions {
		id := domain.EntityID(transaction.ID)
		if _, isAffected := affected[id]; !isAffected {
			continue
		}
		if _, isSampled := sampled[id]; isSampled {
			sample = append(sample, transaction.Clone())
		}
		tally.add(domain.EntityID(transaction.Merchant.ID))
	}
	slices.SortFunc(sample, func(a, b domain.Transaction) int { return cmp.Compare(a.ID, b.ID) })
	return sample, tally.shared()
}

// countOutsideView records which affected transactions the opening view hides, judged by
// committed values as the table shows them. Currencies come from the effective transactions.
func (service *Service) countOutsideView(
	ctx context.Context, state AnalyticalState, ids []domain.EntityID,
	effective []domain.Transaction, preview *MerchantEditPreview,
) error {
	inView, err := service.committedViewMembers(ctx, preview.Revision, state)
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
		preview.OutsideCurrencies = append(preview.OutsideCurrencies, domain.CurrencyScope{
			Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale,
		})
	}
	slices.SortFunc(preview.OutsideCurrencies, func(a, b domain.CurrencyScope) int {
		return cmp.Or(cmp.Compare(a.Currency, b.Currency), cmp.Compare(a.Scale, b.Scale))
	})
	preview.OutsideCurrencies = slices.Compact(preview.OutsideCurrencies)
	return nil
}

// committedViewMembers returns the IDs of committed transactions that pass the view's filters.
// For Amazon profiles a search also matches product names, as it does in the table.
func (service *Service) committedViewMembers(
	ctx context.Context, revision uint64, state AnalyticalState,
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
		return nil, newAppError(AppInvalidOperation, revision, err)
	}
	inView := make(map[string]struct{}, len(members))
	for _, transaction := range members {
		inView[transaction.ID] = struct{}{}
	}
	return inView, nil
}

func (service *Service) readSnapshotLocked(ctx context.Context) (EffectiveSnapshot, error) {
	if _, err := service.refreshLocked(ctx); err != nil {
		return EffectiveSnapshot{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return EffectiveSnapshot{}, mapAppError(err, service.Revision())
	}
	return snapshot, nil
}
