package app

import "github.com/wesm/moneyflow/internal/domain"

// StagedValues lists the post-commit values of one transaction that differ from its
// committed row. Fields that do not change stay nil.
type StagedValues struct {
	Merchant *domain.EntityRef
	Category *domain.CategoryRef
	Hidden   *bool
	Deleted  bool
}

// decorateStagedValues fills Staged on window rows with pending edits. Rows stay committed.
func (service *Service) decorateStagedValues(rows []WebDetailRow) {
	service.mu.RLock()
	defer service.mu.RUnlock()
	pending := make(map[string]struct{})
	for index := range rows {
		id := rows[index].Row.Transaction.ID
		if _, isPending := service.localPending[id]; isPending {
			pending[id] = struct{}{}
		}
	}
	if len(pending) == 0 {
		return
	}
	effective := make(map[string]domain.Transaction, len(pending))
	for _, transaction := range service.transactions {
		if _, isPending := pending[transaction.ID]; isPending {
			effective[transaction.ID] = transaction
		}
	}
	for index := range rows {
		committed := rows[index].Row.Transaction
		if _, isPending := pending[committed.ID]; isPending {
			rows[index].Staged = stagedValues(committed, effective)
		}
	}
}

func stagedValues(
	committed domain.Transaction,
	effective map[string]domain.Transaction,
) *StagedValues {
	current, exists := effective[committed.ID]
	if !exists {
		return &StagedValues{Deleted: true}
	}
	var staged StagedValues
	changed := false
	if current.Merchant != committed.Merchant {
		merchant := current.Merchant
		staged.Merchant = &merchant
		changed = true
	}
	if current.Category != committed.Category {
		category := current.Category
		staged.Category = &category
		changed = true
	}
	if current.Hidden != committed.Hidden {
		hidden := current.Hidden
		staged.Hidden = &hidden
		changed = true
	}
	if !changed {
		return nil
	}
	return &staged
}
