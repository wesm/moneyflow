package store

import "github.com/wesm/moneyflow/internal/domain"

// ProfileTransfer is one detached read of saved financial and provider state.
// Operational state is present only for export eligibility, never for installation.
type ProfileTransfer struct {
	Snapshot       domain.ProfileSnapshot
	Provider       ProviderState
	YNABSplits     []YNABTransactionSplit
	AmazonSettings *AmazonSettings
	AmazonItems    []AmazonOrderItem
	CSV            CSVState
}
