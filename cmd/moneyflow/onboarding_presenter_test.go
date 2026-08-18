package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/onboarding"
)

func TestCancelCLIOnboardingRetriesAStaleCleanupTransition(t *testing.T) {
	t.Parallel()
	fake := &staleOnceOnboardingCanceler{}
	err := cancelCLIOnboarding(context.Background(), fake, onboarding.StatusRequest{
		ProfileID: "profile_ijbeeqscijbeeqscijbeeqscie", AttemptID: "attempt-example",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, fake.statusCalls)
	assert.Equal(t, 2, fake.cancelCalls)
}

type staleOnceOnboardingCanceler struct {
	statusCalls int
	cancelCalls int
}

func (fake *staleOnceOnboardingCanceler) Status(
	context.Context,
	onboarding.StatusRequest,
) (onboarding.Snapshot, error) {
	fake.statusCalls++
	version := uint64(1)
	if fake.statusCalls > 1 {
		version = 2
	}
	return onboarding.Snapshot{
		ProfileID: "profile_ijbeeqscijbeeqscijbeeqscie", AttemptID: "attempt-example",
		StateVersion: version,
	}, nil
}

func (fake *staleOnceOnboardingCanceler) Cancel(
	context.Context,
	onboarding.CancelRequest,
) (onboarding.Snapshot, error) {
	fake.cancelCalls++
	if fake.cancelCalls == 1 {
		return onboarding.Snapshot{}, onboardingStaleTestError()
	}
	return onboarding.Snapshot{}, nil
}

func onboardingStaleTestError() error {
	coordinator, err := onboarding.NewCoordinator(onboarding.Config{InstanceID: "test"})
	if err != nil {
		return err
	}
	const profileID = "profile_ijbeeqscijbeeqscijbeeqscie"
	started, err := coordinator.Start(context.Background(), onboarding.StartRequest{ProfileID: profileID})
	if err != nil {
		return err
	}
	_, err = coordinator.Submit(context.Background(), onboarding.SubmitRequest{
		ProfileID: profileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: 0, Action: onboarding.ActionRetry,
	})
	return err
}
