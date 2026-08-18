package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

func TestAuthenticateGeneratesTOTPAndAnswersOneMFAChallenge(t *testing.T) {
	coordinator, started, connector, _ := newAuthenticationCoordinator(
		t, flowProfilePristine, &fakeCredentialVault{},
	)
	connector.challenge = &provider.Challenge{Kind: "mfa"}
	started = waitForState(t, coordinator, started, StateCredentialsRequired)
	request := credentialSubmitRequest(started)

	snapshot, err := coordinator.Submit(context.Background(), request)
	require.NoError(t, err)
	final := waitForState(t, coordinator, snapshot, StateImporting)
	assert.Equal(t, "287082", connector.credentials.OneTimeCode)
	assert.Equal(t, "287082", connector.challengeReply)
	assert.NotContains(t, mustJSON(t, final), "287082")
}

func TestAuthenticateSavesNewCredentialVaultBeforeSession(t *testing.T) {
	order := []string{}
	vault := &fakeCredentialVault{order: &order}
	coordinator, started, _, sessions := newAuthenticationCoordinator(t, flowProfilePristine, vault)
	sessions.order = &order
	started = waitForState(t, coordinator, started, StateCredentialsRequired)

	next, err := coordinator.Submit(context.Background(), credentialSubmitRequest(started))
	require.NoError(t, err)
	assert.Equal(t, StateImporting, waitForState(t, coordinator, next, StateImporting).State)
	assert.Equal(t, []string{"vault", "session"}, order)
}

func TestAuthenticationFailureReturnsToCredentialEntryWithSanitizedFailure(t *testing.T) {
	coordinator, started, connector, _ := newAuthenticationCoordinator(
		t, flowProfilePristine, &fakeCredentialVault{},
	)
	connector.connectErr = provider.NewError(provider.CodeReconnectRequired)
	connector.connectSession = nil
	started = waitForState(t, coordinator, started, StateCredentialsRequired)

	next, err := coordinator.Submit(context.Background(), credentialSubmitRequest(started))
	require.NoError(t, err)
	final := waitForState(t, coordinator, next, StateCredentialsRequired)
	require.NotNil(t, final.Failure)
	assert.Equal(t, string(provider.CodeReconnectRequired), final.Failure.Code)
	assert.NotContains(t, mustJSON(t, final), "provider-password")
}

func TestBindingMismatchPreventsSessionAndVaultSave(t *testing.T) {
	vault := &fakeCredentialVault{
		exists: true,
		credentials: monarch.StoredCredentials{ //nolint:gosec // synthetic test credential.
			Email: "user@example.invalid", Password: "provider-password",
			TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		},
	}
	coordinator, started, connector, sessions := newAuthenticationCoordinator(t, flowProfileBound, vault)
	connector.connectSession = validTestSession("subscription-other", "USD", 2)
	started = waitForState(t, coordinator, started, StateUnlockRequired)

	next, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionUnlock,
		Unlock: &UnlockInput{AccountPassword: []byte("account-password")},
	})
	require.NoError(t, err)
	final := waitForState(t, coordinator, next, StateIdentityMismatch)
	assert.Equal(t, string(provider.CodeIdentityMismatch), final.Failure.Code)
	assert.Zero(t, sessions.saveCalls)
	assert.Zero(t, vault.saveCalls)
}

func newAuthenticationCoordinator(
	t *testing.T,
	kind flowProfileKind,
	vault *fakeCredentialVault,
) (*Coordinator, Snapshot, *fakeConnector, *fakeSessionStore) {
	t.Helper()
	opened := newFlowOpenedProfile(t, kind)
	remoteID := "subscription-example"
	settings := monarch.ImportConfig{Currency: "USD", Scale: 2}
	sessions := &fakeSessionStore{
		session: validTestSession(remoteID, settings.Currency, settings.Scale),
	}
	if kind == flowProfilePristine && !vault.exists {
		sessions.loadErr = os.ErrNotExist
	} else {
		// Force reauthentication while retaining settings from the saved session.
		sessions.loadErr = nil
	}
	connector := &fakeConnector{
		validateErr:    provider.NewError(provider.CodeReconnectRequired),
		connectSession: validTestSession(remoteID, settings.Currency, settings.Scale),
	}
	coordinator, err := NewCoordinator(Config{
		Random:      bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now:         func() time.Time { return time.Unix(59, 0).UTC() },
		InstanceID:  "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) { return opened, nil },
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				Sessions: sessions, Credentials: vault,
				NewConnector: func(monarch.ImportConfig) (provider.Connector, error) {
					return connector, nil
				},
				NewSource:  func(monarch.ImportConfig) (provider.Source, error) { return pendingProviderSource{}, nil },
				InstanceID: "provider-instance",
				Now:        func() time.Time { return time.Unix(59, 0).UTC() },
			}, nil
		},
	})
	require.NoError(t, err)
	startRequest := StartRequest{ProfileID: testProfileID}
	if sessions.loadErr != nil {
		startRequest.Settings = &SettingsInput{Currency: "USD", Scale: 2}
	}
	started, err := coordinator.Start(context.Background(), startRequest)
	require.NoError(t, err)
	t.Cleanup(func() {
		latest, statusErr := coordinator.Status(context.Background(), StatusRequest{
			ProfileID: testProfileID, AttemptID: started.AttemptID,
		})
		if statusErr == nil {
			_, _ = coordinator.Cancel(context.Background(), CancelRequest{
				ProfileID: testProfileID, AttemptID: started.AttemptID,
				ExpectedStateVersion: latest.StateVersion,
			})
		}
	})
	return coordinator, started, connector, sessions
}

func waitForState(
	t *testing.T,
	coordinator *Coordinator,
	snapshot Snapshot,
	want State,
) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := coordinator.Status(context.Background(), StatusRequest{
			ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		})
		require.NoError(t, err)
		if current.State == want {
			return current
		}
		if current.State == StateFailed || current.State == StateIdentityMismatch {
			t.Fatalf("onboarding reached %s instead of %s: %+v", current.State, want, current.Failure)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("onboarding attempt did not reach %s", want)
	return Snapshot{}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func TestIdentityMismatchReauthenticateGoesDirectlyToCredentialEntry(t *testing.T) {
	vault := &fakeCredentialVault{exists: true, credentials: monarch.StoredCredentials{ //nolint:gosec // synthetic test credentials.
		Email: "user@example.invalid", Password: "provider-password",
		TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
	}}
	coordinator, started, connector, _ := newAuthenticationCoordinator(t, flowProfileBound, vault)
	connector.connectSession = validTestSession("subscription-other", "USD", 2)
	started = waitForState(t, coordinator, started, StateUnlockRequired)
	next, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionUnlock,
		Unlock: &UnlockInput{AccountPassword: []byte("account-password")},
	})
	require.NoError(t, err)
	mismatch := waitForState(t, coordinator, next, StateIdentityMismatch)

	next, err = coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: mismatch.AttemptID,
		ExpectedStateVersion: mismatch.StateVersion, Action: ActionReauthenticate,
	})
	require.NoError(t, err)
	assert.Equal(t, StateCredentialsRequired, next.State)
}

func TestCredentialPersistenceFailuresReturnToReentry(t *testing.T) {
	for _, test := range []struct {
		name       string
		sessionErr error
		vaultErr   error
	}{
		{name: "session", sessionErr: errors.New("session write failed")},
		{name: "vault", vaultErr: errors.New("vault write failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			vault := &fakeCredentialVault{saveErr: test.vaultErr}
			coordinator, started, _, sessions := newAuthenticationCoordinator(
				t, flowProfilePristine, vault,
			)
			sessions.saveErr = test.sessionErr
			started = waitForState(t, coordinator, started, StateCredentialsRequired)
			next, err := coordinator.Submit(context.Background(), credentialSubmitRequest(started))
			require.NoError(t, err)
			failed := waitForState(t, coordinator, next, StateCredentialsRequired)
			require.NotNil(t, failed.Failure)
			assert.True(t, failed.Failure.CanReenter)
			assert.False(t, failed.Failure.CanRetry)
			if test.vaultErr != nil {
				assert.Zero(t, sessions.saveCalls)
			}
		})
	}
}
