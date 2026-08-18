package store

import (
	"errors"
	"fmt"
)

// InvalidOperationReason is an allowlisted, value-free validation stage.
type InvalidOperationReason string

// Safe invalid-operation stages that may cross renderer boundaries.
const (
	InvalidOperationRefreshRequest       InvalidOperationReason = "refresh_request"
	InvalidOperationRefreshBinding       InvalidOperationReason = "refresh_binding"
	InvalidOperationRefreshPlanner       InvalidOperationReason = "refresh_planner"
	InvalidOperationRefreshPlan          InvalidOperationReason = "refresh_plan"
	InvalidOperationProviderWriteRequest InvalidOperationReason = "provider_write_request"
	InvalidOperationProviderWritePlanner InvalidOperationReason = "provider_write_planner"
	InvalidOperationProviderWritePlan    InvalidOperationReason = "provider_write_plan"
	InvalidOperationProviderWriteBatch   InvalidOperationReason = "provider_write_batch"
	InvalidOperationProviderRefreshLease InvalidOperationReason = "provider_refresh_lease"
)

// ErrorCode is a stable renderer-neutral storage failure classification.
type ErrorCode string

// Stable storage error codes.
const (
	CodeRevisionConflict   ErrorCode = "revision_conflict"
	CodeInvalidOperation   ErrorCode = "invalid_operation"
	CodeInvalidTarget      ErrorCode = "invalid_target"
	CodeStoreBusy          ErrorCode = "store_busy"
	CodeStoreError         ErrorCode = "store_error"
	CodeSchemaNewer        ErrorCode = "schema_newer"
	CodeSchemaIncompatible ErrorCode = "schema_incompatible"
	CodeStoreCorrupt       ErrorCode = "store_corrupt"
	CodeJournalFull        ErrorCode = "journal_full"
)

var safeDetails = map[ErrorCode]string{
	CodeRevisionConflict:   "profile revision changed",
	CodeInvalidOperation:   "operation is invalid",
	CodeInvalidTarget:      "operation target is invalid",
	CodeStoreBusy:          "profile is busy",
	CodeStoreError:         "profile storage failed",
	CodeSchemaNewer:        "profile schema is newer than this application",
	CodeSchemaIncompatible: "profile schema is incompatible with this application",
	CodeStoreCorrupt:       "profile is corrupt",
	CodeJournalFull:        "pending edit limit reached",
}

var invalidOperationDetails = map[InvalidOperationReason]string{
	InvalidOperationRefreshRequest:       "Moneyflow rejected the local refresh request before writing financial data.",
	InvalidOperationRefreshBinding:       "Moneyflow rejected the local provider binding before writing financial data.",
	InvalidOperationRefreshPlanner:       "Moneyflow could not construct a local refresh plan. No financial data changed.",
	InvalidOperationRefreshPlan:          "Moneyflow rejected its local refresh plan before writing financial data.",
	InvalidOperationProviderWriteRequest: "Moneyflow rejected the provider write request before sending financial data.",
	InvalidOperationProviderWritePlanner: "Moneyflow could not construct a provider write plan. No financial data changed.",
	InvalidOperationProviderWritePlan:    "Moneyflow rejected its provider write plan before sending financial data.",
	InvalidOperationProviderWriteBatch:   "A provider write batch is already active.",
	InvalidOperationProviderRefreshLease: "A provider refresh is already active.",
}

// Error contains only allowlisted renderer detail while retaining an internal diagnostic cause.
type Error struct {
	Code             ErrorCode
	Detail           string
	ObservedRevision *uint64
	CurrentRevision  *uint64
	invalidReason    InvalidOperationReason
	cause            error
}

// NewError creates a safe storage failure without reliable revision metadata.
func NewError(code ErrorCode, cause error) *Error {
	detail, ok := safeDetails[code]
	if !ok {
		panic(fmt.Sprintf("store: unknown error code %q", code))
	}
	return &Error{Code: code, Detail: detail, cause: cause}
}

// NewRevisionError creates a safe storage failure with reliable observed and current revisions.
func NewRevisionError(code ErrorCode, observed, current uint64, cause error) *Error {
	failure := NewError(code, cause)
	failure.ObservedRevision = uint64Pointer(observed)
	failure.CurrentRevision = uint64Pointer(current)
	return failure
}

// NewInvalidOperationError attaches one safe local validation stage to an invalid operation.
func NewInvalidOperationError(reason InvalidOperationReason, cause error) *Error {
	failure := NewError(CodeInvalidOperation, cause)
	if _, ok := invalidOperationDetails[reason]; ok {
		failure.invalidReason = reason
	}
	return failure
}

// InvalidOperationReasonOf extracts an allowlisted stage through ordinary wrapping errors.
func InvalidOperationReasonOf(err error) (InvalidOperationReason, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != CodeInvalidOperation {
		return "", false
	}
	_, ok := invalidOperationDetails[failure.invalidReason]
	return failure.invalidReason, ok
}

// InvalidOperationDetail returns fixed user-facing text for an allowlisted stage.
func InvalidOperationDetail(reason InvalidOperationReason) string {
	return invalidOperationDetails[reason]
}

// Error returns only the stable code and allowlisted safe detail.
func (failure *Error) Error() string {
	if failure == nil {
		return "<nil>"
	}
	return string(failure.Code) + ": " + failure.Detail
}

// Unwrap exposes the diagnostic cause to trusted internal error inspection.
func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func uint64Pointer(value uint64) *uint64 { return &value }
