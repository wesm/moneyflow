package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// MaxReviewTargetLimit bounds one explicit operation-detail request.
const MaxReviewTargetLimit = 400

// ReviewWindow requests one bounded target window for a named operation.
type ReviewWindow struct {
	OperationID string
	Offset      int
	Limit       int
}

// ReviewOperation summarizes one active or inactive journal unit.
type ReviewOperation struct {
	OperationID    string
	Sequence       int64
	Type           domain.OperationType
	Active         bool
	AffectedCount  int
	Before         string
	After          string
	TaxonomyEffect string
}

// ReviewTarget is one bounded transaction row affected by a reviewed operation.
type ReviewTarget struct {
	TransactionID domain.EntityID
	Date          domain.Date
	Merchant      string
	Category      string
	Hidden        bool
}

// ReviewProjection contains summaries first and optional bounded target details.
type ReviewProjection struct {
	Revision           uint64
	Pending            PendingSummary
	Operations         []ReviewOperation
	ActiveOperations   []ReviewOperation
	InactiveOperations []ReviewOperation
	Targets            []ReviewTarget
	Window             Window
}

// Review returns the current journal summary and, when requested, one target window.
func (service *Service) Review(
	ctx context.Context,
	expected uint64,
	window ReviewWindow,
) (ReviewProjection, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return ReviewProjection{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return ReviewProjection{}, mapAppError(err, service.Revision())
	}
	if expected != snapshot.Revision {
		return ReviewProjection{}, newAppError(
			AppRevisionConflict, snapshot.Revision, errors.New("review revision is stale"),
		)
	}
	if window.Offset < 0 || window.Limit < 0 || window.Limit > MaxReviewTargetLimit {
		return ReviewProjection{}, newAppError(
			AppInvalidOperation, snapshot.Revision, errors.New("review window is invalid"),
		)
	}
	limit := window.Limit
	if limit == 0 {
		limit = DefaultWindowLimit
	}
	projection, transactionIDs, found, err := buildReviewProjection(
		snapshot, window.OperationID, window.Offset, limit,
	)
	if err != nil {
		return ReviewProjection{}, newAppError(AppStoreCorrupt, snapshot.Revision, err)
	}
	if window.OperationID == "" {
		return projection, nil
	}
	if !found {
		return ReviewProjection{}, newAppError(
			AppInvalidTarget, snapshot.Revision, errors.New("review operation is missing"),
		)
	}
	start := window.Offset
	for _, operation := range projection.Operations {
		if operation.OperationID == window.OperationID {
			start = min(start, operation.AffectedCount)
			break
		}
	}
	projection.Window = Window{Offset: start, Limit: limit, Count: len(transactionIDs)}
	projection.Targets = reviewTargets(snapshot.Effective, transactionIDs)
	return projection, nil
}

func buildReviewProjection(
	snapshot EffectiveSnapshot,
	detailOperationID string,
	detailOffset int,
	detailLimit int,
) (ReviewProjection, []domain.EntityID, bool, error) {
	projection := ReviewProjection{
		Revision: snapshot.Revision, Pending: pendingSummary(snapshot),
		Operations: make([]ReviewOperation, 0, len(snapshot.Journal)),
	}
	var detailTargets []domain.EntityID
	detailFound := detailOperationID == ""
	state := snapshot.Committed.Clone()
	for index, operation := range snapshot.Journal {
		affectedCount := affectedOperationCount(state, operation)
		if operation.ID == detailOperationID {
			detailFound = true
			detailTargets = affectedOperationWindow(state, operation, detailOffset, detailLimit)
		}
		next, err := ApplyOperation(state, operation)
		if err != nil {
			return ReviewProjection{}, nil, false, fmt.Errorf("review operation[%d]: %w", index, err)
		}
		before, after, effect := reviewValues(state, next, operation)
		summary := ReviewOperation{
			OperationID: operation.ID, Sequence: operation.Sequence, Type: operation.Type,
			Active: index < snapshot.Cursor, AffectedCount: affectedCount,
			Before: before, After: after, TaxonomyEffect: effect,
		}
		projection.Operations = append(projection.Operations, summary)
		if summary.Active {
			projection.ActiveOperations = append(projection.ActiveOperations, summary)
		} else {
			projection.InactiveOperations = append(projection.InactiveOperations, summary)
		}
		state = next
	}
	return projection, detailTargets, detailFound, nil
}

func affectedOperationCount(profile domain.CommittedProfile, operation domain.Operation) int {
	count := 0
	visitAffectedByOperation(profile, operation, func(domain.EntityID) bool {
		count++
		return true
	})
	return count
}

func affectedOperationWindow(
	profile domain.CommittedProfile,
	operation domain.Operation,
	offset int,
	limit int,
) []domain.EntityID {
	if limit <= 0 {
		return nil
	}
	result := make([]domain.EntityID, 0, min(limit, len(profile.Transactions)))
	visited := 0
	visitAffectedByOperation(profile, operation, func(id domain.EntityID) bool {
		if visited >= offset {
			result = append(result, id)
		}
		visited++
		return len(result) < limit
	})
	return result
}

func reviewValues(
	before, after domain.CommittedProfile,
	operation domain.Operation,
) (string, string, string) {
	switch operation.Type {
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge:
		return merchantLabel(before, operation.Targets[0]),
			merchantReviewAfter(after, operation), "merchant"
	case domain.OperationCategoryAssign, domain.OperationCategoryCreate:
		return "", categoryDestinationLabel(after, operation), "category"
	case domain.OperationCategoryLabel, domain.OperationCategoryMove,
		domain.OperationCategoryMerge, domain.OperationCategoryDelete:
		return categoryLabel(before, operation.Targets[0]),
			categoryReviewAfter(after, operation), "category"
	case domain.OperationGroupCreate:
		return "", operation.Create.Label, "group"
	case domain.OperationGroupLabel, domain.OperationGroupMerge, domain.OperationGroupDelete:
		return groupLabel(before, operation.Targets[0]), groupReviewAfter(after, operation), "group"
	case domain.OperationMerchantReassign:
		return "", merchantLabel(after, operation.Reassign.DestinationID), "merchant"
	case domain.OperationTransactionHide:
		return "visible/hidden", "toggled", ""
	default:
		return "", "", ""
	}
}

func merchantReviewAfter(profile domain.CommittedProfile, operation domain.Operation) string {
	if operation.Label != nil {
		return operation.Label.Label
	}
	if operation.Merge != nil {
		return merchantLabel(profile, operation.Merge.DestinationID)
	}
	return ""
}

func categoryReviewAfter(profile domain.CommittedProfile, operation domain.Operation) string {
	switch {
	case operation.Label != nil:
		return operation.Label.Label
	case operation.Move != nil:
		return groupLabel(profile, operation.Move.DestinationID)
	case operation.Merge != nil:
		return categoryLabel(profile, operation.Merge.DestinationID)
	case operation.Delete != nil:
		return categoryLabel(profile, operation.Delete.ReplacementID)
	default:
		return ""
	}
}

func groupReviewAfter(profile domain.CommittedProfile, operation domain.Operation) string {
	switch {
	case operation.Label != nil:
		return operation.Label.Label
	case operation.Merge != nil:
		return groupLabel(profile, operation.Merge.DestinationID)
	case operation.Delete != nil:
		return groupLabel(profile, operation.Delete.ReplacementID)
	default:
		return ""
	}
}

func categoryDestinationLabel(profile domain.CommittedProfile, operation domain.Operation) string {
	if operation.Create != nil {
		return operation.Create.Label
	}
	if operation.Reassign != nil {
		return categoryLabel(profile, operation.Reassign.DestinationID)
	}
	return ""
}

func merchantLabel(profile domain.CommittedProfile, id domain.EntityID) string {
	merchant, _ := merchantWithID(profile, id)
	return merchant.Label
}

func categoryLabel(profile domain.CommittedProfile, id domain.EntityID) string {
	category, _ := categoryWithID(profile, id)
	return category.Label
}

func groupLabel(profile domain.CommittedProfile, id domain.EntityID) string {
	group, _ := groupWithID(profile, id)
	return group.Label
}

func reviewTargets(
	profile domain.CommittedProfile,
	ids []domain.EntityID,
) []ReviewTarget {
	merchants := make(map[domain.EntityID]string, len(profile.Merchants))
	categories := make(map[domain.EntityID]string, len(profile.Categories))
	wanted := make(map[domain.EntityID]struct{}, len(ids))
	transactions := make(map[domain.EntityID]domain.TransactionRecord, len(ids))
	for _, merchant := range profile.Merchants {
		merchants[merchant.ID] = merchant.Label
	}
	for _, category := range profile.Categories {
		categories[category.ID] = category.Label
	}
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	for _, transaction := range profile.Transactions {
		if _, exists := wanted[transaction.ID]; exists {
			transactions[transaction.ID] = transaction
		}
	}
	result := make([]ReviewTarget, 0, len(ids))
	for _, id := range ids {
		transaction, ok := transactions[id]
		if !ok {
			continue
		}
		result = append(result, ReviewTarget{
			TransactionID: transaction.ID, Date: transaction.Date,
			Merchant: merchants[transaction.MerchantID],
			Category: categories[transaction.CategoryID], Hidden: transaction.Hidden,
		})
	}
	return result
}
