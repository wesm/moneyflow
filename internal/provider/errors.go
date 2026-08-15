package provider

import "errors"

// ErrorCode is one renderer-neutral provider failure classification.
type ErrorCode string

// Stable provider failure codes for the read/import/refresh slice.
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
}

// Error is a redacted provider failure. Raw transport and response errors never enter this type.
type Error struct {
	code ErrorCode
}

// NewError constructs a provider failure from a stable code.
func NewError(code ErrorCode) *Error {
	return &Error{code: code}
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

// ErrorCodes returns the complete stable code set in declaration order.
func ErrorCodes() []ErrorCode {
	return append([]ErrorCode(nil), errorCodes...)
}
