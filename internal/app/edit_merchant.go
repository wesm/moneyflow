package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

// BuildMerchantOperation resolves one merchant-edit intent into a deterministic journal draft.
func BuildMerchantOperation(
	snapshot EffectiveSnapshot,
	request MutationRequest,
	metadata OperationMetadata,
) (MutationPlan, error) {
	if request.Action != ActionEditMerchant {
		return MutationPlan{}, mutationError(
			MutationInvalidOperation,
			errors.New("merchant builder received another action"),
		)
	}
	if err := validateMutationMetadata(metadata); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	targets, err := ResolveTargets(snapshot, request)
	if err != nil {
		return MutationPlan{}, err
	}

	operation := newMutationOperation(request, metadata)
	switch request.Input.Scope {
	case EditScopeEntity:
		err = buildMerchantEntityEdit(&operation, snapshot.Effective, request.Input, targets)
	case EditScopeTransactions:
		err = buildMerchantReassignment(&operation, snapshot.Effective, request.Input, targets)
	default:
		err = errors.New("merchant edit scope is invalid")
	}
	if err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	if err = operation.ValidateDraft(); err != nil {
		return MutationPlan{}, mutationError(MutationInvalidOperation, err)
	}
	return mutationPlan(request, targets, operation), nil
}

func buildMerchantEntityEdit(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
	targets ResolvedTargets,
) error {
	sourceID, err := singleSourceMerchant(profile, targets)
	if err != nil {
		return err
	}
	source, ok := merchantWithID(profile, sourceID)
	if !ok || source.Retired {
		return errors.New("source merchant is retired or missing")
	}
	key, err := domain.CollisionKey(input.Label)
	if err != nil {
		return err
	}
	if source.Label == input.Label {
		return errors.New("merchant label is unchanged")
	}

	collision, collided := activeMerchantWithCollisionKey(profile, key)
	if collided && collision.ID != sourceID {
		if input.DestinationID == "" || input.DestinationID != collision.ID {
			return errors.New("merchant collision requires the explicit merge destination")
		}
		operation.Type = domain.OperationMerchantMerge
		operation.Targets = []domain.EntityID{sourceID}
		operation.Merge = &domain.MergePayload{
			SourceID: sourceID, DestinationID: collision.ID,
		}
		return nil
	}
	if input.DestinationID != "" && input.DestinationID != sourceID {
		return errors.New("merchant rename destination does not match source")
	}
	operation.Type = domain.OperationMerchantLabel
	operation.Targets = []domain.EntityID{sourceID}
	operation.Label = &domain.LabelPayload{
		EntityID: sourceID, Label: input.Label, CollisionKey: key,
	}
	return nil
}

func buildMerchantReassignment(
	operation *domain.Operation,
	profile domain.CommittedProfile,
	input EditInput,
	targets ResolvedTargets,
) error {
	if len(targets.TransactionIDs) == 0 {
		return errors.New("merchant reassignment has no transaction targets")
	}
	if input.DestinationID == "" {
		return errors.New("merchant reassignment destination is empty")
	}
	operation.Type = domain.OperationMerchantReassign
	operation.Targets = append([]domain.EntityID(nil), targets.TransactionIDs...)
	operation.Reassign = &domain.ReassignPayload{DestinationID: input.DestinationID}

	destination, found := merchantWithID(profile, input.DestinationID)
	if found {
		if destination.Retired {
			return errors.New("merchant reassignment destination is retired")
		}
		return nil
	}
	key, err := domain.CollisionKey(input.Label)
	if err != nil {
		return err
	}
	if _, collided := activeMerchantWithCollisionKey(profile, key); collided {
		return errors.New("new merchant label collides with an existing merchant")
	}
	operation.Reassign.CreatedMerchant = &domain.Merchant{
		ID: input.DestinationID, Label: input.Label, CollisionKey: key,
	}
	return nil
}

func singleSourceMerchant(
	profile domain.CommittedProfile,
	targets ResolvedTargets,
) (domain.EntityID, error) {
	if len(targets.EntityIDs) == 1 {
		if _, ok := merchantWithID(profile, targets.EntityIDs[0]); ok {
			return targets.EntityIDs[0], nil
		}
	}
	var sourceID domain.EntityID
	for _, targetID := range targets.TransactionIDs {
		transaction, ok := transactionRecordWithID(profile, targetID)
		if !ok {
			return "", errors.New("merchant edit transaction target is missing")
		}
		if sourceID == "" {
			sourceID = transaction.MerchantID
			continue
		}
		if sourceID != transaction.MerchantID {
			return "", errors.New("entity merchant edit spans multiple merchants")
		}
	}
	if sourceID == "" {
		return "", errors.New("merchant edit has no source merchant")
	}
	return sourceID, nil
}

func merchantWithID(
	profile domain.CommittedProfile,
	id domain.EntityID,
) (domain.Merchant, bool) {
	for _, merchant := range profile.Merchants {
		if merchant.ID == id {
			return merchant, true
		}
	}
	return domain.Merchant{}, false
}

func activeMerchantWithCollisionKey(
	profile domain.CommittedProfile,
	key string,
) (domain.Merchant, bool) {
	for _, merchant := range profile.Merchants {
		if !merchant.Retired && merchant.CollisionKey == key {
			return merchant, true
		}
	}
	return domain.Merchant{}, false
}

func transactionRecordWithID(
	profile domain.CommittedProfile,
	id domain.EntityID,
) (domain.TransactionRecord, bool) {
	for _, transaction := range profile.Transactions {
		if transaction.ID == id {
			return transaction, true
		}
	}
	return domain.TransactionRecord{}, false
}
