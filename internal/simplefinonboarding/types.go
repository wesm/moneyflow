// Package simplefinonboarding coordinates claim-once, credential-blind SimpleFIN setup.
package simplefinonboarding

import (
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
)

// ProtocolVersion is the presenter protocol version.
const ProtocolVersion = uint16(1)

// State identifies the current setup phase.
type State string

// Stable setup phases.
const (
	StateInspect             State = "inspect"
	StateCredentialsRequired State = "credentials_required"
	StateClaiming            State = "claiming"
	StateSavingSession       State = "saving_session"
	StateImporting           State = "importing"
	StateComplete            State = "complete"
	StateFailed              State = "failed"
	StateCanceled            State = "canceled"
	StateIdentityMismatch    State = "identity_mismatch"
)

// ActionType identifies an explicit setup action.
type ActionType string

// Stable setup actions. No retry action repeats a claim.
const (
	ActionConnect     ActionType = "connect"
	ActionRetrySave   ActionType = "retry_save"
	ActionRetryImport ActionType = "retry_import"
)

// Failure contains only allowlisted recovery guidance.
type Failure struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	CanRetry   bool   `json:"can_retry"`
	CanReenter bool   `json:"can_reenter"`
}

// Snapshot is the detached presenter state; it never contains credentials.
type Snapshot struct {
	ProtocolVersion      uint16                  `json:"protocol_version"`
	ProfileID            string                  `json:"profile_id"`
	AttemptID            string                  `json:"attempt_id"`
	StateVersion         uint64                  `json:"state_version"`
	State                State                   `json:"state"`
	ProviderKind         string                  `json:"provider_kind"`
	Settings             *simplefin.ImportConfig `json:"settings,omitempty"`
	Progress             provider.Progress       `json:"progress"`
	ImportedTransactions int                     `json:"imported_transactions"`
	NextEligible         time.Time               `json:"next_eligible"`
	Failure              *Failure                `json:"failure,omitempty"`
}

// StartRequest selects the profile and its presenter.
type StartRequest struct {
	ProfileID, Renderer string
	RemoveIfAbandoned   bool
}

// StatusRequest identifies a profile-bound attempt.
type StatusRequest struct{ ProfileID, AttemptID string }

// SubmitRequest guards an action with the last observed state version.
type SubmitRequest struct {
	ProfileID, AttemptID string
	ExpectedStateVersion uint64
	Action               ActionType
	Input                []byte
	Settings             simplefin.ImportConfig
}

// CancelRequest stops one versioned attempt.
type CancelRequest struct {
	ProfileID, AttemptID string
	ExpectedStateVersion uint64
}

// Error is a safe coordinator failure code.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

// CodeOf returns a stable coordinator code, if present.
func CodeOf(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}
