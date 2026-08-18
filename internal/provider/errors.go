package provider

import (
	"errors"
	"time"
)

// MaxRetryAfter bounds provider-controlled scheduling metadata.
const MaxRetryAfter = 24 * time.Hour

// ErrorCode is one renderer-neutral provider failure classification.
type ErrorCode string

// DataInvalidReason is an allowlisted, value-free provider validation classification.
type DataInvalidReason string

// WriteFailureReason is an allowlisted, value-free provider write attention reason.
type WriteFailureReason string

// Safe provider validation reasons that may cross renderer boundaries.
const (
	DataInvalidEntity            DataInvalidReason = "entity"
	DataInvalidDuplicateIdentity DataInvalidReason = "duplicate_identity"
	DataInvalidTransactionID     DataInvalidReason = "transaction_identity"
	DataInvalidTransactionDate   DataInvalidReason = "transaction_date"
	DataInvalidTransactionAmount DataInvalidReason = "transaction_amount"
	DataInvalidSnapshot          DataInvalidReason = "snapshot"
)

// Stable provider failure codes for read, refresh, and write orchestration.
const (
	CodeReconnectRequired            ErrorCode = "provider_reconnect_required"
	CodeIdentityMismatch             ErrorCode = "provider_identity_mismatch"
	CodeSnapshotUnstable             ErrorCode = "provider_snapshot_unstable"
	CodeRefreshInProgress            ErrorCode = "provider_refresh_in_progress"
	CodeDeletionConfirmationRequired ErrorCode = "provider_deletion_confirmation_required"
	CodeConfirmationInvalid          ErrorCode = "provider_confirmation_invalid"
	CodeRefreshStale                 ErrorCode = "provider_refresh_stale"
	CodeRateLimited                  ErrorCode = "provider_rate_limited"
	CodeUnavailable                  ErrorCode = "provider_unavailable"
	CodeDataInvalid                  ErrorCode = "provider_data_invalid"
	CodeWriteInProgress              ErrorCode = "provider_write_in_progress"
	CodeWriteAttentionRequired       ErrorCode = "provider_write_attention_required"
	CodeWriteStale                   ErrorCode = "provider_write_stale"
	CodeWritePaused                  ErrorCode = "provider_write_paused"
	CodeWriteNotEligible             ErrorCode = "provider_write_not_eligible"
	CodeWriteUnsupported             ErrorCode = "provider_write_unsupported"
)

// Stable provider write attention reasons.
const (
	WriteUnavailableExhausted WriteFailureReason = "provider_write_unavailable_exhausted"
	WriteResponseIncomplete   WriteFailureReason = "provider_write_response_incomplete"
	WriteTargetNotFound       WriteFailureReason = "provider_write_target_not_found"
	WriteRejected             WriteFailureReason = "provider_write_rejected"
	WriteIdentityConflict     WriteFailureReason = "provider_write_identity_conflict"
	WriteRetiredIdentity      WriteFailureReason = "provider_write_retired_identity"
	WriteExpectationInvalid   WriteFailureReason = "provider_write_expectation_invalid"
)

var errorDetails = map[ErrorCode]string{
	CodeReconnectRequired:            "reconnect through the CLI",
	CodeIdentityMismatch:             "the remote profile does not match this local profile",
	CodeSnapshotUnstable:             "the provider snapshot changed while it was read",
	CodeRefreshInProgress:            "another process is refreshing this profile",
	CodeDeletionConfirmationRequired: "confirm the proposed remote removals",
	CodeConfirmationInvalid:          "the refresh confirmation is no longer valid",
	CodeRefreshStale:                 "a newer provider snapshot already committed",
	CodeRateLimited:                  "the provider rate limit prevented refresh",
	CodeUnavailable:                  "the provider is temporarily unavailable",
	CodeDataInvalid:                  "the provider returned invalid data",
	CodeWriteInProgress:              "another process is writing this profile",
	CodeWriteAttentionRequired:       "the provider write requires attention",
	CodeWriteStale:                   "the provider write state changed",
	CodeWritePaused:                  "the provider write is paused",
	CodeWriteNotEligible:             "the provider write is not eligible yet",
	CodeWriteUnsupported:             "the requested change cannot be written to this provider",
}

var writeFailureReasons = []WriteFailureReason{
	WriteUnavailableExhausted,
	WriteResponseIncomplete,
	WriteTargetNotFound,
	WriteRejected,
	WriteIdentityConflict,
	WriteRetiredIdentity,
	WriteExpectationInvalid,
}

var dataInvalidDetails = map[DataInvalidReason]string{
	DataInvalidEntity:            "A provider entity has a missing or malformed identity or label.",
	DataInvalidDuplicateIdentity: "The provider returned a duplicate stable identity.",
	DataInvalidTransactionID:     "A transaction has a missing or malformed stable identity.",
	DataInvalidTransactionDate:   "A transaction has an invalid date.",
	DataInvalidTransactionAmount: "A transaction amount cannot be represented at the configured currency scale.",
	DataInvalidSnapshot:          "The normalized provider snapshot violates the import contract.",
}

var errorCodes = []ErrorCode{
	CodeReconnectRequired,
	CodeIdentityMismatch,
	CodeSnapshotUnstable,
	CodeRefreshInProgress,
	CodeDeletionConfirmationRequired,
	CodeConfirmationInvalid,
	CodeRefreshStale,
	CodeRateLimited,
	CodeUnavailable,
	CodeDataInvalid,
	CodeWriteInProgress,
	CodeWriteAttentionRequired,
	CodeWriteStale,
	CodeWritePaused,
	CodeWriteNotEligible,
	CodeWriteUnsupported,
}

// Error is a redacted provider failure. Raw transport and response errors never enter this type.
type Error struct {
	code        ErrorCode
	retryAfter  time.Duration
	reason      DataInvalidReason
	writeReason WriteFailureReason
}

// NewWriteFailure constructs a redacted provider write failure.
func NewWriteFailure(reason WriteFailureReason) *Error {
	failure := NewError(CodeWriteAttentionRequired)
	if knownWriteFailureReason(reason) {
		failure.writeReason = reason
	}
	return failure
}

// NewError constructs a provider failure from a stable code.
func NewError(code ErrorCode) *Error {
	if !knownErrorCode(code) {
		code = CodeDataInvalid
	}
	return &Error{code: code}
}

// NewDataInvalidError constructs a redacted provider validation failure.
func NewDataInvalidError(reason DataInvalidReason) *Error {
	failure := NewError(CodeDataInvalid)
	if _, ok := dataInvalidDetails[reason]; ok {
		failure.reason = reason
	}
	return failure
}

// NewErrorWithRetry constructs a rate-limit failure with bounded safe retry metadata.
func NewErrorWithRetry(code ErrorCode, retryAfter time.Duration) *Error {
	failure := NewError(code)
	if failure.code != CodeRateLimited || retryAfter <= 0 {
		return failure
	}
	failure.retryAfter = min(retryAfter, MaxRetryAfter)
	return failure
}

// Error returns only the stable code and its fixed allowlisted detail.
func (failure *Error) Error() string {
	detail, ok := errorDetails[failure.code]
	if !ok {
		return "provider_error: unknown provider failure"
	}
	return string(failure.code) + ": " + detail
}

// Code returns the stable renderer-neutral failure code.
func (failure *Error) Code() ErrorCode {
	return failure.code
}

// CodeOf extracts a stable provider code through ordinary wrapping errors.
func CodeOf(err error) (ErrorCode, bool) {
	var failure *Error
	if !errors.As(err, &failure) {
		return "", false
	}
	return failure.Code(), true
}

// RetryAfterOf extracts bounded retry timing through ordinary wrapping errors.
func RetryAfterOf(err error) (time.Duration, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure.code != CodeRateLimited || failure.retryAfter <= 0 {
		return 0, false
	}
	return failure.retryAfter, true
}

// DataInvalidReasonOf extracts an allowlisted validation reason through wrapping errors.
func DataInvalidReasonOf(err error) (DataInvalidReason, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure.code != CodeDataInvalid {
		return "", false
	}
	_, ok := dataInvalidDetails[failure.reason]
	return failure.reason, ok
}

// WriteFailureReasonOf extracts an allowlisted write reason through wrapping errors.
func WriteFailureReasonOf(err error) (WriteFailureReason, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure.code != CodeWriteAttentionRequired ||
		!knownWriteFailureReason(failure.writeReason) {
		return "", false
	}
	return failure.writeReason, true
}

// WriteFailureReasons returns the complete stable write-reason set in declaration order.
func WriteFailureReasons() []WriteFailureReason {
	return append([]WriteFailureReason(nil), writeFailureReasons...)
}

// DataInvalidDetail returns fixed user-facing text for an allowlisted validation reason.
func DataInvalidDetail(reason DataInvalidReason) string {
	return dataInvalidDetails[reason]
}

// ErrorCodes returns the complete stable code set in declaration order.
func ErrorCodes() []ErrorCode {
	return append([]ErrorCode(nil), errorCodes...)
}

func knownErrorCode(code ErrorCode) bool {
	_, ok := errorDetails[code]
	return ok
}

func knownWriteFailureReason(reason WriteFailureReason) bool {
	for _, candidate := range writeFailureReasons {
		if reason == candidate {
			return true
		}
	}
	return false
}
