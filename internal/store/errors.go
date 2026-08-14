package store

import "fmt"

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
}

// Error contains only allowlisted renderer detail while retaining an internal diagnostic cause.
type Error struct {
	Code             ErrorCode
	Detail           string
	ObservedRevision *uint64
	CurrentRevision  *uint64
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
