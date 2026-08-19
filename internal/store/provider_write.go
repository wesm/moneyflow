package store

import (
	"errors"
	"fmt"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// WriteBatchPhase is the durable provider-write lifecycle state.
type WriteBatchPhase string

// WriteResumeTarget records which operation kind must be resumed after a parked phase.
type WriteResumeTarget string

const (
	// WritePhaseWriting sends eligible absolute transaction items.
	WritePhaseWriting WriteBatchPhase = "writing"
	// WritePhaseReconciling folds successful results or an authoritative snapshot.
	WritePhaseReconciling WriteBatchPhase = "reconciling"
	// WritePhasePaused requires explicit user resumption.
	WritePhasePaused WriteBatchPhase = "paused"
	// WritePhaseReconnectRequired waits for replacement session material.
	WritePhaseReconnectRequired WriteBatchPhase = "reconnect_required"
	// WritePhaseRateLimited waits until the provider eligibility time.
	WritePhaseRateLimited WriteBatchPhase = "rate_limited"
	// WritePhaseAttentionRequired requires retry or reconciliation choice.
	WritePhaseAttentionRequired WriteBatchPhase = "attention_required"
	// WritePhaseReconcileConfirmationRequired waits for an explicit deletion confirmation.
	WritePhaseReconcileConfirmationRequired WriteBatchPhase = "reconcile_confirmation_required"

	// WriteResumeWriting returns a parked batch to transaction writes or finalization.
	WriteResumeWriting WriteResumeTarget = "writing"
	// WriteResumeReconciling returns a parked batch to authoritative provider reconciliation.
	WriteResumeReconciling WriteResumeTarget = "reconciling"
)

// WriteExpectationKind describes how a merchant response is interpreted.
type WriteExpectationKind string

const (
	// WriteExpectationExisting expects one active provider merchant identity.
	WriteExpectationExisting WriteExpectationKind = "existing"
	// WriteExpectationMergeDestination expects the active merge destination identity.
	WriteExpectationMergeDestination WriteExpectationKind = "merge_destination"
	// WriteExpectationNew establishes a new or rotated provider identity.
	WriteExpectationNew WriteExpectationKind = "new"
)

// WriteItemState is one durable outbound item's state.
type WriteItemState string

// WriteItemKind distinguishes absolute field updates from transaction deletion.
type WriteItemKind string

const (
	// WriteItemUpdate applies one or more absolute transaction field values.
	WriteItemUpdate WriteItemKind = "update"
	// WriteItemDelete removes one transaction from the provider.
	WriteItemDelete WriteItemKind = "delete"

	// WriteItemPending remains eligible for a provider attempt.
	WriteItemPending WriteItemState = "pending"
	// WriteItemSucceeded has a normalized durable response.
	WriteItemSucceeded WriteItemState = "succeeded"
	// WriteItemFailed requires retry or reconciliation.
	WriteItemFailed WriteItemState = "failed"
)

// WriteAttentionClass controls whether Resume can retry an attention state.
type WriteAttentionClass string

const (
	// WriteAttentionRetryable permits an explicit Resume attempt.
	WriteAttentionRetryable WriteAttentionClass = "retryable"
	// WriteAttentionReconcileOnly can only abandon intent into authoritative refresh.
	WriteAttentionReconcileOnly WriteAttentionClass = "reconcile_only"
)

// WriteAttentionReason is an allowlisted, value-free durable failure reason.
type WriteAttentionReason string

const (
	// WriteAttentionUnavailableExhausted means bounded transport attempts were exhausted.
	WriteAttentionUnavailableExhausted WriteAttentionReason = "provider_write_unavailable_exhausted"
	// WriteAttentionResponseIncomplete means the provider outcome could not be normalized.
	WriteAttentionResponseIncomplete WriteAttentionReason = "provider_write_response_incomplete"
	// WriteAttentionOutcomeUnknown means the request may have been applied and must be reconciled.
	WriteAttentionOutcomeUnknown WriteAttentionReason = "provider_write_outcome_unknown"
	// WriteAttentionTargetNotFound means a remote transaction or category disappeared.
	WriteAttentionTargetNotFound WriteAttentionReason = "provider_write_target_not_found"
	// WriteAttentionRejected means the provider deterministically rejected the desired value.
	WriteAttentionRejected WriteAttentionReason = "provider_write_rejected"
	// WriteAttentionIdentityConflict means a strict new-name group returned contradictory identity.
	WriteAttentionIdentityConflict WriteAttentionReason = "provider_write_identity_conflict"
	// WriteAttentionRetiredIdentity means a response resolved to an invalid retired owner.
	WriteAttentionRetiredIdentity WriteAttentionReason = "provider_write_retired_identity"
	// WriteAttentionExpectationInvalid means persisted expectation invariants failed.
	WriteAttentionExpectationInvalid WriteAttentionReason = "provider_write_expectation_invalid"
)

// ProviderIdentityLineage retains provider identities moved out of active mappings.
type ProviderIdentityLineage struct {
	Kind           domain.EntityKind
	Namespace      string
	ExternalID     string
	PriorLocalID   domain.EntityID
	CurrentLocalID domain.EntityID
	ProviderLabel  string
	Disposition    string
	BatchVersion   uint64
}

// WriteBatch is the identity-free durable batch status plus internal frozen-prefix identity.
type WriteBatch struct {
	ID                   string
	Phase                WriteBatchPhase
	ResumeTarget         WriteResumeTarget
	Version              uint64
	ReviewedRevision     uint64
	PreparedRevision     uint64
	RefreshGeneration    uint64
	FrozenCursor         int
	FrozenPrefixDigest   string
	FrozenOperationCount int
	TotalItems           int
	CompletedItems       int
	FailedItems          int
	OverrideCount        int
	AttentionClass       WriteAttentionClass
	AttentionReason      WriteAttentionReason
	PreparedAt           time.Time
	UpdatedAt            time.Time
	NextEligible         time.Time
}

// Clone returns an independently owned batch value.
func (batch WriteBatch) Clone() WriteBatch { return batch }

// WriteBatchStatus is the identity-free batch projection returned with general provider state.
type WriteBatchStatus = WriteBatch

// WriteItem stores one absolute, safely resendable transaction update.
type WriteItem struct {
	ID                          string
	BatchID                     string
	Position                    int
	Kind                        WriteItemKind
	TransactionID               domain.EntityID
	TransactionExternalID       string
	RequestedMerchantLocalID    domain.EntityID
	RequestedMerchantName       *string
	RequestedCategoryExternalID *string
	RequestedHidden             *bool
	OriginatingOperationIDs     []string
	Expectation                 WriteExpectationKind
	ExpectedMerchantExternalID  string
	NewGroupKey                 string
	GroupLeader                 bool
	State                       WriteItemState
	AttemptCount                int
}

// Clone returns an item with independently owned optional fields and operation IDs.
func (item WriteItem) Clone() WriteItem {
	item.RequestedMerchantName = clonePointer(item.RequestedMerchantName)
	item.RequestedCategoryExternalID = clonePointer(item.RequestedCategoryExternalID)
	item.RequestedHidden = clonePointer(item.RequestedHidden)
	item.OriginatingOperationIDs = append([]string(nil), item.OriginatingOperationIDs...)
	return item
}

// WriteItemGroup serializes the first write that may create one remote merchant identity.
type WriteItemGroup struct {
	Key          string
	LeaderItemID string
	ItemIDs      []string
}

// Clone returns a group with independently owned item IDs.
func (group WriteItemGroup) Clone() WriteItemGroup {
	group.ItemIDs = append([]string(nil), group.ItemIDs...)
	return group
}

// WriteResult stores only normalized response fields, never a raw provider payload.
type WriteResult struct {
	ItemID                string
	Kind                  WriteItemKind
	TransactionExternalID string
	MerchantExternalID    *string
	MerchantLabel         *string
	CategoryExternalID    *string
	Hidden                *bool
	OverrideCount         int
	AlreadyAbsent         bool
	RecordedAt            time.Time
}

// Clone returns a result with independently owned optional fields.
func (result WriteResult) Clone() WriteResult {
	result.MerchantExternalID = clonePointer(result.MerchantExternalID)
	result.MerchantLabel = clonePointer(result.MerchantLabel)
	result.CategoryExternalID = clonePointer(result.CategoryExternalID)
	result.Hidden = clonePointer(result.Hidden)
	return result
}

// Validate checks the normalized result union before it reaches persistence.
func (result WriteResult) Validate() error {
	if result.ItemID == "" || result.TransactionExternalID == "" {
		return errors.New("provider write result identity is incomplete")
	}
	switch result.Kind {
	case WriteItemUpdate:
		if result.AlreadyAbsent {
			return errors.New("provider update result cannot be already absent")
		}
	case WriteItemDelete:
		if result.MerchantExternalID != nil || result.MerchantLabel != nil ||
			result.CategoryExternalID != nil || result.Hidden != nil || result.OverrideCount != 0 {
			return errors.New("provider delete result contains update fields")
		}
	default:
		return errors.New("provider write result kind is invalid")
	}
	return nil
}

// LastWriteSummary is the counts-only record retained after batch detail is removed.
type LastWriteSummary struct {
	CompletedAt       time.Time
	CommittedRevision uint64
	OperationCount    int
	ItemCount         int
	OverrideCount     int
}

// ProviderWriteState is the detailed store-only batch projection used by orchestration.
type ProviderWriteState struct {
	Batch   *WriteBatch
	Items   []WriteItem
	Groups  []WriteItemGroup
	Results []WriteResult
}

// Clone returns independently owned detailed write state.
func (state ProviderWriteState) Clone() ProviderWriteState {
	if state.Batch != nil {
		batch := state.Batch.Clone()
		state.Batch = &batch
	}
	state.Items = cloneSlice(state.Items, WriteItem.Clone)
	state.Groups = cloneSlice(state.Groups, WriteItemGroup.Clone)
	state.Results = cloneSlice(state.Results, WriteResult.Clone)
	return state
}

// PrepareProviderWriteInputs contains every value the pure write planner may consult.
type PrepareProviderWriteInputs struct {
	Snapshot        domain.ProfileSnapshot
	ProviderState   ProviderState
	ProposedBatchID string
	ProposedItemIDs []string
	ObservedAt      time.Time
}

// PrepareProviderWritePlan is the complete deterministic durable batch plan.
type PrepareProviderWritePlan struct {
	FrozenOperationIDs []string
	FrozenPrefixDigest string
	Items              []WriteItem
	Groups             []WriteItemGroup
	Lineage            []ProviderIdentityLineage
	Allocations        []LabelAllocation
}

// Clone returns independently owned planner output.
func (plan PrepareProviderWritePlan) Clone() PrepareProviderWritePlan {
	plan.FrozenOperationIDs = append([]string(nil), plan.FrozenOperationIDs...)
	plan.Items = cloneSlice(plan.Items, WriteItem.Clone)
	plan.Groups = cloneSlice(plan.Groups, WriteItemGroup.Clone)
	plan.Lineage = append([]ProviderIdentityLineage(nil), plan.Lineage...)
	plan.Allocations = append([]LabelAllocation(nil), plan.Allocations...)
	return plan
}

// PrepareProviderWritePlanner is a closed, deterministic callback with no store access.
type PrepareProviderWritePlanner func(PrepareProviderWriteInputs) (PrepareProviderWritePlan, error)

// PrepareProviderWriteRequest atomically validates a reviewed revision and acquires its write lease.
type PrepareProviderWriteRequest struct {
	ExpectedRevision   uint64
	ReviewedRevision   uint64
	ExpectedGeneration uint64
	Lease              ProviderOperationLease
	ProposedBatchID    string
	ProposedItemIDs    []string
	ObservedAt         time.Time
}

// PrepareProviderWriteCommit reports the newly durable batch and semantic revision.
type PrepareProviderWriteCommit struct {
	Revision uint64
	Batch    WriteBatch
}

// ClaimProviderWriteRequest claims a bounded set of eligible items under the active lease.
type ClaimProviderWriteRequest struct {
	BatchID         string
	ExpectedVersion uint64
	LeaseOwnerID    string
	LeaseKind       ProviderOperationKind
	ObservedAt      time.Time
	Limit           int
}

// RecordProviderWriteResultRequest persists one normalized provider response.
type RecordProviderWriteResultRequest struct {
	BatchID         string
	ExpectedVersion uint64
	LeaseOwnerID    string
	LeaseKind       ProviderOperationKind
	ItemID          string
	Result          WriteResult
	ObservedAt      time.Time
}

// ParkProviderWriteRequest moves a batch to a durable non-running phase.
type ParkProviderWriteRequest struct {
	BatchID         string
	ExpectedVersion uint64
	LeaseOwnerID    string
	LeaseKind       ProviderOperationKind
	Phase           WriteBatchPhase
	AttentionClass  WriteAttentionClass
	AttentionReason WriteAttentionReason
	NextEligible    time.Time
	ObservedAt      time.Time
}

// ResumeProviderWriteRequest reacquires a lease and returns a parked batch to writing.
type ResumeProviderWriteRequest struct {
	BatchID         string
	ExpectedVersion uint64
	Lease           ProviderOperationLease
	ObservedAt      time.Time
}

// FinalizeProviderWriteInputs contains authoritative store state for a pure finalization plan.
type FinalizeProviderWriteInputs struct {
	Snapshot      domain.ProfileSnapshot
	ProviderState ProviderState
	WriteState    ProviderWriteState
	ObservedAt    time.Time
}

// Clone returns independently owned finalization inputs for a pure callback boundary.
func (inputs FinalizeProviderWriteInputs) Clone() FinalizeProviderWriteInputs {
	inputs.Snapshot = inputs.Snapshot.Clone()
	inputs.ProviderState = inputs.ProviderState.Clone()
	inputs.WriteState = inputs.WriteState.Clone()
	return inputs
}

// FinalizeProviderWritePlan contains the complete response-adjusted committed state.
type FinalizeProviderWritePlan struct {
	Effective   domain.CommittedProfile
	KnownDrills []domain.DrillIdentity
	Allocations []LabelAllocation
	Lineage     []ProviderIdentityLineage
	Summary     LastWriteSummary
}

// FinalizeProviderWritePlanner is a pure final-state callback.
type FinalizeProviderWritePlanner func(FinalizeProviderWriteInputs) (FinalizeProviderWritePlan, error)

// FinalizeProviderWriteRequest authorizes one atomic final fold.
type FinalizeProviderWriteRequest struct {
	BatchID            string
	ExpectedVersion    uint64
	ExpectedRevision   uint64
	ExpectedGeneration uint64
	LeaseOwnerID       string
	LeaseKind          ProviderOperationKind
	ObservedAt         time.Time
}

// FinalizeProviderWriteCommit reports the completed semantic revision and counts.
type FinalizeProviderWriteCommit struct {
	Revision uint64
	Summary  LastWriteSummary
}

// ReconcileProviderWriteRequest abandons the frozen intent into one authoritative snapshot.
type ReconcileProviderWriteRequest struct {
	BatchID            string
	ExpectedVersion    uint64
	ExpectedRevision   uint64
	ExpectedGeneration uint64
	LeaseOwnerID       string
	Candidate          domain.ImportSnapshot
	ProposedIDs        map[string]domain.EntityID
	ProposedSuffixes   map[string]string
	ObservedAt         time.Time
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneSlice[T any](values []T, clone func(T) T) []T {
	if values == nil {
		return nil
	}
	result := make([]T, len(values))
	for index, value := range values {
		result[index] = clone(value)
	}
	return result
}

// Validate checks the closed planner output before persistence.
func (plan PrepareProviderWritePlan) Validate(inputs PrepareProviderWriteInputs) error {
	if inputs.ProposedBatchID == "" || plan.FrozenPrefixDigest == "" {
		return errors.New("provider write plan identity is incomplete")
	}
	if len(plan.FrozenOperationIDs) == 0 || len(plan.Items) == 0 {
		return errors.New("provider write plan has no durable work")
	}
	if len(plan.Items) != len(inputs.ProposedItemIDs) {
		return errors.New("provider write plan item count differs from proposed IDs")
	}
	seenOperations := make(map[string]struct{}, len(plan.FrozenOperationIDs))
	for _, operationID := range plan.FrozenOperationIDs {
		if operationID == "" {
			return errors.New("provider write plan operation ID is empty")
		}
		if _, exists := seenOperations[operationID]; exists {
			return errors.New("provider write plan operation IDs are not unique")
		}
		seenOperations[operationID] = struct{}{}
	}
	seenTransactions := make(map[domain.EntityID]struct{}, len(plan.Items))
	for index, item := range plan.Items {
		if item.ID != inputs.ProposedItemIDs[index] || item.Position != index ||
			item.TransactionID == "" || item.TransactionExternalID == "" ||
			item.State != WriteItemPending || item.AttemptCount != 0 {
			return fmt.Errorf("provider write plan item %d is invalid", index)
		}
		if err := item.Validate(); err != nil {
			return fmt.Errorf("provider write plan item %d: %w", index, err)
		}
		if _, exists := seenTransactions[item.TransactionID]; exists {
			return errors.New("provider write plan has duplicate transaction")
		}
		seenTransactions[item.TransactionID] = struct{}{}
	}
	return nil
}

// Validate checks the durable item union independently of transport or persistence.
func (item WriteItem) Validate() error {
	hasUpdate := item.RequestedMerchantName != nil || item.RequestedCategoryExternalID != nil ||
		item.RequestedHidden != nil
	switch item.Kind {
	case WriteItemDelete:
		if hasUpdate || item.RequestedMerchantLocalID != "" || item.Expectation != "" ||
			item.ExpectedMerchantExternalID != "" || item.NewGroupKey != "" || item.GroupLeader {
			return errors.New("delete item contains update fields")
		}
	case WriteItemUpdate:
		if !hasUpdate {
			return errors.New("update item has no requested field")
		}
		if item.RequestedMerchantName == nil {
			if item.RequestedMerchantLocalID != "" || item.Expectation != "" ||
				item.ExpectedMerchantExternalID != "" || item.NewGroupKey != "" || item.GroupLeader {
				return errors.New("update item merchant expectation is inconsistent")
			}
			break
		}
		if item.RequestedMerchantLocalID == "" {
			return errors.New("update item merchant local ID is empty")
		}
		switch item.Expectation {
		case WriteExpectationExisting, WriteExpectationMergeDestination:
			if item.ExpectedMerchantExternalID == "" || item.NewGroupKey != "" || item.GroupLeader {
				return errors.New("update item existing merchant expectation is incomplete")
			}
		case WriteExpectationNew:
			if item.ExpectedMerchantExternalID != "" || item.NewGroupKey == "" {
				return errors.New("update item new merchant expectation is incomplete")
			}
		default:
			return errors.New("update item merchant expectation is invalid")
		}
	default:
		return errors.New("write item kind is invalid")
	}
	return nil
}
