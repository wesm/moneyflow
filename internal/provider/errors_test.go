package provider_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
)

func TestProviderErrorContainsOnlyCodeAndFixedDetail(t *testing.T) {
	t.Parallel()

	failure := provider.NewError(provider.CodeReconnectRequired)
	assert.Equal(t, "provider_reconnect_required: reconnect through the CLI", failure.Error())
	assert.Nil(t, errors.Unwrap(failure))

	code, ok := provider.CodeOf(failure)
	require.True(t, ok)
	assert.Equal(t, provider.CodeReconnectRequired, code)
}

func TestProviderErrorCodesAreCompleteAndUnique(t *testing.T) {
	t.Parallel()

	want := []provider.ErrorCode{
		provider.CodeReconnectRequired,
		provider.CodeIdentityMismatch,
		provider.CodeSnapshotUnstable,
		provider.CodeRefreshInProgress,
		provider.CodeDeletionConfirmationRequired,
		provider.CodeConfirmationInvalid,
		provider.CodeRefreshStale,
		provider.CodeRateLimited,
		provider.CodeUnavailable,
		provider.CodeDataInvalid,
	}
	assert.Equal(t, want, provider.ErrorCodes())
	seen := make(map[provider.ErrorCode]struct{}, len(want))
	for _, code := range want {
		_, duplicate := seen[code]
		assert.False(t, duplicate, code)
		seen[code] = struct{}{}
		assert.NotEmpty(t, provider.NewError(code).Error())
	}
}

func TestProviderCodeOfRejectsUnrelatedErrors(t *testing.T) {
	t.Parallel()

	_, ok := provider.CodeOf(errors.New("unrelated"))
	assert.False(t, ok)
}

func TestProviderErrorReplacesUnknownCodes(t *testing.T) {
	t.Parallel()

	failure := provider.NewError(provider.ErrorCode("private remote response"))
	code, ok := provider.CodeOf(failure)
	require.True(t, ok)
	assert.Equal(t, provider.CodeDataInvalid, code)
	assert.NotContains(t, failure.Error(), "private remote response")
}

func TestProviderRetryAfterIsBoundedAndSurvivesWrapping(t *testing.T) {
	t.Parallel()

	failure := provider.NewErrorWithRetry(provider.CodeRateLimited, 48*time.Hour)
	retryAfter, ok := provider.RetryAfterOf(fmt.Errorf("refresh failed: %w", failure))
	require.True(t, ok)
	assert.Equal(t, provider.MaxRetryAfter, retryAfter)

	_, ok = provider.RetryAfterOf(provider.NewErrorWithRetry(provider.CodeUnavailable, time.Hour))
	assert.False(t, ok, "only rate-limit failures carry retry timing")
}
