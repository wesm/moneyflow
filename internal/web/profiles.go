package web

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

const defaultProfileIdleTimeout = 15 * time.Minute

var (
	// ErrProfileRegistryClosed reports acquisition after server shutdown begins.
	ErrProfileRegistryClosed = errors.New("profile registry is closed")
	// ErrProfileEvicting reports acquisition while recovery owns the profile lifecycle.
	ErrProfileEvicting = errors.New("profile is being evicted")
)

// ProfileLease is the API-facing lifetime contract implemented by the registry.
type ProfileLease = api.ProfileLease

// RegistryProfile is one opened service and the exact cleanup that owns it.
type RegistryProfile struct {
	ID      string
	Paths   home.Paths
	Service *app.Service
	Close   func() error
}

// RegistryProfileOpener opens a canonical profile under its shared lifecycle lock.
type RegistryProfileOpener func(context.Context, string) (RegistryProfile, error)

// ProfileRegistryConfig supplies deterministic registry dependencies.
type ProfileRegistryConfig struct {
	Open        RegistryProfileOpener
	Now         func() time.Time
	IdleTimeout time.Duration
}

// ProfileRegistry lazily owns one cached service per canonical profile ID.
type ProfileRegistry struct {
	mutex       sync.Mutex
	open        RegistryProfileOpener
	now         func() time.Time
	idleTimeout time.Duration
	entries     map[string]*registryEntry
	changed     chan struct{}
	closed      bool
}

type registryEntry struct {
	profile  RegistryProfile
	ready    chan struct{}
	opening  bool
	evicting bool
	evicted  chan struct{}
	evictErr error
	refs     int
	lastUsed time.Time
}

type profileLease struct {
	registry *ProfileRegistry
	entry    *registryEntry
	once     sync.Once
	err      error
}

var _ api.ProfileResolver = (*ProfileRegistry)(nil)

// NewProfileRegistry constructs an empty lazy registry.
func NewProfileRegistry(config ProfileRegistryConfig) (*ProfileRegistry, error) {
	if config.Open == nil {
		return nil, errors.New("new profile registry: opener is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = defaultProfileIdleTimeout
	}
	if config.IdleTimeout < 0 {
		return nil, errors.New("new profile registry: idle timeout is invalid")
	}
	return &ProfileRegistry{
		open: config.Open, now: config.Now, idleTimeout: config.IdleTimeout,
		entries: make(map[string]*registryEntry), changed: make(chan struct{}),
	}, nil
}

// Acquire returns a reference-counted lease, coalescing concurrent first opens.
func (registry *ProfileRegistry) Acquire(
	ctx context.Context,
	profileID string,
) (api.ProfileLease, error) {
	if !profilecatalog.ValidProfileID(profileID) {
		return nil, errors.New("acquire profile: profile ID is invalid")
	}
	for {
		registry.mutex.Lock()
		if registry.closed {
			registry.mutex.Unlock()
			return nil, ErrProfileRegistryClosed
		}
		if entry, ok := registry.entries[profileID]; ok {
			if entry.evicting {
				registry.mutex.Unlock()
				return nil, ErrProfileEvicting
			}
			if entry.opening {
				ready := entry.ready
				registry.mutex.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ready:
					continue
				}
			}
			entry.refs++
			entry.lastUsed = registry.now()
			registry.signalLocked()
			registry.mutex.Unlock()
			return &profileLease{registry: registry, entry: entry}, nil
		}
		entry := &registryEntry{opening: true, ready: make(chan struct{})}
		registry.entries[profileID] = entry
		registry.signalLocked()
		registry.mutex.Unlock()

		profile, err := registry.open(ctx, profileID)
		opened := err == nil
		if opened {
			err = validateRegistryProfile(profile, profileID)
		}
		registry.mutex.Lock()
		entry.opening = false
		close(entry.ready)
		if err != nil || registry.closed || entry.evicting {
			terminalErr := err
			if terminalErr == nil && registry.closed {
				terminalErr = ErrProfileRegistryClosed
			}
			if terminalErr == nil {
				terminalErr = ErrProfileEvicting
			}
			delete(registry.entries, profileID)
			registry.signalLocked()
			registry.mutex.Unlock()
			if opened && profile.Close != nil {
				terminalErr = errors.Join(terminalErr, profile.Close())
			}
			return nil, terminalErr
		}
		entry.profile = profile
		entry.refs = 1
		entry.lastUsed = registry.now()
		registry.signalLocked()
		registry.mutex.Unlock()
		return &profileLease{registry: registry, entry: entry}, nil
	}
}

func validateRegistryProfile(profile RegistryProfile, expectedID string) error {
	if profile.ID != expectedID || profile.Service == nil || profile.Close == nil ||
		profile.Paths.Root == "" {
		return errors.New("open profile registry entry: opened profile is incomplete")
	}
	return nil
}

// Service returns the leased application service.
func (lease *profileLease) Service() *app.Service { return lease.entry.profile.Service }

// Release drops exactly one reference and makes the profile eligible for idle close.
func (lease *profileLease) Release() error {
	if lease == nil || lease.registry == nil || lease.entry == nil {
		return nil
	}
	lease.once.Do(func() {
		lease.registry.mutex.Lock()
		defer lease.registry.mutex.Unlock()
		if lease.entry.refs < 1 {
			lease.err = errors.New("release profile lease: reference count is invalid")
			return
		}
		lease.entry.refs--
		lease.entry.lastUsed = lease.registry.now()
		lease.registry.signalLocked()
	})
	return lease.err
}

// Evict waits for active requests, closes the cached service, and permits a fresh open.
// Concurrent evictions of one profile share the owner's terminal close result.
func (registry *ProfileRegistry) Evict(ctx context.Context, profileID string) error {
	if !profilecatalog.ValidProfileID(profileID) {
		return errors.New("evict profile: profile ID is invalid")
	}
	for {
		registry.mutex.Lock()
		entry, ok := registry.entries[profileID]
		if !ok {
			registry.mutex.Unlock()
			return nil
		}
		if !entry.evicting {
			entry.evicting = true
			entry.evicted = make(chan struct{})
			registry.signalLocked()
			registry.mutex.Unlock()
			return registry.finishEviction(ctx, profileID, entry)
		}
		evicted := entry.evicted
		registry.mutex.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-evicted:
		}
		registry.mutex.Lock()
		if registry.entries[profileID] == entry {
			registry.mutex.Unlock()
			continue
		}
		err := entry.evictErr
		registry.mutex.Unlock()
		return err
	}
}

// finishEviction is run by the evictor that owns an entry. It publishes the terminal close
// result to waiters, or wakes them to take over when the owner's context ends first.
func (registry *ProfileRegistry) finishEviction(
	ctx context.Context,
	profileID string,
	entry *registryEntry,
) error {
	for {
		registry.mutex.Lock()
		if registry.entries[profileID] != entry {
			close(entry.evicted)
			registry.mutex.Unlock()
			return nil
		}
		if !entry.opening && entry.refs == 0 {
			registry.mutex.Unlock()
			err := entry.profile.Close()
			registry.mutex.Lock()
			entry.evictErr = err
			if registry.entries[profileID] == entry {
				delete(registry.entries, profileID)
			}
			close(entry.evicted)
			registry.signalLocked()
			registry.mutex.Unlock()
			return err
		}
		changed := registry.changed
		registry.mutex.Unlock()
		select {
		case <-ctx.Done():
			registry.mutex.Lock()
			if registry.entries[profileID] == entry && !registry.closed {
				entry.evicting = false
			}
			close(entry.evicted)
			registry.signalLocked()
			registry.mutex.Unlock()
			return ctx.Err()
		case <-changed:
		}
	}
}

// CloseIdle closes every unused profile whose fixed idle period elapsed.
func (registry *ProfileRegistry) CloseIdle(ctx context.Context) error {
	registry.mutex.Lock()
	now := registry.now()
	ids := make([]string, 0)
	for id, entry := range registry.entries {
		if !entry.opening && !entry.evicting && entry.refs == 0 &&
			now.Sub(entry.lastUsed) >= registry.idleTimeout {
			ids = append(ids, id)
		}
	}
	registry.mutex.Unlock()
	var result error
	for _, id := range ids {
		if err := registry.Evict(ctx, id); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// Close rejects new acquisitions and closes every service after active leases finish.
func (registry *ProfileRegistry) Close(ctx context.Context) error {
	registry.mutex.Lock()
	registry.closed = true
	ids := make([]string, 0, len(registry.entries))
	for id := range registry.entries {
		ids = append(ids, id)
	}
	registry.signalLocked()
	registry.mutex.Unlock()
	var result error
	for _, id := range ids {
		if err := registry.Evict(ctx, id); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// OnboardingOpener leases the same cached service used by finance requests.
func (registry *ProfileRegistry) OnboardingOpener() onboarding.ProfileOpener {
	return func(ctx context.Context, profileID string) (onboarding.OpenedProfile, error) {
		lease, err := registry.Acquire(ctx, profileID)
		if err != nil {
			return onboarding.OpenedProfile{}, err
		}
		concrete, ok := lease.(*profileLease)
		if !ok {
			_ = lease.Release()
			return onboarding.OpenedProfile{}, fmt.Errorf("open onboarding profile: lease type is invalid")
		}
		return onboarding.OpenedProfile{
			ID: profileID, Paths: concrete.entry.profile.Paths,
			Service: lease.Service(), Close: lease.Release,
		}, nil
	}
}

func (registry *ProfileRegistry) signalLocked() {
	close(registry.changed)
	registry.changed = make(chan struct{})
}
