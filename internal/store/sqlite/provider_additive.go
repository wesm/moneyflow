package sqlite

import (
	"errors"
	"reflect"
	"slices"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func validateAdditiveMaterialization(snapshot domain.ProfileSnapshot, binding *store.ProviderBinding,
	allocations []store.LabelAllocation, candidate domain.ImportSnapshot, proposed map[string]domain.EntityID, plan store.RefreshPlan,
) error {
	invalid := errors.New("additive refresh must preserve local state and materialize only unseen transactions")
	if plan.Cursor != snapshot.Cursor || !slices.EqualFunc(plan.Journal, snapshot.Journal, func(a, b domain.Operation) bool { return reflect.DeepEqual(a, b) }) || len(plan.Lineage) != 0 {
		return invalid
	}
	before, after := snapshot.Committed, plan.Committed
	if !unchangedRows(before.Accounts, after.Accounts, func(v domain.Account) domain.EntityID { return v.ID }) ||
		!unchangedRows(before.Merchants, after.Merchants, func(v domain.Merchant) domain.EntityID { return v.ID }) ||
		!slices.EqualFunc(before.Groups, after.Groups, func(a, b domain.CategoryGroup) bool { return reflect.DeepEqual(a, b) }) ||
		!slices.EqualFunc(before.Categories, after.Categories, func(a, b domain.Category) bool { return reflect.DeepEqual(a, b) }) ||
		!unchangedRows(before.Transactions, after.Transactions, func(v domain.TransactionRecord) domain.EntityID { return v.ID }) {
		return invalid
	}
	identities, err := externalIdentitySupersetIndex(before.ExternalIdentities, after.ExternalIdentities, nil)
	if err != nil {
		return err
	}
	known := make(map[externalIdentityIndexKey]domain.ExternalIdentity)
	for _, value := range before.ExternalIdentities {
		known[externalIdentityIndexKey{value.Namespace, value.ExternalID}] = value
	}
	needed := make(map[externalIdentityIndexKey]bool)
	key := func(kind domain.EntityKind, external string) externalIdentityIndexKey {
		return externalIdentityIndexKey{binding.Kind + "/" + string(kind), external}
	}
	newCandidate := domain.ImportSnapshot{ObservedAt: candidate.ObservedAt}
	for _, row := range candidate.Transactions {
		if _, exists := known[key(domain.EntityKindTransaction, row.ExternalID)]; exists {
			continue
		}
		row.CategoryExternalID, row.SystemCategoryID, row.Notes, row.Hidden, row.Pending = "", "", "", false, false
		newCandidate.Transactions = append(newCandidate.Transactions, row)
		needed[key(domain.EntityKindTransaction, row.ExternalID)] = true
		needed[key(domain.EntityKindAccount, row.AccountExternalID)] = true
		needed[key(domain.EntityKindMerchant, row.MerchantExternalID)] = true
	}
	for _, entity := range candidate.Accounts {
		lookup := key(entity.Kind, entity.ExternalID)
		if _, exists := known[lookup]; !exists && needed[lookup] {
			newCandidate.Accounts = append(newCandidate.Accounts, entity)
		}
	}
	for _, entity := range candidate.Merchants {
		lookup := key(entity.Kind, entity.ExternalID)
		if _, exists := known[lookup]; !exists && needed[lookup] {
			newCandidate.Merchants = append(newCandidate.Merchants, entity)
		}
	}
	for lookup, identity := range identities {
		if _, exists := known[lookup]; exists {
			continue
		}
		if !needed[lookup] || proposed[lookup.namespace+"\x00"+lookup.externalID] != identity.EntityID {
			return invalid
		}
	}
	allocationIndex := make(map[externalIdentityIndexKey]store.LabelAllocation)
	for _, value := range plan.Allocations {
		allocationIndex[externalIdentityIndexKey{value.Namespace, value.ExternalID}] = value
	}
	for _, value := range allocations {
		if allocationIndex[externalIdentityIndexKey{value.Namespace, value.ExternalID}] != value {
			return invalid
		}
	}
	if len(plan.Allocations) != len(allocations)+len(newCandidate.Accounts)+len(newCandidate.Merchants) {
		return invalid
	}
	accounts := make(map[domain.EntityID]domain.Account)
	for _, value := range after.Accounts {
		accounts[value.ID] = value
	}
	merchants := make(map[domain.EntityID]domain.Merchant)
	for _, value := range after.Merchants {
		merchants[value.ID] = value
	}
	for _, entity := range append(slices.Clone(newCandidate.Accounts), newCandidate.Merchants...) {
		lookup := key(entity.Kind, entity.ExternalID)
		identity, allocation := identities[lookup], allocationIndex[lookup]
		collision, collisionErr := domain.CollisionKey(allocation.DisplayLabel)
		if collisionErr != nil || allocation.ProviderLabel != entity.Label {
			return invalid
		}
		if entity.Kind == domain.EntityKindAccount {
			if accounts[identity.EntityID] != (domain.Account{ID: identity.EntityID, Label: allocation.DisplayLabel, CollisionKey: collision}) {
				return invalid
			}
		} else if !reflect.DeepEqual(merchants[identity.EntityID], domain.Merchant{ID: identity.EntityID, Label: allocation.DisplayLabel, CollisionKey: collision}) {
			return invalid
		}
	}
	// Preserve alias identities on disk, but verify new rows against the current merge destination.
	for lookup, identity := range identities {
		if identity.EntityType != domain.EntityKindMerchant || !needed[lookup] {
			continue
		}
		for steps := 0; ; steps++ {
			merchant, exists := merchants[identity.EntityID]
			if !exists || steps >= len(merchants) {
				return invalid
			}
			if merchant.MergeDestination == nil {
				if merchant.Retired {
					return invalid
				}
				break
			}
			identity.EntityID = *merchant.MergeDestination
		}
		identities[lookup] = identity
	}
	expected := store.RefreshSummary{ImportedAccounts: len(newCandidate.Accounts), ImportedMerchants: len(newCandidate.Merchants),
		ImportedTransactions: len(newCandidate.Transactions), RetainedOperations: snapshot.Cursor}
	if plan.Summary != expected || len(after.Transactions) != len(before.Transactions)+expected.ImportedTransactions ||
		len(after.Accounts) != len(before.Accounts)+expected.ImportedAccounts || len(after.Merchants) != len(before.Merchants)+expected.ImportedMerchants {
		return invalid
	}
	if err = validateCandidateMaterialization(binding, newCandidate, after, plan.Allocations, identities, nil); err != nil {
		return err
	}
	return validateLabelAllocations(plan.Allocations)
}

func unchangedRows[T any](before, after []T, id func(T) domain.EntityID) bool {
	index := make(map[domain.EntityID]T, len(after))
	for _, value := range after {
		index[id(value)] = value
	}
	for _, value := range before {
		next, exists := index[id(value)]
		if !exists || !reflect.DeepEqual(value, next) {
			return false
		}
	}
	return true
}
