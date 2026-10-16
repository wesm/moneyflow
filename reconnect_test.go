package moneyflow

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// syntheticConnector signs in to the synthetic provider's account with any credentials.
type syntheticConnector struct{ now func() time.Time }

func (connector syntheticConnector) Connect(
	context.Context,
	provider.Credentials,
	provider.ChallengeResponder,
) (provider.Session, error) {
	now := connector.now()
	return monarch.Session{
		Version: 2, Token: "synthetic-token", DeviceUUID: "synthetic-device",
		RemoteProfileID: "synthetic-ui-scenario",
		Import:          monarch.ImportConfig{Currency: "USD", Scale: 2},
		IssuedAt:        now, ValidatedAt: now,
	}, nil
}

func (syntheticConnector) Validate(
	context.Context,
	provider.Session,
) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-ui-scenario"}, nil
}

// connectableMonarch adds sign-in to syntheticMonarch: a connector for the synthetic account
// and the profile's own session store and credential vault.
func connectableMonarch(providerRoot string) assembly.MonarchFactory {
	return func(paths home.Paths, config monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		runtime, err := syntheticMonarch(providerRoot)(paths, config)
		if err != nil {
			return runtime, err
		}
		if runtime.Sessions, err = monarch.NewSessionStore(paths); err != nil {
			return runtime, err
		}
		if runtime.Credentials, err = monarch.NewCredentialVault(paths); err != nil {
			return runtime, err
		}
		runtime.Connector = syntheticConnector{now: runtime.Now}
		return runtime, nil
	}
}

func TestReconnectThroughTheAPIKeepsTheProfile(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: connectableMonarch(providerRoot)},
	})
	before := currentProfile(t.Context(), instance, acme)
	require.Equal(t, 1, before.PendingEdits)
	onboardingPath := "api/v1/profiles/" + acme + "/onboarding/"
	started := serveAPI(t, instance, http.MethodPost, onboardingPath+"start",
		encode(t, api.OnboardingStartBody{ProtocolVersion: onboarding.ProtocolVersion}))
	require.Equal(t, http.StatusOK, started.Code, started.Body.String())
	var status api.OnboardingStatusResponse
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &status))
	attemptPath := onboardingPath + status.AttemptID
	// waitFor polls the attempt's status route until the attempt reaches want. It has no deadline,
	// so a wait that never ends fails at the go test timeout, with a goroutine dump. An attempt
	// that reports a failure waits for the user and never moves on its own, so the test fails at
	// once. That covers failed, identity_mismatch and a rejected sign-in, which returns to
	// credentials_required.
	waitFor := func(want onboarding.State) api.OnboardingStatusResponse {
		for {
			response := serveAPI(t, instance, http.MethodGet, attemptPath+"/status", "")
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var current api.OnboardingStatusResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &current))
			if current.State == want {
				return current
			}
			if current.Failure != nil {
				require.FailNowf(t, "onboarding stopped", "state %s instead of %s: %+v",
					current.State, want, current.Failure)
			}
			time.Sleep(time.Millisecond)
		}
	}
	status = waitFor(onboarding.StateCredentialsRequired)
	secrets := []string{
		"synthetic-provider-password", "JBSWY3DPEHPK3PXP", "synthetic-account-password",
	}

	submitted := serveAPI(t, instance, http.MethodPost, attemptPath+"/submit",
		encode(t, api.OnboardingSubmitBody{
			ProtocolVersion: onboarding.ProtocolVersion, ExpectedStateVersion: status.StateVersion,
			Action: onboarding.ActionSubmitCredentials,
			Credentials: &api.OnboardingCredentialsInput{
				Email: "user@example.com", Password: secrets[0], TOTPSecret: secrets[1],
				AccountPassword: secrets[2], Confirmation: secrets[2],
			},
		}))
	require.Equal(t, http.StatusOK, submitted.Code, submitted.Body.String())
	waitFor(onboarding.StateComplete)

	after := currentProfile(t.Context(), instance, acme)
	assert.Equal(t, acme, after.ID)
	assert.Equal(t, before.PendingEdits, after.PendingEdits)
	assert.Equal(t, StateReady, after.State)
}

// panickingSimpleFIN panics like provider code that a SimpleFIN setup job calls.
func panickingSimpleFIN(home.Paths) (simplefinonboarding.Runtime, error) {
	panic("synthetic factory failure")
}

// stateAfterInspection starts a setup attempt with a POST to base+"start" and returns the state
// in which its job stops inspecting the profile. It polls the attempt's status at
// base+attemptID+statusSuffix with no deadline, so a job that never ends fails at the go test
// timeout, with a goroutine dump.
func stateAfterInspection(
	t *testing.T,
	instance *Instance,
	base, body, statusSuffix string,
) string {
	t.Helper()
	response := serveAPI(t, instance, http.MethodPost, base+"start", body)
	var status struct {
		AttemptID string `json:"attempt_id"`
		State     string `json:"state"`
	}
	for {
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
		if status.State != "inspect" {
			return status.State
		}
		time.Sleep(time.Millisecond)
		response = serveAPI(t, instance, http.MethodGet, base+status.AttemptID+statusSuffix, "")
	}
}

func TestTheHostLogsAPanicInASetupJob(t *testing.T) {
	instance, logged := openLoggedInstance(t, testRoot(t), seams{factories: assembly.Factories{
		Monarch: panickingMonarch, SimpleFIN: panickingSimpleFIN,
	}})
	acme := addProfile(t, instance, "Acme", "monarch")
	beta := addProfile(t, instance, "Beta", "simplefin")

	monarchState := stateAfterInspection(t, instance, "api/v1/profiles/"+acme+"/onboarding/",
		encode(t, api.OnboardingStartBody{ProtocolVersion: onboarding.ProtocolVersion}), "/status")
	simplefinState := stateAfterInspection(t, instance,
		"api/v1/profiles/"+beta+"/simplefin-onboarding/",
		encode(t, api.SimpleFINOnboardingStartBody{
			ProtocolVersion: simplefinonboarding.ProtocolVersion,
		}), "")

	assert.Equal(t, "failed", monarchState)
	assert.Equal(t, "failed", simplefinState)
	assert.Contains(t, logged.String(), mcp.LogEventKey+"=onboarding_job_panicked")
	assert.Contains(t, logged.String(), mcp.LogEventKey+"=simplefin_onboarding_job_panicked")
}
