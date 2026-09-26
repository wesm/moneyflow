package simplefinonboarding

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
)

// SessionStore owns the saved Access URL and exact import settings.
type SessionStore interface {
	Load() (simplefin.Session, provider.SessionFingerprint, error)
	Save(simplefin.Session) error
}

// OpenedProfile owns a service and its shared lifecycle lock.
type OpenedProfile struct {
	ID      string
	Paths   home.Paths
	Service *app.Service
	Close   func() error
}

// Runtime supplies the provider operations for one opened profile.
type Runtime struct {
	Sessions   SessionStore
	Claim      func(context.Context, string) (string, error)
	NewSource  func(app.ProviderConnectionState, simplefin.Session) (provider.ReaderSource, error)
	InstanceID string
	Now        func() time.Time
}

// Config supplies coordinator lifecycle dependencies.
type Config struct {
	Random      io.Reader
	Now         func() time.Time
	InstanceID  string
	OpenProfile func(context.Context, string) (OpenedProfile, error)
	Runtime     func(home.Paths) (Runtime, error)
	Rollback    func(context.Context, string) error
}
type attemptFlow struct {
	opened            *OpenedProfile
	lock              *home.Lock
	runtime           Runtime
	connection        app.ProviderConnectionState
	session           *simplefin.Session
	saved             bool
	retain            bool
	renderer          string
	removeIfAbandoned bool
	releaseOnce       sync.Once
	releaseErr        error
}

func (flow *attemptFlow) release() error {
	flow.releaseOnce.Do(func() {
		flow.session = nil
		if flow.lock != nil {
			flow.releaseErr = flow.lock.Release()
			flow.lock = nil
		}
		if flow.opened != nil {
			flow.releaseErr = errors.Join(flow.releaseErr, flow.opened.Close())
			flow.opened = nil
		}
	})
	return flow.releaseErr
}
