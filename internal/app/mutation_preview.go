package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// MutationPreviewRow is one exact affected transaction before and after a prospective edit.
type MutationPreviewRow struct {
	TransactionID domain.EntityID
	Before        domain.Transaction
	After         domain.Transaction
}

// MutationPreview is a bounded, side-effect-free projection of one validated mutation.
type MutationPreview struct {
	Revision             uint64
	AffectedCount        int
	Window               Window
	Rows                 []MutationPreviewRow
	SelectionDisposition SelectionDisposition
	Pending              PendingSummary
}

// PreviewMutation validates and projects one category mutation without allocating operation
// identity or changing durable or transient service state.
func (service *Service) PreviewMutation(
	ctx context.Context,
	request MutationRequest,
) (MutationPreview, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if _, err := service.refreshLocked(ctx); err != nil {
		return MutationPreview{}, err
	}
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return MutationPreview{}, mapAppError(err, service.Revision())
	}
	if request.ExpectedRevision != snapshot.Revision {
		return MutationPreview{}, newAppError(
			AppRevisionConflict, snapshot.Revision, errors.New("mutation preview revision is stale"),
		)
	}
	if err = service.validateProviderWriteIdle(); err != nil {
		return MutationPreview{}, mapAppError(err, snapshot.Revision)
	}
	if request.Action != ActionEditCategory {
		return MutationPreview{}, newAppError(
			AppInvalidOperation, snapshot.Revision,
			errors.New("mutation preview action is unsupported"),
		)
	}
	intent, err := buildCategoryAssignmentIntent(snapshot, request)
	if err != nil {
		return MutationPreview{}, mapAppError(err, snapshot.Revision)
	}
	plan, err := intent.materialize(request, OperationMetadata{
		OperationID: "mutation_preview", CreatedAt: time.UnixMilli(1).UTC(),
	})
	if err != nil {
		return MutationPreview{}, mapAppError(err, snapshot.Revision)
	}
	if err = service.validateProviderMutation(snapshot, plan.Operation); err != nil {
		return MutationPreview{}, mapAppError(err, snapshot.Revision)
	}
	window, err := normalizeWindow(request.Window)
	if err != nil {
		return MutationPreview{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	operation := plan.Operation.Clone()
	operation.Sequence = int64(len(snapshot.Journal) + 1)
	after, err := ApplyOperation(snapshot.Effective, operation)
	if err != nil {
		return MutationPreview{}, newAppError(AppStoreCorrupt, snapshot.Revision, err)
	}
	rows, err := mutationPreviewRows(snapshot.Effective, after, operation.Targets)
	if err != nil {
		return MutationPreview{}, newAppError(AppStoreCorrupt, snapshot.Revision, err)
	}
	resultWindow := windowResult(window, len(rows))
	start := resultWindow.Offset
	end := start + resultWindow.Count
	return MutationPreview{
		Revision: snapshot.Revision, AffectedCount: len(rows), Window: resultWindow,
		Rows:                 append([]MutationPreviewRow(nil), rows[start:end]...),
		SelectionDisposition: plan.SelectionDisposition, Pending: pendingSummary(snapshot),
	}, nil
}

func mutationPreviewRows(
	beforeProfile domain.CommittedProfile,
	afterProfile domain.CommittedProfile,
	targets []domain.EntityID,
) ([]MutationPreviewRow, error) {
	before, err := beforeProfile.MaterializeTransactions()
	if err != nil {
		return nil, err
	}
	after, err := afterProfile.MaterializeTransactions()
	if err != nil {
		return nil, err
	}
	beforeByID := make(map[domain.EntityID]domain.Transaction, len(before))
	for _, transaction := range before {
		beforeByID[domain.EntityID(transaction.ID)] = transaction
	}
	afterByID := make(map[domain.EntityID]domain.Transaction, len(after))
	for _, transaction := range after {
		afterByID[domain.EntityID(transaction.ID)] = transaction
	}
	ids := append([]domain.EntityID(nil), targets...)
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	rows := make([]MutationPreviewRow, 0, len(ids))
	for _, id := range ids {
		beforeTransaction, beforeOK := beforeByID[id]
		afterTransaction, afterOK := afterByID[id]
		if !beforeOK || !afterOK {
			return nil, errors.New("mutation preview target is missing")
		}
		rows = append(rows, MutationPreviewRow{
			TransactionID: id, Before: beforeTransaction.Clone(), After: afterTransaction.Clone(),
		})
	}
	return rows, nil
}
