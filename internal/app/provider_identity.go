package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// IdentityPlanningInput contains every value a deterministic provider import may consult.
type IdentityPlanningInput struct {
	Provider         string
	Import           domain.ImportSnapshot
	Committed        domain.CommittedProfile
	Effective        domain.CommittedProfile
	Allocations      []store.LabelAllocation
	Lineage          []store.ProviderIdentityLineage
	ProposedIDs      map[string]domain.EntityID
	ProposedSuffixes map[string]string
}

// IdentityPlan is a complete refreshed committed base plus sticky label decisions.
type IdentityPlan struct {
	Committed   domain.CommittedProfile
	Allocations []store.LabelAllocation
	Lineage     []store.ProviderIdentityLineage
}

// ProviderIdentityKey returns the canonical lookup key used for proposed local material.
func ProviderIdentityKey(
	provider string,
	kind domain.EntityKind,
	externalID string,
) string {
	return providerNamespace(provider, kind) + "\x00" + externalID
}

// PlanProviderIdentities maps one complete import without consulting clocks, randomness, or I/O.
func PlanProviderIdentities(input IdentityPlanningInput) (IdentityPlan, error) {
	if err := validateIdentityPlanningInput(input); err != nil {
		return IdentityPlan{}, err
	}
	prepared, lineage, err := prepareHistoricalProviderMerchants(input)
	if err != nil {
		return IdentityPlan{}, err
	}

	planner, err := newIdentityPlanner(prepared)
	if err != nil {
		return IdentityPlan{}, err
	}
	if err = planner.planDimensions(); err != nil {
		return IdentityPlan{}, err
	}
	if err = planner.planTransactions(); err != nil {
		return IdentityPlan{}, err
	}
	planner.retireMissingEntities()
	planner.repairRetiredCategoryParents()
	plan := planner.finish()
	plan.Lineage = lineage
	if err = plan.Committed.Validate(); err != nil {
		return IdentityPlan{}, fmt.Errorf("plan provider identities: result: %w", err)
	}
	return plan, nil
}

func prepareHistoricalProviderMerchants(
	input IdentityPlanningInput,
) (IdentityPlanningInput, []store.ProviderIdentityLineage, error) {
	prepared := input
	prepared.Import = input.Import.Clone()
	prepared.Committed = input.Committed.Clone()
	prepared.Effective = input.Effective.Clone()
	prepared.Allocations = append([]store.LabelAllocation(nil), input.Allocations...)
	lineage := append([]store.ProviderIdentityLineage(nil), input.Lineage...)
	namespace := providerNamespace(input.Provider, domain.EntityKindMerchant)
	historical := make(map[string]int)
	for index, value := range lineage {
		if value.Kind == domain.EntityKindMerchant && value.Namespace == namespace {
			historical[value.ExternalID] = index
		}
	}
	merchantByID := merchantIndexByID(prepared.Committed.Merchants)
	for _, identity := range prepared.Committed.ExternalIdentities {
		if identity.EntityType != domain.EntityKindMerchant || identity.Namespace != namespace {
			continue
		}
		merchant, exists := merchantByID[identity.EntityID]
		if !exists || !merchant.Retired || merchant.MergeDestination == nil {
			continue
		}
		if _, exists = historical[identity.ExternalID]; !exists {
			historical[identity.ExternalID] = len(lineage)
			lineage = append(lineage, store.ProviderIdentityLineage{
				Kind: domain.EntityKindMerchant, Namespace: namespace,
				ExternalID: identity.ExternalID, PriorLocalID: identity.EntityID,
				CurrentLocalID: identity.EntityID, ProviderLabel: merchant.Label,
				Disposition: "alias", BatchVersion: 1,
			})
		}
	}
	references := make(map[string]int)
	for _, transaction := range prepared.Import.Transactions {
		references[transaction.MerchantExternalID]++
	}
	merchants := make([]domain.ImportEntity, 0, len(prepared.Import.Merchants))
	for _, merchant := range prepared.Import.Merchants {
		lineageIndex, isHistorical := historical[merchant.ExternalID]
		if !isHistorical {
			merchants = append(merchants, merchant)
			continue
		}
		if references[merchant.ExternalID] == 0 {
			continue
		}
		proposed := prepared.ProposedIDs[ProviderIdentityKey(
			input.Provider, domain.EntityKindMerchant, merchant.ExternalID,
		)]
		if proposed == "" {
			return IdentityPlanningInput{}, nil,
				errors.New("plan provider identities: historical merchant needs a fresh local ID")
		}
		prepared.Committed.ExternalIdentities = removeProviderIdentity(
			prepared.Committed.ExternalIdentities, namespace, merchant.ExternalID,
		)
		lineage[lineageIndex].CurrentLocalID = proposed
		lineage[lineageIndex].Disposition = "reactivated"
		merchants = append(merchants, merchant)
	}
	prepared.Import.Merchants = merchants
	slices.SortFunc(lineage, func(left, right store.ProviderIdentityLineage) int {
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	return prepared, lineage, nil
}

func removeProviderIdentity(
	values []domain.ExternalIdentity,
	namespace string,
	externalID string,
) []domain.ExternalIdentity {
	result := make([]domain.ExternalIdentity, 0, len(values))
	for _, value := range values {
		if value.Namespace == namespace && value.ExternalID == externalID {
			continue
		}
		result = append(result, value)
	}
	return result
}

type identityPlanner struct {
	input             IdentityPlanningInput
	accounts          map[domain.EntityID]domain.Account
	merchants         map[domain.EntityID]domain.Merchant
	groups            map[domain.EntityID]domain.CategoryGroup
	categories        map[domain.EntityID]domain.Category
	transactions      map[domain.EntityID]domain.TransactionRecord
	identities        map[string]domain.ExternalIdentity
	allocations       map[string]store.LabelAllocation
	reservedIDs       map[domain.EntityID]domain.EntityKind
	imported          map[domain.EntityKind]map[string]struct{}
	providerEntityIDs map[domain.EntityKind]map[domain.EntityID]struct{}
	labels            *providerLabelPlanner
}

func newIdentityPlanner(input IdentityPlanningInput) (*identityPlanner, error) {
	planner := &identityPlanner{
		input:             input,
		accounts:          make(map[domain.EntityID]domain.Account),
		merchants:         make(map[domain.EntityID]domain.Merchant),
		groups:            make(map[domain.EntityID]domain.CategoryGroup),
		categories:        make(map[domain.EntityID]domain.Category),
		transactions:      make(map[domain.EntityID]domain.TransactionRecord),
		identities:        make(map[string]domain.ExternalIdentity),
		allocations:       make(map[string]store.LabelAllocation),
		reservedIDs:       make(map[domain.EntityID]domain.EntityKind),
		imported:          make(map[domain.EntityKind]map[string]struct{}),
		providerEntityIDs: make(map[domain.EntityKind]map[domain.EntityID]struct{}),
	}
	for _, kind := range allProviderEntityKinds() {
		planner.imported[kind] = make(map[string]struct{})
		planner.providerEntityIDs[kind] = make(map[domain.EntityID]struct{})
	}
	for _, value := range input.Committed.Accounts {
		planner.accounts[value.ID] = value
		planner.reservedIDs[value.ID] = domain.EntityKindAccount
	}
	for _, value := range input.Committed.Merchants {
		planner.merchants[value.ID] = value
		planner.reservedIDs[value.ID] = domain.EntityKindMerchant
	}
	for _, value := range input.Committed.Groups {
		planner.groups[value.ID] = value
		planner.reservedIDs[value.ID] = domain.EntityKindGroup
	}
	for _, value := range input.Committed.Categories {
		planner.categories[value.ID] = value
		planner.reservedIDs[value.ID] = domain.EntityKindCategory
	}
	for _, value := range input.Committed.Transactions {
		planner.transactions[value.ID] = value
		planner.reservedIDs[value.ID] = domain.EntityKindTransaction
	}
	for _, entity := range effectiveEntityIDs(input.Effective) {
		if existing, reserved := planner.reservedIDs[entity.id]; reserved && existing != entity.kind {
			return nil, fmt.Errorf(
				"plan provider identities: effective local ID %q has conflicting kinds",
				entity.id,
			)
		}
		planner.reservedIDs[entity.id] = entity.kind
	}
	for _, identity := range input.Committed.ExternalIdentities {
		key := externalIdentityKey(identity.Namespace, identity.ExternalID)
		planner.identities[key] = identity
		if existing, ok := planner.reservedIDs[identity.EntityID]; ok && existing != identity.EntityType {
			return nil, fmt.Errorf("plan provider identities: local ID %q has conflicting kinds", identity.EntityID)
		}
		planner.reservedIDs[identity.EntityID] = identity.EntityType
		if identity.Namespace == providerNamespace(input.Provider, identity.EntityType) {
			planner.providerEntityIDs[identity.EntityType][identity.EntityID] = struct{}{}
		}
	}
	for _, allocation := range input.Allocations {
		key := externalIdentityKey(allocation.Namespace, allocation.ExternalID)
		if _, duplicate := planner.allocations[key]; duplicate {
			return nil, errors.New("plan provider identities: duplicate label allocation")
		}
		planner.allocations[key] = allocation
	}
	var err error
	planner.labels, err = newProviderLabelPlanner(input, planner)
	if err != nil {
		return nil, err
	}
	return planner, nil
}

func (planner *identityPlanner) planDimensions() error {
	for _, batch := range []struct {
		kind     domain.EntityKind
		entities []domain.ImportEntity
	}{
		{domain.EntityKindAccount, planner.input.Import.Accounts},
		{domain.EntityKindMerchant, planner.input.Import.Merchants},
		{domain.EntityKindGroup, planner.input.Import.Groups},
		{domain.EntityKindCategory, planner.input.Import.Categories},
	} {
		entities := append([]domain.ImportEntity(nil), batch.entities...)
		slices.SortFunc(entities, func(left, right domain.ImportEntity) int {
			return strings.Compare(left.ExternalID, right.ExternalID)
		})
		for _, imported := range entities {
			localID, err := planner.resolveIdentity(batch.kind, imported.ExternalID)
			if err != nil {
				return err
			}
			planner.imported[batch.kind][imported.ExternalID] = struct{}{}
			label, collisionKey, allocation, err := planner.labels.allocate(
				batch.kind, imported.ExternalID, localID, imported.Label,
			)
			if err != nil {
				return err
			}
			planner.allocations[externalIdentityKey(allocation.Namespace, allocation.ExternalID)] =
				allocation
			switch batch.kind {
			case domain.EntityKindAccount:
				planner.accounts[localID] = domain.Account{
					ID: localID, Label: label, CollisionKey: collisionKey,
				}
			case domain.EntityKindMerchant:
				planner.merchants[localID] = domain.Merchant{
					ID: localID, Label: label, CollisionKey: collisionKey,
				}
			case domain.EntityKindGroup:
				if localID == domain.UncategorizedGroupID {
					return errors.New("plan provider identities: provider group uses protected local ID")
				}
				planner.groups[localID] = domain.CategoryGroup{
					ID: localID, Label: label, CollisionKey: collisionKey,
				}
			case domain.EntityKindCategory:
				if localID == domain.UncategorizedCategoryID {
					return errors.New("plan provider identities: provider category uses protected local ID")
				}
				parentID := domain.UncategorizedGroupID
				if imported.ParentExternalID != "" {
					var parentErr error
					parentID, parentErr = planner.resolveExistingIdentity(
						domain.EntityKindGroup, imported.ParentExternalID,
					)
					if parentErr != nil {
						return parentErr
					}
				}
				planner.categories[localID] = domain.Category{
					ID: localID, GroupID: parentID, Label: label, CollisionKey: collisionKey,
				}
			}
		}
	}
	return nil
}

func (planner *identityPlanner) planTransactions() error {
	for id := range planner.providerEntityIDs[domain.EntityKindTransaction] {
		delete(planner.transactions, id)
	}
	transactions := append([]domain.ImportTransaction(nil), planner.input.Import.Transactions...)
	slices.SortFunc(transactions, func(left, right domain.ImportTransaction) int {
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	for _, imported := range transactions {
		localID, err := planner.resolveIdentity(domain.EntityKindTransaction, imported.ExternalID)
		if err != nil {
			return err
		}
		accountID, err := planner.resolveExistingIdentity(
			domain.EntityKindAccount, imported.AccountExternalID,
		)
		if err != nil {
			return err
		}
		merchantID, err := planner.resolveExistingIdentity(
			domain.EntityKindMerchant, imported.MerchantExternalID,
		)
		if err != nil {
			return err
		}
		categoryID := domain.UncategorizedCategoryID
		if imported.CategoryExternalID != "" {
			categoryID, err = planner.resolveExistingIdentity(
				domain.EntityKindCategory, imported.CategoryExternalID,
			)
			if err != nil {
				return err
			}
		}
		if existing, exists := planner.transactions[localID]; exists &&
			existing.Provider != planner.input.Provider {
			return fmt.Errorf("plan provider identities: transaction local ID %q is already active", localID)
		}
		planner.transactions[localID] = domain.TransactionRecord{
			ID: localID, ProviderID: imported.ExternalID, Provider: planner.input.Provider,
			AccountID: accountID, MerchantID: merchantID, CategoryID: categoryID,
			Date: imported.Date, Amount: imported.Amount, Notes: imported.Notes,
			Hidden: imported.Hidden, Pending: imported.Pending,
		}
		planner.imported[domain.EntityKindTransaction][imported.ExternalID] = struct{}{}
	}
	return nil
}

func (planner *identityPlanner) resolveIdentity(
	kind domain.EntityKind,
	externalID string,
) (domain.EntityID, error) {
	namespace := providerNamespace(planner.input.Provider, kind)
	key := externalIdentityKey(namespace, externalID)
	if identity, exists := planner.identities[key]; exists {
		if identity.EntityType != kind {
			return "", fmt.Errorf("plan provider identities: %s identity has wrong kind", externalID)
		}
		return identity.EntityID, nil
	}
	localID, exists := planner.input.ProposedIDs[ProviderIdentityKey(
		planner.input.Provider, kind, externalID,
	)]
	if !exists || localID == "" {
		return "", fmt.Errorf("plan provider identities: proposed local ID is missing for %s", externalID)
	}
	if reservedKind, reserved := planner.reservedIDs[localID]; reserved {
		return "", fmt.Errorf(
			"plan provider identities: proposed local ID %q is already reserved for %s",
			localID, reservedKind,
		)
	}
	identity := domain.ExternalIdentity{
		EntityType: kind, EntityID: localID, Namespace: namespace, ExternalID: externalID,
	}
	planner.identities[key] = identity
	planner.reservedIDs[localID] = kind
	planner.providerEntityIDs[kind][localID] = struct{}{}
	return localID, nil
}

func (planner *identityPlanner) resolveExistingIdentity(
	kind domain.EntityKind,
	externalID string,
) (domain.EntityID, error) {
	identity, exists := planner.identities[externalIdentityKey(
		providerNamespace(planner.input.Provider, kind), externalID,
	)]
	if !exists || identity.EntityType != kind {
		return "", fmt.Errorf("plan provider identities: related %s %q is not mapped", kind, externalID)
	}
	return identity.EntityID, nil
}

func (planner *identityPlanner) retireMissingEntities() {
	for id := range planner.providerEntityIDs[domain.EntityKindAccount] {
		identity := planner.externalIdentityForID(domain.EntityKindAccount, id)
		if _, present := planner.imported[domain.EntityKindAccount][identity.ExternalID]; !present {
			value := planner.accounts[id]
			value.Retired = true
			planner.accounts[id] = value
		}
	}
	for id := range planner.providerEntityIDs[domain.EntityKindMerchant] {
		identity := planner.externalIdentityForID(domain.EntityKindMerchant, id)
		if _, present := planner.imported[domain.EntityKindMerchant][identity.ExternalID]; !present {
			value := planner.merchants[id]
			value.Retired = true
			planner.merchants[id] = value
		}
	}
	for id := range planner.providerEntityIDs[domain.EntityKindGroup] {
		identity := planner.externalIdentityForID(domain.EntityKindGroup, id)
		if _, present := planner.imported[domain.EntityKindGroup][identity.ExternalID]; !present {
			value := planner.groups[id]
			value.Retired = true
			planner.groups[id] = value
		}
	}
	for id := range planner.providerEntityIDs[domain.EntityKindCategory] {
		identity := planner.externalIdentityForID(domain.EntityKindCategory, id)
		if _, present := planner.imported[domain.EntityKindCategory][identity.ExternalID]; !present {
			value := planner.categories[id]
			value.Retired = true
			planner.categories[id] = value
		}
	}
}

func (planner *identityPlanner) repairRetiredCategoryParents() {
	for id, category := range planner.categories {
		group, exists := planner.groups[category.GroupID]
		if category.Retired && (!exists || group.Retired) {
			category.GroupID = domain.UncategorizedGroupID
			planner.categories[id] = category
		}
	}
}

func (planner *identityPlanner) externalIdentityForID(
	kind domain.EntityKind,
	id domain.EntityID,
) domain.ExternalIdentity {
	namespace := providerNamespace(planner.input.Provider, kind)
	for _, identity := range planner.identities {
		if identity.Namespace == namespace && identity.EntityID == id {
			return identity
		}
	}
	return domain.ExternalIdentity{}
}

func (planner *identityPlanner) finish() IdentityPlan {
	plan := IdentityPlan{Committed: domain.CommittedProfile{
		Accounts:           mapValues(planner.accounts),
		Merchants:          mapValues(planner.merchants),
		Groups:             mapValues(planner.groups),
		Categories:         mapValues(planner.categories),
		Transactions:       mapValues(planner.transactions),
		ExternalIdentities: mapValues(planner.identities),
	}, Allocations: mapValues(planner.allocations)}
	slices.SortFunc(plan.Committed.Accounts, compareEntityID(func(value domain.Account) domain.EntityID { return value.ID }))
	slices.SortFunc(plan.Committed.Merchants, compareEntityID(func(value domain.Merchant) domain.EntityID { return value.ID }))
	slices.SortFunc(plan.Committed.Groups, compareEntityID(func(value domain.CategoryGroup) domain.EntityID { return value.ID }))
	slices.SortFunc(plan.Committed.Categories, compareEntityID(func(value domain.Category) domain.EntityID { return value.ID }))
	slices.SortFunc(plan.Committed.Transactions, compareEntityID(func(value domain.TransactionRecord) domain.EntityID { return value.ID }))
	slices.SortFunc(plan.Committed.ExternalIdentities, func(left, right domain.ExternalIdentity) int {
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	slices.SortFunc(plan.Allocations, func(left, right store.LabelAllocation) int {
		if comparison := strings.Compare(string(left.Kind), string(right.Kind)); comparison != 0 {
			return comparison
		}
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	return plan
}

func validateIdentityPlanningInput(input IdentityPlanningInput) error {
	if input.Provider == "" || strings.TrimSpace(input.Provider) != input.Provider ||
		strings.Contains(input.Provider, "/") {
		return errors.New("plan provider identities: provider is invalid")
	}
	if err := input.Import.Validate(); err != nil {
		return fmt.Errorf("plan provider identities: import: %w", err)
	}
	if err := input.Committed.Validate(); err != nil {
		return fmt.Errorf("plan provider identities: committed: %w", err)
	}
	if err := input.Effective.Validate(); err != nil {
		return fmt.Errorf("plan provider identities: effective: %w", err)
	}
	return nil
}

func providerNamespace(provider string, kind domain.EntityKind) string {
	return provider + "/" + string(kind)
}

func externalIdentityKey(namespace, externalID string) string {
	return namespace + "\x00" + externalID
}

func allProviderEntityKinds() []domain.EntityKind {
	return []domain.EntityKind{
		domain.EntityKindAccount,
		domain.EntityKindMerchant,
		domain.EntityKindGroup,
		domain.EntityKindCategory,
		domain.EntityKindTransaction,
	}
}

func mapValues[K comparable, V any](values map[K]V) []V {
	result := make([]V, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func compareEntityID[T any](id func(T) domain.EntityID) func(T, T) int {
	return func(left, right T) int {
		return strings.Compare(string(id(left)), string(id(right)))
	}
}

type typedEntityID struct {
	id   domain.EntityID
	kind domain.EntityKind
}

func effectiveEntityIDs(profile domain.CommittedProfile) []typedEntityID {
	ids := make([]typedEntityID, 0,
		len(profile.Accounts)+len(profile.Merchants)+len(profile.Groups)+
			len(profile.Categories)+len(profile.Transactions),
	)
	for _, value := range profile.Accounts {
		ids = append(ids, typedEntityID{value.ID, domain.EntityKindAccount})
	}
	for _, value := range profile.Merchants {
		ids = append(ids, typedEntityID{value.ID, domain.EntityKindMerchant})
	}
	for _, value := range profile.Groups {
		ids = append(ids, typedEntityID{value.ID, domain.EntityKindGroup})
	}
	for _, value := range profile.Categories {
		ids = append(ids, typedEntityID{value.ID, domain.EntityKindCategory})
	}
	for _, value := range profile.Transactions {
		ids = append(ids, typedEntityID{value.ID, domain.EntityKindTransaction})
	}
	return ids
}
