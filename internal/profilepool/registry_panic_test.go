package profilepool

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestProfileRegistryOpeningPanicWakesAcquisitionWaiters(t *testing.T) {
	root := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		continueOpen := make(chan struct{})
		var opens, closes int
		service, err := app.NewService(nil)
		require.NoError(t, err)
		registry, err := NewProfileRegistry(ProfileRegistryConfig{
			Open: func(context.Context, string) (RegistryProfile, error) {
				opens++
				if opens == 1 {
					<-continueOpen
					panic("synthetic open failure")
				}
				return RegistryProfile{
					ID: registryTestProfileID, Service: service, Paths: home.Paths{Root: root},
					Close: func() error { closes++; return nil },
				}, nil
			},
		})
		require.NoError(t, err)
		panicked := make(chan any, 1)
		go func() {
			defer func() { panicked <- recover() }()
			_, _ = registry.Acquire(ctx, registryTestProfileID)
		}()
		synctest.Wait()
		type result struct {
			lease ProfileLease
			err   error
		}
		acquired := make(chan result, 1)
		go func() {
			lease, acquireErr := registry.Acquire(ctx, registryTestProfileID)
			acquired <- result{lease: lease, err: acquireErr}
		}()
		synctest.Wait()
		close(continueOpen)
		assert.Equal(t, "synthetic open failure", <-panicked)
		waiter := <-acquired
		require.NoError(t, waiter.err)
		assert.Same(t, service, waiter.lease.Service())
		require.NoError(t, waiter.lease.Release())
		require.NoError(t, registry.Evict(ctx, registryTestProfileID))
		assert.Equal(t, 2, opens)
		assert.Equal(t, 1, closes)
	})
}

func TestProfileRegistryOpeningPanicUnblocksEvictionAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "eviction"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				continueOpen := make(chan struct{})
				registry, err := NewProfileRegistry(ProfileRegistryConfig{
					Open: func(context.Context, string) (RegistryProfile, error) {
						<-continueOpen
						panic("synthetic open failure")
					},
				})
				require.NoError(t, err)
				panicked := make(chan any, 1)
				go func() {
					defer func() { panicked <- recover() }()
					_, _ = registry.Acquire(ctx, registryTestProfileID)
				}()
				synctest.Wait()
				closed := make(chan error, 1)
				go func() {
					if shutdown {
						closed <- registry.Close(ctx)
						return
					}
					closed <- registry.Evict(ctx, registryTestProfileID)
				}()
				synctest.Wait()
				close(continueOpen)
				assert.Equal(t, "synthetic open failure", <-panicked)
				require.NoError(t, <-closed)
				require.NoError(t, registry.Close(ctx))
			})
		})
	}
}

func TestProfileRegistryOnboardingRecoversAfterOpeningPanic(t *testing.T) {
	for _, action := range []onboarding.ActionType{
		onboarding.ActionRetry, onboarding.ActionReauthenticate,
	} {
		t.Run(string(action), func(t *testing.T) {
			paths, err := home.ResolveRoot(t.TempDir(), nil, "")
			require.NoError(t, err)
			var opens, closes atomic.Int32
			registry, err := NewProfileRegistry(ProfileRegistryConfig{
				Open: func(ctx context.Context, id string) (RegistryProfile, error) {
					if opens.Add(1) == 1 {
						panic("synthetic open failure")
					}
					handle, openErr := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
					if openErr != nil {
						return RegistryProfile{}, openErr
					}
					service, openErr := app.NewProfileService(ctx, handle)
					if openErr != nil {
						_ = handle.Close()
						return RegistryProfile{}, openErr
					}
					return RegistryProfile{
						ID: id, Service: service, Paths: paths,
						Close: func() error { closes.Add(1); return handle.Close() },
					}, nil
				},
			})
			require.NoError(t, err)
			coordinator, err := onboarding.NewCoordinator(onboarding.Config{
				InstanceID: "test-instance", OpenProfile: registry.OnboardingOpener(),
				Runtime: func(paths home.Paths) (onboarding.Runtime, error) {
					vault, vaultErr := ynab.NewCredentialVault(paths)
					return onboarding.Runtime{
						ProviderKind: "ynab", InstanceID: "provider-instance", YNABVault: vault,
						NewYNABClient: func([]byte) (onboarding.YNABPlanClient, error) {
							t.Error("credential entry must not contact the provider")
							return nil, nil
						},
						NewYNABSource: func(ynab.StoredCredentials, *provider.SnapshotResult) (
							provider.ReaderSource, provider.WriterSource, error,
						) {
							t.Error("credential entry must not create a provider source")
							return nil, nil, nil
						},
					}, vaultErr
				},
			})
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = coordinator.Close(ctx)
				_ = registry.Close(ctx)
			})
			started, err := coordinator.Start(t.Context(), onboarding.StartRequest{
				ProfileID: registryTestProfileID, ProviderKind: "ynab",
			})
			require.NoError(t, err)
			waitForState := func(state onboarding.State) onboarding.Snapshot {
				t.Helper()
				var snapshot onboarding.Snapshot
				require.Eventually(t, func() bool {
					var statusErr error
					snapshot, statusErr = coordinator.Status(t.Context(), onboarding.StatusRequest{
						ProfileID: registryTestProfileID, AttemptID: started.AttemptID,
					})
					return statusErr == nil && snapshot.State == state
				}, time.Second, time.Millisecond)
				return snapshot
			}
			failed := waitForState(onboarding.StateFailed)
			_, err = coordinator.Submit(t.Context(), onboarding.SubmitRequest{
				ProfileID: registryTestProfileID, AttemptID: failed.AttemptID,
				ExpectedStateVersion: failed.StateVersion, Action: action,
			})
			require.NoError(t, err)
			waitForState(onboarding.StateCredentialsRequired)
			lease, err := registry.Acquire(t.Context(), registryTestProfileID)
			require.NoError(t, err)
			require.NoError(t, lease.Release())
			assert.Equal(t, int32(2), opens.Load())
			require.NoError(t, coordinator.Close(t.Context()))
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			require.NoError(t, registry.Evict(ctx, registryTestProfileID))
			assert.Equal(t, int32(1), closes.Load())
		})
	}
}
