// Package profilecatalog discovers and manages local Moneyflow profiles.
package profilecatalog

import "errors"

// Code is a stable renderer-neutral catalog error classification.
type Code string

// Stable profile catalog error codes.
const (
	CodeProfileNotFound     Code = "profile_not_found"
	CodeProfileAmbiguous    Code = "profile_ambiguous"
	CodeProfileNameConflict Code = "profile_name_conflict"
	CodeProfileInvalid      Code = "profile_invalid"
	CodeManifestUnsupported Code = "profile_manifest_unsupported"
	CodeProfileBusy         Code = "profile_busy"
	CodeRecoveryIncomplete  Code = "profile_recovery_incomplete"
	CodeRecoveryUnavailable Code = "profile_recovery_unavailable"
)

var safeDetails = map[Code]string{
	CodeProfileNotFound:     "profile was not found",
	CodeProfileAmbiguous:    "profile selection is ambiguous",
	CodeProfileNameConflict: "profile name is already in use",
	CodeProfileInvalid:      "profile metadata is invalid",
	CodeManifestUnsupported: "profile metadata requires another Moneyflow version",
	CodeProfileBusy:         "profile is in use",
	CodeRecoveryIncomplete:  "profile recovery is incomplete",
	CodeRecoveryUnavailable: "profile cannot be recovered by this Moneyflow version",
}

// Error retains a trusted cause while exposing only fixed renderer-safe detail.
type Error struct {
	Code   Code
	detail string
	cause  error
}

func newError(code Code, cause error) *Error {
	return &Error{Code: code, detail: safeDetails[code], cause: cause}
}

// Error returns only the stable code and allowlisted detail.
func (failure *Error) Error() string {
	if failure == nil {
		return "<nil>"
	}
	return string(failure.Code) + ": " + failure.detail
}

// Unwrap exposes the diagnostic cause to trusted internal callers.
func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

// CodeOf returns a catalog code through ordinary error wrapping.
func CodeOf(err error) Code {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}
