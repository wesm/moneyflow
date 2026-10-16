package onboarding

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
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

func TestCoordinatorCloseCancelsRunningAttemptsAndReleasesProfiles(t *testing.T) {
	t.Parallel()
	coordinator, started, _ := newTestCoordinator(t)
	var closes atomic.Int32
	coordinator.mu.Lock()
	current := coordinator.attempts[started.AttemptID]
	current.flow.opened = &OpenedProfile{Close: func() error {
		closes.Add(1)
		return nil
	}}
	coordinator.startJobLocked(current, func(ctx context.Context, _ string) { <-ctx.Done() })
	coordinator.mu.Unlock()

	require.NoError(t, coordinator.Close(context.Background()))
	require.NoError(t, coordinator.Close(context.Background()))
	assert.Equal(t, int32(1), closes.Load())
	_, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}

// coordinatorHoldingAProfile returns a coordinator whose one attempt holds an opened profile, whose
// close runs closeProfile. When job is not nil, the attempt runs it.
func coordinatorHoldingAProfile(
	t *testing.T,
	logger *slog.Logger,
	closeProfile func() error,
	job func(context.Context, string),
) (*Coordinator, *attempt) {
	t.Helper()
	coordinator, started, _ := newTestCoordinator(t)
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	coordinator.logger = logger
	current := coordinator.attempts[started.AttemptID]
	current.flow.opened = &OpenedProfile{Close: closeProfile}
	if job != nil {
		coordinator.startJobLocked(current, job)
	}
	return coordinator, current
}

func TestCloseReleasesTheProfileOfAJobThatOutlivesItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var released atomic.Bool
		finishJob := make(chan struct{})
		// The job ignores cancellation, like provider work that outlives Close's 30 seconds.
		coordinator, _ := coordinatorHoldingAProfile(t, nil,
			func() error { released.Store(true); return nil },
			func(context.Context, string) { <-finishJob })
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()

		// Until the job ends, a failed check must not stop the test, or the bubble deadlocks.
		assert.ErrorIs(t, coordinator.Close(ctx), context.DeadlineExceeded)
		synctest.Wait()
		assert.False(t, released.Load(), "the attempt keeps its profile while its job runs")
		close(finishJob)
		synctest.Wait()
		require.True(t, released.Load(), "the attempt released its profile when its job ended")
	})
}

func TestCloseWhoseContextEndedStillCancelsAndReleasesAnIdleAttempt(t *testing.T) {
	var closes atomic.Int32
	coordinator, current := coordinatorHoldingAProfile(t, nil,
		func() error { closes.Add(1); return nil }, nil)
	ended, cancel := context.WithCancel(t.Context())
	cancel()

	require.NoError(t, coordinator.Close(ended))

	assert.Equal(t, int32(1), closes.Load(), "Close released the profile before it returned")
	assert.ErrorIs(t, current.context.Err(), context.Canceled, "Close canceled the attempt")
	_, err := coordinator.Status(t.Context(), StatusRequest{
		ProfileID: testProfileID, AttemptID: current.id,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
}

func TestReleasePendingReleasesAnEndedJobEvenWhenItsContextEnded(t *testing.T) {
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	close(done)
	failure := errors.New("synthetic close failure")
	// A wait that chose at random between the ended job and the ended context would hand about half
	// of these releases to a goroutine, and their error would not reach the caller.
	for range 64 {
		flow := &attemptFlow{opened: &OpenedProfile{Close: func() error { return failure }}}
		err := (&Coordinator{}).releasePending(ended, []pendingRelease{{done: done, flow: flow}})
		require.ErrorIs(t, err, failure)
	}
}

func TestAStuckJobHoldsOnlyItsOwnProfile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var closes atomic.Int32
		closeProfile := func() error { closes.Add(1); return nil }
		stuck, finished := make(chan struct{}), make(chan struct{})
		close(finished)
		// The stuck job comes first, so the idle and the ended attempt are released only when the
		// wait goes on past it.
		pending := []pendingRelease{
			{done: stuck, flow: &attemptFlow{opened: &OpenedProfile{Close: closeProfile}}},
			{flow: &attemptFlow{opened: &OpenedProfile{Close: closeProfile}}},
			{done: finished, flow: &attemptFlow{opened: &OpenedProfile{Close: closeProfile}}},
		}
		ended, cancel := context.WithCancel(t.Context())
		cancel()

		// Until the stuck job ends, a failed check must not stop the test, or the bubble deadlocks.
		assert.ErrorIs(t, (&Coordinator{}).releasePending(ended, pending), context.Canceled)
		assert.Equal(t, int32(2), closes.Load(), "the idle and the ended attempt were released")
		synctest.Wait()
		assert.Equal(t, int32(2), closes.Load(), "the stuck attempt keeps its profile")
		close(stuck)
		synctest.Wait()
		require.Equal(t, int32(3), closes.Load(), "the stuck attempt was released after its job")
	})
}

func TestALateReleaseFailureIsLoggedWithoutItsText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logged strings.Builder
		finishJob := make(chan struct{})
		coordinator, _ := coordinatorHoldingAProfile(t, slog.New(slog.NewTextHandler(&logged, nil)),
			func() error { return errors.New("synthetic close failure") },
			func(context.Context, string) { <-finishJob })
		ended, cancel := context.WithCancel(t.Context())
		cancel()
		assert.ErrorIs(t, coordinator.Close(ended), context.Canceled)

		close(finishJob)
		synctest.Wait()

		assert.Equal(t, 1, strings.Count(logged.String(), "\n"), logged.String())
		assert.Contains(t, logged.String(), "level=ERROR")
		assert.Contains(t, logged.String(), "moneyflow_event=onboarding_release_failed")
		assert.Contains(t, logged.String(), "moneyflow_cause=internal_error")
		assert.NotContains(t, logged.String(), "synthetic close failure")
	})
}

func TestStartAfterCloseRefusesWithoutStartingAJob(t *testing.T) {
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	for name, closeContext := range map[string]context.Context{
		"after Close":                       context.Background(),
		"after a Close whose context ended": ended,
	} {
		t.Run(name, func(t *testing.T) {
			coordinator, err := NewCoordinator(Config{
				InstanceID: "test-instance",
				OpenProfile: func(context.Context, string) (OpenedProfile, error) {
					return OpenedProfile{}, errors.New("synthetic open failure")
				},
				Runtime: panickingRuntime,
			})
			require.NoError(t, err)
			require.NoError(t, coordinator.Close(closeContext))

			_, err = coordinator.Start(context.Background(), StartRequest{ProfileID: testProfileID})

			require.ErrorIs(t, err, ErrClosed)
			coordinator.mu.Lock()
			defer coordinator.mu.Unlock()
			assert.Empty(t, coordinator.attempts, "a refused start keeps no attempt and runs no job")
		})
	}
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
		MonarchCredentials: &CredentialInput{
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

func TestCancelProfileCancelsEveryAttemptForOneProfileAndReleasesLeases(t *testing.T) {
	t.Parallel()
	coordinator, first, _ := newTestCoordinator(t)
	coordinator.random = strings.NewReader(strings.Repeat("b", 64))
	second, err := coordinator.Start(context.Background(), StartRequest{ProfileID: testProfileID})
	require.NoError(t, err)
	otherProfileID := "profile_ijbeeqscijbeeqscijbeeqscia"
	coordinator.random = strings.NewReader(strings.Repeat("c", 64))
	other, err := coordinator.Start(context.Background(), StartRequest{ProfileID: otherProfileID})
	require.NoError(t, err)

	var closes atomic.Int32
	coordinator.mu.Lock()
	for _, id := range []string{first.AttemptID, second.AttemptID} {
		current := coordinator.attempts[id]
		current.flow.opened = &OpenedProfile{Close: func() error {
			closes.Add(1)
			return nil
		}}
	}
	running := coordinator.attempts[first.AttemptID]
	coordinator.startJobLocked(running, func(ctx context.Context, _ string) { <-ctx.Done() })
	coordinator.mu.Unlock()

	require.NoError(t, coordinator.CancelProfile(context.Background(), testProfileID))
	require.NoError(t, coordinator.CancelProfile(context.Background(), testProfileID))
	assert.Equal(t, int32(2), closes.Load())
	for _, id := range []string{first.AttemptID, second.AttemptID} {
		snapshot, statusErr := coordinator.Status(context.Background(), StatusRequest{
			ProfileID: testProfileID, AttemptID: id,
		})
		require.NoError(t, statusErr)
		assert.Equal(t, StateCanceled, snapshot.State)
	}
	untouched, err := coordinator.Status(context.Background(), StatusRequest{
		ProfileID: otherProfileID, AttemptID: other.AttemptID,
	})
	require.NoError(t, err)
	assert.Equal(t, StateInspect, untouched.State)
	assert.Equal(t, CodeOnboardingExpired, CodeOf(coordinator.CancelProfile(context.Background(), "nope")))
}
