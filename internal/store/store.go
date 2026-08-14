// Package store defines the application-owned durable profile boundary.
package store

import (
	"context"
	"errors"
	"fmt"

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
	Close() error
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
