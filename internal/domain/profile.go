package domain

import (
	"errors"
	"fmt"
)

// CommittedProfile is the durable base state before pending journal operations are replayed.
type CommittedProfile struct {
	Accounts           []Account
	Merchants          []Merchant
	Groups             []CategoryGroup
	Categories         []Category
	Transactions       []TransactionRecord
	ExternalIdentities []ExternalIdentity
}

// DrillIdentity records one historically known stable analytical key.
type DrillIdentity struct {
	Dimension Dimension
	Currency  Currency
	Scale     uint8
	Key       string
}

// CanonicalKey validates and serializes the complete analytical identity for sorting and sets.
func (identity DrillIdentity) CanonicalKey() (string, error) {
	if identity.Dimension == DimensionTime || !identity.Dimension.Valid() || identity.Key == "" {
		return "", errors.New("canonical drill identity: invalid analytical identity")
	}
	if !validCurrency(identity.Currency) || identity.Scale > 9 {
		return "", errors.New("canonical drill identity: invalid money partition")
	}
	return fmt.Sprintf(
		"%s\x00%s\x00%03d\x00%s",
		identity.Dimension,
		identity.Currency,
		identity.Scale,
		identity.Key,
	), nil
}

// ProfileSnapshot is one consistent committed base plus its pending operation history.
type ProfileSnapshot struct {
	Revision    uint64
	Cursor      int
	Committed   CommittedProfile
	Journal     []Operation
	KnownDrills []DrillIdentity
}

// Clone returns a deep, independently owned profile snapshot.
func (snapshot ProfileSnapshot) Clone() ProfileSnapshot {
	snapshot.Committed = snapshot.Committed.Clone()
	journal := snapshot.Journal
	snapshot.Journal = make([]Operation, len(journal))
	for index := range journal {
		snapshot.Journal[index] = journal[index].Clone()
	}
	snapshot.KnownDrills = append([]DrillIdentity(nil), snapshot.KnownDrills...)
	return snapshot
}

// Validate checks cursor-count semantics, stable journal order, and canonical known identities.
func (snapshot ProfileSnapshot) Validate() error {
	if snapshot.Cursor < 0 || snapshot.Cursor > len(snapshot.Journal) {
		return errors.New("validate profile snapshot: cursor is outside journal")
	}
	if err := snapshot.Committed.Validate(); err != nil {
		return fmt.Errorf("validate profile snapshot: %w", err)
	}
	var previousSequence int64
	for index, operation := range snapshot.Journal {
		if err := operation.ValidateStored(); err != nil {
			return fmt.Errorf("validate profile snapshot: journal[%d]: %w", index, err)
		}
		if index > 0 && operation.Sequence <= previousSequence {
			return errors.New("validate profile snapshot: journal sequences are not strictly increasing")
		}
		previousSequence = operation.Sequence
	}
	previous := ""
	for index, identity := range snapshot.KnownDrills {
		canonical, err := identity.CanonicalKey()
		if err != nil {
			return fmt.Errorf("validate profile snapshot: known drill[%d]: %w", index, err)
		}
		if index > 0 && canonical <= previous {
			return errors.New("validate profile snapshot: known drills are not strictly sorted and unique")
		}
		previous = canonical
	}
	return nil
}

// Clone returns a deep, independently owned profile value.
func (profile CommittedProfile) Clone() CommittedProfile {
	clone := profile
	clone.Accounts = append([]Account(nil), profile.Accounts...)
	clone.Merchants = append([]Merchant(nil), profile.Merchants...)
	for index := range clone.Merchants {
		clone.Merchants[index].MergeDestination = cloneEntityID(profile.Merchants[index].MergeDestination)
	}
	clone.Groups = append([]CategoryGroup(nil), profile.Groups...)
	for index := range clone.Groups {
		clone.Groups[index].MergeDestination = cloneEntityID(profile.Groups[index].MergeDestination)
	}
	clone.Categories = append([]Category(nil), profile.Categories...)
	for index := range clone.Categories {
		clone.Categories[index].MergeDestination = cloneEntityID(profile.Categories[index].MergeDestination)
	}
	clone.Transactions = append([]TransactionRecord(nil), profile.Transactions...)
	for index := range clone.Transactions {
		clone.Transactions[index].Metadata = cloneStringMap(profile.Transactions[index].Metadata)
	}
	clone.ExternalIdentities = append([]ExternalIdentity(nil), profile.ExternalIdentities...)
	return clone
}

// Validate checks stable references, protected sentinels, and active label uniqueness.
func (profile CommittedProfile) Validate() error {
	accounts := make(map[EntityID]Account, len(profile.Accounts))
	merchants := make(map[EntityID]Merchant, len(profile.Merchants))
	groups := make(map[EntityID]CategoryGroup, len(profile.Groups))
	categories := make(map[EntityID]Category, len(profile.Categories))
	allIDs := make(map[EntityID]EntityKind)

	for _, account := range profile.Accounts {
		if err := validateEntityLabel("account", account.ID, account.Label, account.CollisionKey); err != nil {
			return err
		}
		if err := addEntityID(allIDs, account.ID, EntityKindAccount); err != nil {
			return err
		}
		accounts[account.ID] = account
	}
	for _, merchant := range profile.Merchants {
		if err := validateEntityLabel("merchant", merchant.ID, merchant.Label, merchant.CollisionKey); err != nil {
			return err
		}
		if err := addEntityID(allIDs, merchant.ID, EntityKindMerchant); err != nil {
			return err
		}
		merchants[merchant.ID] = merchant
	}
	for _, group := range profile.Groups {
		if err := validateEntityLabel("group", group.ID, group.Label, group.CollisionKey); err != nil {
			return err
		}
		if err := addEntityID(allIDs, group.ID, EntityKindGroup); err != nil {
			return err
		}
		if group.Protected && (group.Retired || group.MergeDestination != nil) {
			return fmt.Errorf("validate profile: protected group %q cannot be retired or merged", group.ID)
		}
		groups[group.ID] = group
	}
	for _, category := range profile.Categories {
		if err := validateEntityLabel("category", category.ID, category.Label, category.CollisionKey); err != nil {
			return err
		}
		if err := addEntityID(allIDs, category.ID, EntityKindCategory); err != nil {
			return err
		}
		if category.Protected && (category.Retired || category.MergeDestination != nil) {
			return fmt.Errorf("validate profile: protected category %q cannot be retired or merged", category.ID)
		}
		categories[category.ID] = category
	}

	if group, ok := groups[UncategorizedGroupID]; !ok || !group.Protected || group.Retired ||
		group.Label != UncategorizedLabel || group.CollisionKey != UncategorizedCollisionKey {
		return errors.New("validate profile: protected Uncategorized group is missing or invalid")
	}
	if category, ok := categories[UncategorizedCategoryID]; !ok || !category.Protected ||
		category.Retired || category.GroupID != UncategorizedGroupID ||
		category.Label != UncategorizedLabel || category.CollisionKey != UncategorizedCollisionKey {
		return errors.New("validate profile: protected Uncategorized category is missing or invalid")
	}
	if err := validateActiveCollisions("account", profile.Accounts, func(value Account) (string, bool) { return value.CollisionKey, value.Retired }); err != nil {
		return err
	}
	if err := validateActiveCollisions("merchant", profile.Merchants, func(value Merchant) (string, bool) { return value.CollisionKey, value.Retired }); err != nil {
		return err
	}
	if err := validateActiveCollisions("group", profile.Groups, func(value CategoryGroup) (string, bool) { return value.CollisionKey, value.Retired }); err != nil {
		return err
	}
	if err := validateActiveCollisions("category", profile.Categories, func(value Category) (string, bool) { return value.CollisionKey, value.Retired }); err != nil {
		return err
	}

	for _, merchant := range profile.Merchants {
		if err := validateMergeDestination("merchant", merchant.ID, merchant.Retired, merchant.MergeDestination, merchants, func(value Merchant) bool { return value.Retired }); err != nil {
			return err
		}
	}
	for _, group := range profile.Groups {
		if err := validateMergeDestination("group", group.ID, group.Retired, group.MergeDestination, groups, func(value CategoryGroup) bool { return value.Retired }); err != nil {
			return err
		}
	}
	for _, category := range profile.Categories {
		group, ok := groups[category.GroupID]
		if !ok || group.Retired {
			return fmt.Errorf("validate profile: category %q references missing or retired group %q", category.ID, category.GroupID)
		}
		if err := validateMergeDestination("category", category.ID, category.Retired, category.MergeDestination, categories, func(value Category) bool { return value.Retired }); err != nil {
			return err
		}
	}

	for _, transaction := range profile.Transactions {
		if transaction.ID == "" {
			return errors.New("validate profile: transaction ID is empty")
		}
		if err := addEntityID(allIDs, transaction.ID, EntityKindTransaction); err != nil {
			return err
		}
		if account, ok := accounts[transaction.AccountID]; !ok || account.Retired {
			return fmt.Errorf("validate profile: transaction %q references missing or retired account %q", transaction.ID, transaction.AccountID)
		}
		if merchant, ok := merchants[transaction.MerchantID]; !ok || merchant.Retired {
			return fmt.Errorf("validate profile: transaction %q references missing or retired merchant %q", transaction.ID, transaction.MerchantID)
		}
		if category, ok := categories[transaction.CategoryID]; !ok || category.Retired {
			return fmt.Errorf("validate profile: transaction %q references missing or retired category %q", transaction.ID, transaction.CategoryID)
		}
		account := accounts[transaction.AccountID]
		merchant := merchants[transaction.MerchantID]
		category := categories[transaction.CategoryID]
		group := groups[category.GroupID]
		if _, err := NewTransaction(Transaction{
			ID: string(transaction.ID), ProviderID: transaction.ProviderID, Provider: transaction.Provider,
			Account: EntityRef{ID: string(account.ID), Name: account.Label}, Date: transaction.Date,
			Merchant: EntityRef{ID: string(merchant.ID), Name: merchant.Label},
			Category: CategoryRef{
				ID: string(category.ID), Name: category.Label, GroupID: string(group.ID), Group: group.Label,
			},
			Amount: transaction.Amount, Notes: transaction.Notes, Hidden: transaction.Hidden,
			Pending: transaction.Pending, Metadata: transaction.Metadata,
		}); err != nil {
			return fmt.Errorf("validate profile: transaction %q: %w", transaction.ID, err)
		}
	}

	type providerIdentityKey struct{ namespace, externalID string }
	type localProviderIdentityKey struct {
		kind      EntityKind
		entityID  EntityID
		namespace string
	}
	seenExternal := make(map[providerIdentityKey]struct{}, len(profile.ExternalIdentities))
	seenLocalExternal := make(map[localProviderIdentityKey]struct{}, len(profile.ExternalIdentities))
	for _, identity := range profile.ExternalIdentities {
		if identity.Namespace == "" || identity.ExternalID == "" {
			return errors.New("validate profile: external identity is incomplete")
		}
		if identity.EntityID == "" {
			return fmt.Errorf(
				"validate profile: external identity references unknown %s %q",
				identity.EntityType,
				identity.EntityID,
			)
		}
		kind, entityExists := allIDs[identity.EntityID]
		transactionTombstone := !entityExists && identity.EntityType == EntityKindTransaction
		if (!entityExists && !transactionTombstone) || (entityExists && kind != identity.EntityType) {
			return fmt.Errorf("validate profile: external identity references unknown %s %q", identity.EntityType, identity.EntityID)
		}
		key := providerIdentityKey{identity.Namespace, identity.ExternalID}
		if _, exists := seenExternal[key]; exists {
			return fmt.Errorf("validate profile: duplicate external identity %q", identity.ExternalID)
		}
		seenExternal[key] = struct{}{}
		localKey := localProviderIdentityKey{
			kind: identity.EntityType, entityID: identity.EntityID, namespace: identity.Namespace,
		}
		if _, exists := seenLocalExternal[localKey]; exists {
			return fmt.Errorf(
				"validate profile: duplicate local external identity for %s %q",
				identity.EntityType,
				identity.EntityID,
			)
		}
		seenLocalExternal[localKey] = struct{}{}
	}
	return nil
}

// MaterializeTransactions joins stable entity references into analytics transactions.
func (profile CommittedProfile) MaterializeTransactions() ([]Transaction, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	accounts := make(map[EntityID]Account, len(profile.Accounts))
	merchants := make(map[EntityID]Merchant, len(profile.Merchants))
	groups := make(map[EntityID]CategoryGroup, len(profile.Groups))
	categories := make(map[EntityID]Category, len(profile.Categories))
	for _, value := range profile.Accounts {
		accounts[value.ID] = value
	}
	for _, value := range profile.Merchants {
		merchants[value.ID] = value
	}
	for _, value := range profile.Groups {
		groups[value.ID] = value
	}
	for _, value := range profile.Categories {
		categories[value.ID] = value
	}

	materialized := make([]Transaction, 0, len(profile.Transactions))
	for _, record := range profile.Transactions {
		account := accounts[record.AccountID]
		merchant := merchants[record.MerchantID]
		category := categories[record.CategoryID]
		group := groups[category.GroupID]
		transaction, err := NewTransaction(Transaction{
			ID: string(record.ID), ProviderID: record.ProviderID, Provider: record.Provider,
			Account:  EntityRef{ID: string(account.ID), Name: account.Label},
			Date:     record.Date,
			Merchant: EntityRef{ID: string(merchant.ID), Name: merchant.Label},
			Category: CategoryRef{ID: string(category.ID), Name: category.Label, GroupID: string(group.ID), Group: group.Label},
			Amount:   record.Amount, Notes: record.Notes, Hidden: record.Hidden, Pending: record.Pending,
			Metadata: record.Metadata,
		})
		if err != nil {
			return nil, fmt.Errorf("materialize transaction %q: %w", record.ID, err)
		}
		materialized = append(materialized, transaction)
	}
	return materialized, nil
}

func cloneEntityID(value *EntityID) *EntityID {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func addEntityID(ids map[EntityID]EntityKind, id EntityID, kind EntityKind) error {
	if id == "" {
		return fmt.Errorf("validate profile: %s ID is empty", kind)
	}
	if previous, exists := ids[id]; exists {
		return fmt.Errorf("validate profile: duplicate entity ID %q (%s and %s)", id, previous, kind)
	}
	ids[id] = kind
	return nil
}

func validateEntityLabel(kind string, id EntityID, label, storedKey string) error {
	if id == "" {
		return fmt.Errorf("validate profile: %s ID is empty", kind)
	}
	key, err := CollisionKey(label)
	if err != nil {
		return fmt.Errorf("validate profile: %s %q label: %w", kind, id, err)
	}
	if key != storedKey {
		return fmt.Errorf("validate profile: %s %q collision key does not match label", kind, id)
	}
	return nil
}

func validateActiveCollisions[T any](kind string, values []T, fields func(T) (string, bool)) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		key, retired := fields(value)
		if retired {
			continue
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("validate profile: duplicate active %s collision key %q", kind, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateMergeDestination[T any](kind string, id EntityID, retired bool, destination *EntityID, values map[EntityID]T, isRetired func(T) bool) error {
	if destination != nil && !retired {
		return fmt.Errorf("validate profile: active %s %q has a merge destination", kind, id)
	}
	if destination == nil {
		return nil
	}
	value, exists := values[*destination]
	if !exists || isRetired(value) || *destination == id {
		return fmt.Errorf("validate profile: %s %q has invalid merge destination %q", kind, id, *destination)
	}
	return nil
}
