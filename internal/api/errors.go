// Package api adapts stateless browser requests to the application service.
package api

import (
	"errors"
	"net/http"
)

// ErrorCode is a stable machine-readable API failure code.
type ErrorCode string

const (
	// CodeInvalidViewState identifies malformed or unsupported direct URL state.
	CodeInvalidViewState ErrorCode = "invalid_view_state"
	// CodeViewStateTooLarge identifies a valid transition that cannot fit the URL contract.
	CodeViewStateTooLarge ErrorCode = "view_state_too_large"
	// CodeInvalidOrigin identifies a persistent request from a noncanonical browser origin.
	CodeInvalidOrigin ErrorCode = "invalid_origin"
	// CodeInvalidToken identifies a malformed token or invalid signed claims.
	CodeInvalidToken ErrorCode = "invalid_token"
	// CodeTokenExpired permits one bootstrap refresh and unchanged request retry.
	CodeTokenExpired ErrorCode = "token_expired"
	// CodeRevisionConflict requires a fresh projection and explicit reinvocation.
	CodeRevisionConflict ErrorCode = "revision_conflict"
	// CodeInvalidOperation identifies unsupported or malformed persistent intent.
	CodeInvalidOperation ErrorCode = "invalid_operation"
	// CodeInvalidTarget identifies a target that is unavailable for the requested action.
	CodeInvalidTarget ErrorCode = "invalid_target"
	// CodeSelectionStale returns deterministic selection recovery without applying an action.
	CodeSelectionStale ErrorCode = "selection_stale"
	// CodeStoreBusy requires an explicit retry after the bounded store wait.
	CodeStoreBusy ErrorCode = "store_busy"
	// CodeStoreError identifies a rolled-back runtime storage failure.
	CodeStoreError ErrorCode = "store_error"
	// CodeProviderReconnectRequired requires a CLI reconnect before another refresh.
	CodeProviderReconnectRequired ErrorCode = "provider_reconnect_required"
	// CodeProviderIdentityMismatch refuses a session for another remote profile.
	CodeProviderIdentityMismatch ErrorCode = "provider_identity_mismatch"
	// CodeProviderSnapshotUnstable rejects an internally inconsistent complete read.
	CodeProviderSnapshotUnstable ErrorCode = "provider_snapshot_unstable"
	// CodeProviderRefreshInProgress reports another live refresh lease owner.
	CodeProviderRefreshInProgress ErrorCode = "provider_refresh_in_progress"
	// CodeProviderDeletionConfirmationRequired requires an explicit second mutation.
	CodeProviderDeletionConfirmationRequired ErrorCode = "provider_deletion_confirmation_required"
	// CodeProviderConfirmationInvalid rejects a stale, expired, or foreign confirmation.
	CodeProviderConfirmationInvalid ErrorCode = "provider_confirmation_invalid"
	// CodeProviderRefreshStale rejects a candidate built against an older generation.
	CodeProviderRefreshStale ErrorCode = "provider_refresh_stale"
	// CodeProviderRateLimited reports exhausted bounded provider backoff.
	CodeProviderRateLimited ErrorCode = "provider_rate_limited"
	// CodeProviderUnavailable reports exhausted transient provider failures.
	CodeProviderUnavailable ErrorCode = "provider_unavailable"
	// CodeProviderDataInvalid rejects provider data that violates domain invariants.
	CodeProviderDataInvalid ErrorCode = "provider_data_invalid"
	// CodeProfileNotFound reports an absent local profile.
	CodeProfileNotFound ErrorCode = "profile_not_found"
	// CodeProfileAmbiguous reports a non-unique local profile selector.
	CodeProfileAmbiguous ErrorCode = "profile_ambiguous"
	// CodeProfileNameConflict reports a normalized display-name collision.
	CodeProfileNameConflict ErrorCode = "profile_name_conflict"
	// CodeProfileInvalid reports invalid local profile metadata.
	CodeProfileInvalid ErrorCode = "profile_invalid"
	// CodeProfileManifestUnsupported reports an unknown manifest version.
	CodeProfileManifestUnsupported ErrorCode = "profile_manifest_unsupported"
	// CodeProfileBusy reports a conflicting profile lifecycle owner.
	CodeProfileBusy ErrorCode = "profile_busy"
	// CodeProfileRecoveryIncomplete reports ambiguous crash-recovery state.
	CodeProfileRecoveryIncomplete ErrorCode = "profile_recovery_incomplete"
	// CodeProfileRecoveryUnavailable reports an explicitly non-recoverable profile.
	CodeProfileRecoveryUnavailable ErrorCode = "profile_recovery_unavailable"
	// CodeOnboardingStale reports a state-version conflict.
	CodeOnboardingStale ErrorCode = "onboarding_stale"
	// CodeOnboardingExpired reports an unavailable process-local attempt.
	CodeOnboardingExpired ErrorCode = "onboarding_expired"
	// CodeOnboardingCanceled reports a canceled process-local attempt.
	CodeOnboardingCanceled ErrorCode = "onboarding_canceled"
	// CodeOnboardingLocalOnly reports a profile that cannot be connected.
	CodeOnboardingLocalOnly ErrorCode = "onboarding_local_only"
	// CodeCredentialUnlockFailed reports a rejected local vault password.
	CodeCredentialUnlockFailed ErrorCode = "credential_unlock_failed" // #nosec G101 -- stable protocol code
	// CodeCredentialInputInvalid reports invalid transient setup input.
	CodeCredentialInputInvalid ErrorCode = "credential_input_invalid" // #nosec G101 -- stable protocol code
	// CodeProviderConnectInProgress reports another provider-connect lock owner.
	CodeProviderConnectInProgress ErrorCode = "provider_connect_in_progress"
	// CodeProviderWriteInProgress reports a live outbound-write owner.
	CodeProviderWriteInProgress ErrorCode = "provider_write_in_progress"
	// CodeProviderWriteAttentionRequired reports a parked batch requiring user action.
	CodeProviderWriteAttentionRequired ErrorCode = "provider_write_attention_required"
	// CodeProviderWriteStale rejects a control for an older batch version.
	CodeProviderWriteStale ErrorCode = "provider_write_stale"
	// CodeProviderWritePaused reports a durably paused batch.
	CodeProviderWritePaused ErrorCode = "provider_write_paused"
	// CodeProviderWriteNotEligible reports a rate-limit deadline that has not arrived.
	CodeProviderWriteNotEligible ErrorCode = "provider_write_not_eligible"
	// CodeProviderWriteUnsupported rejects an unwritable reviewed prefix.
	CodeProviderWriteUnsupported ErrorCode = "provider_write_unsupported"
	// CodeExportInvalid rejects malformed export intent.
	CodeExportInvalid ErrorCode = "export_invalid"
	// CodeExportEmpty reports no committed rows at execution time.
	CodeExportEmpty ErrorCode = "export_empty"
	// CodeExportBusy reports another live export execution.
	CodeExportBusy ErrorCode = "export_busy"
	// CodeExportCancelled reports a server-observed export cancellation.
	CodeExportCancelled ErrorCode = "export_cancelled"
	// CodeExportFailed reports an encoding or private-filesystem failure.
	CodeExportFailed ErrorCode = "export_failed"
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
	Type                           string                       `json:"type,omitempty" format:"uri" default:"about:blank"`
	Title                          string                       `json:"title"`
	Status                         int                          `json:"status"`
	Detail                         string                       `json:"detail"`
	Code                           string                       `json:"code"`
	CurrentRevision                string                       `json:"current_revision,omitempty" pattern:"^[0-9]+$"`
	Selection                      *SelectionDisposition        `json:"selection,omitempty"`
	Provider                       *ProviderStatusResponse      `json:"provider,omitempty"`
	ProviderWrite                  *ProviderWriteStatusResponse `json:"provider_write,omitempty"`
	ProviderWriteConfirmationToken string                       `json:"provider_write_confirmation_token,omitempty" maxLength:"4096"`
}

var errUnsupportedProviderVersion = errors.New("unsupported provider request version")

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
