package app

import (
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// EditScope distinguishes stable-entity edits from exact transaction reassignment.
type EditScope string

const (
	// EditScopeEntity applies an identity-preserving label edit or explicit entity merge.
	EditScopeEntity EditScope = "entity"
	// EditScopeTransactions reassigns only the resolved transaction targets.
	EditScopeTransactions EditScope = "transactions"
)

// TaxonomyAction identifies one category or category-group structural intent.
type TaxonomyAction string

const (
	// TaxonomyCreate creates a new stable taxonomy entity.
	TaxonomyCreate TaxonomyAction = "create"
	// TaxonomyRename changes a taxonomy entity label without changing its identity.
	TaxonomyRename TaxonomyAction = "rename"
	// TaxonomyMove moves a category to another group.
	TaxonomyMove TaxonomyAction = "move"
	// TaxonomyMerge retires a source identity into an explicit destination.
	TaxonomyMerge TaxonomyAction = "merge"
	// TaxonomyDelete retires a source identity into an explicit replacement.
	TaxonomyDelete TaxonomyAction = "delete"
)

// EditInput carries renderer-neutral values for one editing intent.
type EditInput struct {
	Scope         EditScope
	Taxonomy      TaxonomyAction
	EntityID      domain.EntityID
	Label         string
	DestinationID domain.EntityID
	GroupID       domain.EntityID
	ReplacementID domain.EntityID
}

// MutationRequest combines durable view state, exact selection, focus, and edit input.
type MutationRequest struct {
	Action           ActionID
	ExpectedRevision uint64
	State            ViewState
	Selection        SelectionValue
	Target           *RowTarget
	Input            EditInput
}

// ResolvedTargets are stable local identities fixed at operation-creation time.
type ResolvedTargets struct {
	TransactionIDs []domain.EntityID
	EntityIDs      []domain.EntityID
	FromSelection  bool
}

// SelectionDisposition tells a renderer how successful targeting affects transient selection.
type SelectionDisposition string

const (
	// SelectionCleared matches successful explicit-selection bulk edits.
	SelectionCleared SelectionDisposition = "cleared"
	// SelectionPreserved matches focused-row and focused-aggregate edits.
	SelectionPreserved SelectionDisposition = "preserved"
)

// OperationMetadata supplies server-created journal identity and canonical creation time.
type OperationMetadata struct {
	OperationID string
	CreatedAt   time.Time
}

// MutationMode selects the single journal-store mutation represented by a plan.
type MutationMode string

const (
	// MutationAppend appends the plan operation after truncating any redo tail.
	MutationAppend MutationMode = "append"
	// MutationCancelHide rewrites active hide operations for the exact cancellation targets.
	MutationCancelHide MutationMode = "cancel_hide"
)

// MutationPlan is one validated store mutation plus its renderer state disposition.
type MutationPlan struct {
	Mode                 MutationMode
	Operation            domain.Operation
	CancelHideTargets    []domain.EntityID
	SelectionDisposition SelectionDisposition
	State                ViewState
}

// MutationErrorCode is a stable renderer-neutral mutation failure classification.
type MutationErrorCode string

const (
	// MutationInvalidOperation identifies malformed or unsupported editing intent.
	MutationInvalidOperation MutationErrorCode = "invalid_operation"
	// MutationInvalidTarget identifies an entity that cannot be resolved for editing.
	MutationInvalidTarget MutationErrorCode = "invalid_target"
	// MutationSelectionStale requires the caller to accept a refreshed or cleared selection.
	MutationSelectionStale MutationErrorCode = "selection_stale"
	// MutationRevisionConflict requires a fresh projection before the action can be retried.
	MutationRevisionConflict MutationErrorCode = "revision_conflict"
)

var mutationDetails = map[MutationErrorCode]string{
	MutationInvalidOperation: "The requested operation is invalid.",
	MutationInvalidTarget:    "The requested target is no longer available.",
	MutationSelectionStale:   "The selection changed and must be reviewed.",
	MutationRevisionConflict: "The profile changed and must be refreshed.",
}

// MutationError retains only allowlisted public details plus typed recovery state.
type MutationError struct {
	Code            MutationErrorCode
	Detail          string
	CurrentRevision uint64
	Selection       SelectionValue
	cause           error
}

func (failure *MutationError) Error() string {
	if failure == nil {
		return "<nil>"
	}
	return failure.Detail
}

func (failure *MutationError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func mutationError(code MutationErrorCode, cause error) *MutationError {
	detail, ok := mutationDetails[code]
	if !ok {
		panic("unknown mutation error code")
	}
	return &MutationError{Code: code, Detail: detail, cause: cause}
}

func staleSelectionError(
	current uint64,
	selection SelectionValue,
	cause error,
) *MutationError {
	failure := mutationError(MutationSelectionStale, cause)
	failure.CurrentRevision = current
	failure.Selection = selection
	return failure
}

func selectionDisposition(targets ResolvedTargets) SelectionDisposition {
	if targets.FromSelection {
		return SelectionCleared
	}
	return SelectionPreserved
}

func newMutationOperation(
	request MutationRequest,
	metadata OperationMetadata,
) domain.Operation {
	return domain.Operation{
		ID:              metadata.OperationID,
		PayloadVersion:  1,
		CreatedRevision: request.ExpectedRevision,
		CreatedAt:       metadata.CreatedAt,
	}
}

func mutationPlan(
	request MutationRequest,
	targets ResolvedTargets,
	operation domain.Operation,
) MutationPlan {
	return MutationPlan{
		Mode:                 MutationAppend,
		Operation:            operation,
		SelectionDisposition: selectionDisposition(targets),
		State:                request.State.Clone(),
	}
}

func validateMutationMetadata(metadata OperationMetadata) error {
	if metadata.OperationID == "" {
		return errors.New("operation identity is empty")
	}
	if metadata.CreatedAt.IsZero() ||
		metadata.CreatedAt != time.UnixMilli(metadata.CreatedAt.UnixMilli()).UTC() {
		return errors.New("operation creation time is not canonical UTC milliseconds")
	}
	return nil
}
