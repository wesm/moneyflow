package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestRebaseDiscardsRedoAndShrinksResolvedTransactionTargets(t *testing.T) {
	t.Parallel()

	oldBase := replayProfile(t)
	newBase := oldBase.Clone()
	newBase.Transactions = newBase.Transactions[:1]
	journal := []domain.Operation{
		labelOperation(1, domain.OperationMerchantLabel, "merchant_a", "Renamed"),
		reassignOperation(
			2, domain.OperationCategoryAssign, "category_b", "transaction_a", "transaction_b",
		),
		labelOperation(3, domain.OperationCategoryLabel, "category_a", "Inactive"),
	}

	result, err := app.RebaseProviderJournal(oldBase, newBase, journal, 2)
	require.NoError(t, err)
	require.Len(t, result.Journal, 2)
	assert.Equal(t, []domain.EntityID{"transaction_a"}, result.Journal[1].Targets)
	assert.Equal(t, int64(2), result.Journal[1].Sequence)
	assert.Equal(t, 2, result.Cursor)
	assert.Equal(t, app.RebaseSummary{
		RemovedTargets: 1, RetainedOperations: 2, DiscardedRedoOperations: 1,
	}, result.Summary)
	assert.Equal(t, []app.RebaseDetail{{
		OperationID: "operation_2", OperationType: domain.OperationCategoryAssign,
		RemovedTargets: 1,
	}}, result.Details)
}

func TestRebaseRemovesEmptyOperationsAndDecrementsCursor(t *testing.T) {
	t.Parallel()

	oldBase := replayProfile(t)
	newBase := oldBase.Clone()
	newBase.Transactions = newBase.Transactions[1:]
	journal := []domain.Operation{
		reassignOperation(1, domain.OperationCategoryAssign, "category_b", "transaction_a"),
		labelOperation(2, domain.OperationMerchantLabel, "merchant_b", "Still Active"),
	}

	result, err := app.RebaseProviderJournal(oldBase, newBase, journal, len(journal))
	require.NoError(t, err)
	require.Len(t, result.Journal, 1)
	assert.Equal(t, "operation_2", result.Journal[0].ID)
	assert.Equal(t, 1, result.Cursor)
	assert.Equal(t, 1, result.Summary.RemovedOperations)
	assert.Equal(t, 1, result.Summary.RemovedTargets)
	assert.Equal(t, []app.RebaseDetail{{
		OperationID: "operation_1", OperationType: domain.OperationCategoryAssign,
		RemovedTargets: 1, Removed: true,
	}}, result.Details)
}

func TestRebaseValidatesJournalCreatedEntitiesInOperationOrder(t *testing.T) {
	t.Parallel()

	base := replayProfile(t)
	createGroup := createGroupOperation(1)
	createCategory := createCategoryOperation(2)
	createCategory.Create.ParentID = "group_new"
	createCategory.Targets = []domain.EntityID{"category_new"}
	relabel := labelOperation(
		3, domain.OperationCategoryLabel, "category_new", "Created Then Renamed",
	)

	result, err := app.RebaseProviderJournal(
		base, base, []domain.Operation{createGroup, createCategory, relabel}, 3,
	)
	require.NoError(t, err)
	assert.Len(t, result.Journal, 3)
	effective := replayRebased(t, base, result)
	assert.Equal(t, "Created Then Renamed", categoryByID(t, effective, "category_new").Label)
}

func TestRebaseRemovesOperationsWithMissingStructuralDependencies(t *testing.T) {
	t.Parallel()

	oldBase := replayProfile(t)
	newBase := oldBase.Clone()
	newBase.Merchants[0].Retired = true
	newBase.Transactions[0].MerchantID = "merchant_b"
	journal := []domain.Operation{
		labelOperation(1, domain.OperationMerchantLabel, "merchant_a", "No Longer There"),
		mergeOperation(2, domain.OperationMerchantMerge, "merchant_a", "merchant_b"),
	}

	result, err := app.RebaseProviderJournal(oldBase, newBase, journal, 2)
	require.NoError(t, err)
	assert.Empty(t, result.Journal)
	assert.Zero(t, result.Cursor)
	assert.Equal(t, 2, result.Summary.RemovedOperations)
}

func TestRebaseStructuralOperationSweepsNewMembership(t *testing.T) {
	t.Parallel()

	oldBase := replayProfile(t)
	newBase := oldBase.Clone()
	newTransaction := newBase.Transactions[0]
	newTransaction.ID = "transaction_new"
	newTransaction.ProviderID = "provider-new"
	newBase.Transactions = append(newBase.Transactions, newTransaction)
	operation := mergeOperation(
		1, domain.OperationMerchantMerge, "merchant_a", "merchant_b",
	)

	result, err := app.RebaseProviderJournal(oldBase, newBase, []domain.Operation{operation}, 1)
	require.NoError(t, err)
	effective := replayRebased(t, newBase, result)
	assert.Equal(t, domain.EntityID("merchant_b"), transactionByID(
		t, effective, "transaction_new",
	).MerchantID)
}

func TestRebaseHideTargetsPreserveIntendedState(t *testing.T) {
	t.Parallel()

	oldBase := replayProfile(t)
	newBase := oldBase.Clone()
	newBase.Transactions[0].Hidden = true
	operation := hideOperation(1, "transaction_a", "transaction_b")

	result, err := app.RebaseProviderJournal(oldBase, newBase, []domain.Operation{operation}, 1)
	require.NoError(t, err)
	require.Len(t, result.Journal, 1)
	assert.Equal(t, []domain.EntityID{"transaction_b"}, result.Journal[0].Targets)
	assert.Equal(t, 1, result.Summary.RemovedTargets)
	assert.Equal(t, 1, result.Summary.RebasedHideTargets)
	effective := replayRebased(t, newBase, result)
	assert.True(t, transactionByID(t, effective, "transaction_a").Hidden)
	assert.False(t, transactionByID(t, effective, "transaction_b").Hidden)
}

func TestRebaseNormalizesRepeatedActiveHideTogglesPerTransaction(t *testing.T) {
	t.Parallel()

	base := replayProfile(t)
	result, err := app.RebaseProviderJournal(base, base, []domain.Operation{
		hideOperation(1, "transaction_a"),
		hideOperation(2, "transaction_a", "transaction_b"),
	}, 2)
	require.NoError(t, err)
	require.Len(t, result.Journal, 1)
	assert.Equal(t, "operation_2", result.Journal[0].ID)
	assert.Equal(t, []domain.EntityID{"transaction_b"}, result.Journal[0].Targets)
	assert.Equal(t, 1, result.Cursor)
	assert.Equal(t, 1, result.Summary.RemovedOperations)
	assert.Equal(t, 2, result.Summary.RemovedTargets)
	effective := replayRebased(t, base, result)
	assert.False(t, transactionByID(t, effective, "transaction_a").Hidden)
	assert.False(t, transactionByID(t, effective, "transaction_b").Hidden)
}

func replayRebased(
	t *testing.T,
	base domain.CommittedProfile,
	result app.RebaseResult,
) domain.CommittedProfile {
	t.Helper()
	effective, err := app.Replay(domain.ProfileSnapshot{
		Revision: 1, Cursor: result.Cursor, Committed: base, Journal: result.Journal,
	})
	require.NoError(t, err)
	return effective.Effective
}
