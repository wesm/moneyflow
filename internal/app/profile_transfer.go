package app

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// TransferSummary reports comparison counts and exact totals without individual rows.
type TransferSummary struct {
	Counts     map[string]int
	Hidden     int
	Statistics []domain.CurrencyStats
}

// SummarizeProfileTransfer uses ordinary application analytics on committed rows.
func SummarizeProfileTransfer(state store.ProfileTransfer) (TransferSummary, error) {
	transactions, err := state.Snapshot.Committed.MaterializeTransactions()
	if err != nil {
		return TransferSummary{}, store.NewError(store.CodeInvalidOperation, err)
	}
	service, err := NewService(transactions)
	if err != nil {
		return TransferSummary{}, store.NewError(store.CodeInvalidOperation, err)
	}
	return transferSummary(state, service)
}

// VerifyProfileTransfer reloads through the ordinary service before catalog publication.
func VerifyProfileTransfer(ctx context.Context, profile store.Profile, expected TransferSummary) error {
	service, err := NewProfileService(ctx, profile)
	if err != nil {
		return err
	}
	state, err := profile.LoadProfileTransfer(ctx)
	if err != nil {
		return err
	}
	actual, err := transferSummary(state, service)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, actual) {
		return errors.New("profile import: saved counts or exact totals do not match")
	}
	return nil
}

func transferSummary(state store.ProfileTransfer, service *Service) (TransferSummary, error) {
	p := state.Snapshot.Committed
	summary := TransferSummary{Counts: map[string]int{
		"account": len(p.Accounts), "merchant": len(p.Merchants), "group": len(p.Groups), "category": len(p.Categories),
		"transaction": len(p.Transactions), "external_identity": len(p.ExternalIdentities), "known_drill": len(state.Snapshot.KnownDrills),
		"provider_binding": 0, "label_allocation": len(state.Provider.Allocations), "provider_lineage": len(state.Provider.Lineage),
		"write_restriction": len(state.Provider.WriteRestrictions), "ynab_split": len(state.YNABSplits), "amazon_settings": 0, "amazon_item": len(state.AmazonItems),
	}}
	if state.Provider.Binding != nil {
		summary.Counts["provider_binding"] = 1
	}
	if state.AmazonSettings != nil {
		summary.Counts["amazon_settings"] = 1
	}
	if state.CSV.Settings != nil {
		summary.Counts["csv_settings"] = 1
	}
	for kind, count := range map[string]int{"csv_file": len(state.CSV.Files), "csv_row": len(state.CSV.Rows), "csv_file_row": len(state.CSV.Membership)} {
		if count != 0 {
			summary.Counts[kind] = count
		}
	}
	for _, transaction := range p.Transactions {
		if transaction.Hidden {
			summary.Hidden++
		}
	}
	session := NewSession()
	session.ShowHidden, session.ShowTransfers = true, true
	result, err := service.Query(session)
	if err != nil {
		return TransferSummary{}, err
	}
	summary.Statistics = result.Statistics
	return summary, nil
}

// PrepareProfileTransfer checks eligibility before excluding redo and runtime state.
// It does not change the source profile or its detached input.
func PrepareProfileTransfer(state store.ProfileTransfer, now time.Time) (store.ProfileTransfer, int, error) {
	if state.Snapshot.Cursor != 0 {
		return store.ProfileTransfer{}, 0, errors.New("profile export: commit or undo active edits first")
	}
	if state.Provider.Write != nil {
		return store.ProfileTransfer{}, 0, errors.New("profile export: finish or reconcile the provider write batch first")
	}
	if state.Provider.Lease != nil && state.Provider.Lease.ExpiresAt.After(now) {
		return store.ProfileTransfer{}, 0, errors.New("profile export: wait for the provider operation to finish")
	}
	excluded := len(state.Snapshot.Journal)
	state.Snapshot = state.Snapshot.Clone()
	state.Snapshot.Journal = nil
	state.Provider = state.Provider.Clone()
	state.Provider.Lease = nil
	state.Provider.Refresh = store.RefreshState{}
	state.Provider.LastWrite = store.LastWriteSummary{}
	if state.AmazonSettings != nil {
		settings := *state.AmazonSettings
		settings.TaxonomySourceProfileID = ""
		state.AmazonSettings = &settings
	}
	return state, excluded, nil
}

// ValidateProfileTransfer checks shared profile invariants without exposing row values.
// SQLite checks auxiliary row constraints during atomic installation; split
// totals need an aggregate check here on both export and import.
func ValidateProfileTransfer(state store.ProfileTransfer, providerKind string) error {
	switch providerKind {
	case "local", "monarch", "ynab", "simplefin", "amazon", "csv":
	default:
		return errors.New("profile transfer: unsupported provider kind")
	}
	if state.Snapshot.Cursor != 0 || len(state.Snapshot.Journal) != 0 {
		return errors.New("profile transfer: pending operations are not transferable")
	}
	if err := state.Snapshot.Validate(); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if (providerKind == "csv") != (state.CSV.Settings != nil) {
		return errors.New("profile transfer: CSV settings do not match header")
	}
	if err := store.ValidateCSVState(state.CSV, state.Snapshot.Committed); err != nil {
		return err
	}
	if providerKind == "csv" && (state.Provider.Binding != nil || len(state.Provider.Lineage) != 0 || len(state.Provider.WriteRestrictions) != 0) {
		return errors.New("profile transfer: CSV profiles cannot contain remote state")
	}
	if state.Provider.Binding != nil && state.Provider.Binding.Kind != providerKind {
		return errors.New("profile transfer: provider binding does not match header")
	}
	if (state.AmazonSettings != nil || len(state.AmazonItems) != 0) && providerKind != "amazon" {
		return errors.New("profile transfer: Amazon records require an Amazon profile")
	}
	if len(state.YNABSplits) != 0 && providerKind != "ynab" {
		return errors.New("profile transfer: split records require a YNAB profile")
	}
	if len(state.YNABSplits) != 0 {
		binding := state.Provider.Binding
		if binding == nil {
			return errors.New("profile transfer: splits require provider binding")
		}
		parents := make(map[domain.EntityID]domain.Money, len(state.Snapshot.Committed.Transactions))
		for _, transaction := range state.Snapshot.Committed.Transactions {
			parents[transaction.ID] = transaction.Amount
		}
		totals := make(map[domain.EntityID]domain.Money)
		for _, split := range state.YNABSplits {
			total, exists := totals[split.ParentTransactionID]
			if !exists {
				total = domain.Money{Currency: binding.Currency, Scale: binding.Scale}
			}
			total, err := total.Add(domain.Money{Minor: split.AmountMinor, Currency: binding.Currency, Scale: binding.Scale})
			if err != nil {
				return errors.New("profile transfer: split total overflows")
			}
			totals[split.ParentTransactionID] = total
		}
		for parent, total := range totals {
			if total != parents[parent] {
				return errors.New("profile transfer: split total does not match parent")
			}
		}
	}
	return nil
}
