// Package onboarding coordinates renderer-neutral provider setup attempts.
package onboarding

import "errors"

// Code is a stable renderer-neutral onboarding failure classification.
type Code string

// Stable onboarding failure codes.
const (
	CodeOnboardingStale           Code = "onboarding_stale"
	CodeOnboardingExpired         Code = "onboarding_expired"
	CodeOnboardingCanceled        Code = "onboarding_canceled"
	CodeOnboardingLocalOnly       Code = "onboarding_local_only"
	CodeCredentialUnlockFailed    Code = "credential_unlock_failed" // #nosec G101 -- stable protocol code
	CodeCredentialInputInvalid    Code = "credential_input_invalid" // #nosec G101 -- stable protocol code
	CodeProviderConnectInProgress Code = "provider_connect_in_progress"
)

var safeErrorDetails = map[Code]string{
	CodeOnboardingStale:           "the onboarding state changed",
	CodeOnboardingExpired:         "the onboarding attempt expired",
	CodeOnboardingCanceled:        "the onboarding attempt was canceled",
	CodeOnboardingLocalOnly:       "the profile contains local data and cannot be connected",
	CodeCredentialUnlockFailed:    "the saved credentials could not be unlocked",
	CodeCredentialInputInvalid:    "the submitted onboarding input is invalid",
	CodeProviderConnectInProgress: "another provider connection is already in progress",
}

// Error exposes only a stable code and fixed renderer-safe detail.
type Error struct {
	Code  Code
	cause error
}

func newError(code Code, cause error) *Error {
	return &Error{Code: code, cause: cause}
}

// Error returns a credential-blind renderer-safe message.
func (failure *Error) Error() string {
	if failure == nil {
		return "<nil>"
	}
	return string(failure.Code) + ": " + safeErrorDetails[failure.Code]
}

// Unwrap exposes the internal cause to trusted callers.
func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// CodeOf extracts an onboarding code through ordinary error wrapping.
func CodeOf(err error) Code {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

// ErrorForCode reconstructs one fixed, renderer-safe onboarding error without a raw cause.
func ErrorForCode(code Code) error {
	if _, ok := safeErrorDetails[code]; !ok {
		return nil
	}
	return newError(code, nil)
}
