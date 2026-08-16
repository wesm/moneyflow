package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ImportEntity is one provider-owned dimension identity before local ID mapping.
type ImportEntity struct {
	Kind       EntityKind
	ExternalID string
	Label      string
	// ParentExternalID is empty only when a category uses the protected Uncategorized group.
	ParentExternalID string
}

// ImportTransaction is one provider-owned transaction before local ID mapping.
type ImportTransaction struct {
	ExternalID         string
	AccountExternalID  string
	MerchantExternalID string
	// CategoryExternalID is empty only when the protected Uncategorized category applies.
	CategoryExternalID string
	Date               Date
	Amount             Money
	Notes              string
	Hidden             bool
	Pending            bool
}

// ImportSnapshot is one complete provider observation ready for identity mapping and persistence.
// Provider adapters may inspect pending rows while checking snapshot integrity, but omit them here.
type ImportSnapshot struct {
	Accounts     []ImportEntity
	Merchants    []ImportEntity
	Groups       []ImportEntity
	Categories   []ImportEntity
	Transactions []ImportTransaction
	ObservedAt   time.Time
}

// Clone returns a snapshot with independently owned slices.
func (snapshot ImportSnapshot) Clone() ImportSnapshot {
	snapshot.Accounts = append([]ImportEntity(nil), snapshot.Accounts...)
	snapshot.Merchants = append([]ImportEntity(nil), snapshot.Merchants...)
	snapshot.Groups = append([]ImportEntity(nil), snapshot.Groups...)
	snapshot.Categories = append([]ImportEntity(nil), snapshot.Categories...)
	snapshot.Transactions = append([]ImportTransaction(nil), snapshot.Transactions...)
	return snapshot
}

// Validate checks provider-neutral identity, relationship, date, and exact-money invariants.
func (snapshot ImportSnapshot) Validate() error {
	if snapshot.ObservedAt.IsZero() {
		return errors.New("validate import snapshot: observation time is zero")
	}
	accounts, err := validateImportEntities("account", EntityKindAccount, snapshot.Accounts, false)
	if err != nil {
		return err
	}
	merchants, err := validateImportEntities(
		"merchant", EntityKindMerchant, snapshot.Merchants, false,
	)
	if err != nil {
		return err
	}
	groups, err := validateImportEntities("group", EntityKindGroup, snapshot.Groups, false)
	if err != nil {
		return err
	}
	categories, err := validateImportEntities(
		"category", EntityKindCategory, snapshot.Categories, true,
	)
	if err != nil {
		return err
	}
	for index, category := range snapshot.Categories {
		if category.ParentExternalID == "" {
			continue
		}
		if _, ok := groups[category.ParentExternalID]; !ok {
			return fmt.Errorf(
				"validate import snapshot: category[%d] references unknown group",
				index,
			)
		}
	}

	transactions := make(map[string]struct{}, len(snapshot.Transactions))
	for index, transaction := range snapshot.Transactions {
		if err := validateImportTransaction(transaction); err != nil {
			return fmt.Errorf("validate import snapshot: transaction[%d]: %w", index, err)
		}
		if _, duplicate := transactions[transaction.ExternalID]; duplicate {
			return fmt.Errorf(
				"validate import snapshot: duplicate external identity for transaction %q",
				transaction.ExternalID,
			)
		}
		transactions[transaction.ExternalID] = struct{}{}
		if _, ok := accounts[transaction.AccountExternalID]; !ok {
			return fmt.Errorf("validate import snapshot: transaction[%d] references unknown account", index)
		}
		if _, ok := merchants[transaction.MerchantExternalID]; !ok {
			return fmt.Errorf("validate import snapshot: transaction[%d] references unknown merchant", index)
		}
		if transaction.CategoryExternalID != "" {
			if _, ok := categories[transaction.CategoryExternalID]; !ok {
				return fmt.Errorf("validate import snapshot: transaction[%d] references unknown category", index)
			}
		}
	}
	return nil
}

func validateImportEntities(
	name string,
	wantKind EntityKind,
	entities []ImportEntity,
	permitParent bool,
) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(entities))
	for index, entity := range entities {
		if entity.Kind != wantKind {
			return nil, fmt.Errorf("validate import snapshot: %s[%d] has invalid kind", name, index)
		}
		if entity.ExternalID == "" || strings.TrimSpace(entity.ExternalID) != entity.ExternalID {
			return nil, fmt.Errorf("validate import snapshot: %s[%d] has invalid external ID", name, index)
		}
		normalizedLabel, labelErr := NormalizeDisplayLabel(entity.Label)
		if labelErr != nil || normalizedLabel != entity.Label {
			return nil, fmt.Errorf("validate import snapshot: %s[%d] has invalid label", name, index)
		}
		if !permitParent && entity.ParentExternalID != "" {
			return nil, fmt.Errorf("validate import snapshot: %s[%d] has unexpected parent", name, index)
		}
		if _, duplicate := seen[entity.ExternalID]; duplicate {
			return nil, fmt.Errorf(
				"validate import snapshot: duplicate external identity for %s %q",
				name,
				entity.ExternalID,
			)
		}
		seen[entity.ExternalID] = struct{}{}
	}
	return seen, nil
}

func validateImportTransaction(transaction ImportTransaction) error {
	for name, value := range map[string]string{
		"external ID": transaction.ExternalID,
		"account":     transaction.AccountExternalID,
		"merchant":    transaction.MerchantExternalID,
	} {
		if value == "" || strings.TrimSpace(value) != value {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if transaction.CategoryExternalID != "" &&
		strings.TrimSpace(transaction.CategoryExternalID) != transaction.CategoryExternalID {
		return errors.New("invalid category")
	}
	if _, err := NewDate(transaction.Date.Year(), transaction.Date.Month(), transaction.Date.Day()); err != nil {
		return fmt.Errorf("invalid date: %w", err)
	}
	if !validCurrency(transaction.Amount.Currency) || transaction.Amount.Scale > 9 {
		return errors.New("invalid money partition")
	}
	return nil
}
