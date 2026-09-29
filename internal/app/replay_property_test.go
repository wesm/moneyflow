package app_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestReplayRandomizedMatchesRepeatedApplyOperation(t *testing.T) {
	t.Parallel()

	const seed uint64 = 0x20260814
	random := deterministicRandom{state: seed}
	committed := replayProfile(t)
	snapshot := domain.ProfileSnapshot{Revision: 1, Committed: committed}
	var sequence int64
	for step := range 200 {
		switch {
		case len(snapshot.Journal) == 0 || random.intN(4) != 0:
			if snapshot.Cursor < len(snapshot.Journal) {
				snapshot.Journal = snapshot.Journal[:snapshot.Cursor]
			}
			sequence++
			operation := randomReplayOperation(&random, sequence)
			snapshot.Journal = append(snapshot.Journal, operation)
			snapshot.Cursor = len(snapshot.Journal)
		case random.intN(2) == 0 && snapshot.Cursor > 0:
			snapshot.Cursor--
		case snapshot.Cursor < len(snapshot.Journal):
			snapshot.Cursor++
		}
		snapshot.Revision++

		replayed, err := app.Replay(snapshot)
		require.NoError(t, err, "seed=%d step=%d", seed, step)
		repeated := committed.Clone()
		for index := range snapshot.Cursor {
			repeated, err = app.ApplyOperation(repeated, snapshot.Journal[index])
			require.NoError(t, err, "seed=%d step=%d operation=%d", seed, step, index)
		}
		assert.Equal(t, repeated, replayed.Effective, "seed=%d step=%d", seed, step)
	}
}

type deterministicRandom struct {
	state uint64
}

func (random *deterministicRandom) intN(limit uint64) uint64 {
	random.state = random.state*6364136223846793005 + 1442695040888963407
	return random.state % limit
}

func randomReplayOperation(random *deterministicRandom, sequence int64) domain.Operation {
	var operation domain.Operation
	switch random.intN(3) {
	case 0:
		target := domain.EntityID("transaction_a")
		if random.intN(2) == 1 {
			target = "transaction_b"
		}
		operation = hideOperation(sequence, target)
	case 1:
		label := fmt.Sprintf("Merchant A %03d", sequence)
		operation = labelOperation(
			sequence,
			domain.OperationMerchantLabel,
			"merchant_a",
			label,
		)
	default:
		destination := domain.EntityID("category_a")
		if random.intN(2) == 1 {
			destination = "category_b"
		}
		operation = reassignOperation(
			sequence,
			domain.OperationCategoryAssign,
			destination,
			"transaction_a",
		)
	}
	operation.ID = fmt.Sprintf("operation_%06d", sequence)
	return operation
}
