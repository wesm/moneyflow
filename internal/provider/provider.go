// Package provider defines renderer-neutral, read-only financial-provider capabilities.
package provider

import (
	"context"

	"github.com/wesm/moneyflow/internal/domain"
)

// ProfileIdentity is the stable remote profile identity used to lock a local binding.
type ProfileIdentity struct {
	Kind     string
	RemoteID string
}

// Progress describes counts-only progress for one bounded snapshot attempt.
type Progress struct {
	Partition string
	Fetched   int
	Total     int
	Attempt   int
	Pass      int
}

// ProgressFunc observes counts-only snapshot progress.
type ProgressFunc func(Progress)

// Credentials are transient login input and must never be persisted.
type Credentials struct {
	Login       string
	Password    string
	OneTimeCode string
}

// Challenge describes one transient authentication challenge.
type Challenge struct {
	Kind   string
	Prompt string
}

// ChallengeResponder supplies a transient challenge response.
type ChallengeResponder func(context.Context, Challenge) (string, error)

// Session is opaque provider-owned session material.
type Session interface {
	ProviderKind() string
}

// Connector creates and validates provider-owned sessions.
type Connector interface {
	Connect(context.Context, Credentials, ChallengeResponder) (Session, error)
	Validate(context.Context, Session) (ProfileIdentity, error)
}

// Reader probes identity and fetches one complete read-only snapshot.
type Reader interface {
	ProbeIdentity(context.Context) (ProfileIdentity, error)
	FetchSnapshot(context.Context, ProgressFunc) (domain.ImportSnapshot, error)
}

// Optional preserves the difference between an omitted field and its zero value.
type Optional[T any] struct {
	Value   T
	Present bool
}

// Some constructs one present optional field.
func Some[T any](value T) Optional[T] {
	return Optional[T]{Value: value, Present: true}
}

// TransactionUpdate is one absolute provider transaction mutation.
type TransactionUpdate struct {
	TransactionExternalID string
	MerchantName          Optional[string]
	CategoryExternalID    Optional[string]
	Hidden                Optional[bool]
}

// TransactionUpdateResult is the provider-owned transaction state returned by one mutation.
type TransactionUpdateResult struct {
	TransactionExternalID string
	MerchantExternalID    Optional[string]
	MerchantLabel         Optional[string]
	CategoryExternalID    Optional[string]
	Hidden                Optional[bool]
}

// Writer applies exactly one absolute transaction mutation per call.
type Writer interface {
	ProbeIdentity(context.Context) (ProfileIdentity, error)
	UpdateTransaction(context.Context, TransactionUpdate) (TransactionUpdateResult, error)
}

// SessionFingerprint is an opaque session-file generation fingerprint.
type SessionFingerprint string

// Source opens readers and detects an atomically replaced session file.
type Source interface {
	Reader(context.Context, bool) (Reader, SessionFingerprint, error)
	Writer(context.Context, bool) (Writer, SessionFingerprint, error)
	Changed(SessionFingerprint) (bool, error)
}
