package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestProviderDeletionConfirmationFoldsExactCandidateAndReleasesLease(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, profileHandle := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 20, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 30), fingerprint: "session-a",
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 6))

	blocked, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)
	assert.NotEmpty(t, blocked.Status.ConfirmationToken)
	state, stateErr := profileHandle.ProviderState(ctx)
	require.NoError(t, stateErr)
	assert.Nil(t, state.Lease)
	assert.Equal(t, uint64(1), state.Refresh.Generation)

	confirmed, err := service.ConfirmProviderRefresh(ctx, app.ProviderRefreshRequest{
		Manual: true, ConfirmationToken: blocked.Status.ConfirmationToken,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	assert.Equal(t, uint64(2), confirmed.Generation)
	loaded, err := profileHandle.Load(ctx)
	require.NoError(t, err)
	assert.Len(t, loaded.Committed.Transactions, 6)
}

func TestProviderConfirmationRejectsWrongProcessExpiryAndGenerationChange(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		instanceID string
	}{
		{"wrong process", "instance-b"},
		{"lost candidate", "instance-a"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service, profileHandle := newProviderRefreshService(t)
			now := time.Date(2026, time.August, 15, 20, 30, 0, 0, time.UTC)
			source := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				snapshot: providerSnapshot(t, now, 30), fingerprint: "session-a",
			}
			configureProviderRefreshService(t, service, source, now, "instance-a")
			_, err := service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			source.setSnapshot(providerSnapshot(t, now.Add(time.Minute), 6))
			blocked, err := service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)
			other, err := app.NewProfileService(context.Background(), profileHandle)
			require.NoError(t, err)
			configureProviderRefreshService(t, other, source, now, test.instanceID)
			_, err = other.ConfirmProviderRefresh(context.Background(), app.ProviderRefreshRequest{
				ConfirmationToken: blocked.Status.ConfirmationToken,
				State:             app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			assertProviderAppCode(t, err, provider.CodeConfirmationInvalid)
		})
	}
}

func TestProviderConfirmationExpiresAndIsInvalidatedByAnotherFold(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"expiry", "generation"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			service, profileHandle := newProviderRefreshService(t)
			current := time.Date(2026, time.August, 15, 21, 0, 0, 0, time.UTC)
			source := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				snapshot: providerSnapshot(t, current, 30), fingerprint: "session-a",
			}
			require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
				Source: source, Provider: "monarch", Renderer: "tui", InstanceID: "instance-a",
				Now: func() time.Time { return current }, Random: &incrementingReader{},
				ConfirmationTTL: time.Minute,
			}))
			_, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			require.NoError(t, err)
			source.setSnapshot(providerSnapshot(t, current.Add(time.Minute), 6))
			blocked, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
				Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			assertProviderAppCode(t, err, provider.CodeDeletionConfirmationRequired)

			if mode == "expiry" {
				current = current.Add(2 * time.Minute)
			} else {
				other, otherErr := app.NewProfileService(ctx, profileHandle)
				require.NoError(t, otherErr)
				source.setSnapshot(providerSnapshot(t, current, 30))
				configureProviderRefreshService(t, other, source, current, "instance-b")
				_, otherErr = other.RefreshProvider(ctx, app.ProviderRefreshRequest{
					Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
				})
				require.NoError(t, otherErr)
			}
			_, err = service.ConfirmProviderRefresh(ctx, app.ProviderRefreshRequest{
				ConfirmationToken: blocked.Status.ConfirmationToken,
				State:             app.DefaultViewState(), Selection: app.EmptySelection(),
			})
			assertProviderAppCode(t, err, provider.CodeConfirmationInvalid)
		})
	}
}

func TestProviderIntegrityFailureCannotCreateOrUseConfirmation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 15, 22, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: providerSnapshot(t, now, 30), fingerprint: "session-a",
		fetchErr: provider.NewError(provider.CodeSnapshotUnstable),
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")
	result, err := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeSnapshotUnstable)
	assert.Empty(t, result.Status.ConfirmationToken)
	_, err = service.ConfirmProviderRefresh(ctx, app.ProviderRefreshRequest{
		ConfirmationToken: "unissued-confirmation",
		State:             app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	assertProviderAppCode(t, err, provider.CodeConfirmationInvalid)
}
