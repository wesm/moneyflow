package app

import (
	"errors"
	"sort"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// ResolveTargets fixes one selection-or-focus intent to ordered stable local identities.
func ResolveTargets(
	snapshot EffectiveSnapshot,
	request MutationRequest,
) (ResolvedTargets, error) {
	if request.ExpectedRevision != snapshot.Revision {
		failure := mutationError(
			MutationRevisionConflict,
			errors.New("mutation expectation differs from effective snapshot"),
		)
		failure.CurrentRevision = snapshot.Revision
		return ResolvedTargets{}, failure
	}
	if err := request.State.Validate(); err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
	}
	selectionValue := request.Selection
	if selectionValue == "" {
		selectionValue = EmptySelection()
	}
	document, err := decodeSelection(selectionValue)
	if err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidTarget, err)
	}
	selection, explicitErr := resolveExplicitSelection(request.State.Current, document)
	if explicitErr == nil && len(selection.IDs) > 0 && selection.Kind == IdentityTransaction {
		resolved, resolveErr := resolveExactTransactionSelection(snapshot.Effective, selection)
		return selectionTargets(document, selectionValue, snapshot.Revision, resolved, resolveErr)
	}
	if err := snapshot.Effective.Validate(); err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
	}
	// Table scopes follow committed rows even when pending edits change their fields.
	transactions, err := snapshot.Committed.MaterializeTransactions()
	if err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
	}
	service, err := NewService(transactions)
	if err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
	}
	service.now = request.now
	source := targetSource{
		profile: snapshot.Committed, transactions: transactions, service: service,
		currencies: service.currenciesFor(&request.State.Current, document.State),
	}
	selection, err = service.resolveSelectionDocument(
		request.State.Current, document, source.currencies,
	)
	if err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidTarget, err)
	}

	if len(selection.IDs) > 0 {
		resolved, resolveErr := resolveSelectionTargets(source, request.State.Current, selection)
		if resolveErr == nil {
			resolveErr = validateEffectiveTargets(snapshot.Effective, resolved)
		}
		return selectionTargets(document, selectionValue, snapshot.Revision, resolved, resolveErr)
	}
	resolved, err := resolveFocusedTarget(source, request.State.Current, request.Target)
	if err != nil {
		return ResolvedTargets{}, err
	}
	if err = validateEffectiveTargets(snapshot.Effective, resolved); err != nil {
		return ResolvedTargets{}, err
	}
	return resolved, nil
}

// selectionTargets returns the targets of a selection defined at revision. A selection
// defined at another revision fails as stale; the failure carries the selection rebound to
// revision when its targets still resolve, so the client can confirm it again.
func selectionTargets(
	document selectionDocument,
	value SelectionValue,
	revision uint64,
	resolved ResolvedTargets,
	resolveErr error,
) (ResolvedTargets, error) {
	if document.Revision == nil || *document.Revision != revision {
		refreshed := EmptySelection()
		if resolveErr == nil {
			var err error
			refreshed, err = BindSelectionRevision(value, revision)
			if err != nil {
				return ResolvedTargets{}, mutationError(MutationInvalidTarget, err)
			}
		}
		return ResolvedTargets{}, staleSelectionError(
			revision, refreshed, errors.New("selection was defined at another profile revision"),
		)
	}
	if resolveErr != nil {
		return ResolvedTargets{}, resolveErr
	}
	resolved.FromSelection = true
	return resolved, nil
}

func validateEffectiveTargets(profile domain.CommittedProfile, targets ResolvedTargets) error {
	available := make(map[domain.EntityID]struct{}, len(profile.Transactions))
	for _, transaction := range profile.Transactions {
		available[transaction.ID] = struct{}{}
	}
	for _, id := range targets.TransactionIDs {
		if _, ok := available[id]; !ok {
			return mutationError(MutationInvalidTarget, errors.New("transaction target has a pending deletion"))
		}
	}
	return nil
}

func resolveExplicitSelection(
	state AnalyticalState,
	document selectionDocument,
) (SelectionSnapshot, error) {
	if document.Base != selectionBaseExplicit {
		return SelectionSnapshot{}, errors.New("selection is not explicit")
	}
	kind := identityKindForState(state)
	if document.Kind != kind && !document.isUniversalEmpty() {
		return SelectionSnapshot{}, invalidSelection(errors.New("selection kind does not match result"))
	}
	ids := identitySliceSet(document.IDs)
	for _, identity := range document.Exclude {
		delete(ids, identity)
	}
	for _, identity := range document.Include {
		ids[identity] = struct{}{}
	}
	return SelectionSnapshot{Kind: kind, IDs: ids}, nil
}

func resolveExactTransactionSelection(
	profile domain.CommittedProfile,
	selection SelectionSnapshot,
) (ResolvedTargets, error) {
	identities := sortedIdentitySet(selection.IDs)
	available := make(map[string]struct{}, len(profile.Transactions))
	for _, transaction := range profile.Transactions {
		available[string(transaction.ID)] = struct{}{}
	}
	resolved := ResolvedTargets{TransactionIDs: make([]domain.EntityID, len(identities))}
	for index, identity := range identities {
		if _, ok := available[identity]; !ok {
			return ResolvedTargets{}, mutationError(
				MutationInvalidTarget,
				errors.New("selected transaction does not exist"),
			)
		}
		resolved.TransactionIDs[index] = domain.EntityID(identity)
	}
	return resolved, nil
}

// targetSource is what edit targets resolve against: a committed profile, its transactions,
// a service over them, and the currencies the request computed from that service.
type targetSource struct {
	profile      domain.CommittedProfile
	transactions []domain.Transaction
	service      *Service
	currencies   []domain.CurrencyCount
}

func resolveSelectionTargets(
	source targetSource,
	state AnalyticalState,
	selection SelectionSnapshot,
) (ResolvedTargets, error) {
	identities := sortedIdentitySet(selection.IDs)
	if selection.Kind == IdentityTransaction {
		available := make(map[string]struct{}, len(source.profile.Transactions))
		for _, transaction := range source.profile.Transactions {
			available[string(transaction.ID)] = struct{}{}
		}
		resolved := ResolvedTargets{TransactionIDs: make([]domain.EntityID, len(identities))}
		for index, identity := range identities {
			if _, ok := available[identity]; !ok {
				return ResolvedTargets{}, mutationError(
					MutationInvalidTarget,
					errors.New("selected transaction does not exist"),
				)
			}
			resolved.TransactionIDs[index] = domain.EntityID(identity)
		}
		return resolved, nil
	}
	rows, err := aggregateRowsByIdentity(source, state)
	if err != nil {
		return ResolvedTargets{}, err
	}
	resolved := ResolvedTargets{}
	for _, identity := range identities {
		row, ok := rows[identity]
		if !ok {
			return ResolvedTargets{}, mutationError(
				MutationInvalidTarget,
				errors.New("selected aggregate does not resolve"),
			)
		}
		err = appendAggregateTargets(&resolved, source.profile, source.transactions, state, row)
		if err != nil {
			return ResolvedTargets{}, err
		}
	}
	if err = requireTransactionTargets(resolved); err != nil {
		return ResolvedTargets{}, err
	}
	canonicalizeResolvedTargets(&resolved)
	return resolved, nil
}

func resolveFocusedTarget(
	source targetSource,
	state AnalyticalState,
	target *RowTarget,
) (ResolvedTargets, error) {
	if target == nil || target.Identity == "" || target.Kind != identityKindForState(state) {
		return ResolvedTargets{}, mutationError(
			MutationInvalidTarget,
			errors.New("focused row target is missing or has the wrong kind"),
		)
	}
	if target.Kind == IdentityTransaction {
		result, err := source.service.Query(sessionFromAnalyticalState(state))
		if err != nil {
			return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
		}
		for _, row := range result.DetailRows {
			if row.Transaction.ID == target.Identity {
				return ResolvedTargets{
					TransactionIDs: []domain.EntityID{domain.EntityID(target.Identity)},
				}, nil
			}
		}
		return ResolvedTargets{}, mutationError(
			MutationInvalidTarget,
			errors.New("focused transaction does not resolve in the submitted view"),
		)
	}
	rows, err := aggregateRowsByIdentity(source, state)
	if err != nil {
		return ResolvedTargets{}, err
	}
	row, ok := rows[target.Identity]
	if !ok {
		return ResolvedTargets{}, mutationError(
			MutationInvalidTarget,
			errors.New("focused aggregate does not resolve in the submitted view"),
		)
	}
	resolved := ResolvedTargets{}
	err = appendAggregateTargets(&resolved, source.profile, source.transactions, state, row)
	if err != nil {
		return ResolvedTargets{}, err
	}
	if err = requireTransactionTargets(resolved); err != nil {
		return ResolvedTargets{}, err
	}
	canonicalizeResolvedTargets(&resolved)
	return resolved, nil
}

func aggregateRowsByIdentity(
	source targetSource,
	state AnalyticalState,
) (map[string]domain.AggregateRow, error) {
	result, err := source.service.webQuery(state, source.currencies)
	if err != nil {
		return nil, mutationError(MutationInvalidOperation, err)
	}
	if result.AggregateRows == nil {
		return nil, mutationError(
			MutationInvalidTarget,
			errors.New("submitted view does not contain aggregate rows"),
		)
	}
	rows := make(map[string]domain.AggregateRow, len(result.AggregateRows))
	for _, row := range result.AggregateRows {
		rows[AggregateIdentity(row)] = row
	}
	return rows, nil
}

// appendAggregateTargets adds a row's transactions and entity. A row without transactions,
// such as an empty Time period, adds nothing.
func appendAggregateTargets(
	resolved *ResolvedTargets,
	profile domain.CommittedProfile,
	transactions []domain.Transaction,
	state AnalyticalState,
	row domain.AggregateRow,
) error {
	filtered, err := aggregateTransactions(transactions, state, row)
	if err != nil || len(filtered) == 0 {
		return err
	}
	if row.Dimension != domain.DimensionTime {
		if !activeDimensionEntity(profile, row.Dimension, domain.EntityID(row.Key)) {
			return mutationError(
				MutationInvalidTarget,
				errors.New("aggregate entity is retired or missing"),
			)
		}
		resolved.EntityIDs = append(resolved.EntityIDs, domain.EntityID(row.Key))
	}
	for _, transaction := range filtered {
		resolved.TransactionIDs = append(
			resolved.TransactionIDs,
			domain.EntityID(transaction.ID),
		)
	}
	return nil
}

// requireTransactionTargets rejects aggregate targets whose rows have no transactions.
func requireTransactionTargets(resolved ResolvedTargets) error {
	if len(resolved.TransactionIDs) == 0 {
		return mutationError(
			MutationInvalidTarget,
			errors.New("aggregate has no current transaction targets"),
		)
	}
	return nil
}

func aggregateTransactions(
	transactions []domain.Transaction, state AnalyticalState, row domain.AggregateRow,
) ([]domain.Transaction, error) {
	query := analyticalQuerySpec(state)
	period := row.Period
	if period != nil {
		periodCopy := *period
		period = &periodCopy
	}
	query.Drilldowns = append(query.Drilldowns, domain.Drilldown{
		Dimension: row.Dimension,
		Currency:  row.Total.Currency,
		Scale:     row.Total.Scale,
		Key:       row.Key,
		Period:    period,
	})
	filtered, err := analytics.Filter(transactions, query)
	if err != nil {
		return nil, mutationError(MutationInvalidOperation, err)
	}
	return filtered, nil
}

func activeDimensionEntity(
	profile domain.CommittedProfile,
	dimension domain.Dimension,
	id domain.EntityID,
) bool {
	switch dimension {
	case domain.DimensionMerchant:
		for _, value := range profile.Merchants {
			if value.ID == id {
				return !value.Retired
			}
		}
	case domain.DimensionCategory:
		for _, value := range profile.Categories {
			if value.ID == id {
				return !value.Retired
			}
		}
	case domain.DimensionGroup:
		for _, value := range profile.Groups {
			if value.ID == id {
				return !value.Retired
			}
		}
	case domain.DimensionAccount:
		for _, value := range profile.Accounts {
			if value.ID == id {
				return !value.Retired
			}
		}
	default:
		return false
	}
	return false
}

func canonicalizeResolvedTargets(targets *ResolvedTargets) {
	targets.TransactionIDs = sortedUniqueEntityIDs(targets.TransactionIDs)
	targets.EntityIDs = sortedUniqueEntityIDs(targets.EntityIDs)
}

func sortedUniqueEntityIDs(values []domain.EntityID) []domain.EntityID {
	sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
