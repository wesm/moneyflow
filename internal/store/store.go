// Package store defines the application-owned durable profile boundary.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// Profile persists one committed profile and its revision-checked operation journal.
type Profile interface {
	CurrentRevision(context.Context) (uint64, error)
	Load(context.Context) (domain.ProfileSnapshot, error)
	CreateSeededProfile(context.Context, domain.CommittedProfile) (uint64, error)
	Append(context.Context, uint64, domain.Operation) (uint64, error)
	MoveCursor(context.Context, uint64, int) (uint64, error)
	CancelHide(context.Context, uint64, []domain.EntityID) (uint64, error)
	Fold(context.Context, uint64, FoldPlan) (uint64, error)
	ProviderState(context.Context) (ProviderState, error)
	AcquireProviderOperationLease(context.Context, ProviderOperationLease, time.Time) (ProviderOperationLease, bool, error)
	RenewProviderOperationLease(context.Context, string, ProviderOperationKind, time.Time, time.Time) (bool, error)
	ReleaseProviderOperationLease(context.Context, string, ProviderOperationKind) error
	ProviderWriteState(context.Context) (ProviderWriteState, error)
	PrepareProviderWrite(context.Context, PrepareProviderWriteRequest, PrepareProviderWritePlanner) (PrepareProviderWriteCommit, error)
	ClaimProviderWriteItems(context.Context, ClaimProviderWriteRequest) ([]WriteItem, error)
	RecordProviderWriteResult(context.Context, RecordProviderWriteResultRequest) (WriteBatch, error)
	ParkProviderWrite(context.Context, ParkProviderWriteRequest) (WriteBatch, error)
	ResumeProviderWrite(context.Context, ResumeProviderWriteRequest) (WriteBatch, error)
	FinalizeProviderWrite(context.Context, FinalizeProviderWriteRequest, FinalizeProviderWritePlanner) (FinalizeProviderWriteCommit, error)
	ReconcileProviderWrite(context.Context, ReconcileProviderWriteRequest, RefreshPlanner) (RefreshCommit, error)
	// Refresh-only wrappers remain during the port while callers move to the generalized lease.
	AcquireRefreshLease(context.Context, RefreshLease, time.Time) (RefreshLease, bool, error)
	RenewRefreshLease(context.Context, string, time.Time, time.Time) (bool, error)
	ReleaseRefreshLease(context.Context, string) error
	RecordRefreshFailure(context.Context, RefreshFailure) error
	ApplyProviderRefresh(context.Context, AtomicRefreshRequest, RefreshPlanner) (RefreshCommit, error)
	Close() error
}

// ProviderBinding locks one local profile to one remote provider profile.
type ProviderBinding struct {
	Kind            string
	Namespace       string
	RemoteProfileID string
	Currency        domain.Currency
	Scale           uint8
	BoundAt         time.Time
}

// RefreshState is counts-only provider bookkeeping. Generation changes only after a fold.
type RefreshState struct {
	Generation           uint64
	LastAttempt          time.Time
	LastSuccess          time.Time
	NextEligible         time.Time
	StatusCode           string
	ImportedTransactions int
	RemovedTransactions  int
}

// ProviderOperationKind identifies the one provider network operation coordinated by a lease.
type ProviderOperationKind string

// Supported provider operation lease purposes.
const (
	ProviderOperationRefresh   ProviderOperationKind = "refresh"
	ProviderOperationWrite     ProviderOperationKind = "write"
	ProviderOperationReconcile ProviderOperationKind = "reconcile"
)

// ProviderOperationLease coordinates provider network work without providing correctness.
type ProviderOperationLease struct {
	OwnerID   string
	Renderer  string
	Kind      ProviderOperationKind
	ExpiresAt time.Time
}

// RefreshLease is the compatibility projection used by the existing refresh orchestrator.
type RefreshLease struct {
	OwnerID   string
	Renderer  string
	ExpiresAt time.Time
}

// LabelAllocation preserves one provider identity's sticky display-label decision.
type LabelAllocation struct {
	Kind             domain.EntityKind
	Namespace        string
	ExternalID       string
	BaseCollisionKey string
	DisplayLabel     string
	ProviderLabel    string
	SuffixToken      string
	Unsuffixed       bool
}

// ProviderState is a short-lived projection of provider metadata and pristine eligibility.
type ProviderState struct {
	Revision    uint64
	Binding     *ProviderBinding
	Refresh     RefreshState
	Lease       *ProviderOperationLease
	Allocations []LabelAllocation
	Lineage     []ProviderIdentityLineage
	Write       *WriteBatchStatus
	LastWrite   LastWriteSummary
	Pristine    bool
}

// Clone returns independently owned provider state for a pure callback boundary.
func (state ProviderState) Clone() ProviderState {
	if state.Binding != nil {
		binding := *state.Binding
		state.Binding = &binding
	}
	if state.Lease != nil {
		lease := *state.Lease
		state.Lease = &lease
	}
	state.Allocations = append([]LabelAllocation(nil), state.Allocations...)
	state.Lineage = append([]ProviderIdentityLineage(nil), state.Lineage...)
	if state.Write != nil {
		write := state.Write.Clone()
		state.Write = &write
	}
	return state
}

// RefreshFailure records allowlisted operational failure bookkeeping.
type RefreshFailure struct {
	OwnerID      string
	Code         string
	AttemptedAt  time.Time
	NextEligible time.Time
}

// RefreshInputs contains every authoritative value a refresh planner may consult.
type RefreshInputs struct {
	Snapshot         domain.ProfileSnapshot
	Binding          *ProviderBinding
	Refresh          RefreshState
	Allocations      []LabelAllocation
	Lineage          []ProviderIdentityLineage
	Candidate        domain.ImportSnapshot
	ProposedIDs      map[string]domain.EntityID
	ProposedSuffixes map[string]string
	ObservedAt       time.Time
}

// RefreshPlan is the complete logical state produced by one pure refresh calculation.
type RefreshPlan struct {
	Committed   domain.CommittedProfile
	Effective   domain.CommittedProfile
	Journal     []domain.Operation
	Cursor      int
	KnownDrills []domain.DrillIdentity
	Allocations []LabelAllocation
	Lineage     []ProviderIdentityLineage
	Summary     RefreshSummary
}

// RefreshSummary contains counts safe for durable status and logs.
type RefreshSummary struct {
	ImportedAccounts        int
	ImportedMerchants       int
	ImportedGroups          int
	ImportedCategories      int
	ImportedTransactions    int
	RemovedTransactions     int
	RemovedOperations       int
	RemovedTargets          int
	RetainedOperations      int
	RebasedHideTargets      int
	DiscardedRedoOperations int
}

// RefreshPlanner deterministically produces a complete refresh plan without store access.
type RefreshPlanner func(RefreshInputs) (RefreshPlan, error)

// AtomicRefreshRequest carries a validated provider observation into the atomic fold.
type AtomicRefreshRequest struct {
	ExpectedGeneration uint64
	LeaseOwnerID       string
	Binding            *ProviderBinding
	Candidate          domain.ImportSnapshot
	ProposedIDs        map[string]domain.EntityID
	ProposedSuffixes   map[string]string
	ObservedAt         time.Time
}

// RefreshCommit reports the two semantic versions and counts committed by a refresh.
type RefreshCommit struct {
	Revision   uint64
	Generation uint64
	Summary    RefreshSummary
}

// RefreshApplier is the narrow atomic provider-fold capability.
type RefreshApplier interface {
	ApplyProviderRefresh(context.Context, AtomicRefreshRequest, RefreshPlanner) (RefreshCommit, error)
}

// FoldPlan is the validated application result to commit atomically.
type FoldPlan struct {
	ReviewedRevision   uint64
	ActiveOperationIDs []string
	Effective          domain.CommittedProfile
	KnownDrills        []domain.DrillIdentity
}

// Clone returns a plan with independently owned slices and effective state.
func (plan FoldPlan) Clone() FoldPlan {
	plan.ActiveOperationIDs = append([]string(nil), plan.ActiveOperationIDs...)
	plan.Effective = plan.Effective.Clone()
	plan.KnownDrills = append([]domain.DrillIdentity(nil), plan.KnownDrills...)
	return plan
}

// Validate checks the plan against the revision used for the fold transaction.
func (plan FoldPlan) Validate(expectedRevision uint64) error {
	if plan.ReviewedRevision != expectedRevision {
		return errors.New("validate fold plan: reviewed revision does not match expectation")
	}
	seenOperations := make(map[string]struct{}, len(plan.ActiveOperationIDs))
	for _, operationID := range plan.ActiveOperationIDs {
		if operationID == "" {
			return errors.New("validate fold plan: active operation ID is empty")
		}
		if _, exists := seenOperations[operationID]; exists {
			return errors.New("validate fold plan: active operation IDs are not unique")
		}
		seenOperations[operationID] = struct{}{}
	}
	if err := plan.Effective.Validate(); err != nil {
		return fmt.Errorf("validate fold plan: effective profile: %w", err)
	}
	previous := ""
	for index, identity := range plan.KnownDrills {
		canonical, err := canonicalDrillIdentity(identity)
		if err != nil {
			return fmt.Errorf("validate fold plan: known drill[%d]: %w", index, err)
		}
		if index > 0 && canonical <= previous {
			return errors.New("validate fold plan: known drills are not strictly sorted and unique")
		}
		previous = canonical
	}
	return nil
}

func canonicalDrillIdentity(identity domain.DrillIdentity) (string, error) {
	return identity.CanonicalKey()
}
