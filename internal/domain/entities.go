package domain

import (
	"errors"
	"io"
)

// EntityID is an opaque stable local identity.
type EntityID string

// EntityKind selects the fixed prefix used by a local identity.
type EntityKind string

// Supported entity kinds and protected profile sentinels.
const (
	// EntityKindAccount identifies account entities.
	EntityKindAccount EntityKind = "account"
	// EntityKindMerchant identifies merchant entities.
	EntityKindMerchant EntityKind = "merchant"
	// EntityKindGroup identifies category-group entities.
	EntityKindGroup EntityKind = "group"
	// EntityKindCategory identifies category entities.
	EntityKindCategory EntityKind = "category"
	// EntityKindTransaction identifies transaction entities.
	EntityKindTransaction EntityKind = "transaction"

	// UncategorizedGroupID and UncategorizedCategoryID are protected profile sentinels.
	UncategorizedGroupID EntityID = "group_system_uncategorized"
	// UncategorizedCategoryID is the protected fallback category.
	UncategorizedCategoryID EntityID = "category_system_uncategorized"
)

// Account is one stable local account identity.
type Account struct {
	ID           EntityID
	Label        string
	CollisionKey string
	Retired      bool
}

// Merchant is one stable local merchant identity.
type Merchant struct {
	ID               EntityID
	Label            string
	CollisionKey     string
	Retired          bool
	MergeDestination *EntityID
}

// CategoryGroup is one stable local category-group identity.
type CategoryGroup struct {
	ID               EntityID
	Label            string
	CollisionKey     string
	Protected        bool
	Retired          bool
	MergeDestination *EntityID
}

// Category is one stable local category identity.
type Category struct {
	ID               EntityID
	GroupID          EntityID
	Label            string
	CollisionKey     string
	Protected        bool
	Retired          bool
	MergeDestination *EntityID
}

// TransactionRecord stores one committed transaction using only stable local references.
type TransactionRecord struct {
	ID         EntityID
	ProviderID string
	Provider   string
	AccountID  EntityID
	MerchantID EntityID
	CategoryID EntityID
	Date       Date
	Amount     Money
	Notes      string
	Hidden     bool
	Pending    bool
	Metadata   map[string]string
}

// ExternalIdentity maps a provider-owned identity onto one stable local identity.
type ExternalIdentity struct {
	EntityType EntityKind
	EntityID   EntityID
	Namespace  string
	ExternalID string
}

// NewEntityID creates one opaque 128-bit local identity.
func NewEntityID(kind EntityKind, random io.Reader) (EntityID, error) {
	switch kind {
	case EntityKindAccount, EntityKindMerchant, EntityKindGroup, EntityKindCategory,
		EntityKindTransaction:
	default:
		return "", errors.New("new entity ID: invalid kind")
	}
	id, err := randomID(string(kind)+"_", random)
	return EntityID(id), err
}

// NewOperationID creates one opaque 128-bit journal identity.
func NewOperationID(random io.Reader) (string, error) {
	return randomID("operation_", random)
}
