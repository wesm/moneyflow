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
}

// ProgressFunc observes counts-only snapshot progress.
type ProgressFunc func(Progress)

// Credentials are transient login input and must never be persisted.
type Credentials struct {
	Login    string
	Password string
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

// SessionFingerprint is an opaque session-file generation fingerprint.
type SessionFingerprint string

// Source opens readers and detects an atomically replaced session file.
type Source interface {
	Reader(context.Context, bool) (Reader, SessionFingerprint, error)
	Changed(SessionFingerprint) (bool, error)
}
