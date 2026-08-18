package onboarding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider/monarch"
)

func TestUnlockClearsSubmittedPasswordAndNeverCopiesItToSnapshot(t *testing.T) {
	vault := &fakeCredentialVault{
		exists: true,
		credentials: monarch.StoredCredentials{ //nolint:gosec // synthetic test credential.
			Email: "user@example.invalid", Password: "provider-password",
			TOTPSecret: "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		},
	}
	coordinator, started, connector, _ := newAuthenticationCoordinator(t, flowProfilePristine, vault)
	started = waitForState(t, coordinator, started, StateUnlockRequired)
	accountPassword := []byte("account-password")

	snapshot, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionUnlock,
		Unlock: &UnlockInput{AccountPassword: accountPassword},
	})
	require.NoError(t, err)
	final := waitForState(t, coordinator, snapshot, StateImporting)
	assert.Equal(t, make([]byte, len(accountPassword)), accountPassword)
	encoded, err := json.Marshal(final)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "account-password")
	assert.Equal(t, 1, vault.loadCalls)
	assert.Equal(t, 1, connector.connectCalls)
}

func TestWrongVaultPasswordReturnsToUnlockWithoutStartingAuthentication(t *testing.T) {
	vault := &fakeCredentialVault{exists: true, loadErr: monarch.ErrCredentialUnlock}
	coordinator, started, connector, _ := newAuthenticationCoordinator(t, flowProfilePristine, vault)
	started = waitForState(t, coordinator, started, StateUnlockRequired)

	next, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionUnlock,
		Unlock: &UnlockInput{AccountPassword: []byte("wrong-password")},
	})
	require.NoError(t, err)
	final := waitForState(t, coordinator, next, StateUnlockRequired)
	require.NotNil(t, final.Failure)
	assert.Equal(t, string(CodeCredentialUnlockFailed), final.Failure.Code)
	assert.Zero(t, connector.connectCalls)
}

func TestCredentialInputRejectsMismatchedAccountPasswordsAndClearsSecrets(t *testing.T) {
	coordinator, started, connector, _ := newAuthenticationCoordinator(
		t, flowProfilePristine, &fakeCredentialVault{},
	)
	started = waitForState(t, coordinator, started, StateCredentialsRequired)
	request := credentialSubmitRequest(started)
	request.Credentials.Confirmation = []byte("different-password")

	_, err := coordinator.Submit(context.Background(), request)
	assert.Equal(t, CodeCredentialInputInvalid, CodeOf(err))
	for _, secret := range credentialBuffers(request.Credentials) {
		assert.Equal(t, make([]byte, len(secret)), secret)
	}
	assert.Zero(t, connector.connectCalls)
	current, statusErr := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	require.NoError(t, statusErr)
	assert.Equal(t, StateCredentialsRequired, current.State)
}

func TestCredentialInputRejectsUnexpectedPayloadForAction(t *testing.T) {
	coordinator, started, _, _ := newAuthenticationCoordinator(
		t, flowProfilePristine, &fakeCredentialVault{},
	)
	started = waitForState(t, coordinator, started, StateCredentialsRequired)

	_, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionSubmitCredentials,
		Credentials: credentialSubmitRequest(started).Credentials,
		Unlock:      &UnlockInput{AccountPassword: []byte("unexpected")},
	})
	assert.Equal(t, CodeCredentialInputInvalid, CodeOf(err))
}

func credentialSubmitRequest(started Snapshot) SubmitRequest {
	return SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionSubmitCredentials,
		Credentials: &CredentialInput{
			Email: []byte("user@example.invalid"), Password: []byte("provider-password"),
			TOTPSecret:      []byte("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"),
			AccountPassword: []byte("account-password"), Confirmation: []byte("account-password"),
		},
	}
}

func credentialBuffers(input *CredentialInput) [][]byte {
	if input == nil {
		return nil
	}
	return [][]byte{
		input.Email, input.Password, input.TOTPSecret, input.AccountPassword, input.Confirmation,
	}
}
