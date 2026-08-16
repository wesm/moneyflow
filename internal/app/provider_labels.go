package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

const (
	providerSuffixSeparator = " · "
	minimumSuffixLength     = 4
	suffixLengthStep        = 2
)

type providerLabelPlanner struct {
	input        IdentityPlanningInput
	planner      *identityPlanner
	reservations map[domain.EntityKind]map[string]domain.EntityID
	candidates   map[domain.EntityKind][]providerLabelCandidate
	planned      map[string]plannedProviderLabel
}

type providerLabelCandidate struct {
	kind       domain.EntityKind
	externalID string
	localID    domain.EntityID
	label      string
	baseKey    string
}

type plannedProviderLabel struct {
	label        string
	collisionKey string
	allocation   store.LabelAllocation
	providerKey  string
}

func newProviderLabelPlanner(
	input IdentityPlanningInput,
	planner *identityPlanner,
) (*providerLabelPlanner, error) {
	labels := &providerLabelPlanner{
		input: input, planner: planner,
		reservations: make(map[domain.EntityKind]map[string]domain.EntityID),
		candidates:   make(map[domain.EntityKind][]providerLabelCandidate),
		planned:      make(map[string]plannedProviderLabel),
	}
	for _, kind := range allProviderEntityKinds()[:4] {
		labels.reservations[kind] = make(map[string]domain.EntityID)
	}
	labels.reserveEffectiveUserLabels()
	if err := labels.prepareCandidates(); err != nil {
		return nil, err
	}
	if err := labels.planAll(); err != nil {
		return nil, err
	}
	return labels, nil
}

func (planner *providerLabelPlanner) allocate(
	kind domain.EntityKind,
	externalID string,
	localID domain.EntityID,
	_ string,
) (string, string, store.LabelAllocation, error) {
	key := ProviderIdentityKey(planner.input.Provider, kind, externalID)
	planned, exists := planner.planned[key]
	if !exists || planned.allocation.ExternalID != externalID ||
		planned.allocation.Kind != kind || planned.allocation.Namespace != providerNamespace(
		planner.input.Provider, kind,
	) || planned.providerKey != key {
		return "", "", store.LabelAllocation{}, errors.New(
			"plan provider labels: prepared allocation is missing",
		)
	}
	identity, exists := planner.planner.identities[externalIdentityKey(
		planned.allocation.Namespace, externalID,
	)]
	if !exists || identity.EntityID != localID {
		return "", "", store.LabelAllocation{}, errors.New(
			"plan provider labels: prepared allocation has a different local ID",
		)
	}
	return planned.label, planned.collisionKey, planned.allocation, nil
}

func (planner *providerLabelPlanner) prepareCandidates() error {
	for _, batch := range []struct {
		kind     domain.EntityKind
		entities []domain.ImportEntity
	}{
		{domain.EntityKindAccount, planner.input.Import.Accounts},
		{domain.EntityKindMerchant, planner.input.Import.Merchants},
		{domain.EntityKindGroup, planner.input.Import.Groups},
		{domain.EntityKindCategory, planner.input.Import.Categories},
	} {
		for _, imported := range batch.entities {
			localID, err := planner.planner.resolveIdentity(batch.kind, imported.ExternalID)
			if err != nil {
				return err
			}
			baseKey, err := domain.CollisionKey(imported.Label)
			if err != nil {
				return fmt.Errorf("plan provider labels: %s label: %w", batch.kind, err)
			}
			planner.candidates[batch.kind] = append(
				planner.candidates[batch.kind],
				providerLabelCandidate{
					kind: batch.kind, externalID: imported.ExternalID, localID: localID,
					label: imported.Label, baseKey: baseKey,
				},
			)
		}
	}
	return nil
}

func (planner *providerLabelPlanner) planAll() error {
	for _, kind := range allProviderEntityKinds()[:4] {
		candidates := planner.candidates[kind]
		slices.SortFunc(candidates, func(left, right providerLabelCandidate) int {
			if comparison := strings.Compare(left.baseKey, right.baseKey); comparison != 0 {
				return comparison
			}
			return strings.Compare(left.externalID, right.externalID)
		})
		planner.reserveHistoricalOwners(kind, candidates)
		planner.reserveIncomingNaturalLabels(kind, candidates)

		// Existing unsuffixed owners claim their unchanged base before new colliders are considered.
		for _, candidate := range candidates {
			allocation, exists := planner.currentAllocation(candidate)
			if !exists || !allocation.Unsuffixed || allocation.BaseCollisionKey != candidate.baseKey {
				continue
			}
			if owner, reserved := planner.reservations[kind][candidate.baseKey]; !reserved || owner == candidate.localID {
				planner.reservations[kind][candidate.baseKey] = candidate.localID
			}
		}

		for _, candidate := range candidates {
			if err := planner.planCandidate(candidate); err != nil {
				return err
			}
		}
	}
	return nil
}

func (planner *providerLabelPlanner) reserveIncomingNaturalLabels(
	kind domain.EntityKind,
	candidates []providerLabelCandidate,
) {
	for _, candidate := range candidates {
		if _, reserved := planner.reservations[kind][candidate.baseKey]; !reserved {
			planner.reservations[kind][candidate.baseKey] = candidate.localID
		}
	}
}

func (planner *providerLabelPlanner) reserveHistoricalOwners(
	kind domain.EntityKind,
	candidates []providerLabelCandidate,
) {
	currentBases := make(map[string]string, len(candidates))
	for _, candidate := range candidates {
		currentBases[candidate.externalID] = candidate.baseKey
	}
	allocations := make([]store.LabelAllocation, 0)
	namespace := providerNamespace(planner.input.Provider, kind)
	for _, allocation := range planner.planner.allocations {
		if allocation.Kind == kind && allocation.Namespace == namespace {
			allocations = append(allocations, allocation)
		}
	}
	slices.SortFunc(allocations, func(left, right store.LabelAllocation) int {
		return strings.Compare(left.ExternalID, right.ExternalID)
	})
	for _, allocation := range allocations {
		identity, exists := planner.planner.identities[externalIdentityKey(
			allocation.Namespace, allocation.ExternalID,
		)]
		if !exists {
			continue
		}
		key := allocation.BaseCollisionKey
		if !allocation.Unsuffixed {
			var err error
			key, err = domain.CollisionKey(allocation.DisplayLabel)
			if err != nil {
				continue
			}
		} else if currentBase, present := currentBases[allocation.ExternalID]; present &&
			currentBase != allocation.BaseCollisionKey {
			continue
		}
		if _, reserved := planner.reservations[kind][key]; !reserved {
			planner.reservations[kind][key] = identity.EntityID
		}
	}
}

func (planner *providerLabelPlanner) planCandidate(candidate providerLabelCandidate) error {
	allocation, hasAllocation := planner.currentAllocation(candidate)
	owner, baseReserved := planner.reservations[candidate.kind][candidate.baseKey]
	canOwnBase := !baseReserved || owner == candidate.localID
	stickyBase := hasAllocation && allocation.BaseCollisionKey == candidate.baseKey

	if canOwnBase && (!stickyBase || allocation.Unsuffixed) {
		planner.reservations[candidate.kind][candidate.baseKey] = candidate.localID
		return planner.storePlanned(candidate, candidate.label, "", true)
	}
	if stickyBase && !allocation.Unsuffixed {
		if err := planner.tryStoredSuffix(candidate, allocation.SuffixToken); err == nil {
			return nil
		}
	}
	return planner.planSuffixed(candidate, allocation)
}

func (planner *providerLabelPlanner) tryStoredSuffix(
	candidate providerLabelCandidate,
	suffix string,
) error {
	label := candidate.label + providerSuffixSeparator + suffix
	key, err := domain.CollisionKey(label)
	if err != nil {
		return err
	}
	if owner, reserved := planner.reservations[candidate.kind][key]; reserved && owner != candidate.localID {
		return errors.New("stored suffix collides")
	}
	planner.reservations[candidate.kind][key] = candidate.localID
	return planner.storePlanned(candidate, label, suffix, false)
}

func (planner *providerLabelPlanner) planSuffixed(
	candidate providerLabelCandidate,
	allocation store.LabelAllocation,
) error {
	providerKey := ProviderIdentityKey(
		planner.input.Provider, candidate.kind, candidate.externalID,
	)
	material := planner.input.ProposedSuffixes[providerKey]
	if !validSuffixMaterial(material) {
		return fmt.Errorf(
			"plan provider labels: suffix material is missing or invalid for %s",
			candidate.externalID,
		)
	}
	start := minimumSuffixLength
	if allocation.BaseCollisionKey == candidate.baseKey && len(allocation.SuffixToken) >= start {
		material = allocation.SuffixToken + material
		start = len(allocation.SuffixToken) + suffixLengthStep
	}
	for length := start; length <= len(material); length += suffixLengthStep {
		suffix := material[:length]
		label := candidate.label + providerSuffixSeparator + suffix
		key, err := domain.CollisionKey(label)
		if err != nil {
			return err
		}
		if owner, reserved := planner.reservations[candidate.kind][key]; reserved && owner != candidate.localID {
			continue
		}
		planner.reservations[candidate.kind][key] = candidate.localID
		return planner.storePlanned(candidate, label, suffix, false)
	}
	return fmt.Errorf("plan provider labels: suffix space exhausted for %s", candidate.externalID)
}

func (planner *providerLabelPlanner) storePlanned(
	candidate providerLabelCandidate,
	label string,
	suffix string,
	unsuffixed bool,
) error {
	collisionKey, err := domain.CollisionKey(label)
	if err != nil {
		return err
	}
	namespace := providerNamespace(planner.input.Provider, candidate.kind)
	key := ProviderIdentityKey(planner.input.Provider, candidate.kind, candidate.externalID)
	planner.planned[key] = plannedProviderLabel{
		label: label, collisionKey: collisionKey, providerKey: key,
		allocation: store.LabelAllocation{
			Kind: candidate.kind, Namespace: namespace, ExternalID: candidate.externalID,
			BaseCollisionKey: candidate.baseKey, DisplayLabel: label,
			SuffixToken: suffix, Unsuffixed: unsuffixed,
		},
	}
	return nil
}

func (planner *providerLabelPlanner) currentAllocation(
	candidate providerLabelCandidate,
) (store.LabelAllocation, bool) {
	allocation, exists := planner.planner.allocations[externalIdentityKey(
		providerNamespace(planner.input.Provider, candidate.kind), candidate.externalID,
	)]
	return allocation, exists
}

func (planner *providerLabelPlanner) reserveEffectiveUserLabels() {
	providerIDs := planner.planner.providerEntityIDs
	committedLabels := profileEntityLabels(planner.input.Committed)
	for kind, labels := range profileEntityLabels(planner.input.Effective) {
		for id, value := range labels {
			if value.retired {
				continue
			}
			_, providerOwned := providerIDs[kind][id]
			committed, committedExists := committedLabels[kind][id]
			pendingOverride := providerOwned && committedExists &&
				(committed.label != value.label || committed.collisionKey != value.collisionKey)
			if !providerOwned || pendingOverride {
				planner.reservations[kind][value.collisionKey] = id
			}
		}
	}
}

type entityLabel struct {
	label        string
	collisionKey string
	retired      bool
}

func profileEntityLabels(
	profile domain.CommittedProfile,
) map[domain.EntityKind]map[domain.EntityID]entityLabel {
	labels := map[domain.EntityKind]map[domain.EntityID]entityLabel{
		domain.EntityKindAccount:  {},
		domain.EntityKindMerchant: {},
		domain.EntityKindGroup:    {},
		domain.EntityKindCategory: {},
	}
	for _, value := range profile.Accounts {
		labels[domain.EntityKindAccount][value.ID] = entityLabel{value.Label, value.CollisionKey, value.Retired}
	}
	for _, value := range profile.Merchants {
		labels[domain.EntityKindMerchant][value.ID] = entityLabel{value.Label, value.CollisionKey, value.Retired}
	}
	for _, value := range profile.Groups {
		labels[domain.EntityKindGroup][value.ID] = entityLabel{value.Label, value.CollisionKey, value.Retired}
	}
	for _, value := range profile.Categories {
		labels[domain.EntityKindCategory][value.ID] = entityLabel{value.Label, value.CollisionKey, value.Retired}
	}
	return labels
}

func validSuffixMaterial(value string) bool {
	if len(value) < minimumSuffixLength || len(value)%suffixLengthStep != 0 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}
