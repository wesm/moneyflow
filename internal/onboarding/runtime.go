package onboarding

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

// SessionStore is the provider-owned durable session surface used by onboarding.
type SessionStore interface {
	Load() (monarch.Session, provider.SessionFingerprint, error)
	Save(monarch.Session) error
	Delete() error
}

// CredentialVault is the password-protected Monarch credential surface used by onboarding.
type CredentialVault interface {
	Exists() (bool, error)
	Load([]byte) (monarch.StoredCredentials, error)
	Save(monarch.StoredCredentials, []byte) error
}

// OpenedProfile owns one profile service and its exact cleanup.
type OpenedProfile struct {
	ID      string
	Paths   home.Paths
	Service *app.Service
	Close   func() error
}

// ProfileOpener opens one canonical profile ID under its shared lifecycle lock.
type ProfileOpener func(context.Context, string) (OpenedProfile, error)

// Runtime contains renderer-neutral Monarch dependencies for one profile.
type Runtime struct {
	Sessions     SessionStore
	Credentials  CredentialVault
	NewConnector func(monarch.ImportConfig) (provider.Connector, error)
	NewSource    func(monarch.ImportConfig) (provider.Source, error)
	InstanceID   string
	Now          func() time.Time
}

// RuntimeFactory constructs provider dependencies beneath one validated profile root.
type RuntimeFactory func(home.Paths) (Runtime, error)

type attemptFlow struct {
	opened          *OpenedProfile
	providerLock    *home.Lock
	runtime         Runtime
	connection      app.ProviderConnectionState
	selectedConfig  *monarch.ImportConfig
	explicitConfig  *monarch.ImportConfig
	retainedSession *monarch.Session
	identity        *provider.ProfileIdentity
	renderer        string
	monthToDate     bool
	retryState      State
	imported        int
	taken           bool
	releaseOnce     sync.Once
	releaseErr      error
}

func (flow *attemptFlow) release() error {
	if flow == nil {
		return nil
	}
	flow.releaseOnce.Do(func() {
		var lockErr error
		if flow.providerLock != nil {
			lockErr = flow.providerLock.Release()
			flow.providerLock = nil
		}
		var closeErr error
		if flow.opened != nil && flow.opened.Close != nil {
			closeErr = flow.opened.Close()
			flow.opened = nil
		}
		flow.releaseErr = errors.Join(lockErr, closeErr)
	})
	return flow.releaseErr
}
