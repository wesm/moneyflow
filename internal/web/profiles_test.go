package web

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
)

const registryTestProfileID = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestProfileRegistryCachesByIDAndClosesAfterIdle(t *testing.T) {
	t.Parallel()
	clock := &registryClock{now: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
	var opens int
	var closes int
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: clock.Now, IdleTimeout: 15 * time.Minute,
		Open: func(context.Context, string) (RegistryProfile, error) {
			opens++
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { closes++; return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)

	first, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, first.Release())
	second, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	assert.Equal(t, 1, opens)
	require.NoError(t, second.Release())

	clock.Advance(15 * time.Minute)
	require.NoError(t, registry.CloseIdle(context.Background()))
	assert.Equal(t, 1, closes)
}

func TestProfileRegistryEvictWaitsForActiveLeaseAndBlocksNewAcquire(t *testing.T) {
	t.Parallel()
	var closes int
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: time.Now,
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { closes++; return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)

	evicted := make(chan error, 1)
	go func() { evicted <- registry.Evict(context.Background(), registryTestProfileID) }()
	require.Eventually(t, func() bool {
		candidate, acquireErr := registry.Acquire(context.Background(), registryTestProfileID)
		if candidate != nil {
			require.NoError(t, candidate.Release())
		}
		return acquireErr != nil
	}, time.Second, time.Millisecond)
	assert.Equal(t, 0, closes)
	require.NoError(t, lease.Release())
	require.NoError(t, <-evicted)
	assert.Equal(t, 1, closes)
}

func TestProfileRegistryCoalescesConcurrentOpenAndClosesOnShutdown(t *testing.T) {
	t.Parallel()
	var mutex sync.Mutex
	var opens int
	var closes int
	started := make(chan struct{})
	continueOpen := make(chan struct{})
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: time.Now,
		Open: func(context.Context, string) (RegistryProfile, error) {
			mutex.Lock()
			opens++
			mutex.Unlock()
			close(started)
			<-continueOpen
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error {
					mutex.Lock()
					defer mutex.Unlock()
					closes++
					return nil
				},
			}, serviceErr
		},
	})
	require.NoError(t, err)

	type result struct {
		lease ProfileLease
		err   error
	}
	results := make(chan result, 2)
	go func() {
		lease, acquireErr := registry.Acquire(context.Background(), registryTestProfileID)
		results <- result{lease: lease, err: acquireErr}
	}()
	<-started
	go func() {
		lease, acquireErr := registry.Acquire(context.Background(), registryTestProfileID)
		results <- result{lease: lease, err: acquireErr}
	}()
	close(continueOpen)
	first := <-results
	second := <-results
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	assert.Same(t, first.lease.Service(), second.lease.Service())
	mutex.Lock()
	assert.Equal(t, 1, opens)
	mutex.Unlock()
	require.NoError(t, first.lease.Release())
	require.NoError(t, second.lease.Release())
	require.NoError(t, registry.Close(context.Background()))
	mutex.Lock()
	assert.Equal(t, 1, closes)
	mutex.Unlock()
}

func TestProfileRegistryCanceledEvictionRestoresAcquisition(t *testing.T) {
	t.Parallel()
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: time.Now,
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, registry.Evict(ctx, registryTestProfileID), context.Canceled)

	second, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, second.Release())
	require.NoError(t, lease.Release())
	require.NoError(t, registry.Close(context.Background()))
}

func TestProfileRegistryShutdownDuringOpenReportsClosed(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	continueOpen := make(chan struct{})
	var closes int
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: time.Now,
		Open: func(context.Context, string) (RegistryProfile, error) {
			close(started)
			<-continueOpen
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { closes++; return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)

	acquired := make(chan error, 1)
	go func() {
		_, acquireErr := registry.Acquire(context.Background(), registryTestProfileID)
		acquired <- acquireErr
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, registry.Close(ctx), context.Canceled)
	_, acquireErr := registry.Acquire(context.Background(), registryTestProfileID)
	assert.ErrorIs(t, acquireErr, ErrProfileRegistryClosed)
	close(continueOpen)

	assert.ErrorIs(t, <-acquired, ErrProfileRegistryClosed)
	assert.Equal(t, 1, closes)
}

func TestProfileRegistryOnboardingOpenerUsesCachedService(t *testing.T) {
	t.Parallel()
	var opens int
	var closes int
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: time.Now,
		Open: func(context.Context, string) (RegistryProfile, error) {
			opens++
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { closes++; return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)

	requestLease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	onboardingLease, err := registry.OnboardingOpener()(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	assert.Same(t, requestLease.Service(), onboardingLease.Service)
	assert.Equal(t, 1, opens)
	require.NoError(t, onboardingLease.Close())
	require.NoError(t, requestLease.Release())
	require.NoError(t, registry.Evict(context.Background(), registryTestProfileID))
	assert.Equal(t, 1, closes)
}

type registryClock struct {
	mutex sync.Mutex
	now   time.Time
}

func (clock *registryClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.now
}

func (clock *registryClock) Advance(delta time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.now = clock.now.Add(delta)
}
