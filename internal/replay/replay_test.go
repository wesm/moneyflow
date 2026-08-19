package replay_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/replay"
)

func TestReplayTransactionDeleteUndoRedo(t *testing.T) {
	t.Parallel()

	committed, err := fixture.CommittedProfile(fixture.Generate(20260818, 2))
	require.NoError(t, err)
	target := committed.Transactions[0].ID
	other := committed.Transactions[1].ID
	operation := transactionDeleteOperation(1, target)
	snapshot := domain.ProfileSnapshot{
		Revision: 1, Cursor: 1, Committed: committed, Journal: []domain.Operation{operation},
	}

	deleted, err := replay.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{other}, replayTransactionIDs(deleted.Effective))

	snapshot.Cursor = 0
	restored, err := replay.Replay(snapshot)
	require.NoError(t, err)
	assert.ElementsMatch(t, []domain.EntityID{target, other}, replayTransactionIDs(restored.Effective))

	snapshot.Cursor = 1
	redone, err := replay.Replay(snapshot)
	require.NoError(t, err)
	assert.Equal(t, []domain.EntityID{other}, replayTransactionIDs(redone.Effective))
}

func TestReplayRejectsOperationTargetingDeletedTransaction(t *testing.T) {
	t.Parallel()

	committed, err := fixture.CommittedProfile(fixture.Generate(20260818, 2))
	require.NoError(t, err)
	target := committed.Transactions[0].ID
	hide := storedOperation(2, domain.OperationTransactionHide, target)
	hide.HideToggle = &domain.HideTogglePayload{}

	_, err = replay.Replay(domain.ProfileSnapshot{
		Revision: 2, Cursor: 2, Committed: committed,
		Journal: []domain.Operation{transactionDeleteOperation(1, target), hide},
	})
	require.ErrorContains(t, err, "transaction target is missing")
}

func transactionDeleteOperation(sequence int64, targets ...domain.EntityID) domain.Operation {
	return domain.Operation{
		ID: "transaction-delete", Sequence: sequence, Type: domain.OperationTransactionDelete,
		PayloadVersion: 1, CreatedRevision: 1,
		CreatedAt: time.Date(2026, time.August, 18, 14, 0, 0, 0, time.UTC),
		Targets:   targets, TransactionDelete: &domain.TransactionDeletePayload{},
	}
}

func replayTransactionIDs(profile domain.CommittedProfile) []domain.EntityID {
	result := make([]domain.EntityID, len(profile.Transactions))
	for index, transaction := range profile.Transactions {
		result[index] = transaction.ID
	}
	return result
}
