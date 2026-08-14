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
	if err := snapshot.Effective.Validate(); err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
	}
	transactions, err := snapshot.Effective.MaterializeTransactions()
	if err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidOperation, err)
	}
	service, err := NewService(transactions)
	if err != nil {
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
	selection, err := service.resolveSelectionPayload(request.State.Current, document.payload())
	if err != nil {
		return ResolvedTargets{}, mutationError(MutationInvalidTarget, err)
	}

	if len(selection.IDs) > 0 {
		resolved, resolveErr := resolveSelectionTargets(
			snapshot.Effective,
			transactions,
			service,
			request.State.Current,
			selection,
		)
		if document.Revision == nil || *document.Revision != snapshot.Revision {
			refreshed := EmptySelection()
			if resolveErr == nil {
				refreshed, err = BindSelectionRevision(selectionValue, snapshot.Revision)
				if err != nil {
					return ResolvedTargets{}, mutationError(MutationInvalidTarget, err)
				}
			}
			return ResolvedTargets{}, staleSelectionError(
				snapshot.Revision,
				refreshed,
				errors.New("selection was defined at another profile revision"),
			)
		}
		if resolveErr != nil {
			return ResolvedTargets{}, resolveErr
		}
		resolved.FromSelection = true
		return resolved, nil
	}
	return resolveFocusedTarget(
		snapshot.Effective,
		transactions,
		service,
		request.State.Current,
		request.Target,
	)
}

func resolveSelectionTargets(
	profile domain.CommittedProfile,
	transactions []domain.Transaction,
	service *Service,
	state AnalyticalState,
	selection SelectionSnapshot,
) (ResolvedTargets, error) {
	identities := sortedIdentitySet(selection.IDs)
	if selection.Kind == IdentityTransaction {
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
	rows, err := aggregateRowsByIdentity(service, state)
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
		if err = appendAggregateTargets(&resolved, profile, transactions, state, row); err != nil {
			return ResolvedTargets{}, err
		}
	}
	canonicalizeResolvedTargets(&resolved)
	return resolved, nil
}

func resolveFocusedTarget(
	profile domain.CommittedProfile,
	transactions []domain.Transaction,
	service *Service,
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
		result, err := service.Query(sessionFromAnalyticalState(state))
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
	rows, err := aggregateRowsByIdentity(service, state)
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
	if err = appendAggregateTargets(&resolved, profile, transactions, state, row); err != nil {
		return ResolvedTargets{}, err
	}
	canonicalizeResolvedTargets(&resolved)
	return resolved, nil
}

func aggregateRowsByIdentity(
	service *Service,
	state AnalyticalState,
) (map[string]domain.AggregateRow, error) {
	result, err := service.Query(sessionFromAnalyticalState(state))
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

func appendAggregateTargets(
	resolved *ResolvedTargets,
	profile domain.CommittedProfile,
	transactions []domain.Transaction,
	state AnalyticalState,
	row domain.AggregateRow,
) error {
	if row.Dimension != domain.DimensionTime {
		if !activeDimensionEntity(profile, row.Dimension, domain.EntityID(row.Key)) {
			return mutationError(
				MutationInvalidTarget,
				errors.New("aggregate entity is retired or missing"),
			)
		}
		resolved.EntityIDs = append(resolved.EntityIDs, domain.EntityID(row.Key))
	}
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
		return mutationError(MutationInvalidOperation, err)
	}
	if len(filtered) == 0 {
		return mutationError(
			MutationInvalidTarget,
			errors.New("aggregate has no current transaction targets"),
		)
	}
	for _, transaction := range filtered {
		resolved.TransactionIDs = append(
			resolved.TransactionIDs,
			domain.EntityID(transaction.ID),
		)
	}
	return nil
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
