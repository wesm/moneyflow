package onboarding

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testProfileID = "profile_ijbeeqscijbeeqscijbeeqscie"

type testClock struct {
	now time.Time
}

func (clock *testClock) Now() time.Time { return clock.now }

func (clock *testClock) Advance(elapsed time.Duration) { clock.now = clock.now.Add(elapsed) }

func TestAttemptRejectsStaleAndDuplicateTransition(t *testing.T) {
	coordinator, started := coordinatorAtRetryableFailure(t)

	next, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionRetry,
	})
	require.NoError(t, err)
	_, err = coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionRetry,
	})
	assert.Equal(t, CodeOnboardingStale, CodeOf(err))
	assert.Greater(t, next.StateVersion, started.StateVersion)
}

func TestRunningJobAndStatusPollKeepAttemptActive(t *testing.T) {
	coordinator, started, clock, finishJob := coordinatorWithRunningJob(t)
	clock.Advance(31 * time.Minute)
	_, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	require.NoError(t, err)
	finishJob()

	clock.Advance(29 * time.Minute)
	_, err = coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	require.NoError(t, err)
	clock.Advance(31 * time.Minute)
	_, err = coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}

func TestAttemptExpiresAfterThirtyIdleMinutes(t *testing.T) {
	coordinator, started, clock := newTestCoordinator(t)
	clock.Advance(30*time.Minute + time.Nanosecond)

	_, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}

func TestAttemptRejectsProfileMismatch(t *testing.T) {
	coordinator, started, _ := newTestCoordinator(t)

	_, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: "profile_other", AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}

func TestAttemptIsInvalidAfterCoordinatorRestart(t *testing.T) {
	_, started, clock := newTestCoordinator(t)
	restarted, err := NewCoordinator(Config{
		Random: strings.NewReader(strings.Repeat("r", 64)), Now: clock.Now,
		InstanceID: "second-instance",
	})
	require.NoError(t, err)

	_, err = restarted.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}

func TestAttemptCancelIsIdempotent(t *testing.T) {
	coordinator, started, _ := newTestCoordinator(t)
	request := CancelRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
	}

	canceled, err := coordinator.Cancel(context.Background(), request)
	require.NoError(t, err)
	again, err := coordinator.Cancel(context.Background(), request)
	require.NoError(t, err)
	assert.Equal(t, StateCanceled, canceled.State)
	assert.Equal(t, canceled, again)
}

func TestSubmitClearsCallerOwnedSecretBuffers(t *testing.T) {
	coordinator, started, _ := newTestCoordinator(t)
	accountPassword := []byte("account-secret")
	email := []byte("user@example.invalid")
	password := []byte("provider-secret")
	totp := []byte("JBSWY3DPEHPK3PXP")
	confirmation := []byte("account-secret")

	_, _ = coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion, Action: ActionUnlock,
		Unlock: &UnlockInput{AccountPassword: accountPassword},
		Credentials: &CredentialInput{
			Email: email, Password: password, TOTPSecret: totp,
			AccountPassword: accountPassword, Confirmation: confirmation,
		},
	})

	for _, secret := range [][]byte{accountPassword, email, password, totp, confirmation} {
		assert.Equal(t, make([]byte, len(secret)), secret)
	}
}

func TestSnapshotHasNoSecretBearingFields(t *testing.T) {
	_, started, _ := newTestCoordinator(t)
	encoded, err := json.Marshal(started)
	require.NoError(t, err)
	for _, forbidden := range []string{"password", "email", "totp", "credential", "secret"} {
		assert.NotContains(t, strings.ToLower(string(encoded)), forbidden)
	}

	typeOfSnapshot := reflect.TypeFor[Snapshot]()
	for index := range typeOfSnapshot.NumField() {
		name := strings.ToLower(typeOfSnapshot.Field(index).Name)
		for _, forbidden := range []string{"password", "email", "totp", "credential", "secret"} {
			assert.NotContains(t, name, forbidden)
		}
	}
}

func newTestCoordinator(t *testing.T) (*Coordinator, Snapshot, *testClock) {
	t.Helper()
	clock := &testClock{now: time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)}
	coordinator, err := NewCoordinator(Config{
		Random: strings.NewReader(strings.Repeat("a", 128)), Now: clock.Now,
		InstanceID: "test-instance",
	})
	require.NoError(t, err)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: testProfileID})
	require.NoError(t, err)
	return coordinator, started, clock
}

func coordinatorAtRetryableFailure(t *testing.T) (*Coordinator, Snapshot) {
	t.Helper()
	coordinator, started, _ := newTestCoordinator(t)
	coordinator.mu.Lock()
	attempt := coordinator.attempts[started.AttemptID]
	attempt.state = StateFailed
	attempt.failure = &Failure{Code: "provider_unavailable", Message: "Try again.", CanRetry: true}
	attempt.stateVersion++
	started = attempt.snapshot()
	coordinator.mu.Unlock()
	return coordinator, started
}

func coordinatorWithRunningJob(
	t *testing.T,
) (*Coordinator, Snapshot, *testClock, func()) {
	t.Helper()
	coordinator, started, clock := newTestCoordinator(t)
	coordinator.mu.Lock()
	attempt := coordinator.attempts[started.AttemptID]
	attempt.running = true
	coordinator.mu.Unlock()
	finish := func() {
		coordinator.mu.Lock()
		attempt.running = false
		coordinator.mu.Unlock()
	}
	return coordinator, started, clock, finish
}

func TestStartingAttemptProactivelyReapsOtherExpiredAttempts(t *testing.T) {
	coordinator, first, clock := newTestCoordinator(t)
	clock.Advance(31 * time.Minute)
	coordinator.random = strings.NewReader(strings.Repeat("b", 64))
	_, err := coordinator.Start(context.Background(), StartRequest{ProfileID: testProfileID})
	require.NoError(t, err)

	_, err = coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: first.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}
