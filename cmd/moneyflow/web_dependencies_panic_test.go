package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilepool"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

func TestWebProfileOpenerOwnsProfileUntilProviderSetupCompletes(t *testing.T) {
	setupErr := errors.New("synthetic setup failure")
	closeErr := errors.New("synthetic close failure")
	for _, test := range []struct {
		name       string
		panicSetup bool
		setupErr   error
		closeErr   error
	}{
		{name: "panic", panicSetup: true},
		{name: "panic with close error", panicSetup: true, closeErr: closeErr},
		{name: "setup and close errors", setupErr: setupErr, closeErr: closeErr},
		{name: "success"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "profile")
			bindCommandProfile(t, root, time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC))
			seed, err := openProfile(t.Context(), ProfileOptions{ExplicitHome: root})
			require.NoError(t, err)
			require.NoError(t, seed.Close())
			var opened OpenedProfile
			closeCalls := 0
			factoryCalls := 0
			profiles := webProfileOpener{
				explicitHome: root,
				opener: func(ctx context.Context, options ProfileOptions) (OpenedProfile, error) {
					var openErr error
					opened, openErr = openProfile(ctx, options)
					if openErr != nil {
						return OpenedProfile{}, openErr
					}
					closeProfile := opened.Close
					t.Cleanup(func() { _ = closeProfile() })
					opened.Close = func() error {
						closeCalls++
						return errors.Join(closeProfile(), test.closeErr)
					}
					return opened, nil
				},
				streams: IOStreams{
					OpenMonarch: func(home.Paths, monarch.ImportConfig) (MonarchCommandRuntime, error) {
						factoryCalls++
						if test.panicSetup {
							panic("synthetic provider factory failure")
						}
						return MonarchCommandRuntime{
							ReadSource: &commandProviderSource{}, WriteSource: &commandProviderSource{},
							InstanceID: "web-test",
						}, test.setupErr
					},
				},
			}
			var registered profilepool.RegistryProfile
			var openErr error
			open := func() { registered, openErr = profiles.open(t.Context(), seed.ID) }
			if test.panicSetup {
				require.PanicsWithValue(t, "synthetic provider factory failure", open)
			} else {
				open()
				if test.setupErr != nil {
					assert.ErrorIs(t, openErr, setupErr)
					assert.ErrorIs(t, openErr, closeErr)
				} else {
					require.NoError(t, openErr)
					assert.Zero(t, closeCalls, "the returned profile owns cleanup")
					_, connectionErr := registered.Service.ProviderConnection(t.Context())
					require.NoError(t, connectionErr)
					require.NoError(t, registered.Close())
				}
			}
			assert.Equal(t, 1, factoryCalls)
			assert.Equal(t, 1, closeCalls)
			_, err = opened.Service.ProviderConnection(t.Context())
			assert.Error(t, err, "the opened SQLite handle must be closed")
			exclusive, err := home.TryLockExisting(opened.Paths.Root, home.LockProfile, home.LockExclusive)
			require.NoError(t, err, "the profile lifecycle lock must be released")
			require.NoError(t, exclusive.Release())
		})
	}
}
