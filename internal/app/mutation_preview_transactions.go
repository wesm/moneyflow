package app

import (
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/replay"
)

func previewTransactionMutation(service *Service, snapshot EffectiveSnapshot, request MutationRequest) (MutationPreview, error) {
	plan, err := buildMutationPlan(snapshot, request, OperationMetadata{
		OperationID: "mutation_preview", CreatedAt: time.UnixMilli(1).UTC(),
	})
	if err != nil {
		return MutationPreview{}, mapAppError(err, snapshot.Revision)
	}
	if plan.Mode != MutationCancelHide {
		if err = service.validateProviderMutation(snapshot, plan.Operation); err != nil {
			return MutationPreview{}, mapAppError(err, snapshot.Revision)
		}
	}
	window, err := normalizeWindow(request.Window)
	if err != nil {
		return MutationPreview{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
	}
	ids := make(map[domain.EntityID]bool)
	var after domain.CommittedProfile
	if plan.Mode == MutationCancelHide {
		after = snapshot.Effective.Clone()
		for _, id := range plan.CancelHideTargets {
			ids[id] = true
		}
		// Cancellation removes every originating active toggle, not necessarily one
		// toggle per target. Fold their parity into the preview without rewriting history.
		flips := make(map[domain.EntityID]bool, len(ids))
		for _, operation := range snapshot.Journal[:snapshot.Cursor] {
			if operation.Type != domain.OperationTransactionHide {
				continue
			}
			for _, id := range operation.Targets {
				if ids[id] {
					flips[id] = !flips[id]
				}
			}
		}
		for index := range after.Transactions {
			if flips[after.Transactions[index].ID] {
				after.Transactions[index].Hidden = !after.Transactions[index].Hidden
			}
		}
	} else {
		operation := plan.Operation
		operation.Sequence = 1
		after, err = replay.ApplyOperation(snapshot.Effective, operation)
		if err != nil {
			return MutationPreview{}, newAppError(AppInvalidOperation, snapshot.Revision, err)
		}
		if operation.Type == domain.OperationMerchantLabel || operation.Type == domain.OperationMerchantMerge {
			for _, row := range snapshot.Effective.Transactions {
				if row.MerchantID == operation.Targets[0] {
					ids[row.ID] = true
				}
			}
		} else {
			for _, id := range operation.Targets {
				ids[id] = true
			}
		}
	}
	beforeRows, err := snapshot.Effective.MaterializeTransactions()
	if err != nil {
		return MutationPreview{}, newAppError(AppStoreCorrupt, snapshot.Revision, err)
	}
	afterRows, err := after.MaterializeTransactions()
	if err != nil {
		return MutationPreview{}, newAppError(AppStoreCorrupt, snapshot.Revision, err)
	}
	afterByID := make(map[string]domain.Transaction, len(ids))
	for _, row := range afterRows {
		if ids[domain.EntityID(row.ID)] {
			afterByID[row.ID] = row
		}
	}
	resultWindow := windowResult(window, len(ids))
	rows := make([]MutationPreviewRow, 0, resultWindow.Count)
	index := 0
	for _, row := range beforeRows {
		if !ids[domain.EntityID(row.ID)] {
			continue
		}
		if index >= resultWindow.Offset && len(rows) < resultWindow.Count {
			next, exists := afterByID[row.ID]
			rows = append(rows, MutationPreviewRow{TransactionID: domain.EntityID(row.ID), Before: row, After: next, Deleted: !exists})
		}
		index++
	}
	return MutationPreview{
		Revision: snapshot.Revision, AffectedCount: len(ids), Window: resultWindow,
		Rows: rows, SelectionDisposition: plan.SelectionDisposition, Pending: pendingSummary(snapshot),
	}, nil
}
