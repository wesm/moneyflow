package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

func TestStableErrorCodesAndSafeRendererDetail(t *testing.T) {
	t.Parallel()

	codes := []store.ErrorCode{
		store.CodeRevisionConflict, store.CodeInvalidOperation, store.CodeInvalidTarget,
		store.CodeStoreBusy, store.CodeStoreError, store.CodeSchemaNewer,
		store.CodeSchemaIncompatible, store.CodeStoreCorrupt, store.CodeJournalFull,
	}
	require.Len(t, codes, 9)
	for _, code := range codes {
		failure := store.NewError(code, errors.New(
			`merchant Private Merchant transaction_123 SELECT * FROM x /private/profile.db`,
		))
		assert.Equal(t, code, failure.Code)
		assert.NotEmpty(t, failure.Detail)
		assert.NotContains(t, failure.Error(), "Private Merchant")
		assert.NotContains(t, failure.Error(), "transaction_123")
		assert.NotContains(t, failure.Error(), "SELECT")
		assert.NotContains(t, failure.Error(), "/private/profile.db")
		assert.ErrorContains(t, errors.Unwrap(failure), "Private Merchant")
	}
}

func TestJournalFullErrorUsesAllowlistedDetail(t *testing.T) {
	t.Parallel()

	failure := store.NewError(store.CodeJournalFull, errors.New("private target detail"))
	assert.Equal(t, "journal_full: pending edit limit reached", failure.Error())
	assert.NotContains(t, failure.Error(), "private target detail")
}

func TestRevisionErrorExposesOnlyReliableRevisionNumbers(t *testing.T) {
	t.Parallel()

	failure := store.NewRevisionError(store.CodeRevisionConflict, 4, 5, errors.New("diagnostic"))
	assert.Equal(t, uint64(4), *failure.ObservedRevision)
	assert.Equal(t, uint64(5), *failure.CurrentRevision)
	assert.Equal(t, "revision_conflict: profile revision changed", fmt.Sprint(failure))
}

func TestErrorRejectsUnknownCode(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() { _ = store.NewError("unknown", nil) })
}

func TestInvalidOperationReasonIsAllowlistedAndSurvivesWrapping(t *testing.T) {
	t.Parallel()

	failure := store.NewInvalidOperationError(
		store.InvalidOperationRefreshPlan,
		errors.New("private planner detail"),
	)
	reason, ok := store.InvalidOperationReasonOf(fmt.Errorf("fold failed: %w", failure))
	require.True(t, ok)
	assert.Equal(t, store.InvalidOperationRefreshPlan, reason)
	assert.Equal(
		t,
		"Moneyflow rejected its local refresh plan before writing financial data.",
		store.InvalidOperationDetail(reason),
	)
	assert.NotContains(t, failure.Error(), "private planner detail")

	unknown := store.NewInvalidOperationError(
		store.InvalidOperationReason("private provider value"),
		errors.New("private planner detail"),
	)
	_, ok = store.InvalidOperationReasonOf(unknown)
	assert.False(t, ok)
}
