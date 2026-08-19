package replay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/replay"
)

func TestProviderRebaseTransactionDeleteShrinksOrRemovesTargets(t *testing.T) {
	t.Parallel()

	oldBase, err := fixture.CommittedProfile(fixture.Generate(20260818, 3))
	require.NoError(t, err)
	targets := []domain.EntityID{oldBase.Transactions[0].ID, oldBase.Transactions[1].ID}
	if targets[1] < targets[0] {
		targets[0], targets[1] = targets[1], targets[0]
	}
	operation := transactionDeleteOperation(1, targets...)
	redo := storedOperation(2, domain.OperationTransactionHide, oldBase.Transactions[2].ID)
	redo.HideToggle = &domain.HideTogglePayload{}
	journal := []domain.Operation{operation, redo}

	t.Run("present", func(t *testing.T) {
		result, rebaseErr := replay.RebaseProviderJournal(oldBase, oldBase, journal, 1)
		require.NoError(t, rebaseErr)
		require.Len(t, result.Journal, 1)
		assert.Equal(t, operation.ID, result.Journal[0].ID)
		assert.Equal(t, targets, result.Journal[0].Targets)
		assert.Equal(t, 1, result.Cursor)
		assert.Equal(t, 1, result.Summary.DiscardedRedoOperations)
	})

	t.Run("partial", func(t *testing.T) {
		newBase := oldBase.Clone()
		newBase.Transactions = removeReplayTransaction(newBase.Transactions, targets[0])
		result, rebaseErr := replay.RebaseProviderJournal(oldBase, newBase, journal, 1)
		require.NoError(t, rebaseErr)
		require.Len(t, result.Journal, 1)
		assert.Equal(t, []domain.EntityID{targets[1]}, result.Journal[0].Targets)
		assert.Equal(t, operation.ID, result.Journal[0].ID)
		assert.Equal(t, 1, result.Cursor)
		assert.Equal(t, 1, result.Summary.RemovedTargets)
		assert.Equal(t, 1, result.Summary.DiscardedRedoOperations)
		effective, replayErr := replay.Replay(domain.ProfileSnapshot{
			Committed: newBase, Journal: result.Journal, Cursor: result.Cursor,
		})
		require.NoError(t, replayErr)
		assert.NotContains(t, replayTransactionIDs(effective.Effective), targets[1])
	})

	t.Run("empty", func(t *testing.T) {
		newBase := oldBase.Clone()
		newBase.Transactions = removeReplayTransaction(newBase.Transactions, targets[0])
		newBase.Transactions = removeReplayTransaction(newBase.Transactions, targets[1])
		result, rebaseErr := replay.RebaseProviderJournal(oldBase, newBase, journal, 1)
		require.NoError(t, rebaseErr)
		assert.Empty(t, result.Journal)
		assert.Zero(t, result.Cursor)
		assert.Equal(t, 1, result.Summary.RemovedOperations)
		assert.Equal(t, 2, result.Summary.RemovedTargets)
		assert.Equal(t, 1, result.Summary.DiscardedRedoOperations)
	})
}

func removeReplayTransaction(
	transactions []domain.TransactionRecord,
	id domain.EntityID,
) []domain.TransactionRecord {
	result := make([]domain.TransactionRecord, 0, len(transactions)-1)
	for _, transaction := range transactions {
		if transaction.ID != id {
			result = append(result, transaction)
		}
	}
	return result
}
