// Package profilepool keeps opened profile services shared and evictable.
package profilepool

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
	ErrProfileRegistryClosed = fmt.Errorf("profile registry is closed: %w", api.ErrProfileUnavailable)
	// ErrProfileEvicting reports acquisition while recovery owns the profile lifecycle.
	ErrProfileEvicting = fmt.Errorf("profile is being evicted: %w", api.ErrProfileUnavailable)
)

// ProfileLease is the API-facing lifetime contract implemented by the registry.
type ProfileLease = api.ProfileLease

// RegistryProfile is one opened service and the exact cleanup that owns it.
type RegistryProfile struct {
	ID        string
	Paths     home.Paths
	Service   *app.Service
	Temporary bool
	Close     func() error
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
	// reopen marks an eviction by CloseUnused. An acquisition waits for it and opens the profile
	// again instead of failing.
	reopen   bool
	evicted  chan struct{}
	evictErr error
	// evictWaiters counts Evict calls blocked on the current owner's terminal result.
	evictWaiters int
	refs         int
	lastUsed     time.Time
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

// Acquire returns a reference-counted lease, coalescing concurrent first opens. It fails while an
// eviction owns the profile, except one by CloseUnused, which it waits for.
func (registry *ProfileRegistry) Acquire(
	ctx context.Context,
	profileID string,
) (api.ProfileLease, error) {
	if !profilecatalog.ValidProfileID(profileID) {
		return nil, fmt.Errorf("acquire profile: profile ID is invalid: %w", api.ErrProfileNotFound)
	}
	for {
		registry.mutex.Lock()
		if registry.closed {
			registry.mutex.Unlock()
			return nil, ErrProfileRegistryClosed
		}
		if entry, ok := registry.entries[profileID]; ok {
			if entry.evicting && !entry.reopen {
				registry.mutex.Unlock()
				return nil, ErrProfileEvicting
			}
			if entry.opening || entry.evicting {
				done := entry.ready
				if entry.evicting {
					done = entry.evicted
				}
				registry.mutex.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-done:
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

		profile, err := registry.openPendingProfile(ctx, profileID, entry)
		opened := err == nil
		if opened {
			err = validateRegistryProfile(profile, profileID)
		}
		registry.mutex.Lock()
		if err != nil || registry.closed || entry.evicting {
			terminalErr := err
			if terminalErr == nil && registry.closed {
				terminalErr = ErrProfileRegistryClosed
			}
			if terminalErr == nil {
				terminalErr = ErrProfileEvicting
			}
			// The entry still reads as opening, so an eviction or Close waiting on it returns only
			// after the profile's store and lock are closed.
			if opened && profile.Close != nil {
				registry.mutex.Unlock()
				terminalErr = errors.Join(terminalErr, profile.Close())
				registry.mutex.Lock()
			}
			entry.opening = false
			close(entry.ready)
			delete(registry.entries, profileID)
			registry.signalLocked()
			registry.mutex.Unlock()
			return nil, terminalErr
		}
		entry.opening = false
		close(entry.ready)
		entry.profile = profile
		entry.refs = 1
		entry.lastUsed = registry.now()
		registry.signalLocked()
		registry.mutex.Unlock()
		return &profileLease{registry: registry, entry: entry}, nil
	}
}

func (registry *ProfileRegistry) openPendingProfile(
	ctx context.Context,
	profileID string,
	entry *registryEntry,
) (RegistryProfile, error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			registry.mutex.Lock()
			entry.opening = false
			delete(registry.entries, profileID)
			close(entry.ready)
			registry.signalLocked()
			registry.mutex.Unlock()
			panic(recovered)
		}
	}()
	return registry.open(ctx, profileID)
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

// ProfileRoot returns the exact already-opened profile root.
func (lease *profileLease) ProfileRoot() string { return lease.entry.profile.Paths.Root }

// Temporary reports whether the profile is disposed with this process.
func (lease *profileLease) Temporary() bool { return lease.entry.profile.Temporary }

// Release drops exactly one reference and makes the profile eligible for idle close. Once the
// registry is closed, the last release closes a profile that no eviction is closing and returns
// the close error.
func (lease *profileLease) Release() error {
	if lease == nil || lease.registry == nil || lease.entry == nil {
		return nil
	}
	lease.once.Do(func() { lease.err = lease.registry.release(lease.entry) })
	return lease.err
}

// release drops one reference to entry. Once the registry is closed, the last release evicts an
// entry that no eviction owns, such as one that Close stopped waiting for when its context ended.
func (registry *ProfileRegistry) release(entry *registryEntry) error {
	registry.mutex.Lock()
	if entry.refs < 1 {
		registry.mutex.Unlock()
		return errors.New("release profile lease: reference count is invalid")
	}
	entry.refs--
	entry.lastUsed = registry.now()
	registry.signalLocked()
	if !registry.closed || entry.evicting || entry.refs > 0 {
		registry.mutex.Unlock()
		return nil
	}
	entry.evicting = true
	entry.evicted = make(chan struct{})
	registry.mutex.Unlock()
	return registry.finishEviction(context.Background(), entry.profile.ID, entry)
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
		entry.evictWaiters++
		registry.mutex.Unlock()
		select {
		case <-ctx.Done():
			registry.mutex.Lock()
			entry.evictWaiters--
			registry.mutex.Unlock()
			return ctx.Err()
		case <-evicted:
		}
		registry.mutex.Lock()
		entry.evictWaiters--
		if registry.entries[profileID] == entry {
			registry.mutex.Unlock()
			continue
		}
		err := entry.evictErr
		registry.mutex.Unlock()
		return err
	}
}

// finishEviction is run by the evictor that owns an entry. Once no lease holds the profile, it
// closes the profile and publishes the result to waiters, even after the owner's context ends.
// While a lease holds the profile or it is still opening, an owner whose context ends gives the
// entry up and wakes the waiters to take over.
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
		// The owner gives the entry up only here, after the check above, so a release that drops
		// the last reference as the owner's context ends never leaves the profile open.
		if err := ctx.Err(); err != nil {
			entry.evicting = false
			close(entry.evicted)
			registry.signalLocked()
			registry.mutex.Unlock()
			return err
		}
		changed := registry.changed
		registry.mutex.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
	}
}

// CloseUnused closes the profile at once when no lease holds it, so the next acquisition opens it
// again; an acquisition during the close waits for it. It never waits itself: it reports false,
// and closes nothing, while a lease holds the profile, or it is opening, being evicted or
// temporary. It reports true once the profile is closed, with the close's error, and true when
// the profile is not open.
func (registry *ProfileRegistry) CloseUnused(profileID string) (bool, error) {
	registry.mutex.Lock()
	entry, ok := registry.entries[profileID]
	if !ok {
		registry.mutex.Unlock()
		return true, nil
	}
	// Closing a temporary profile deletes it, as CloseIdle says.
	if entry.opening || entry.evicting || entry.profile.Temporary || entry.refs > 0 {
		registry.mutex.Unlock()
		return false, nil
	}
	entry.evicting, entry.reopen = true, true
	entry.evicted = make(chan struct{})
	registry.signalLocked()
	registry.mutex.Unlock()
	// No lease can start now, so the eviction closes the profile without waiting.
	return true, registry.finishEviction(context.Background(), profileID, entry)
}

// CloseIdle closes every unused profile whose fixed idle period elapsed.
func (registry *ProfileRegistry) CloseIdle(ctx context.Context) error {
	registry.mutex.Lock()
	now := registry.now()
	ids := make([]string, 0)
	for id, entry := range registry.entries {
		// Closing a temporary profile deletes it, so it stays open until shutdown.
		if !entry.opening && !entry.evicting && !entry.profile.Temporary && entry.refs == 0 &&
			now.Sub(entry.lastUsed) >= registry.idleTimeout {
			ids = append(ids, id)
		}
	}
	registry.mutex.Unlock()
	var result error
	for _, id := range ids {
		if err := registry.Evict(ctx, id); err != nil {
			result = errors.Join(result, fmt.Errorf("close idle profile %s: %w", id, err))
		}
	}
	return result
}

// Close rejects new acquisitions and closes every service after active leases finish. When ctx
// ends first, Close returns its error, and a profile still in use closes on its last release.
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
