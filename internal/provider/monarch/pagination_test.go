package monarch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestPaginationCountChangeRestartsCompleteSnapshot(t *testing.T) {
	t.Parallel()

	visible := []Transaction{
		snapshotTransaction("transaction-a", false, false),
		snapshotTransaction("transaction-b", false, false),
		snapshotTransaction("transaction-c", false, false),
	}
	scenario := snapshotScenario{
		Visible: visible,
		BeforePage: func(attempt int, hidden bool, offset int, _ int, total int, rows []Transaction) (int, []Transaction) {
			if attempt == 1 && !hidden && offset > 0 {
				return len(visible) + 1, rows
			}
			return total, rows
		},
	}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	snapshot, err := client.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, snapshot.Transactions, len(visible)+1)
	assert.Equal(t, 2, server.CompleteAttempts())
}

func TestPaginationCountDecreaseRestartsCompleteSnapshot(t *testing.T) {
	t.Parallel()

	visible := []Transaction{
		snapshotTransaction("transaction-a", false, false),
		snapshotTransaction("transaction-b", false, false),
		snapshotTransaction("transaction-c", false, false),
	}
	scenario := snapshotScenario{
		Visible: visible,
		BeforePage: func(attempt int, hidden bool, offset int, _ int, total int, rows []Transaction) (int, []Transaction) {
			if attempt == 1 && !hidden && offset > 0 {
				return total - 1, rows
			}
			return total, rows
		},
	}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	snapshot, err := client.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, snapshot.Transactions, len(visible)+1)
	assert.Equal(t, 2, server.CompleteAttempts())
}

func TestPaginationDuplicateTransactionIDExhaustsAttempts(t *testing.T) {
	t.Parallel()

	server := newSnapshotServer(t, snapshotScenario{Visible: []Transaction{
		snapshotTransaction("transaction-duplicate", false, false),
		snapshotTransaction("transaction-duplicate", false, false),
	}})
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeSnapshotUnstable)
	assert.Equal(t, 3, server.CompleteAttempts())
}

func TestHiddenFlipBetweenPartitionsRestartsWholeAttempt(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{BeforePage: func(attempt int, hidden bool, _ int, limit int, total int, rows []Transaction) (int, []Transaction) {
		if attempt == 1 && !hidden && limit == 1 {
			return total + 1, rows
		}
		return total, rows
	}}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, server.CompleteAttempts())
}

func TestPaginationIntegrityFailureCannotReturnPartialRows(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{BeforePage: func(_ int, _ bool, _ int, _ int, total int, rows []Transaction) (int, []Transaction) {
		return total + 1, rows
	}}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	snapshot, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeSnapshotUnstable)
	assert.Empty(t, snapshot.Transactions)
	assert.Equal(t, 3, server.CompleteAttempts())
}
