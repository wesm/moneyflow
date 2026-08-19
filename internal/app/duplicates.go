package app

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// DuplicateWindowRequest independently bounds presented groups and their flattened rows.
type DuplicateWindowRequest struct {
	GroupOffset int
	GroupLimit  int
	RowOffset   int
	RowLimit    int
}

// DuplicateRow is one presentation-safe transaction in a likely-duplicate group.
type DuplicateRow struct {
	GroupNumber   int
	Target        RowTarget
	Transaction   domain.Transaction
	MatchingLabel string
	Flags         domain.RowFlags
}

// DuplicateGroupProjection is one stable presentation-numbered group window.
type DuplicateGroupProjection struct {
	Number int
	Rows   []DuplicateRow
}

// DuplicateProjection contains one bounded duplicate-review projection.
type DuplicateProjection struct {
	Revision          uint64
	State             ViewState
	Selection         SelectionValue
	SelectionCount    int
	TotalGroups       int
	TotalTransactions int
	GroupWindow       Window
	RowWindow         Window
	Groups            []DuplicateGroupProjection
	Status            string
}

type duplicateProjectedRow struct {
	groupNumber   int
	matchingLabel string
	transactionID string
}

// ProjectDuplicates finds duplicates across the complete filtered result before windowing output.
func (service *Service) ProjectDuplicates(
	ctx context.Context,
	expectedRevision uint64,
	state ViewState,
	selection SelectionValue,
	window DuplicateWindowRequest,
) (DuplicateProjection, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return DuplicateProjection{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return DuplicateProjection{}, mapAppError(err, service.Revision())
	}
	if expectedRevision != snapshot.Revision {
		return DuplicateProjection{}, newAppError(
			AppRevisionConflict, snapshot.Revision, errors.New("duplicate projection revision is stale"),
		)
	}
	if err = state.Validate(); err != nil {
		return DuplicateProjection{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	groupRequest, rowRequest, err := normalizeDuplicateWindow(window)
	if err != nil {
		return DuplicateProjection{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	resolvedSession, _, err := service.resolveViewSession(state.Current)
	if err != nil {
		return DuplicateProjection{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	resolvedSession.Mode = domain.ResultModeDetail
	resolvedSession.SubGrouping = nil
	resolvedSession.Sort = dateDescending()
	detailState := resolvedSession.ViewState().Current
	result, err := service.Query(resolvedSession)
	if err != nil {
		return DuplicateProjection{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	selectionSnapshot, err := service.ResolveSelection(detailState, selection)
	if err != nil {
		return DuplicateProjection{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	selection, err = BindSelectionRevision(selection, snapshot.Revision)
	if err != nil {
		return DuplicateProjection{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}

	transactions := make([]domain.Transaction, len(result.DetailRows))
	flags := make(map[string]domain.RowFlags, len(result.DetailRows))
	byID := make(map[string]domain.Transaction, len(result.DetailRows))
	for index, row := range result.DetailRows {
		transactions[index] = row.Transaction.Clone()
		_, row.Flags.Selected = selectionSnapshot.IDs[row.Transaction.ID]
		flags[row.Transaction.ID] = row.Flags
		byID[row.Transaction.ID] = row.Transaction.Clone()
	}
	groups := analytics.FindDuplicates(transactions, service.duplicateMatchingLabels(snapshot))
	groupWindow := windowResult(groupRequest, len(groups))
	groupStart := min(groupWindow.Offset, len(groups))
	selectedGroups := groups[groupStart : groupStart+groupWindow.Count]
	flattened := make([]duplicateProjectedRow, 0)
	for index, group := range selectedGroups {
		groupNumber := groupWindow.Offset + index + 1
		for _, transactionID := range group.TransactionIDs {
			flattened = append(flattened, duplicateProjectedRow{
				groupNumber: groupNumber, matchingLabel: group.MatchingLabel,
				transactionID: string(transactionID),
			})
		}
	}
	rowWindow := windowResult(rowRequest, len(flattened))
	rowStart := min(rowWindow.Offset, len(flattened))
	visibleRows := flattened[rowStart : rowStart+rowWindow.Count]
	projectedGroups := make([]DuplicateGroupProjection, 0, groupWindow.Count)
	for _, row := range visibleRows {
		if len(projectedGroups) == 0 || projectedGroups[len(projectedGroups)-1].Number != row.groupNumber {
			projectedGroups = append(projectedGroups, DuplicateGroupProjection{Number: row.groupNumber})
		}
		transaction := byID[row.transactionID]
		projectedGroups[len(projectedGroups)-1].Rows = append(
			projectedGroups[len(projectedGroups)-1].Rows,
			DuplicateRow{
				GroupNumber: row.groupNumber,
				Target:      RowTarget{Kind: IdentityTransaction, Identity: row.transactionID},
				Transaction: transaction.Clone(), MatchingLabel: row.matchingLabel,
				Flags: flags[row.transactionID],
			},
		)
	}
	totalTransactions := 0
	for _, group := range groups {
		totalTransactions += len(group.TransactionIDs)
	}
	projection := DuplicateProjection{
		Revision: snapshot.Revision, State: state.Clone(), Selection: selection,
		SelectionCount: len(selectionSnapshot.IDs), TotalGroups: len(groups),
		TotalTransactions: totalTransactions, GroupWindow: groupWindow, RowWindow: rowWindow,
		Groups: projectedGroups,
	}
	if len(groups) == 0 {
		projection.Status = "No duplicate transactions match the current view."
	}
	return projection, nil
}

func normalizeDuplicateWindow(window DuplicateWindowRequest) (WindowRequest, WindowRequest, error) {
	group, err := normalizeWindow(WindowRequest{Offset: window.GroupOffset, Limit: window.GroupLimit})
	if err != nil {
		return WindowRequest{}, WindowRequest{}, err
	}
	rows, err := normalizeWindow(WindowRequest{Offset: window.RowOffset, Limit: window.RowLimit})
	if err != nil {
		return WindowRequest{}, WindowRequest{}, err
	}
	return group, rows, nil
}

func (service *Service) duplicateMatchingLabels(snapshot EffectiveSnapshot) map[domain.EntityID]string {
	service.mu.RLock()
	providerState := cloneProviderState(service.providerState)
	service.mu.RUnlock()
	allocations := make(map[string]store.LabelAllocation, len(providerState.Allocations))
	for _, allocation := range providerState.Allocations {
		if allocation.Kind != domain.EntityKindMerchant || allocation.ProviderLabel == "" {
			continue
		}
		allocations[providerIdentityKey(allocation.Kind, allocation.Namespace, allocation.ExternalID)] = allocation
	}
	active := make(map[domain.EntityID]struct{}, len(snapshot.Effective.Merchants))
	for _, merchant := range snapshot.Effective.Merchants {
		if !merchant.Retired {
			active[merchant.ID] = struct{}{}
		}
	}
	labels := make(map[domain.EntityID]string)
	for _, identity := range snapshot.Effective.ExternalIdentities {
		if identity.EntityType != domain.EntityKindMerchant {
			continue
		}
		if _, ok := active[identity.EntityID]; !ok {
			continue
		}
		allocation, ok := allocations[providerIdentityKey(
			identity.EntityType, identity.Namespace, identity.ExternalID,
		)]
		if ok {
			labels[identity.EntityID] = allocation.ProviderLabel
		}
	}
	return labels
}

func providerIdentityKey(kind domain.EntityKind, namespace, externalID string) string {
	return string(kind) + "\x00" + namespace + "\x00" + externalID
}
