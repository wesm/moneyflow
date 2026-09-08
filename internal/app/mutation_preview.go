package app

import (
	"context"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// MutationPreviewRow is one exact affected transaction before and after a prospective edit.
type MutationPreviewRow struct {
	TransactionID domain.EntityID
	Before        domain.Transaction
	After         domain.Transaction
	Deleted       bool
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

// PreviewMutation validates and projects one supported mutation without allocating operation
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
	snapshot, err := service.effectiveSnapshotReadOnly()
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
	if request.Action == ActionEditMerchant || request.Action == ActionToggleHidden || request.Action == ActionDeleteTransaction {
		return previewTransactionMutation(service, snapshot, request)
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
	rows, err := service.categoryMutationPreviewRows(snapshot.Effective, plan.Operation)
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

func (service *Service) effectiveSnapshotReadOnly() (EffectiveSnapshot, error) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.snapshot == nil {
		return EffectiveSnapshot{}, errors.New("profile service has no snapshot")
	}
	// Snapshot values are immutable after publication. Interaction serialization prevents a
	// semantic reload while this preview is being planned, so a shallow view avoids copying the
	// complete profile for a read-only operation.
	return *service.snapshot, nil
}

func (service *Service) categoryMutationPreviewRows(
	profile domain.CommittedProfile,
	operation domain.Operation,
) ([]MutationPreviewRow, error) {
	var category domain.Category
	var found bool
	switch operation.Type {
	case domain.OperationCategoryAssign:
		category, found = categoryWithID(profile, operation.Reassign.DestinationID)
	case domain.OperationCategoryCreate:
		category = domain.Category{
			ID: operation.Create.EntityID, GroupID: operation.Create.ParentID,
			Label: operation.Create.Label, CollisionKey: operation.Create.CollisionKey,
		}
		found = true
	default:
		return nil, errors.New("mutation preview operation is not a category assignment")
	}
	group, groupFound := groupWithID(profile, category.GroupID)
	if !found || category.Retired || !groupFound || group.Retired {
		return nil, errors.New("mutation preview category is missing or retired")
	}
	targets := make(map[string]struct{}, len(operation.Targets))
	for _, target := range operation.Targets {
		targets[string(target)] = struct{}{}
	}
	beforeByID := make(map[string]domain.Transaction, len(targets))
	service.mu.RLock()
	for _, transaction := range service.transactions {
		if _, ok := targets[transaction.ID]; ok {
			beforeByID[transaction.ID] = transaction.Clone()
		}
	}
	service.mu.RUnlock()
	rows := make([]MutationPreviewRow, 0, len(operation.Targets))
	for _, target := range operation.Targets {
		before, ok := beforeByID[string(target)]
		if !ok {
			return nil, errors.New("mutation preview target is missing")
		}
		after := before.Clone()
		after.Category = domain.CategoryRef{
			ID: string(category.ID), Name: category.Label,
			GroupID: string(group.ID), Group: group.Label,
		}
		validated, err := domain.NewTransaction(after)
		if err != nil {
			return nil, err
		}
		rows = append(rows, MutationPreviewRow{
			TransactionID: target, Before: before, After: validated,
		})
	}
	return rows, nil
}
