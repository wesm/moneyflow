package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/onboarding"
)

func TestOnboardingStatusIsCredentialBlind(t *testing.T) {
	t.Parallel()
	coordinator := &apiOnboardingFake{}
	server := newOnboardingAPIServer(t, coordinator)
	startPath, err := ProfileAPIPath("/", testProfileID, "onboarding/start")
	require.NoError(t, err)
	started := requestScopedMutation(t, server, testProfileID, startPath, OnboardingStartBody{
		ProtocolVersion: onboarding.ProtocolVersion,
	})
	require.Equal(t, http.StatusOK, started.Code, started.Body.String())
	var snapshot OnboardingStatusResponse
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &snapshot))

	secrets := []string{
		"user@example.com", "synthetic-provider-password", "JBSWY3DPEHPK3PXP",
		"synthetic-account-password",
	}
	submitPath, err := ProfileAPIPath(
		"/", testProfileID, "onboarding/"+snapshot.AttemptID+"/submit",
	)
	require.NoError(t, err)
	submitted := requestScopedMutation(t, server, testProfileID, submitPath, OnboardingSubmitBody{
		ProtocolVersion: onboarding.ProtocolVersion, ExpectedStateVersion: snapshot.StateVersion,
		Action: onboarding.ActionSubmitCredentials,
		Credentials: &OnboardingCredentialsInput{
			Email: secrets[0], Password: secrets[1], TOTPSecret: secrets[2],
			AccountPassword: secrets[3], Confirmation: secrets[3],
		},
	})
	require.Equal(t, http.StatusOK, submitted.Code, submitted.Body.String())

	statusPath, err := ProfileAPIPath(
		"/", testProfileID, "onboarding/"+snapshot.AttemptID+"/status",
	)
	require.NoError(t, err)
	status := requestServer(t, server, http.MethodGet, statusPath, nil)
	require.Equal(t, http.StatusOK, status.Code, status.Body.String())
	for _, secret := range secrets {
		assert.NotContains(t, status.Body.String(), secret)
		assert.NotContains(t, submitted.Body.String(), secret)
	}
}

func TestOnboardingMutationRejectsAnotherProfileToken(t *testing.T) {
	t.Parallel()
	server := newOnboardingAPIServer(t, &apiOnboardingFake{})
	path, err := ProfileAPIPath("/", testProfileID, "onboarding/start")
	require.NoError(t, err)
	response := requestScopedMutation(t, server, otherProfileID, path, OnboardingStartBody{
		ProtocolVersion: onboarding.ProtocolVersion,
	})
	assert.Equal(t, http.StatusForbidden, response.Code)
}

func TestOnboardingMutationRejectsEncodedProfileNamespaceBeforeDispatch(t *testing.T) {
	t.Parallel()
	coordinator := &apiOnboardingFake{}
	server := newOnboardingAPIServer(t, coordinator)
	response := requestServer(
		t,
		server,
		http.MethodPost,
		"/api/v1/%70rofiles/"+testProfileID+"/onboarding/start",
		strings.NewReader(`{"protocol_version":1,"month_to_date":false}`),
	)
	assert.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	assert.Zero(t, coordinator.starts.Load())
}

func TestOnboardingStatusMapsStaleAndExpiredAttemptsWithoutRawErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		code   onboarding.Code
		status int
	}{
		{name: "stale", code: onboarding.CodeOnboardingStale, status: http.StatusConflict},
		{name: "expired", code: onboarding.CodeOnboardingExpired, status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newOnboardingAPIServer(t, &apiOnboardingFake{
				statusErr: errors.Join(onboarding.ErrorForCode(test.code), errors.New("raw-secret")),
			})
			path, err := ProfileAPIPath("/", testProfileID, "onboarding/attempt_example/status")
			require.NoError(t, err)
			response := requestServer(t, server, http.MethodGet, path, nil)
			assert.Equal(t, test.status, response.Code)
			assert.Contains(t, response.Body.String(), string(test.code))
			assert.NotContains(t, response.Body.String(), "raw-secret")
		})
	}
}

func TestCompletedOnboardingProfileLeaseIsTakenAndReleasedOnce(t *testing.T) {
	t.Parallel()
	coordinator := &apiOnboardingFake{snapshot: onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion, AttemptID: "attempt_example",
		ProfileID: testProfileID, StateVersion: 7,
		State: onboarding.StateComplete, ProviderKind: "monarch",
	}}
	server := newOnboardingAPIServer(t, coordinator)
	path, err := ProfileAPIPath("/", testProfileID, "onboarding/attempt_example/status")
	require.NoError(t, err)
	for range 2 {
		response := requestServer(t, server, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	}
	assert.Equal(t, int32(1), coordinator.takes.Load())
	assert.Equal(t, int32(1), coordinator.closes.Load())
}

func TestCompletedOnboardingWaitsForOneReleaseAndPreservesReleaseFailure(t *testing.T) {
	t.Parallel()
	closeStarted := make(chan struct{})
	continueClose := make(chan struct{})
	coordinator := &apiOnboardingFake{
		snapshot: onboarding.Snapshot{
			ProtocolVersion: onboarding.ProtocolVersion, AttemptID: "attempt_example",
			ProfileID: testProfileID, StateVersion: 7,
			State: onboarding.StateComplete, ProviderKind: "monarch",
		},
		closeStarted:  closeStarted,
		continueClose: continueClose,
		closeErr:      errors.New("synthetic release failure"),
	}
	server := newOnboardingAPIServer(t, coordinator)
	path, err := ProfileAPIPath("/", testProfileID, "onboarding/attempt_example/status")
	require.NoError(t, err)
	responses := make(chan *httptest.ResponseRecorder, 2)
	go func() { responses <- requestServer(t, server, http.MethodGet, path, nil) }()
	select {
	case <-closeStarted:
	case <-time.After(time.Second):
		t.Fatal("completed onboarding did not start releasing its lease")
	}
	go func() { responses <- requestServer(t, server, http.MethodGet, path, nil) }()
	select {
	case <-responses:
		t.Fatal("concurrent completion returned before the profile lease was released")
	case <-time.After(25 * time.Millisecond):
	}
	close(continueClose)
	for range 2 {
		response := <-responses
		assert.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	}
	assert.Equal(t, int32(1), coordinator.takes.Load())
	assert.Equal(t, int32(1), coordinator.closes.Load())
}

func newOnboardingAPIServer(t testing.TB, coordinator OnboardingCoordinator) *Server {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), Onboarding: coordinator,
		BasePath: "/", Version: "test",
	})
	require.NoError(t, err)
	return server
}

type apiOnboardingFake struct {
	snapshot      onboarding.Snapshot
	statusErr     error
	closeErr      error
	closeStarted  chan struct{}
	continueClose chan struct{}
	starts        atomic.Int32
	takes         atomic.Int32
	closes        atomic.Int32
}

func (coordinator *apiOnboardingFake) Start(
	_ context.Context,
	request onboarding.StartRequest,
) (onboarding.Snapshot, error) {
	coordinator.starts.Add(1)
	coordinator.snapshot = onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion, AttemptID: "attempt_example",
		ProfileID: request.ProfileID, StateVersion: 1,
		State: onboarding.StateCredentialsRequired, ProviderKind: "monarch",
	}
	return coordinator.snapshot, nil
}

func (coordinator *apiOnboardingFake) Status(
	context.Context,
	onboarding.StatusRequest,
) (onboarding.Snapshot, error) {
	if coordinator.statusErr != nil {
		return onboarding.Snapshot{}, coordinator.statusErr
	}
	return coordinator.snapshot, nil
}

func (coordinator *apiOnboardingFake) Submit(
	_ context.Context,
	request onboarding.SubmitRequest,
) (onboarding.Snapshot, error) {
	coordinator.snapshot.StateVersion = request.ExpectedStateVersion + 1
	coordinator.snapshot.State = onboarding.StateAuthenticating
	return coordinator.snapshot, nil
}

func (coordinator *apiOnboardingFake) Cancel(
	_ context.Context,
	request onboarding.CancelRequest,
) (onboarding.Snapshot, error) {
	coordinator.snapshot.StateVersion = request.ExpectedStateVersion + 1
	coordinator.snapshot.State = onboarding.StateCanceled
	return coordinator.snapshot, nil
}

func (coordinator *apiOnboardingFake) TakeOpenedProfile(
	context.Context,
	onboarding.StatusRequest,
) (onboarding.OpenedProfile, error) {
	coordinator.takes.Add(1)
	return onboarding.OpenedProfile{Close: func() error {
		coordinator.closes.Add(1)
		if coordinator.closeStarted != nil {
			close(coordinator.closeStarted)
		}
		if coordinator.continueClose != nil {
			<-coordinator.continueClose
		}
		return coordinator.closeErr
	}}, nil
}
