// Package api adapts stateless browser requests to the application service.
package api

// ErrorCode is a stable machine-readable API failure code.
type ErrorCode string

const (
	// CodeInvalidViewState identifies malformed or unsupported direct URL state.
	CodeInvalidViewState ErrorCode = "invalid_view_state"
	// CodeViewStateTooLarge identifies a valid transition that cannot fit the URL contract.
	CodeViewStateTooLarge ErrorCode = "view_state_too_large"
)

// SafeError separates public detail from an internal diagnostic cause.
type SafeError struct {
	Code   ErrorCode
	Detail string
	cause  error
}

// Error returns only the safe public detail.
func (err *SafeError) Error() string {
	return err.Detail
}

// Unwrap returns the internal cause for diagnostics and error matching.
func (err *SafeError) Unwrap() error {
	return err.cause
}

func newSafeError(code ErrorCode, detail string, cause error) *SafeError {
	return &SafeError{Code: code, Detail: detail, cause: cause}
}
