package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

// ApplyProviderRefresh validates and persists one complete provider refresh atomically.
func (profile *profile) ApplyProviderRefresh(
	ctx context.Context,
	request store.AtomicRefreshRequest,
	planner store.RefreshPlanner,
) (store.RefreshCommit, error) {
	if err := validateAtomicRefreshRequest(request, planner); err != nil {
		return store.RefreshCommit{}, store.NewError(store.CodeInvalidOperation, err)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	defer func() { _ = finish(false) }()

	refresh, err := loadRefreshState(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if refresh.Generation != request.ExpectedGeneration {
		return store.RefreshCommit{}, refreshGenerationConflict(
			request.ExpectedGeneration,
			refresh.Generation,
		)
	}
	lease, err := loadRefreshLease(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if lease == nil || lease.OwnerID != request.LeaseOwnerID ||
		!lease.ExpiresAt.After(request.ObservedAt) {
		return store.RefreshCommit{}, store.NewError(
			store.CodeRevisionConflict,
			errors.New("refresh owner no longer holds the lease"),
		)
	}
	snapshot, err := loadSnapshot(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if err = snapshot.Validate(); err != nil {
		return store.RefreshCommit{}, store.NewError(store.CodeStoreCorrupt, err)
	}
	binding, err := loadProviderBinding(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	binding, err = resolveRefreshBinding(ctx, connection, binding, request.Binding)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	allocations, err := loadLabelAllocations(ctx, connection)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	inputs := store.RefreshInputs{
		Snapshot: snapshot.Clone(), Binding: cloneProviderBinding(binding), Refresh: refresh,
		Allocations: append([]store.LabelAllocation(nil), allocations...),
		Candidate:   request.Candidate.Clone(), ProposedIDs: cloneEntityIDMap(request.ProposedIDs),
		ProposedSuffixes: cloneStringMap(request.ProposedSuffixes), ObservedAt: request.ObservedAt,
	}
	plan, err := planner(inputs)
	if err != nil {
		return store.RefreshCommit{}, store.NewError(store.CodeInvalidOperation, err)
	}
	plan = cloneRefreshPlan(plan)
	if err = validateRefreshPlan(
		snapshot, binding, allocations, request.Candidate, request.ProposedIDs, plan,
	); err != nil {
		return store.RefreshCommit{}, store.NewError(store.CodeInvalidOperation, err)
	}

	if err = applyProviderCommitted(
		ctx, connection, snapshot.Committed, plan.Committed,
		snapshot.KnownDrills, plan.KnownDrills,
	); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = replaceRefreshJournal(ctx, connection, snapshot.Journal, plan.Journal); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = replaceLabelAllocations(ctx, connection, allocations, plan.Allocations); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = persistRefreshBinding(ctx, connection, binding); err != nil {
		return store.RefreshCommit{}, err
	}
	nextRevision, err := incrementRevision(snapshot.Revision)
	if err != nil {
		return store.RefreshCommit{}, err
	}
	if refresh.Generation >= math.MaxInt64 {
		return store.RefreshCommit{}, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("refresh generation is exhausted"),
		)
	}
	nextGeneration := refresh.Generation + 1
	if err = updateJournalState(
		ctx, connection, snapshot.Revision, nextRevision, plan.Cursor,
	); err != nil {
		return store.RefreshCommit{}, err
	}
	if err = updateRefreshSuccess(
		ctx, connection, refresh.Generation, nextGeneration, request.ObservedAt, plan.Summary,
	); err != nil {
		return store.RefreshCommit{}, err
	}
	if _, err = connection.ExecContext(ctx, `
		DELETE FROM provider_refresh_lease WHERE singleton = 1 AND owner_id = ?`,
		request.LeaseOwnerID,
	); err != nil {
		return store.RefreshCommit{}, mapDriverError(err, store.CodeStoreError)
	}
	if err = finish(true); err != nil {
		return store.RefreshCommit{}, err
	}
	return store.RefreshCommit{
		Revision: nextRevision, Generation: nextGeneration, Summary: plan.Summary,
	}, nil
}

func validateAtomicRefreshRequest(
	request store.AtomicRefreshRequest,
	planner store.RefreshPlanner,
) error {
	if planner == nil {
		return errors.New("refresh planner is nil")
	}
	if err := validateLeaseOwner(request.LeaseOwnerID); err != nil {
		return err
	}
	if err := validateMillisecondTime("refresh observation", request.ObservedAt); err != nil {
		return err
	}
	if err := request.Candidate.Validate(); err != nil {
		return err
	}
	if !request.Candidate.ObservedAt.Equal(request.ObservedAt) {
		return errors.New("refresh candidate observation does not match request")
	}
	for key, value := range request.ProposedIDs {
		if key == "" || value == "" {
			return errors.New("refresh proposed ID is incomplete")
		}
	}
	for key, value := range request.ProposedSuffixes {
		if key == "" || value == "" {
			return errors.New("refresh proposed suffix is incomplete")
		}
	}
	if request.Binding != nil {
		if err := validateProviderBinding(*request.Binding); err != nil {
			return err
		}
	}
	return nil
}

func resolveRefreshBinding(
	ctx context.Context,
	connection *sql.Conn,
	current, requested *store.ProviderBinding,
) (*store.ProviderBinding, error) {
	if current != nil {
		if requested != nil && !reflect.DeepEqual(*current, *requested) {
			return nil, store.NewError(
				store.CodeInvalidOperation,
				errors.New("refresh cannot replace provider binding"),
			)
		}
		return current, nil
	}
	if requested == nil {
		return nil, store.NewError(
			store.CodeInvalidOperation,
			errors.New("initial refresh requires provider binding"),
		)
	}
	populated, err := profilePopulated(ctx, connection)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	if populated {
		return nil, store.NewError(
			store.CodeInvalidOperation,
			errors.New("provider binding requires a pristine profile"),
		)
	}
	return cloneProviderBinding(requested), nil
}

func validateProviderBinding(binding store.ProviderBinding) error {
	if binding.Kind == "" || strings.TrimSpace(binding.Kind) != binding.Kind ||
		binding.Namespace == "" || strings.TrimSpace(binding.Namespace) != binding.Namespace ||
		binding.RemoteProfileID == "" || strings.TrimSpace(binding.RemoteProfileID) != binding.RemoteProfileID {
		return errors.New("provider binding is incomplete")
	}
	return validateMillisecondTime("provider binding time", binding.BoundAt)
}

func validateRefreshPlan(
	snapshot domain.ProfileSnapshot,
	binding *store.ProviderBinding,
	allocations []store.LabelAllocation,
	candidate domain.ImportSnapshot,
	proposedIDs map[string]domain.EntityID,
	plan store.RefreshPlan,
) error {
	if plan.Cursor != len(plan.Journal) {
		return errors.New("refresh plan retains an inactive redo tail")
	}
	plannedSnapshot := domain.ProfileSnapshot{
		Revision: snapshot.Revision, Cursor: plan.Cursor, Committed: plan.Committed,
		Journal: plan.Journal, KnownDrills: plan.KnownDrills,
	}
	targetCount := 0
	for _, operation := range plan.Journal {
		targetCount += len(operation.Targets)
	}
	if len(plan.Journal) > maxJournalOperations || targetCount > maxJournalTargets {
		return errJournalFull
	}
	replayed, err := profilereplay.Replay(plannedSnapshot)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan.Effective, replayed.Effective) {
		return errors.New("refresh effective state does not match authoritative replay")
	}
	if err = requireKnownDrillSuperset(snapshot.KnownDrills, plan.KnownDrills); err != nil {
		return err
	}
	if err = requireDurableEntityIDs(snapshot.Committed, plan.Committed); err != nil {
		return err
	}
	identityIndex, err := externalIdentitySupersetIndex(
		snapshot.Committed.ExternalIdentities,
		plan.Committed.ExternalIdentities,
	)
	if err != nil {
		return err
	}
	if err = requireAllocationSuperset(allocations, plan.Allocations); err != nil {
		return err
	}
	if err = validateProposedExternalIdentities(
		snapshot.Committed, plan.Committed.ExternalIdentities, proposedIDs,
	); err != nil {
		return err
	}
	if err = validateRefreshJournalRewrite(snapshot, plan); err != nil {
		return err
	}
	if err = validateCandidateMaterialization(
		binding,
		candidate,
		plan.Committed,
		plan.Allocations,
		identityIndex,
	); err != nil {
		return err
	}
	if err = validateRefreshSummary(plan.Summary); err != nil {
		return err
	}
	if plan.Summary.ImportedAccounts != len(candidate.Accounts) ||
		plan.Summary.ImportedMerchants != len(candidate.Merchants) ||
		plan.Summary.ImportedGroups != len(candidate.Groups) ||
		plan.Summary.ImportedCategories != len(candidate.Categories) ||
		plan.Summary.ImportedTransactions != len(candidate.Transactions) {
		return errors.New("refresh imported counts do not match candidate")
	}
	return validateLabelAllocations(plan.Allocations)
}

func requireDurableEntityIDs(before, after domain.CommittedProfile) error {
	checks := []struct {
		name   string
		before map[domain.EntityID]struct{}
		after  map[domain.EntityID]struct{}
	}{
		{"account", accountIDs(before.Accounts), accountIDs(after.Accounts)},
		{"merchant", merchantIDs(before.Merchants), merchantIDs(after.Merchants)},
		{"group", groupIDs(before.Groups), groupIDs(after.Groups)},
		{"category", categoryIDs(before.Categories), categoryIDs(after.Categories)},
	}
	for _, check := range checks {
		for id := range check.before {
			if _, exists := check.after[id]; !exists {
				return errors.New("refresh cannot remove a durable " + check.name + " identity")
			}
		}
	}
	return nil
}

type externalIdentityIndexKey struct{ namespace, externalID string }

func externalIdentitySupersetIndex(
	before,
	after []domain.ExternalIdentity,
) (map[externalIdentityIndexKey]domain.ExternalIdentity, error) {
	retained := make(map[externalIdentityIndexKey]domain.ExternalIdentity, len(after))
	for _, identity := range after {
		retained[externalIdentityIndexKey{identity.Namespace, identity.ExternalID}] = identity
	}
	for _, identity := range before {
		lookup := externalIdentityIndexKey{identity.Namespace, identity.ExternalID}
		if next, exists := retained[lookup]; !exists || next != identity {
			return nil, errors.New("refresh cannot remove or remap a durable external identity")
		}
	}
	return retained, nil
}

func requireAllocationSuperset(before, after []store.LabelAllocation) error {
	type key struct{ namespace, externalID string }
	retained := make(map[key]struct{}, len(after))
	for _, allocation := range after {
		retained[key{allocation.Namespace, allocation.ExternalID}] = struct{}{}
	}
	for _, allocation := range before {
		if _, exists := retained[key{allocation.Namespace, allocation.ExternalID}]; !exists {
			return errors.New("refresh cannot remove a durable label allocation")
		}
	}
	return nil
}

func validateRefreshJournalRewrite(
	snapshot domain.ProfileSnapshot,
	plan store.RefreshPlan,
) error {
	if snapshot.Cursor == 0 {
		if plan.Cursor != 0 || len(plan.Journal) != 0 {
			return errors.New("refresh journal does not discard the inactive redo tail")
		}
		return nil
	}
	expected, err := profilereplay.RebaseProviderJournal(
		snapshot.Committed, plan.Committed, snapshot.Journal, snapshot.Cursor,
	)
	if err != nil {
		return err
	}
	if plan.Cursor != expected.Cursor || !reflect.DeepEqual(plan.Journal, expected.Journal) {
		return errors.New("refresh journal does not match authoritative rebase")
	}
	return nil
}

func validateProposedExternalIdentities(
	before domain.CommittedProfile,
	after []domain.ExternalIdentity,
	proposed map[string]domain.EntityID,
) error {
	existing := make(map[externalIdentityIndexKey]struct{}, len(before.ExternalIdentities))
	reserved := make(map[domain.EntityID]struct{})
	for _, identity := range before.ExternalIdentities {
		existing[externalIdentityIndexKey{identity.Namespace, identity.ExternalID}] = struct{}{}
	}
	for _, batch := range [][]domain.EntityID{
		entityIDs(before.Accounts, func(value domain.Account) domain.EntityID { return value.ID }),
		entityIDs(before.Merchants, func(value domain.Merchant) domain.EntityID { return value.ID }),
		entityIDs(before.Groups, func(value domain.CategoryGroup) domain.EntityID { return value.ID }),
		entityIDs(before.Categories, func(value domain.Category) domain.EntityID { return value.ID }),
		entityIDs(before.Transactions, func(value domain.TransactionRecord) domain.EntityID { return value.ID }),
	} {
		for _, id := range batch {
			reserved[id] = struct{}{}
		}
	}
	used := make(map[domain.EntityID]struct{})
	for _, identity := range after {
		lookup := externalIdentityIndexKey{identity.Namespace, identity.ExternalID}
		if _, existed := existing[lookup]; existed {
			continue
		}
		providerName, kindText, separator := strings.Cut(identity.Namespace, "/")
		if !separator || providerName == "" || kindText != string(identity.EntityType) {
			return errors.New("refresh new external identity has an invalid namespace")
		}
		key := providerName + "/" + kindText + "\x00" + identity.ExternalID
		if expected, supplied := proposed[key]; supplied && expected != identity.EntityID {
			return errors.New("refresh new external identity does not use its proposed local ID")
		}
		if _, exists := reserved[identity.EntityID]; exists {
			return errors.New("refresh new external identity reuses a durable local ID")
		}
		if _, exists := used[identity.EntityID]; exists {
			return errors.New("refresh new external identities share a local ID")
		}
		used[identity.EntityID] = struct{}{}
	}
	return nil
}

func entityIDs[T any](values []T, id func(T) domain.EntityID) []domain.EntityID {
	result := make([]domain.EntityID, len(values))
	for index, value := range values {
		result[index] = id(value)
	}
	return result
}

func validateCandidateMaterialization(
	binding *store.ProviderBinding,
	candidate domain.ImportSnapshot,
	committed domain.CommittedProfile,
	allocations []store.LabelAllocation,
	identities map[externalIdentityIndexKey]domain.ExternalIdentity,
) error {
	if binding == nil {
		return errors.New("refresh candidate has no provider binding")
	}
	allocationKeys := make(map[string]domain.EntityKind, len(allocations))
	for _, allocation := range allocations {
		allocationKeys[allocation.Namespace+"\x00"+allocation.ExternalID] = allocation.Kind
	}
	resolve := func(kind domain.EntityKind, externalID string) (domain.EntityID, error) {
		identity, exists := identities[externalIdentityIndexKey{
			binding.Kind + "/" + string(kind),
			externalID,
		}]
		if !exists || identity.EntityType != kind {
			return "", errors.New("refresh candidate identity is not materialized")
		}
		return identity.EntityID, nil
	}
	accounts := entityPresence(committed.Accounts, func(value domain.Account) (domain.EntityID, bool) {
		return value.ID, value.Retired
	})
	merchants := entityPresence(committed.Merchants, func(value domain.Merchant) (domain.EntityID, bool) {
		return value.ID, value.Retired
	})
	groups := entityPresence(committed.Groups, func(value domain.CategoryGroup) (domain.EntityID, bool) {
		return value.ID, value.Retired
	})
	categories := make(map[domain.EntityID]domain.Category, len(committed.Categories))
	for _, value := range committed.Categories {
		categories[value.ID] = value
	}
	for _, batch := range []struct {
		kind     domain.EntityKind
		entities []domain.ImportEntity
		present  map[domain.EntityID]bool
	}{
		{domain.EntityKindAccount, candidate.Accounts, accounts},
		{domain.EntityKindMerchant, candidate.Merchants, merchants},
		{domain.EntityKindGroup, candidate.Groups, groups},
	} {
		for _, imported := range batch.entities {
			if allocationKeys[binding.Kind+"/"+string(batch.kind)+"\x00"+imported.ExternalID] != batch.kind {
				return errors.New("refresh candidate label allocation is not materialized")
			}
			id, err := resolve(batch.kind, imported.ExternalID)
			if err != nil || !batch.present[id] {
				return errors.New("refresh candidate dimension is not active")
			}
		}
	}
	for _, imported := range candidate.Categories {
		if allocationKeys[binding.Kind+"/category\x00"+imported.ExternalID] != domain.EntityKindCategory {
			return errors.New("refresh candidate label allocation is not materialized")
		}
		id, err := resolve(domain.EntityKindCategory, imported.ExternalID)
		if err != nil {
			return err
		}
		parentID, err := resolve(domain.EntityKindGroup, imported.ParentExternalID)
		category, exists := categories[id]
		if err != nil || !exists || category.Retired || category.GroupID != parentID {
			return errors.New("refresh candidate category is not materialized")
		}
	}
	expectedTransactions := make(
		map[domain.EntityID]domain.TransactionRecord,
		len(candidate.Transactions),
	)
	for _, imported := range candidate.Transactions {
		id, err := resolve(domain.EntityKindTransaction, imported.ExternalID)
		if err != nil {
			return err
		}
		accountID, accountErr := resolve(domain.EntityKindAccount, imported.AccountExternalID)
		merchantID, merchantErr := resolve(domain.EntityKindMerchant, imported.MerchantExternalID)
		categoryID, categoryErr := resolve(domain.EntityKindCategory, imported.CategoryExternalID)
		if accountErr != nil || merchantErr != nil || categoryErr != nil {
			return errors.New("refresh candidate transaction is not materialized")
		}
		expectedTransactions[id] = domain.TransactionRecord{
			ID: id, Provider: binding.Kind, ProviderID: imported.ExternalID,
			AccountID: accountID, MerchantID: merchantID, CategoryID: categoryID,
			Date: imported.Date, Amount: imported.Amount, Notes: imported.Notes,
			Hidden: imported.Hidden, Pending: imported.Pending,
		}
	}
	for _, transaction := range committed.Transactions {
		expected, needed := expectedTransactions[transaction.ID]
		if !needed {
			continue
		}
		transaction.Metadata = nil
		if !reflect.DeepEqual(transaction, expected) {
			return errors.New("refresh candidate transaction is not materialized")
		}
		delete(expectedTransactions, transaction.ID)
	}
	if len(expectedTransactions) != 0 {
		return errors.New("refresh candidate transaction is not materialized")
	}
	return nil
}

func entityPresence[T any](values []T, fields func(T) (domain.EntityID, bool)) map[domain.EntityID]bool {
	result := make(map[domain.EntityID]bool, len(values))
	for _, value := range values {
		id, retired := fields(value)
		result[id] = !retired
	}
	return result
}

func requireKnownDrillSuperset(before, after []domain.DrillIdentity) error {
	known := make(map[string]struct{}, len(after))
	for _, identity := range after {
		key, _ := identity.CanonicalKey()
		known[key] = struct{}{}
	}
	for _, identity := range before {
		key, _ := identity.CanonicalKey()
		if _, exists := known[key]; !exists {
			return errors.New("refresh cannot remove a known drill identity")
		}
	}
	return nil
}

func validateRefreshSummary(summary store.RefreshSummary) error {
	for _, value := range []int{
		summary.ImportedAccounts, summary.ImportedMerchants, summary.ImportedGroups,
		summary.ImportedCategories, summary.ImportedTransactions, summary.RemovedTransactions,
		summary.RemovedOperations, summary.RemovedTargets, summary.RetainedOperations,
		summary.RebasedHideTargets, summary.DiscardedRedoOperations,
	} {
		if value < 0 {
			return errors.New("refresh summary contains a negative count")
		}
	}
	return nil
}

func validateLabelAllocations(allocations []store.LabelAllocation) error {
	seen := make(map[string]struct{}, len(allocations))
	unsuffixed := make(map[string]struct{}, len(allocations))
	previous := ""
	for _, allocation := range allocations {
		if allocation.Kind != domain.EntityKindAccount &&
			allocation.Kind != domain.EntityKindMerchant &&
			allocation.Kind != domain.EntityKindGroup &&
			allocation.Kind != domain.EntityKindCategory {
			return errors.New("refresh label allocation kind is invalid")
		}
		if allocation.Namespace == "" || allocation.ExternalID == "" ||
			allocation.BaseCollisionKey == "" || allocation.DisplayLabel == "" ||
			allocation.Unsuffixed == (allocation.SuffixToken != "") {
			return errors.New("refresh label allocation is invalid")
		}
		key := allocation.Namespace + "\x00" + allocation.ExternalID
		if _, exists := seen[key]; exists || (previous != "" && key <= previous) {
			return errors.New("refresh label allocations are not strictly sorted and unique")
		}
		seen[key] = struct{}{}
		previous = key
		if allocation.Unsuffixed {
			ownerKey := string(allocation.Kind) + "\x00" + allocation.BaseCollisionKey
			if _, exists := unsuffixed[ownerKey]; exists {
				return errors.New("refresh label allocation has duplicate unsuffixed owner")
			}
			unsuffixed[ownerKey] = struct{}{}
		}
	}
	return nil
}

func applyProviderCommitted(
	ctx context.Context,
	connection *sql.Conn,
	before, after domain.CommittedProfile,
	beforeDrills, afterDrills []domain.DrillIdentity,
) error {
	if reflect.DeepEqual(before, after) && reflect.DeepEqual(beforeDrills, afterDrills) {
		return nil
	}
	if err := releaseProviderCollisionKeys(ctx, connection, before, after); err != nil {
		return err
	}
	seedStatements, err := prepareSeedStatements(ctx, connection)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer seedStatements.close()
	foldStatements, err := prepareFoldStatements(ctx, connection)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer foldStatements.close()
	if err = upsertProviderAccounts(ctx, connection, seedStatements, before.Accounts, after.Accounts); err != nil {
		return err
	}
	if err = foldGroups(ctx, foldStatements, before.Groups, after.Groups); err != nil {
		return err
	}
	if err = foldMerchants(ctx, foldStatements, before.Merchants, after.Merchants); err != nil {
		return err
	}
	if err = foldCategories(ctx, foldStatements, before.Categories, after.Categories); err != nil {
		return err
	}
	if err = upsertProviderTransactions(
		ctx, connection, seedStatements, foldStatements, before.Transactions, after.Transactions,
	); err != nil {
		return err
	}
	if err = replaceExternalIdentitiesIfChanged(
		ctx, connection, seedStatements, before.ExternalIdentities, after.ExternalIdentities,
	); err != nil {
		return err
	}
	if err = deleteRemovedProviderEntities(ctx, connection, before, after); err != nil {
		return err
	}
	return insertFoldKnownDrills(ctx, connection, beforeDrills, afterDrills)
}

func releaseProviderCollisionKeys(
	ctx context.Context,
	connection *sql.Conn,
	before, after domain.CommittedProfile,
) error {
	if err := releaseFoldCollisionKeys(ctx, connection, before, after); err != nil {
		return err
	}
	afterAccounts := make(map[domain.EntityID]domain.Account, len(after.Accounts))
	for _, account := range after.Accounts {
		afterAccounts[account.ID] = account
	}
	for _, account := range before.Accounts {
		next, exists := afterAccounts[account.ID]
		if !account.Retired && (!exists || next.Retired || next.CollisionKey != account.CollisionKey) {
			if err := releaseFoldCollisionKey(ctx, connection, "accounts", account.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertProviderAccounts(
	ctx context.Context,
	connection *sql.Conn,
	statements *seedStatements,
	before, after []domain.Account,
) error {
	existing := make(map[domain.EntityID]domain.Account, len(before))
	for _, account := range before {
		existing[account.ID] = account
	}
	for _, account := range after {
		previous, found := existing[account.ID]
		if !found {
			if _, err := statements.account.ExecContext(
				ctx, account.ID, account.Label, account.CollisionKey, booleanInteger(account.Retired),
			); err != nil {
				return mapDriverError(err, store.CodeStoreError)
			}
			continue
		}
		if reflect.DeepEqual(previous, account) {
			continue
		}
		if _, err := connection.ExecContext(ctx, `
			UPDATE accounts SET label = ?, collision_key = ?, retired = ? WHERE id = ?`,
			account.Label, account.CollisionKey, booleanInteger(account.Retired), account.ID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func upsertProviderTransactions(
	ctx context.Context,
	connection *sql.Conn,
	seed *seedStatements,
	fold *foldStatements,
	before, after []domain.TransactionRecord,
) error {
	existing := make(map[domain.EntityID]domain.TransactionRecord, len(before))
	desired := make(map[domain.EntityID]struct{}, len(after))
	for _, transaction := range before {
		existing[transaction.ID] = transaction
	}
	for _, transaction := range after {
		desired[transaction.ID] = struct{}{}
		previous, found := existing[transaction.ID]
		if found && reflect.DeepEqual(previous, transaction) {
			continue
		}
		if !found {
			metadata, err := json.Marshal(transaction.Metadata)
			if err != nil {
				return store.NewError(store.CodeInvalidOperation, err)
			}
			if _, err = seed.transaction.ExecContext(
				ctx, transaction.ID, transaction.Provider, transaction.ProviderID,
				transaction.AccountID, transaction.MerchantID, transaction.CategoryID,
				transaction.Date.String(), transaction.Amount.Minor, transaction.Amount.Currency,
				transaction.Amount.Scale, transaction.Notes, booleanInteger(transaction.Hidden),
				booleanInteger(transaction.Pending), string(metadata),
			); err != nil {
				return mapDriverError(err, store.CodeStoreError)
			}
			continue
		}
		if err := updateProviderTransaction(ctx, fold.updateTransaction, transaction); err != nil {
			return err
		}
	}
	for _, transaction := range before {
		if _, retained := desired[transaction.ID]; retained {
			continue
		}
		if _, err := connection.ExecContext(
			ctx, "DELETE FROM transactions WHERE id = ?", transaction.ID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func updateProviderTransaction(
	ctx context.Context,
	statement *sql.Stmt,
	transaction domain.TransactionRecord,
) error {
	metadata, err := json.Marshal(transaction.Metadata)
	if err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if _, err = statement.ExecContext(
		ctx, transaction.Provider, transaction.ProviderID, transaction.AccountID,
		transaction.MerchantID, transaction.CategoryID, transaction.Date.String(),
		transaction.Amount.Minor, transaction.Amount.Currency, transaction.Amount.Scale,
		transaction.Notes, booleanInteger(transaction.Hidden), booleanInteger(transaction.Pending),
		string(metadata), transaction.ID,
	); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func replaceExternalIdentitiesIfChanged(
	ctx context.Context,
	connection *sql.Conn,
	statements *seedStatements,
	before, after []domain.ExternalIdentity,
) error {
	if reflect.DeepEqual(before, after) {
		return nil
	}
	type identityKey struct{ namespace, externalID string }
	existing := make(map[identityKey]domain.ExternalIdentity, len(before))
	desired := make(map[identityKey]domain.ExternalIdentity, len(after))
	for _, identity := range before {
		existing[identityKey{identity.Namespace, identity.ExternalID}] = identity
	}
	for _, identity := range after {
		desired[identityKey{identity.Namespace, identity.ExternalID}] = identity
	}
	for key, identity := range existing {
		replacement, retained := desired[key]
		if retained && reflect.DeepEqual(identity, replacement) {
			continue
		}
		if _, err := connection.ExecContext(ctx, `
			DELETE FROM external_identities WHERE namespace = ? AND external_id = ?`,
			key.namespace, key.externalID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	for _, identity := range after {
		previous, found := existing[identityKey{identity.Namespace, identity.ExternalID}]
		if found && reflect.DeepEqual(previous, identity) {
			continue
		}
		if _, err := statements.externalIdentity.ExecContext(
			ctx, identity.EntityType, identity.EntityID, identity.Namespace, identity.ExternalID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func deleteRemovedProviderEntities(
	ctx context.Context,
	connection *sql.Conn,
	before, after domain.CommittedProfile,
) error {
	if err := deleteRemovedEntityRows(
		ctx, connection, "categories", categoryIDs(before.Categories), categoryIDs(after.Categories),
	); err != nil {
		return err
	}
	if err := deleteRemovedEntityRows(
		ctx, connection, "category_groups", groupIDs(before.Groups), groupIDs(after.Groups),
	); err != nil {
		return err
	}
	if err := deleteRemovedEntityRows(
		ctx, connection, "merchants", merchantIDs(before.Merchants), merchantIDs(after.Merchants),
	); err != nil {
		return err
	}
	return deleteRemovedEntityRows(
		ctx, connection, "accounts", accountIDs(before.Accounts), accountIDs(after.Accounts),
	)
}

func deleteRemovedEntityRows(
	ctx context.Context,
	connection *sql.Conn,
	table string,
	before, after map[domain.EntityID]struct{},
) error {
	query := "DELETE FROM " + table + " WHERE id = ?" //nolint:gosec // table is an internal constant.
	clearMerge := table == "categories" || table == "category_groups" || table == "merchants"
	for id := range before {
		if _, retained := after[id]; retained {
			continue
		}
		if clearMerge {
			clearQuery := "UPDATE " + table + " SET merge_destination_id = NULL WHERE id = ?" //nolint:gosec // table is an internal constant.
			if _, err := connection.ExecContext(ctx, clearQuery, id); err != nil {
				return mapDriverError(err, store.CodeStoreError)
			}
		}
		if _, err := connection.ExecContext(ctx, query, id); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func accountIDs(values []domain.Account) map[domain.EntityID]struct{} {
	result := make(map[domain.EntityID]struct{}, len(values))
	for _, value := range values {
		result[value.ID] = struct{}{}
	}
	return result
}

func merchantIDs(values []domain.Merchant) map[domain.EntityID]struct{} {
	result := make(map[domain.EntityID]struct{}, len(values))
	for _, value := range values {
		result[value.ID] = struct{}{}
	}
	return result
}

func groupIDs(values []domain.CategoryGroup) map[domain.EntityID]struct{} {
	result := make(map[domain.EntityID]struct{}, len(values))
	for _, value := range values {
		result[value.ID] = struct{}{}
	}
	return result
}

func categoryIDs(values []domain.Category) map[domain.EntityID]struct{} {
	result := make(map[domain.EntityID]struct{}, len(values))
	for _, value := range values {
		result[value.ID] = struct{}{}
	}
	return result
}

func replaceRefreshJournal(
	ctx context.Context,
	connection *sql.Conn,
	before, journal []domain.Operation,
) error {
	if reflect.DeepEqual(before, journal) {
		return nil
	}
	if _, err := connection.ExecContext(ctx, "DELETE FROM journal_operations"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	for _, operation := range journal {
		payload, err := encodeOperationPayload(operation)
		if err != nil {
			return store.NewError(store.CodeInvalidOperation, err)
		}
		if err = insertOperation(ctx, connection, operation, payload); err != nil {
			return err
		}
	}
	return nil
}

func replaceLabelAllocations(
	ctx context.Context,
	connection *sql.Conn,
	before, allocations []store.LabelAllocation,
) error {
	if reflect.DeepEqual(before, allocations) {
		return nil
	}
	if _, err := connection.ExecContext(ctx, "DELETE FROM provider_label_allocations"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	statement, err := connection.PrepareContext(ctx, `
		INSERT INTO provider_label_allocations(
			entity_type, namespace, external_id, base_collision_key,
			display_label, suffix_token, unsuffixed
		) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = statement.Close() }()
	for _, allocation := range allocations {
		if _, err = statement.ExecContext(
			ctx, allocation.Kind, allocation.Namespace, allocation.ExternalID,
			allocation.BaseCollisionKey, allocation.DisplayLabel, allocation.SuffixToken,
			booleanInteger(allocation.Unsuffixed),
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func persistRefreshBinding(
	ctx context.Context,
	connection *sql.Conn,
	binding *store.ProviderBinding,
) error {
	if binding == nil {
		return store.NewError(store.CodeStoreCorrupt, errors.New("refresh binding is missing"))
	}
	if _, err := connection.ExecContext(ctx, `
		INSERT INTO provider_binding(
			singleton, kind, namespace, remote_profile_id, bound_at_unix_ms
		) VALUES (1, ?, ?, ?, ?) ON CONFLICT(singleton) DO NOTHING`,
		binding.Kind, binding.Namespace, binding.RemoteProfileID, binding.BoundAt.UnixMilli(),
	); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func updateRefreshSuccess(
	ctx context.Context,
	connection *sql.Conn,
	current, next uint64,
	observedAt time.Time,
	summary store.RefreshSummary,
) error {
	currentInteger, err := sqliteInteger(current)
	if err != nil {
		return err
	}
	nextInteger, err := sqliteInteger(next)
	if err != nil {
		return err
	}
	result, err := connection.ExecContext(ctx, `
		UPDATE provider_refresh_state SET generation = ?, last_attempt_unix_ms = ?,
			last_success_unix_ms = ?, next_eligible_unix_ms = NULL, status_code = '',
			imported_transactions = ?, removed_transactions = ?
		WHERE singleton = 1 AND generation = ?`,
		nextInteger, observedAt.UnixMilli(), observedAt.UnixMilli(),
		summary.ImportedTransactions, summary.RemovedTransactions, currentInteger,
	)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if affected != 1 {
		return store.NewError(
			store.CodeStoreCorrupt,
			errors.New("refresh generation compare-and-advance failed"),
		)
	}
	return nil
}

func refreshGenerationConflict(observed, current uint64) error {
	return store.NewRevisionError(
		store.CodeRevisionConflict,
		observed,
		current,
		errors.New("authoritative refresh generation compare failed"),
	)
}

func cloneProviderBinding(binding *store.ProviderBinding) *store.ProviderBinding {
	if binding == nil {
		return nil
	}
	clone := *binding
	return &clone
}

func cloneEntityIDMap(values map[string]domain.EntityID) map[string]domain.EntityID {
	clone := make(map[string]domain.EntityID, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneStringMap(values map[string]string) map[string]string {
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func cloneRefreshPlan(plan store.RefreshPlan) store.RefreshPlan {
	plan.Committed = plan.Committed.Clone()
	plan.Effective = plan.Effective.Clone()
	operations := make([]domain.Operation, len(plan.Journal))
	for index := range plan.Journal {
		operations[index] = plan.Journal[index].Clone()
	}
	plan.Journal = operations
	plan.KnownDrills = append([]domain.DrillIdentity(nil), plan.KnownDrills...)
	plan.Allocations = append([]store.LabelAllocation(nil), plan.Allocations...)
	return plan
}

// CanonicalRefreshPlan encodes the logical persisted rows of a validated refresh plan.
func CanonicalRefreshPlan(plan store.RefreshPlan) ([]byte, error) {
	plan = cloneRefreshPlan(plan)
	slices.SortFunc(plan.Committed.Accounts, func(a, b domain.Account) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	slices.SortFunc(plan.Committed.Merchants, func(a, b domain.Merchant) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	slices.SortFunc(plan.Committed.Groups, func(a, b domain.CategoryGroup) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	slices.SortFunc(plan.Committed.Categories, func(a, b domain.Category) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	slices.SortFunc(plan.Committed.Transactions, func(a, b domain.TransactionRecord) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
	slices.SortFunc(plan.Committed.ExternalIdentities, func(a, b domain.ExternalIdentity) int {
		if order := strings.Compare(a.Namespace, b.Namespace); order != 0 {
			return order
		}
		return strings.Compare(a.ExternalID, b.ExternalID)
	})
	slices.SortFunc(plan.Allocations, func(a, b store.LabelAllocation) int {
		if order := strings.Compare(a.Namespace, b.Namespace); order != 0 {
			return order
		}
		return strings.Compare(a.ExternalID, b.ExternalID)
	})
	plan.Effective = domain.CommittedProfile{}
	plan.Summary = store.RefreshSummary{
		ImportedTransactions: plan.Summary.ImportedTransactions,
		RemovedTransactions:  plan.Summary.RemovedTransactions,
	}
	return json.Marshal(plan)
}
