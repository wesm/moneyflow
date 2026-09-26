package app

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

// ImportBankCSV imports one parsed file against current durable state.
func (service *Service) ImportBankCSV(ctx context.Context, file bankcsv.File, mapping string, force bool) (store.CSVImportResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	result, err := ImportBankCSVProfile(ctx, service.profile, file, mapping, force)
	if err != nil {
		return result, err
	}
	return result, service.reloadExpected(ctx, result.Revision)
}

// ImportBankCSVProfile also supports first import before catalog publication.
func ImportBankCSVProfile(ctx context.Context, profile store.Profile, file bankcsv.File, mapping string, force bool) (store.CSVImportResult, error) {
	if profile == nil {
		return store.CSVImportResult{}, errors.New("CSV import requires a durable profile")
	}
	definition, err := bankcsv.Lookup(mapping)
	if err != nil {
		return store.CSVImportResult{}, err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	return profile.ApplyCSVImport(ctx, len(file.Rows), func(state store.CSVImportState, ids store.CSVProposedIDs) (store.CSVImportPlan, error) {
		return buildCSVImportPlan(state, ids, file, definition, force, now)
	})
}

func buildCSVImportPlan(state store.CSVImportState, ids store.CSVProposedIDs, file bankcsv.File, mapping bankcsv.Mapping, force bool, now time.Time) (store.CSVImportPlan, error) {
	var plan store.CSVImportPlan
	if state.CSV.Settings != nil && (state.CSV.Settings.Mapping != mapping.Name || state.CSV.Settings.Currency != mapping.Currency || state.CSV.Settings.Scale != mapping.Scale) {
		return plan, errors.New("CSV mapping does not match profile")
	}
	for _, prior := range state.CSV.Files {
		if prior.Key == file.Key && prior.Digest == file.Digest && prior.Skipped == 0 && !force {
			return store.CSVImportPlan{Result: store.CSVImportResult{Unchanged: true}}, nil
		}
	}
	if file.Records != len(file.Rows)+len(file.Skipped) {
		return plan, errors.New("CSV record counts disagree")
	}
	accountKey, err := domain.CollisionKey(file.Account)
	if err != nil || accountKey != file.AccountKey {
		return plan, errors.New("CSV account identity disagrees")
	}
	after := store.CSVImportState{Snapshot: state.Snapshot.Clone(), CSV: store.CSVState{Settings: state.CSV.Settings}}
	if after.CSV.Settings == nil {
		after.CSV.Settings = &store.CSVSettings{Mapping: mapping.Name, Currency: mapping.Currency, Scale: mapping.Scale, CreatedAt: now}
	}
	entities := newCSVEntities(after.Snapshot.Committed, state.Allocations, ids)
	accountID, err := entities.account(file.Account, file.AccountKey)
	if err != nil {
		return plan, err
	}
	transactions := make(map[domain.EntityID]domain.TransactionRecord, len(state.Snapshot.Committed.Transactions))
	for _, row := range state.Snapshot.Committed.Transactions {
		transactions[row.ID] = row
	}
	rows := make(map[string]store.CSVRow, len(state.CSV.Rows)+len(file.Rows))
	for _, row := range state.CSV.Rows {
		rows[row.SourceKey] = row
	}
	prior, other, incoming := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, member := range state.CSV.Membership {
		if member.FileKey == file.Key {
			prior[member.SourceKey] = true
		} else {
			other[member.SourceKey] = true
			after.CSV.Membership = append(after.CSV.Membership, member)
		}
	}
	result := store.CSVImportResult{Skipped: len(file.Skipped)}
	for i, source := range file.Rows {
		if incoming[source.SourceKey] || source.AccountKey != file.AccountKey || source.SourceKey != bankcsv.SourceKey(mapping, source) {
			return plan, errors.New("invalid CSV source identity")
		}
		incoming[source.SourceKey] = true
		saved, exists := rows[source.SourceKey]
		if exists {
			result.Duplicates++
			if saved.Disposition == "deleted" {
				continue
			}
			if !prior[source.SourceKey] && saved.Disposition != "superseded" {
				continue
			}
		} else {
			if i >= len(ids.Transactions) {
				return plan, errors.New("CSV transaction identities exhausted")
			}
			saved = store.CSVRow{Row: source, LocalTransactionID: ids.Transactions[i], Disposition: "present"}
			entities.identities["csv:transaction\x00"+source.SourceKey] = domain.ExternalIdentity{EntityType: domain.EntityKindTransaction, EntityID: saved.LocalTransactionID, Namespace: "csv:transaction", ExternalID: source.SourceKey}
			result.Inserted++
		}
		transaction, live := transactions[saved.LocalTransactionID]
		before := transaction
		if !live {
			transaction = domain.TransactionRecord{ID: saved.LocalTransactionID, Provider: "csv", ProviderID: source.SourceKey, AccountID: accountID, Date: source.Date, Amount: source.Amount}
		}
		if !saved.MerchantOverride || !live {
			transaction.MerchantID, err = entities.merchant(source.Merchant)
			if err != nil {
				return plan, err
			}
		}
		if !saved.CategoryOverride || !live {
			transaction.CategoryID, err = entities.category(source.Category)
			if err != nil {
				return plan, err
			}
		}
		transaction.Notes, transaction.Metadata = source.Notes, maps.Clone(source.Metadata)
		if saved.Disposition == "superseded" {
			result.Restored++
		} else if exists && !reflect.DeepEqual(before, transaction) {
			result.Updated++
		}
		saved.Row, saved.Disposition = source, "present"
		rows[source.SourceKey], transactions[saved.LocalTransactionID] = saved, transaction
	}
	if len(file.Skipped) != 0 {
		for key := range prior {
			incoming[key] = true
		}
	}
	replayed, err := Replay(state.Snapshot)
	if err != nil {
		return plan, err
	}
	protected := affectedTransactionIDs(replayed)
	for key := range prior {
		if incoming[key] || other[key] {
			continue
		}
		saved := rows[key]
		if saved.Disposition != "present" {
			continue
		}
		transaction := transactions[saved.LocalTransactionID]
		_, pending := protected[saved.LocalTransactionID]
		if saved.MerchantOverride || saved.CategoryOverride || transaction.Hidden || pending {
			result.Retained++
			continue
		}
		delete(transactions, saved.LocalTransactionID)
		saved.Disposition = "superseded"
		rows[key] = saved
		result.Superseded++
	}
	for key := range incoming {
		after.CSV.Membership = append(after.CSV.Membership, store.CSVFileRow{FileKey: file.Key, SourceKey: key})
	}
	for _, row := range rows {
		after.CSV.Rows = append(after.CSV.Rows, row)
	}
	for _, previous := range state.CSV.Files {
		if previous.Key != file.Key {
			after.CSV.Files = append(after.CSV.Files, previous)
		}
	}
	after.CSV.Files = append(after.CSV.Files, store.CSVFile{Key: file.Key, AccountKey: file.AccountKey, Digest: file.Digest, CompletedAt: now, Imported: result.Inserted, Duplicates: result.Duplicates, Skipped: result.Skipped})
	slices.SortFunc(after.CSV.Files, func(a, b store.CSVFile) int { return strings.Compare(a.Key, b.Key) })
	slices.SortFunc(after.CSV.Rows, func(a, b store.CSVRow) int { return strings.Compare(a.SourceKey, b.SourceKey) })
	slices.SortFunc(after.CSV.Membership, func(a, b store.CSVFileRow) int {
		if c := strings.Compare(a.FileKey, b.FileKey); c != 0 {
			return c
		}
		return strings.Compare(a.SourceKey, b.SourceKey)
	})
	after.Snapshot.Committed, after.Allocations = entities.finish(transactions)
	rebased, err := profilereplay.RebaseProviderJournal(state.Snapshot.Committed, after.Snapshot.Committed, state.Snapshot.Journal, state.Snapshot.Cursor)
	if err != nil {
		return plan, err
	}
	if rebased.Summary.RemovedOperations != 0 || rebased.Summary.RemovedTargets != 0 {
		return plan, errors.New("CSV import would remove an active edit; resolve pending changes first")
	}
	after.Snapshot.Journal, after.Snapshot.Cursor = rebased.Journal, rebased.Cursor
	result.DiscardedRedo = rebased.Summary.DiscardedRedoOperations
	return store.CSVImportPlan{State: after, Result: result}, nil
}

type csvEntities struct {
	profile                        domain.CommittedProfile
	ids                            store.CSVProposedIDs
	accounts                       map[string]domain.EntityID
	merchants                      map[domain.EntityID]domain.Merchant
	categories                     map[domain.EntityID]domain.Category
	identities                     map[string]domain.ExternalIdentity
	allocations                    map[string]store.LabelAllocation
	merchantLabels, categoryLabels map[string]struct{}
}

func newCSVEntities(profile domain.CommittedProfile, allocations []store.LabelAllocation, ids store.CSVProposedIDs) *csvEntities {
	e := &csvEntities{profile: profile, ids: ids, accounts: map[string]domain.EntityID{}, merchants: map[domain.EntityID]domain.Merchant{}, categories: map[domain.EntityID]domain.Category{}, identities: map[string]domain.ExternalIdentity{}, allocations: map[string]store.LabelAllocation{}, merchantLabels: map[string]struct{}{}, categoryLabels: map[string]struct{}{}}
	for _, a := range profile.Accounts {
		if !a.Retired {
			e.accounts[a.CollisionKey] = a.ID
		}
	}
	for _, m := range profile.Merchants {
		e.merchants[m.ID] = m
		if !m.Retired {
			e.merchantLabels[m.CollisionKey] = struct{}{}
		}
	}
	for _, c := range profile.Categories {
		e.categories[c.ID] = c
		if !c.Retired {
			e.categoryLabels[c.CollisionKey] = struct{}{}
		}
	}
	for _, i := range profile.ExternalIdentities {
		e.identities[i.Namespace+"\x00"+i.ExternalID] = i
	}
	for _, a := range allocations {
		e.allocations[a.Namespace+"\x00"+a.ExternalID] = a
	}
	return e
}

func (e *csvEntities) account(label, key string) (domain.EntityID, error) {
	if id := e.accounts[key]; id != "" {
		return id, nil
	}
	if len(e.ids.Accounts) == 0 {
		return "", errors.New("CSV account identities exhausted")
	}
	id := e.ids.Accounts[0]
	e.ids.Accounts = e.ids.Accounts[1:]
	e.profile.Accounts = append(e.profile.Accounts, domain.Account{ID: id, Label: label, CollisionKey: key})
	e.accounts[key] = id
	e.identities["csv:account\x00"+key] = domain.ExternalIdentity{EntityType: domain.EntityKindAccount, EntityID: id, Namespace: "csv:account", ExternalID: key}
	return id, nil
}

func (e *csvEntities) merchant(label string) (domain.EntityID, error) {
	key, err := domain.CollisionKey(label)
	if err != nil {
		return "", err
	}
	if identity, ok := e.identities["csv:merchant\x00"+key]; ok {
		id := identity.EntityID
		for range len(e.merchants) + 1 {
			m, ok := e.merchants[id]
			if !ok {
				break
			}
			if !m.Retired {
				return id, nil
			}
			if m.MergeDestination == nil {
				break
			}
			id = *m.MergeDestination
		}
		return "", errors.New("CSV merchant alias has no active destination")
	}
	if len(e.ids.Merchants) == 0 {
		return "", errors.New("CSV merchant identities exhausted")
	}
	id := e.ids.Merchants[0]
	e.ids.Merchants = e.ids.Merchants[1:]
	label, collision, _, err := allocateAmazonLabel(domain.EntityKindMerchant, "csv:merchant", key, label, id, e.merchantLabels, e.allocations)
	if err != nil {
		return "", err
	}
	e.merchantLabels[collision] = struct{}{}
	e.merchants[id] = domain.Merchant{ID: id, Label: label, CollisionKey: collision}
	e.identities["csv:merchant\x00"+key] = domain.ExternalIdentity{EntityType: domain.EntityKindMerchant, EntityID: id, Namespace: "csv:merchant", ExternalID: key}
	return id, nil
}

func (e *csvEntities) category(label string) (domain.EntityID, error) {
	key, err := domain.CollisionKey(label)
	if err != nil {
		return "", err
	}
	if key == domain.UncategorizedCollisionKey {
		return domain.UncategorizedCategoryID, nil
	}
	if identity, ok := e.identities["csv:category\x00"+key]; ok {
		id := identity.EntityID
		for range len(e.categories) + 1 {
			c, ok := e.categories[id]
			if !ok {
				break
			}
			if !c.Retired {
				return id, nil
			}
			if c.MergeDestination == nil {
				break
			}
			id = *c.MergeDestination
		}
		return domain.UncategorizedCategoryID, nil
	}
	if len(e.ids.Categories) == 0 {
		return "", errors.New("CSV category identities exhausted")
	}
	id := e.ids.Categories[0]
	e.ids.Categories = e.ids.Categories[1:]
	label, collision, _, err := allocateAmazonLabel(domain.EntityKindCategory, "csv:category", key, label, id, e.categoryLabels, e.allocations)
	if err != nil {
		return "", err
	}
	e.categoryLabels[collision] = struct{}{}
	e.categories[id] = domain.Category{ID: id, Label: label, CollisionKey: collision, GroupID: domain.UncategorizedGroupID}
	e.identities["csv:category\x00"+key] = domain.ExternalIdentity{EntityType: domain.EntityKindCategory, EntityID: id, Namespace: "csv:category", ExternalID: key}
	return id, nil
}

func (e *csvEntities) finish(transactions map[domain.EntityID]domain.TransactionRecord) (domain.CommittedProfile, []store.LabelAllocation) {
	e.profile.Merchants = amazonMerchantValues(e.merchants)
	e.profile.Categories = slices.Collect(maps.Values(e.categories))
	slices.SortFunc(e.profile.Categories, func(a, b domain.Category) int { return strings.Compare(string(a.ID), string(b.ID)) })
	slices.SortFunc(e.profile.Accounts, func(a, b domain.Account) int { return strings.Compare(string(a.ID), string(b.ID)) })
	e.profile.Transactions = amazonTransactionValues(transactions)
	e.profile.ExternalIdentities = amazonIdentityValues(e.identities)
	return e.profile, amazonAllocationValues(e.allocations)
}
