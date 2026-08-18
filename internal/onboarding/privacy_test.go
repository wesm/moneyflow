package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestCredentialFailuresExposeOnlySanitizedState(t *testing.T) {
	forbidden := []string{
		"private-user@example.invalid",
		"provider-password-private",
		"account-password-private",
		"Sensitive Profile Label",
		"/private/example/profile-root",
	}
	rawFailure := errors.New(strings.Join(forbidden, " | "))
	coordinator, started, connector, _ := newAuthenticationCoordinator(
		t, flowProfilePristine, &fakeCredentialVault{},
	)
	connector.connectErr = rawFailure
	connector.connectSession = nil
	started = waitForState(t, coordinator, started, StateCredentialsRequired)
	request := credentialSubmitRequest(started)
	request.Credentials.Email = []byte(forbidden[0])
	request.Credentials.Password = []byte(forbidden[1])
	request.Credentials.AccountPassword = []byte(forbidden[2])
	request.Credentials.Confirmation = []byte(forbidden[2])

	next, err := coordinator.Submit(context.Background(), request)
	require.NoError(t, err)
	failed := waitForState(t, coordinator, next, StateCredentialsRequired)
	encoded, err := json.Marshal(failed)
	require.NoError(t, err)
	visible := string(encoded)
	for _, value := range forbidden {
		assert.NotContains(t, visible, value)
	}
	assert.Equal(t, genericFailureCode, failed.Failure.Code)
	assert.Equal(t, "Authentication with Monarch failed.", failed.Failure.Message)
	assert.Equal(t, make([]byte, len(request.Credentials.Email)), request.Credentials.Email)
	assert.Equal(t, make([]byte, len(request.Credentials.Password)), request.Credentials.Password)
	assert.Equal(
		t,
		make([]byte, len(request.Credentials.AccountPassword)),
		request.Credentials.AccountPassword,
	)
}

func TestProviderFailureCauseDoesNotEnterSnapshot(t *testing.T) {
	forbidden := "credential-private-value /private/example/profile-root"
	coordinator, started := newFlowCoordinator(
		t,
		flowProfilePristine,
		&fakeSessionStore{loadErr: errors.New(forbidden)},
		&fakeCredentialVault{},
		&fakeConnector{identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "remote"}},
		&SettingsInput{Currency: "USD", Scale: 2},
	)
	failed := waitForStableState(t, coordinator, started)
	encoded, err := json.Marshal(failed)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), forbidden)
	assert.NotContains(t, failed.Failure.Message, forbidden)
}
