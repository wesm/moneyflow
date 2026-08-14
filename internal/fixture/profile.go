package fixture

import (
	"fmt"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// SyntheticNamespace identifies external IDs originating in the committed fixture.
const SyntheticNamespace = "fixture"

const syntheticUncategorizedCategoryID = "category-uncategorized"

// CommittedProfile converts validated fixture transactions into stable first-class entities.
func CommittedProfile(transactions []domain.Transaction) (domain.CommittedProfile, error) {
	uncategorizedGroupID, err := syntheticGroupID("Uncategorized")
	if err != nil {
		return domain.CommittedProfile{}, fmt.Errorf("convert fixture profile: Uncategorized group: %w", err)
	}
	accounts := make(map[domain.EntityID]domain.Account)
	merchants := make(map[domain.EntityID]domain.Merchant)
	groups := map[domain.EntityID]domain.CategoryGroup{
		domain.UncategorizedGroupID: {
			ID: domain.UncategorizedGroupID, Label: "Uncategorized",
			CollisionKey: "uncategorized", Protected: true,
		},
	}
	categories := map[domain.EntityID]domain.Category{
		domain.UncategorizedCategoryID: {
			ID: domain.UncategorizedCategoryID, GroupID: domain.UncategorizedGroupID,
			Label: "Uncategorized", CollisionKey: "uncategorized", Protected: true,
		},
	}
	records := make([]domain.TransactionRecord, 0, len(transactions))
	external := make(map[string]domain.ExternalIdentity)

	for index, transaction := range transactions {
		accountID := domain.EntityID(transaction.Account.ID)
		merchantID := domain.EntityID(transaction.Merchant.ID)
		rawGroupID := domain.EntityID(transaction.Category.GroupID)
		groupID := rawGroupID
		if transaction.Category.GroupID == uncategorizedGroupID {
			groupID = domain.UncategorizedGroupID
		}
		rawCategoryID := domain.EntityID(transaction.Category.ID)
		categoryID := rawCategoryID
		if transaction.Category.ID == syntheticUncategorizedCategoryID {
			categoryID = domain.UncategorizedCategoryID
		}
		if groupID == domain.UncategorizedGroupID {
			groupKey, keyErr := domain.CollisionKey(transaction.Category.Group)
			if keyErr != nil || groupKey != "uncategorized" {
				return domain.CommittedProfile{}, fmt.Errorf(
					"convert fixture profile: transaction[%d]: protected group identity is inconsistent",
					index,
				)
			}
		}
		if categoryID == domain.UncategorizedCategoryID {
			categoryKey, keyErr := domain.CollisionKey(transaction.Category.Name)
			if keyErr != nil || categoryKey != "uncategorized" ||
				groupID != domain.UncategorizedGroupID {
				return domain.CommittedProfile{}, fmt.Errorf(
					"convert fixture profile: transaction[%d]: protected category identity is inconsistent",
					index,
				)
			}
		}

		account, err := fixtureAccount(accountID, transaction.Account.Name)
		if err != nil {
			return domain.CommittedProfile{}, fmt.Errorf("convert fixture profile: transaction[%d]: %w", index, err)
		}
		if err = addFixtureAccount(accounts, account); err != nil {
			return domain.CommittedProfile{}, err
		}
		merchant, err := fixtureMerchant(merchantID, transaction.Merchant.Name)
		if err != nil {
			return domain.CommittedProfile{}, fmt.Errorf("convert fixture profile: transaction[%d]: %w", index, err)
		}
		if err = addFixtureMerchant(merchants, merchant); err != nil {
			return domain.CommittedProfile{}, err
		}
		group, err := fixtureGroup(groupID, transaction.Category.Group)
		if err != nil {
			return domain.CommittedProfile{}, fmt.Errorf("convert fixture profile: transaction[%d]: %w", index, err)
		}
		if groupID != domain.UncategorizedGroupID {
			if err = addFixtureGroup(groups, group); err != nil {
				return domain.CommittedProfile{}, err
			}
		}
		category, err := fixtureCategory(categoryID, groupID, transaction.Category.Name)
		if err != nil {
			return domain.CommittedProfile{}, fmt.Errorf("convert fixture profile: transaction[%d]: %w", index, err)
		}
		if categoryID != domain.UncategorizedCategoryID {
			if err = addFixtureCategory(categories, category); err != nil {
				return domain.CommittedProfile{}, err
			}
		}

		transactionID := domain.EntityID(transaction.ID)
		records = append(records, domain.TransactionRecord{
			ID: transactionID, ProviderID: transaction.ProviderID, Provider: transaction.Provider,
			AccountID: accountID, MerchantID: merchantID, CategoryID: categoryID,
			Date: transaction.Date, Amount: transaction.Amount, Notes: transaction.Notes,
			Hidden: transaction.Hidden, Pending: transaction.Pending,
			Metadata: cloneMetadata(transaction.Metadata),
		})
		for _, identity := range []domain.ExternalIdentity{
			{EntityType: domain.EntityKindAccount, EntityID: accountID, Namespace: SyntheticNamespace, ExternalID: transaction.Account.ID},
			{EntityType: domain.EntityKindMerchant, EntityID: merchantID, Namespace: SyntheticNamespace, ExternalID: transaction.Merchant.ID},
			{EntityType: domain.EntityKindGroup, EntityID: groupID, Namespace: SyntheticNamespace, ExternalID: transaction.Category.GroupID},
			{EntityType: domain.EntityKindCategory, EntityID: categoryID, Namespace: SyntheticNamespace, ExternalID: transaction.Category.ID},
			{EntityType: domain.EntityKindTransaction, EntityID: transactionID, Namespace: SyntheticNamespace, ExternalID: transaction.ProviderID},
		} {
			if err = addExternalIdentity(external, identity); err != nil {
				return domain.CommittedProfile{}, fmt.Errorf(
					"convert fixture profile: transaction[%d]: %w",
					index,
					err,
				)
			}
		}
	}

	profile := domain.CommittedProfile{
		Accounts: values(accounts), Merchants: values(merchants), Groups: values(groups),
		Categories: values(categories), Transactions: records, ExternalIdentities: values(external),
	}
	slices.SortFunc(profile.Accounts, func(left, right domain.Account) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Merchants, func(left, right domain.Merchant) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Groups, func(left, right domain.CategoryGroup) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Categories, func(left, right domain.Category) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.Transactions, func(left, right domain.TransactionRecord) int {
		return strings.Compare(string(left.ID), string(right.ID))
	})
	slices.SortFunc(profile.ExternalIdentities, func(left, right domain.ExternalIdentity) int {
		if comparison := strings.Compare(left.Namespace, right.Namespace); comparison != 0 {
			return comparison
		}
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	if err := profile.Validate(); err != nil {
		return domain.CommittedProfile{}, fmt.Errorf("convert fixture profile: %w", err)
	}
	return profile, nil
}

func fixtureAccount(id domain.EntityID, label string) (domain.Account, error) {
	key, err := domain.CollisionKey(label)
	return domain.Account{ID: id, Label: label, CollisionKey: key}, err
}

func fixtureMerchant(id domain.EntityID, label string) (domain.Merchant, error) {
	key, err := domain.CollisionKey(label)
	return domain.Merchant{ID: id, Label: label, CollisionKey: key}, err
}

func fixtureGroup(id domain.EntityID, label string) (domain.CategoryGroup, error) {
	key, err := domain.CollisionKey(label)
	return domain.CategoryGroup{ID: id, Label: label, CollisionKey: key}, err
}

func fixtureCategory(
	id domain.EntityID,
	groupID domain.EntityID,
	label string,
) (domain.Category, error) {
	key, err := domain.CollisionKey(label)
	return domain.Category{ID: id, GroupID: groupID, Label: label, CollisionKey: key}, err
}

func addFixtureAccount(values map[domain.EntityID]domain.Account, candidate domain.Account) error {
	if previous, exists := values[candidate.ID]; exists && previous != candidate {
		return fmt.Errorf("convert fixture profile: inconsistent account %q", candidate.ID)
	}
	values[candidate.ID] = candidate
	return nil
}

func addFixtureMerchant(values map[domain.EntityID]domain.Merchant, candidate domain.Merchant) error {
	if previous, exists := values[candidate.ID]; exists &&
		(previous.ID != candidate.ID || previous.Label != candidate.Label ||
			previous.CollisionKey != candidate.CollisionKey) {
		return fmt.Errorf("convert fixture profile: inconsistent merchant %q", candidate.ID)
	}
	values[candidate.ID] = candidate
	return nil
}

func addFixtureGroup(values map[domain.EntityID]domain.CategoryGroup, candidate domain.CategoryGroup) error {
	if previous, exists := values[candidate.ID]; exists &&
		(previous.ID != candidate.ID || previous.Label != candidate.Label ||
			previous.CollisionKey != candidate.CollisionKey) {
		return fmt.Errorf("convert fixture profile: inconsistent group %q", candidate.ID)
	}
	values[candidate.ID] = candidate
	return nil
}

func addFixtureCategory(values map[domain.EntityID]domain.Category, candidate domain.Category) error {
	if previous, exists := values[candidate.ID]; exists &&
		(previous.ID != candidate.ID || previous.GroupID != candidate.GroupID ||
			previous.Label != candidate.Label || previous.CollisionKey != candidate.CollisionKey) {
		return fmt.Errorf("convert fixture profile: inconsistent category %q", candidate.ID)
	}
	values[candidate.ID] = candidate
	return nil
}

func addExternalIdentity(
	identities map[string]domain.ExternalIdentity,
	identity domain.ExternalIdentity,
) error {
	key := identity.Namespace + "\x00" + identity.ExternalID
	if previous, exists := identities[key]; exists && previous != identity {
		return fmt.Errorf("external identity %q is ambiguous", identity.ExternalID)
	}
	identities[key] = identity
	return nil
}

func values[K comparable, V any](input map[K]V) []V {
	output := make([]V, 0, len(input))
	for _, value := range input {
		output = append(output, value)
	}
	return output
}

func cloneMetadata(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
