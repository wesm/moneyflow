package onboarding

import (
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// ProtocolVersion is the renderer-facing onboarding state-machine version.
const ProtocolVersion = uint16(1)

// State identifies one stable onboarding phase.
type State string

// Stable onboarding states.
const (
	StateInspect             State = "inspect"
	StateValidateSession     State = "validate_session"
	StateSettingsRequired    State = "settings_required"
	StateUnlockRequired      State = "unlock_required"
	StateCredentialsRequired State = "credentials_required"
	StateAuthenticating      State = "authenticating"
	StateImporting           State = "importing"
	StateComplete            State = "complete"
	StateLocalOnly           State = "local_only"
	StateIdentityMismatch    State = "identity_mismatch"
	StateFailed              State = "failed"
	StateCanceled            State = "canceled"
)

// ActionType identifies one guarded presenter transition.
type ActionType string

// Stable onboarding actions.
const (
	ActionConfirmSettings   ActionType = "confirm_settings"
	ActionUnlock            ActionType = "unlock"
	ActionSubmitCredentials ActionType = "submit_credentials" // #nosec G101 -- stable protocol action
	ActionRetry             ActionType = "retry"
	ActionReauthenticate    ActionType = "reauthenticate"
)

// Settings is the exact money interpretation selected for import.
type Settings struct {
	Currency domain.Currency `json:"currency"`
	Scale    uint8           `json:"scale"`
}

// Progress is a credential-blind copy of provider progress.
type Progress struct {
	Phase     string        `json:"phase"`
	Partition string        `json:"partition"`
	Fetched   int           `json:"fetched"`
	Total     int           `json:"total"`
	Attempt   int           `json:"attempt"`
	Pass      int           `json:"pass"`
	Elapsed   time.Duration `json:"elapsed"`
}

// Failure is one sanitized presenter outcome.
type Failure struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	CanRetry   bool   `json:"can_retry"`
	CanReenter bool   `json:"can_reenter"`
}

// Snapshot is the complete credential-blind state visible to a presenter.
type Snapshot struct {
	ProtocolVersion uint16    `json:"protocol_version"`
	AttemptID       string    `json:"attempt_id"`
	ProfileID       string    `json:"profile_id"`
	StateVersion    uint64    `json:"state_version"`
	State           State     `json:"state"`
	ProviderKind    string    `json:"provider_kind"`
	Settings        *Settings `json:"settings,omitempty"`
	Progress        *Progress `json:"progress,omitempty"`
	Failure         *Failure  `json:"failure,omitempty"`
}

// SettingsInput confirms the exact import money interpretation.
type SettingsInput struct {
	Currency domain.Currency
	Scale    uint8
}

// UnlockInput contains one transient encrypted-vault password.
type UnlockInput struct {
	AccountPassword []byte
}

// CredentialInput contains transient Monarch and vault setup secrets.
type CredentialInput struct {
	Email           []byte
	Password        []byte
	TOTPSecret      []byte
	AccountPassword []byte
	Confirmation    []byte
}

// SubmitRequest applies one state-version-guarded action.
type SubmitRequest struct {
	ProfileID            string
	AttemptID            string
	ExpectedStateVersion uint64
	Action               ActionType
	Settings             *SettingsInput
	Unlock               *UnlockInput
	Credentials          *CredentialInput
}

// StartRequest starts one profile-bound onboarding attempt.
type StartRequest struct {
	ProfileID   string
	Settings    *SettingsInput
	Renderer    string
	MonthToDate bool
}

// StatusRequest reads one profile-bound onboarding attempt.
type StatusRequest struct {
	ProfileID string
	AttemptID string
}

// CancelRequest cancels one state-version-guarded onboarding attempt.
type CancelRequest struct {
	ProfileID            string
	AttemptID            string
	ExpectedStateVersion uint64
}
