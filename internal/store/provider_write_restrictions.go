package store

import (
	"errors"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// MapProviderWriteRestrictions resolves provider facts to current committed owners.
// It is pure so refresh planning and authoritative store validation share the same mapping.
func MapProviderWriteRestrictions(kind string, candidate domain.ImportSnapshot, committed domain.CommittedProfile) ([]ProviderWriteRestriction, error) {
	if len(candidate.WriteRestrictions) == 0 {
		return nil, nil
	}
	if kind != "ynab" {
		return nil, errors.New("write restrictions require a YNAB binding")
	}
	identities := make(map[string]domain.EntityID, len(committed.ExternalIdentities))
	for _, identity := range committed.ExternalIdentities {
		identities[identity.Namespace+"\x00"+identity.ExternalID] = identity.EntityID
	}
	active := make(map[string]bool, len(committed.Transactions)+len(committed.Merchants))
	for _, row := range committed.Transactions {
		active["transaction\x00"+string(row.ID)] = true
	}
	for _, row := range committed.Merchants {
		active["merchant\x00"+string(row.ID)] = !row.Retired
	}
	result := make([]ProviderWriteRestriction, 0, len(candidate.WriteRestrictions))
	for _, restriction := range candidate.WriteRestrictions {
		id := identities[kind+"/"+string(restriction.Kind)+"\x00"+restriction.ExternalID]
		if restriction.Reason != "transfer" || !active[string(restriction.Kind)+"\x00"+string(id)] {
			return nil, errors.New("write restriction has no active committed owner")
		}
		result = append(result, ProviderWriteRestriction{Kind: restriction.Kind, EntityID: id, Reason: restriction.Reason})
	}
	slices.SortFunc(result, func(a, b ProviderWriteRestriction) int {
		return strings.Compare(string(a.Kind)+"\x00"+string(a.EntityID), string(b.Kind)+"\x00"+string(b.EntityID))
	})
	return slices.Compact(result), nil
}
