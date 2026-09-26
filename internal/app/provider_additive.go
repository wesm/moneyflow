package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// PlanAdditiveProviderIdentities adds unseen posted rows without changing local truth.
func PlanAdditiveProviderIdentities(input IdentityPlanningInput) (IdentityPlan, error) {
	if err := validateIdentityPlanningInput(input); err != nil {
		return IdentityPlan{}, err
	}
	known := make(map[string]bool, len(input.Committed.ExternalIdentities))
	for _, identity := range input.Committed.ExternalIdentities {
		known[externalIdentityKey(identity.Namespace, identity.ExternalID)] = true
	}
	observation := domain.ImportSnapshot{ObservedAt: input.Import.ObservedAt}
	needed := make(map[string]bool)
	for _, row := range input.Import.Transactions {
		if known[ProviderIdentityKey(input.Provider, domain.EntityKindTransaction, row.ExternalID)] {
			continue
		}
		observation.Transactions = append(observation.Transactions, row)
		needed[ProviderIdentityKey(input.Provider, domain.EntityKindAccount, row.AccountExternalID)] = true
		needed[ProviderIdentityKey(input.Provider, domain.EntityKindMerchant, row.MerchantExternalID)] = true
	}
	for _, entity := range input.Import.Accounts {
		key := ProviderIdentityKey(input.Provider, entity.Kind, entity.ExternalID)
		if needed[key] && !known[key] {
			observation.Accounts = append(observation.Accounts, entity)
		}
	}
	for _, entity := range input.Import.Merchants {
		key := ProviderIdentityKey(input.Provider, entity.Kind, entity.ExternalID)
		if needed[key] && !known[key] {
			observation.Merchants = append(observation.Merchants, entity)
		}
	}
	input.Import = observation
	planner, err := newIdentityPlanner(input)
	if err != nil {
		return IdentityPlan{}, err
	}
	// Only new dimensions reach the existing allocator; existing labels are never rewritten.
	for _, entity := range append(slices.Clone(observation.Accounts), observation.Merchants...) {
		id, resolveErr := planner.resolveExistingIdentity(entity.Kind, entity.ExternalID)
		if resolveErr != nil {
			return IdentityPlan{}, resolveErr
		}
		label, key, allocation, allocateErr := planner.labels.allocate(entity.Kind, entity.ExternalID, id, entity.Label)
		if allocateErr != nil {
			return IdentityPlan{}, allocateErr
		}
		planner.allocations[externalIdentityKey(allocation.Namespace, allocation.ExternalID)] = allocation
		if entity.Kind == domain.EntityKindAccount {
			planner.accounts[id] = domain.Account{ID: id, Label: label, CollisionKey: key}
		} else {
			planner.merchants[id] = domain.Merchant{ID: id, Label: label, CollisionKey: key}
		}
	}
	slices.SortFunc(observation.Transactions, func(a, b domain.ImportTransaction) int { return strings.Compare(a.ExternalID, b.ExternalID) })
	for _, row := range observation.Transactions {
		id, resolveErr := planner.resolveIdentity(domain.EntityKindTransaction, row.ExternalID)
		if resolveErr != nil {
			return IdentityPlan{}, resolveErr
		}
		accountID, resolveErr := planner.resolveExistingIdentity(domain.EntityKindAccount, row.AccountExternalID)
		if resolveErr != nil {
			return IdentityPlan{}, resolveErr
		}
		merchantID, resolveErr := planner.resolveExistingIdentity(domain.EntityKindMerchant, row.MerchantExternalID)
		if resolveErr != nil {
			return IdentityPlan{}, resolveErr
		}
		for steps := 0; ; steps++ {
			merchant, exists := planner.merchants[merchantID]
			if !exists || steps >= len(planner.merchants) {
				return IdentityPlan{}, errors.New("additive import: invalid merchant merge chain")
			}
			if merchant.MergeDestination == nil {
				if merchant.Retired {
					return IdentityPlan{}, errors.New("additive import: retired merchant has no destination")
				}
				break
			}
			merchantID = *merchant.MergeDestination
		}
		planner.transactions[id] = domain.TransactionRecord{ID: id, Provider: input.Provider, ProviderID: row.ExternalID,
			AccountID: accountID, MerchantID: merchantID, CategoryID: domain.UncategorizedCategoryID, Date: row.Date, Amount: row.Amount}
	}
	plan := planner.finish()
	plan.Lineage = slices.Clone(input.Lineage)
	if err = plan.Committed.Validate(); err != nil {
		return IdentityPlan{}, err
	}
	return plan, nil
}

func buildAdditiveRefreshPlan(inputs store.RefreshInputs, effective domain.CommittedProfile) (store.RefreshPlan, error) {
	identities, err := PlanAdditiveProviderIdentities(IdentityPlanningInput{Provider: inputs.Binding.Kind,
		Import: inputs.Candidate, Committed: inputs.Snapshot.Committed, Effective: effective,
		Allocations: inputs.Allocations, Lineage: inputs.Lineage, ProposedIDs: inputs.ProposedIDs, ProposedSuffixes: inputs.ProposedSuffixes})
	if err != nil {
		return store.RefreshPlan{}, err
	}
	known, err := knownDrillsForProviderRefresh(inputs.Snapshot.KnownDrills, identities.Committed, inputs.Snapshot.Journal)
	if err != nil {
		return store.RefreshPlan{}, err
	}
	replayed, err := Replay(domain.ProfileSnapshot{Revision: inputs.Snapshot.Revision, Committed: identities.Committed,
		Journal: inputs.Snapshot.Journal, Cursor: inputs.Snapshot.Cursor, KnownDrills: known})
	if err != nil {
		return store.RefreshPlan{}, err
	}
	return store.RefreshPlan{Committed: identities.Committed, Effective: replayed.Effective, Journal: inputs.Snapshot.Journal,
		Cursor: inputs.Snapshot.Cursor, KnownDrills: known, Allocations: identities.Allocations, Lineage: identities.Lineage,
		Summary: store.RefreshSummary{ImportedTransactions: len(identities.Committed.Transactions) - len(inputs.Snapshot.Committed.Transactions),
			ImportedAccounts:  len(identities.Committed.Accounts) - len(inputs.Snapshot.Committed.Accounts),
			ImportedMerchants: len(identities.Committed.Merchants) - len(inputs.Snapshot.Committed.Merchants), RetainedOperations: inputs.Snapshot.Cursor}}, nil
}
