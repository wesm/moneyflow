package store

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
)

// CSVSettings binds a local profile to one mapping and exact money partition.
type CSVSettings struct {
	Mapping   string
	Currency  domain.Currency
	Scale     uint8
	CreatedAt time.Time
}

// CSVFile retains only the latest snapshot of an opaque source-file identity.
type CSVFile struct {
	Key, AccountKey, Digest       string
	CompletedAt                   time.Time
	Imported, Duplicates, Skipped int
}

// CSVRow retains source facts even after supersession or an explicit user deletion.
type CSVRow struct {
	bankcsv.Row
	LocalTransactionID                 domain.EntityID
	MerchantOverride, CategoryOverride bool
	Disposition                        string
}

// CSVFileRow records one file's claim on a source occurrence.
type CSVFileRow struct{ FileKey, SourceKey string }

// CSVState is the durable reconciliation ledger, included in profile transfer.
type CSVState struct {
	Settings   *CSVSettings
	Files      []CSVFile
	Rows       []CSVRow
	Membership []CSVFileRow
}

// CSVImportState is read inside the same transaction that installs the plan.
type CSVImportState struct {
	Snapshot    domain.ProfileSnapshot
	CSV         CSVState
	Allocations []LabelAllocation
}

// CSVImportResult contains no private row values.
type CSVImportResult struct {
	Revision                                                                              uint64
	Inserted, Duplicates, Updated, Superseded, Restored, Retained, Skipped, DiscardedRedo int
	Unchanged                                                                             bool
}

// CSVImportPlan replaces saved source state and the rebased journal atomically.
type CSVImportPlan struct {
	State  CSVImportState
	Result CSVImportResult
}

// CSVProposedIDs is the store-owned supply of opaque identities for one file.
type CSVProposedIDs struct{ Transactions, Accounts, Merchants, Categories []domain.EntityID }

// CSVImportPlanner makes reconciliation decisions without I/O or clocks.
type CSVImportPlanner func(CSVImportState, CSVProposedIDs) (CSVImportPlan, error)

// ValidateCSVState checks the same cross-record invariants on import and JSONL transfer.
func ValidateCSVState(state CSVState, committed domain.CommittedProfile) error {
	invalid := errors.New("invalid CSV source state")
	if state.Settings == nil {
		if len(state.Files)+len(state.Rows)+len(state.Membership) != 0 {
			return invalid
		}
		for _, transaction := range committed.Transactions {
			if transaction.Provider == "csv" {
				return invalid
			}
		}
		for _, identity := range committed.ExternalIdentities {
			if strings.HasPrefix(identity.Namespace, "csv:") {
				return invalid
			}
		}
		return nil
	}
	settings := state.Settings
	mapping, err := bankcsv.Lookup(settings.Mapping)
	if err != nil || mapping.Currency != settings.Currency || mapping.Scale != settings.Scale || settings.CreatedAt.IsZero() {
		return invalid
	}
	accounts := make(map[string]domain.EntityID, len(committed.Accounts))
	for _, account := range committed.Accounts {
		if !account.Retired {
			accounts[account.CollisionKey] = account.ID
		}
	}
	transactions := make(map[domain.EntityID]domain.TransactionRecord, len(committed.Transactions))
	for _, row := range committed.Transactions {
		transactions[row.ID] = row
	}
	identities := make(map[string]domain.EntityID)
	for _, identity := range committed.ExternalIdentities {
		if identity.Namespace == "csv:transaction" && identity.EntityType == domain.EntityKindTransaction {
			identities[identity.ExternalID] = identity.EntityID
		}
	}
	files := make(map[string]CSVFile, len(state.Files))
	for _, file := range state.Files {
		if !csvDigest(file.Key) || !csvDigest(file.Digest) || file.CompletedAt.IsZero() || file.Imported < 0 || file.Duplicates < 0 || file.Skipped < 0 || accounts[file.AccountKey] == "" {
			return invalid
		}
		if _, exists := files[file.Key]; exists {
			return invalid
		}
		files[file.Key] = file
	}
	rows := make(map[string]CSVRow, len(state.Rows))
	ids := make(map[domain.EntityID]bool, len(state.Rows))
	for _, row := range state.Rows {
		if row.Occurrence < 1 || row.Amount.Currency != settings.Currency || row.Amount.Scale != settings.Scale || accounts[row.AccountKey] == "" || row.LocalTransactionID == "" || identities[row.SourceKey] != row.LocalTransactionID || ids[row.LocalTransactionID] {
			return invalid
		}
		if row.SourceKey != bankcsv.SourceKey(mapping, row.Row) || strings.TrimSpace(row.Merchant) != row.Merchant {
			return invalid
		}
		if _, err := domain.NormalizeDisplayLabel(row.Merchant); err != nil {
			return invalid
		}
		if _, err := domain.NormalizeDisplayLabel(row.Category); err != nil {
			return invalid
		}
		if _, err := domain.ParseDate(row.Date.String()); err != nil {
			return invalid
		}
		if !utf8.ValidString(row.Notes) {
			return invalid
		}
		for key, value := range row.Metadata {
			if !utf8.ValidString(key) || !utf8.ValidString(value) {
				return invalid
			}
		}
		transaction, live := transactions[row.LocalTransactionID]
		switch row.Disposition {
		case "present":
			if !live || transaction.Provider != "csv" || transaction.ProviderID != row.SourceKey || transaction.AccountID != accounts[row.AccountKey] || transaction.Date != row.Date || transaction.Amount != row.Amount {
				return invalid
			}
		case "superseded", "deleted":
			if live {
				return invalid
			}
		default:
			return invalid
		}
		if _, exists := rows[row.SourceKey]; exists {
			return invalid
		}
		rows[row.SourceKey], ids[row.LocalTransactionID] = row, true
	}
	for sourceKey, id := range identities {
		if rows[sourceKey].LocalTransactionID != id {
			return invalid
		}
	}
	seen := make(map[CSVFileRow]bool, len(state.Membership))
	for _, membership := range state.Membership {
		file, fileOK := files[membership.FileKey]
		row, rowOK := rows[membership.SourceKey]
		if !fileOK || !rowOK || file.AccountKey != row.AccountKey || row.Disposition == "superseded" || seen[membership] {
			return invalid
		}
		seen[membership] = true
	}
	for _, transaction := range committed.Transactions {
		if transaction.Provider != "csv" || !ids[transaction.ID] {
			return invalid
		}
	}
	return nil
}

func csvDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
