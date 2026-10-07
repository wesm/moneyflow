package uitest_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/uitest"
)

func TestProviderPersistsOnlyRequestedFields(t *testing.T) {
	root := t.TempDir()
	source, err := uitest.OpenProvider(root)
	require.NoError(t, err)
	result, err := source.UpdateTransaction(t.Context(), provider.TransactionUpdate{
		TransactionExternalID: "current-a", CategoryExternalID: provider.Some("category-health"),
	})
	require.NoError(t, err)
	assert.Equal(t, provider.Some("category-health"), result.CategoryExternalID)
	assert.Equal(t, provider.Some("Example Shop"), result.MerchantLabel)
	assert.Equal(t, provider.Some(false), result.Hidden)
	reopened, err := uitest.OpenProvider(root)
	require.NoError(t, err)
	state := reopened.State()
	assert.Equal(t, "category-health", state.Transactions["current-a"].CategoryExternalID)
	assert.Equal(t, "merchant-shop", state.Transactions["current-a"].MerchantExternalID)
	assert.Equal(t, int64(-1200), state.Transactions["current-a"].Amount.Minor)
	assert.Equal(t, "category-home", state.Transactions["older"].CategoryExternalID)
	require.Len(t, state.Calls, 1)
	assert.Equal(t, "current-a", state.Calls[0].Target)
	assert.Equal(t, "applied", state.Calls[0].Outcome)
}

func TestProviderMerchantIdentityAndClearCategory(t *testing.T) {
	source, err := uitest.OpenProvider(t.TempDir())
	require.NoError(t, err)
	result, err := source.UpdateTransaction(t.Context(), provider.TransactionUpdate{TransactionExternalID: "current-a", MerchantExternalID: provider.Some("merchant-destination"), ClearCategory: true})
	require.NoError(t, err)
	require.Equal(t, provider.Some("Destination Shop"), result.MerchantLabel)
	require.True(t, result.CategoryCleared)
	require.False(t, result.Hidden.Value)
	_, err = source.UpdateTransaction(t.Context(), provider.TransactionUpdate{TransactionExternalID: "current-b", MerchantExternalID: provider.Some("missing")})
	require.Error(t, err)
	require.Equal(t, "merchant-shop", source.State().Transactions["current-b"].MerchantExternalID)
}

func TestProviderUnknownOutcomeSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	source, err := uitest.OpenProvider(root)
	require.NoError(t, err)
	require.NoError(t, source.SetFault(uitest.FaultUnknown))
	_, err = source.UpdateTransaction(t.Context(), provider.TransactionUpdate{
		TransactionExternalID: "current-a", MerchantName: provider.Some("Renamed Shop"),
	})
	require.Error(t, err)
	reopened, err := uitest.OpenProvider(root)
	require.NoError(t, err)
	state := reopened.State()
	result, err := reopened.ReadTransaction(t.Context(), "current-a", state.Transactions["current-a"].Date)
	require.NoError(t, err)
	assert.Equal(t, provider.Some("Renamed Shop"), result.MerchantLabel)
	assert.Equal(t, uitest.FaultNone, state.Fault)
	assert.Equal(t, "unknown", state.Calls[0].Outcome)
	assert.Equal(t, "readback", reopened.State().Calls[1].Method)
}

func TestProviderRejectsMissingUpdateAndDeletesIdempotently(t *testing.T) {
	source, err := uitest.OpenProvider(t.TempDir())
	require.NoError(t, err)
	_, err = source.UpdateTransaction(t.Context(), provider.TransactionUpdate{TransactionExternalID: "missing", Hidden: provider.Some(true)})
	require.Error(t, err)
	first, err := source.DeleteTransaction(t.Context(), "current-a")
	require.NoError(t, err)
	assert.False(t, first.AlreadyAbsent)
	second, err := source.DeleteTransaction(t.Context(), "current-a")
	require.NoError(t, err)
	assert.True(t, second.AlreadyAbsent)
	assert.NotContains(t, source.State().Transactions, "current-a")
}

func TestProviderBlockedWriteCancelsBeforeApplication(t *testing.T) {
	source, err := uitest.OpenProvider(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, source.SetFault(uitest.FaultBlock))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, writeErr := source.UpdateTransaction(ctx, provider.TransactionUpdate{TransactionExternalID: "current-a", Hidden: provider.Some(true)})
		done <- writeErr
	}()
	require.Eventually(t, func() bool { return len(source.State().Calls) == 1 }, time.Second, time.Millisecond)
	cancel()
	select {
	case err = <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("provider write did not stop after cancellation")
	}
	assert.False(t, source.State().Transactions["current-a"].Hidden)
}
