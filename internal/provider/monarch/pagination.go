package monarch

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/provider"
)

const (
	defaultSnapshotPageSize = 1000
	maxSnapshotAttempts     = 3
)

var errSnapshotChanged = errors.New("monarch snapshot changed while reading")

type transactionPartition struct {
	rows  []Transaction
	total int
}

func (client *Client) fetchTransactionPartition(
	ctx context.Context,
	hidden bool,
	attempt int,
	progress provider.ProgressFunc,
) (transactionPartition, error) {
	partitionName := "visible"
	if hidden {
		partitionName = "hidden"
	}
	seen := make(map[string]struct{})
	rows := make([]Transaction, 0)
	offset := 0
	wantTotal := -1
	for {
		page, err := client.GetTransactionsPage(ctx, TransactionPageRequest{
			Offset: offset,
			Limit:  client.options.PageSize,
			Hidden: hidden,
		})
		if err != nil {
			return transactionPartition{}, err
		}
		if page.TotalCount < 0 || (wantTotal >= 0 && page.TotalCount != wantTotal) {
			return transactionPartition{}, errSnapshotChanged
		}
		if wantTotal < 0 {
			wantTotal = page.TotalCount
		}
		if len(page.Results) > client.options.PageSize || offset+len(page.Results) > wantTotal {
			return transactionPartition{}, errSnapshotChanged
		}
		for _, transaction := range page.Results {
			if transaction.ID == "" {
				return transactionPartition{}, provider.NewError(provider.CodeDataInvalid)
			}
			if _, duplicate := seen[transaction.ID]; !duplicate {
				seen[transaction.ID] = struct{}{}
				rows = append(rows, transaction)
			}
		}
		offset += len(page.Results)
		if progress != nil {
			progress(provider.Progress{
				Partition: partitionName,
				Fetched:   len(seen),
				Total:     wantTotal,
				Attempt:   attempt,
			})
		}
		if offset == wantTotal {
			break
		}
		if len(page.Results) == 0 {
			return transactionPartition{}, errSnapshotChanged
		}
	}
	if len(seen) != wantTotal {
		return transactionPartition{}, errSnapshotChanged
	}
	return transactionPartition{rows: rows, total: wantTotal}, nil
}

func (client *Client) verifyPartitionCount(
	ctx context.Context,
	hidden bool,
	want int,
) error {
	page, err := client.GetTransactionsPage(ctx, TransactionPageRequest{
		Offset: 0,
		Limit:  1,
		Hidden: hidden,
	})
	if err != nil {
		return err
	}
	if page.TotalCount != want {
		return errSnapshotChanged
	}
	return nil
}
