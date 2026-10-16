package profilepool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
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

func TestProfileRegistryIdleCloseFailureNamesTheProfile(t *testing.T) {
	t.Parallel()
	clock := &registryClock{now: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: clock.Now, IdleTimeout: 15 * time.Minute,
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service, Paths: home.Paths{Root: t.TempDir()},
				Close: func() error { return errors.New("synthetic close failure") },
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, lease.Release())

	clock.Advance(15 * time.Minute)
	err = registry.CloseIdle(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), registryTestProfileID)
	assert.Contains(t, err.Error(), "synthetic close failure")
}

func TestProfileRegistryKeepsTemporaryProfileUntilShutdown(t *testing.T) {
	t.Parallel()
	clock := &registryClock{now: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
	opens := 0
	closes := 0
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Now: clock.Now, IdleTimeout: 15 * time.Minute,
		Open: func(context.Context, string) (RegistryProfile, error) {
			opens++
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service, Temporary: true,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { closes++; return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)
	first, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, first.Release())

	clock.Advance(time.Hour)
	require.NoError(t, registry.CloseIdle(context.Background()))
	assert.Zero(t, closes, "closing an idle temporary profile would discard its edits")
	second, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	assert.Same(t, first.Service(), second.Service())
	assert.Equal(t, 1, opens)
	require.NoError(t, second.Release())

	require.NoError(t, registry.Close(context.Background()))
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

func TestProfileRegistryConcurrentEvictionClosesExactlyOnce(t *testing.T) {
	t.Parallel()
	continueClose := make(chan struct{})
	var closes atomic.Int32
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error {
					closes.Add(1)
					<-continueClose
					return nil
				},
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, lease.Release())

	results := make(chan error, 2)
	go func() { results <- registry.Evict(context.Background(), registryTestProfileID) }()
	require.Eventually(t, func() bool { return closes.Load() == 1 }, time.Second, time.Millisecond)
	go func() { results <- registry.Evict(context.Background(), registryTestProfileID) }()
	time.Sleep(25 * time.Millisecond)
	assert.Equal(t, int32(1), closes.Load())
	close(continueClose)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	assert.Equal(t, int32(1), closes.Load())
}

func TestProfileRegistryClosesProfileRejectedAfterSuccessfulOpen(t *testing.T) {
	t.Parallel()
	var closes int
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: "profile_baaaaaaaaaaaaaaaaaaaaaaaaa", Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error { closes++; return nil },
			}, serviceErr
		},
	})
	require.NoError(t, err)
	_, err = registry.Acquire(context.Background(), registryTestProfileID)
	assert.ErrorContains(t, err, "opened profile is incomplete")
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
	assert.ErrorIs(t, acquireErr, api.ErrProfileUnavailable)
	close(continueOpen)

	assert.ErrorIs(t, <-acquired, ErrProfileRegistryClosed)
	assert.Equal(t, 1, closes)
}

func TestProfileRegistryWaitsForAnInterruptedOpenToCloseItsProfile(t *testing.T) {
	tests := []struct {
		name       string
		stop       func(context.Context, *ProfileRegistry) error
		acquireErr error
	}{
		{
			name: "shutdown",
			stop: func(ctx context.Context, registry *ProfileRegistry) error {
				return registry.Close(ctx)
			},
			acquireErr: ErrProfileRegistryClosed,
		},
		{
			name: "eviction",
			stop: func(ctx context.Context, registry *ProfileRegistry) error {
				return registry.Evict(ctx, registryTestProfileID)
			},
			acquireErr: ErrProfileEvicting,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				continueOpen := make(chan struct{})
				continueClose := make(chan struct{})
				var profileClosed atomic.Bool
				service, err := app.NewService(nil)
				require.NoError(t, err)
				registry, err := NewProfileRegistry(ProfileRegistryConfig{
					Open: func(context.Context, string) (RegistryProfile, error) {
						<-continueOpen
						return RegistryProfile{
							ID: registryTestProfileID, Service: service, Paths: home.Paths{Root: root},
							Close: func() error {
								<-continueClose
								profileClosed.Store(true)
								return nil
							},
						}, nil
					},
				})
				require.NoError(t, err)
				acquired := make(chan error, 1)
				go func() {
					_, acquireErr := registry.Acquire(ctx, registryTestProfileID)
					acquired <- acquireErr
				}()
				synctest.Wait()
				type outcome struct {
					err           error
					profileClosed bool
				}
				stopped := make(chan outcome, 1)
				go func() {
					stopErr := test.stop(ctx, registry)
					stopped <- outcome{err: stopErr, profileClosed: profileClosed.Load()}
				}()
				synctest.Wait()
				close(continueOpen)
				// The open finishes, finds the registry stopping, and blocks closing its profile.
				synctest.Wait()
				_, err = registry.Acquire(ctx, registryTestProfileID)
				assert.ErrorIs(t, err, test.acquireErr, "the registry answers while the profile closes")
				close(continueClose)

				result := <-stopped
				require.NoError(t, result.err)
				assert.True(t, result.profileClosed, "returned while the opened profile was closing")
				assert.ErrorIs(t, <-acquired, test.acquireErr)
			})
		})
	}
}

func TestProfileRegistryLastReleaseClosesAProfileThatShutdownLeftOpen(t *testing.T) {
	t.Parallel()
	closeErr := errors.New("synthetic close failure")
	closes := 0
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service, Paths: home.Paths{Root: t.TempDir()},
				Close: func() error { closes++; return closeErr },
			}, serviceErr
		},
	})
	require.NoError(t, err)
	first, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	last, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, registry.Close(ctx), context.Canceled)
	assert.Zero(t, closes, "Close leaves a profile in use open")
	require.NoError(t, first.Release())
	assert.Zero(t, closes, "the profile stays open while another lease holds it")

	assert.ErrorIs(t, last.Release(), closeErr, "the last release closes the profile")
	assert.Equal(t, 1, closes)
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	assert.Empty(t, registry.entries)
}

func TestProfileRegistryClosesAProfileWhoseLastReleaseRacesTheOwnersCancel(t *testing.T) {
	stops := map[string]func(context.Context, *ProfileRegistry) error{
		"shutdown": func(ctx context.Context, registry *ProfileRegistry) error {
			return registry.Close(ctx)
		},
		"eviction": func(ctx context.Context, registry *ProfileRegistry) error {
			return registry.Evict(ctx, registryTestProfileID)
		},
	}
	for name, stop := range stops {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				var cancelOnClockRead atomic.Bool
				var closes atomic.Int32
				service, err := app.NewService(nil)
				require.NoError(t, err)
				registry, err := NewProfileRegistry(ProfileRegistryConfig{
					// A release reads the clock while it holds the registry, after it drops its
					// reference. Ending the owner's context there wakes the owner while the release
					// still decides who closes the profile.
					Now: func() time.Time {
						if cancelOnClockRead.Load() {
							cancel()
						}
						return time.Now()
					},
					Open: func(context.Context, string) (RegistryProfile, error) {
						return RegistryProfile{
							ID: registryTestProfileID, Service: service,
							Paths: home.Paths{Root: root},
							Close: func() error { closes.Add(1); return nil },
						}, nil
					},
				})
				require.NoError(t, err)
				lease, err := registry.Acquire(ctx, registryTestProfileID)
				require.NoError(t, err)
				stopped := make(chan error, 1)
				go func() { stopped <- stop(ctx, registry) }()
				synctest.Wait()

				cancelOnClockRead.Store(true)
				require.NoError(t, lease.Release())

				require.ErrorIs(t, ctx.Err(), context.Canceled,
					"the release ended the owner's context")
				stopErr := <-stopped
				assert.Equal(t, int32(1), closes.Load(), "the profile closes exactly once")
				assert.NoError(t, stopErr, "the owner closed the profile")
			})
		})
	}
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

func TestProfileRegistryConcurrentEvictionWaitersObserveCloseFailure(t *testing.T) {
	t.Parallel()
	continueClose := make(chan struct{})
	closeErr := errors.New("synthetic close failure")
	var closes atomic.Int32
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error {
					closes.Add(1)
					<-continueClose
					return closeErr
				},
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, lease.Release())

	results := make(chan error, 2)
	go func() { results <- registry.Evict(context.Background(), registryTestProfileID) }()
	require.Eventually(t, func() bool { return closes.Load() == 1 }, time.Second, time.Millisecond)
	go func() { results <- registry.Evict(context.Background(), registryTestProfileID) }()
	waitForEvictWaiters(t, registry, 1)
	close(continueClose)
	assert.ErrorIs(t, <-results, closeErr)
	assert.ErrorIs(t, <-results, closeErr)
	assert.Equal(t, int32(1), closes.Load())
}

func TestProfileRegistryEvictionWaiterTakesOverAfterOwnerCancels(t *testing.T) {
	t.Parallel()
	var closes atomic.Int32
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error {
					closes.Add(1)
					return nil
				},
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)

	ownerContext, cancelOwner := context.WithCancel(context.Background())
	ownerResult := make(chan error, 1)
	go func() { ownerResult <- registry.Evict(ownerContext, registryTestProfileID) }()
	require.Eventually(t, func() bool {
		registry.mutex.Lock()
		defer registry.mutex.Unlock()
		return registry.entries[registryTestProfileID].evicting
	}, time.Second, time.Millisecond)
	waiterResult := make(chan error, 1)
	go func() { waiterResult <- registry.Evict(context.Background(), registryTestProfileID) }()
	time.Sleep(25 * time.Millisecond)
	cancelOwner()
	assert.ErrorIs(t, <-ownerResult, context.Canceled)
	select {
	case result := <-waiterResult:
		t.Fatalf("waiter returned %v before the lease was released", result)
	case <-time.After(25 * time.Millisecond):
	}
	require.NoError(t, lease.Release())
	require.NoError(t, <-waiterResult)
	assert.Equal(t, int32(1), closes.Load())
	second, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)
	require.NoError(t, second.Release())
	require.NoError(t, registry.Close(context.Background()))
}

func TestProfileRegistryShutdownWaiterTakesOverAfterOwnerCancels(t *testing.T) {
	t.Parallel()
	var closes atomic.Int32
	registry, err := NewProfileRegistry(ProfileRegistryConfig{
		Open: func(context.Context, string) (RegistryProfile, error) {
			service, serviceErr := app.NewService(nil)
			return RegistryProfile{
				ID: registryTestProfileID, Service: service,
				Paths: home.Paths{Root: t.TempDir()}, Close: func() error {
					closes.Add(1)
					return nil
				},
			}, serviceErr
		},
	})
	require.NoError(t, err)
	lease, err := registry.Acquire(context.Background(), registryTestProfileID)
	require.NoError(t, err)

	ownerContext, cancelOwner := context.WithCancel(context.Background())
	ownerResult := make(chan error, 1)
	go func() { ownerResult <- registry.Evict(ownerContext, registryTestProfileID) }()
	require.Eventually(t, func() bool {
		registry.mutex.Lock()
		defer registry.mutex.Unlock()
		return registry.entries[registryTestProfileID].evicting
	}, time.Second, time.Millisecond)
	closeResult := make(chan error, 1)
	go func() { closeResult <- registry.Close(context.Background()) }()
	waitForEvictWaiters(t, registry, 1)
	cancelOwner()
	assert.ErrorIs(t, <-ownerResult, context.Canceled)
	select {
	case result := <-closeResult:
		t.Fatalf("shutdown returned %v before the lease was released", result)
	case <-time.After(25 * time.Millisecond):
	}
	require.NoError(t, lease.Release())
	select {
	case result := <-closeResult:
		require.NoError(t, result)
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after the eviction owner canceled")
	}
	assert.Equal(t, int32(1), closes.Load())
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	assert.Empty(t, registry.entries)
}

func waitForEvictWaiters(t *testing.T, registry *ProfileRegistry, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		registry.mutex.Lock()
		defer registry.mutex.Unlock()
		entry, ok := registry.entries[registryTestProfileID]
		return ok && entry.evictWaiters == want
	}, time.Second, time.Millisecond)
}
