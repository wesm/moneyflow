package replay

import "github.com/wesm/moneyflow/internal/domain"

// VisitAffectedByOperation visits transactions touched by one validated operation
// against its immediately preceding state. Returning false stops the visit.
func VisitAffectedByOperation(profile domain.CommittedProfile, operation domain.Operation, visit func(domain.EntityID) bool) {
	switch operation.Type {
	case domain.OperationMerchantReassign, domain.OperationCategoryAssign, domain.OperationTransactionHide, domain.OperationTransactionDelete:
		visitEntityIDs(operation.Targets, visit)
	case domain.OperationCategoryCreate:
		if len(operation.Targets) == 1 && operation.Targets[0] == operation.Create.EntityID {
			return
		}
		visitEntityIDs(operation.Targets, visit)
	case domain.OperationMerchantLabel, domain.OperationMerchantMerge:
		visitTransactions(profile, visit, func(t domain.TransactionRecord) bool { return t.MerchantID == operation.Targets[0] })
	case domain.OperationCategoryLabel, domain.OperationCategoryMove, domain.OperationCategoryMerge, domain.OperationCategoryDelete:
		visitTransactions(profile, visit, func(t domain.TransactionRecord) bool { return t.CategoryID == operation.Targets[0] })
	case domain.OperationGroupLabel, domain.OperationGroupMerge, domain.OperationGroupDelete:
		categories := map[domain.EntityID]bool{}
		for _, category := range profile.Categories {
			if category.GroupID == operation.Targets[0] {
				categories[category.ID] = true
			}
		}
		visitTransactions(profile, visit, func(t domain.TransactionRecord) bool { return categories[t.CategoryID] })
	}
}

func visitEntityIDs(ids []domain.EntityID, visit func(domain.EntityID) bool) {
	for _, id := range ids {
		if !visit(id) {
			return
		}
	}
}

func visitTransactions(profile domain.CommittedProfile, visit func(domain.EntityID) bool, match func(domain.TransactionRecord) bool) {
	for _, transaction := range profile.Transactions {
		if match(transaction) && !visit(transaction.ID) {
			return
		}
	}
}
