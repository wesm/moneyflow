package app

import (
	"github.com/wesm/moneyflow/internal/domain"
)

// KnownDrillDisposition distinguishes rows, known empty history, and never-observed identities.
type KnownDrillDisposition string

const (
	// DrillPopulated means at least one effective transaction occupies the complete identity.
	DrillPopulated KnownDrillDisposition = "populated"
	// DrillEmpty means the complete identity is valid but has no effective transaction.
	DrillEmpty KnownDrillDisposition = "empty"
	// DrillInvalid means neither effective state nor durable history recognizes the identity.
	DrillInvalid KnownDrillDisposition = "invalid"
)

// ClassifyKnownDrill resolves a complete analytical identity without changing URL state.
func ClassifyKnownDrill(
	snapshot EffectiveSnapshot,
	identity domain.DrillIdentity,
) KnownDrillDisposition {
	key, err := identity.CanonicalKey()
	if err != nil {
		return DrillInvalid
	}
	if drillHasTransaction(snapshot.Effective, identity) {
		return DrillPopulated
	}
	for _, known := range snapshot.KnownDrills {
		knownKey, knownErr := known.CanonicalKey()
		if knownErr == nil && knownKey == key {
			return DrillEmpty
		}
	}
	if drillEntityRetired(snapshot.Effective, identity) ||
		drillEntityIntroduced(snapshot.Committed, snapshot.Effective, identity) {
		return DrillEmpty
	}
	return DrillInvalid
}

func drillHasTransaction(
	profile domain.CommittedProfile,
	identity domain.DrillIdentity,
) bool {
	categories := make(map[domain.EntityID]domain.EntityID, len(profile.Categories))
	for _, category := range profile.Categories {
		categories[category.ID] = category.GroupID
	}
	for _, transaction := range profile.Transactions {
		if transaction.Amount.Currency != identity.Currency ||
			transaction.Amount.Scale != identity.Scale {
			continue
		}
		var key domain.EntityID
		switch identity.Dimension {
		case domain.DimensionAccount:
			key = transaction.AccountID
		case domain.DimensionMerchant:
			key = transaction.MerchantID
		case domain.DimensionCategory:
			key = transaction.CategoryID
		case domain.DimensionGroup:
			key = categories[transaction.CategoryID]
		}
		if string(key) == identity.Key {
			return true
		}
	}
	return false
}

func drillEntityRetired(
	profile domain.CommittedProfile,
	identity domain.DrillIdentity,
) bool {
	id := domain.EntityID(identity.Key)
	switch identity.Dimension {
	case domain.DimensionAccount:
		for _, value := range profile.Accounts {
			if value.ID == id {
				return value.Retired
			}
		}
	case domain.DimensionMerchant:
		for _, value := range profile.Merchants {
			if value.ID == id {
				return value.Retired
			}
		}
	case domain.DimensionCategory:
		for _, value := range profile.Categories {
			if value.ID == id {
				return value.Retired
			}
		}
	case domain.DimensionGroup:
		for _, value := range profile.Groups {
			if value.ID == id {
				return value.Retired
			}
		}
	}
	return false
}

func drillEntityIntroduced(
	committed domain.CommittedProfile,
	effective domain.CommittedProfile,
	identity domain.DrillIdentity,
) bool {
	id := domain.EntityID(identity.Key)
	return profileHasDrillEntity(effective, identity.Dimension, id) &&
		!profileHasDrillEntity(committed, identity.Dimension, id)
}

func profileHasDrillEntity(
	profile domain.CommittedProfile,
	dimension domain.Dimension,
	id domain.EntityID,
) bool {
	switch dimension {
	case domain.DimensionAccount:
		for _, value := range profile.Accounts {
			if value.ID == id {
				return true
			}
		}
	case domain.DimensionMerchant:
		for _, value := range profile.Merchants {
			if value.ID == id {
				return true
			}
		}
	case domain.DimensionCategory:
		for _, value := range profile.Categories {
			if value.ID == id {
				return true
			}
		}
	case domain.DimensionGroup:
		for _, value := range profile.Groups {
			if value.ID == id {
				return true
			}
		}
	}
	return false
}
