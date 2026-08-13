// Package api adapts stateless browser requests to the application service.
package api

import "net/http"

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

// Problem is the single safe RFC 9457-compatible API error envelope.
type Problem struct {
	Type   string `json:"type,omitempty" format:"uri" default:"about:blank"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

// Error satisfies error without exposing request data.
func (problem *Problem) Error() string {
	return problem.Detail
}

// GetStatus supplies the HTTP status to Huma.
func (problem *Problem) GetStatus() int {
	return problem.Status
}

// ContentType ensures Problem Details responses use the standard media type.
func (problem *Problem) ContentType(contentType string) string {
	if contentType == "application/json" {
		return "application/problem+json"
	}
	return contentType
}

func newProblem(status int, code string, detail string) *Problem {
	return &Problem{
		Type: "about:blank", Title: http.StatusText(status), Status: status,
		Detail: detail, Code: code,
	}
}
