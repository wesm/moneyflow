package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAppErrorMapsStableStoreCodesAndReliableRevision(t *testing.T) {
	t.Parallel()

	tests := map[store.ErrorCode]app.AppErrorCode{
		store.CodeRevisionConflict:   app.AppRevisionConflict,
		store.CodeInvalidOperation:   app.AppInvalidOperation,
		store.CodeInvalidTarget:      app.AppInvalidTarget,
		store.CodeStoreBusy:          app.AppStoreBusy,
		store.CodeStoreError:         app.AppStoreError,
		store.CodeSchemaNewer:        app.AppSchemaNewer,
		store.CodeSchemaIncompatible: app.AppSchemaIncompatible,
		store.CodeStoreCorrupt:       app.AppStoreCorrupt,
		store.CodeJournalFull:        app.AppJournalFull,
	}
	for storageCode, appCode := range tests {
		t.Run(string(storageCode), func(t *testing.T) {
			t.Parallel()
			profile := newMemoryProfile(t, 9)
			service, err := app.NewProfileService(context.Background(), profile)
			require.NoError(t, err)
			profile.currentErr = store.NewError(storageCode, errors.New("internal detail"))
			_, err = service.Refresh(context.Background())
			var failure *app.AppError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, appCode, failure.Code)
			assert.Equal(t, uint64(9), failure.CurrentRevision)
			assert.NotContains(t, failure.Error(), "internal detail")
		})
	}
}

func TestAppErrorMapsEveryProviderCodeWithoutRemoteDetails(t *testing.T) {
	t.Parallel()

	for _, code := range provider.ErrorCodes() {
		service, _ := newProviderRefreshService(t)
		now := time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)
		source := &fakeProviderSource{
			identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
			snapshot: providerSnapshot(t, now, 1), fingerprint: "session-a",
		}
		if code == provider.CodeIdentityMismatch {
			source.identity.RemoteID = ""
		} else {
			source.fetchErr = provider.NewError(code)
		}
		configureProviderRefreshService(t, service, source, now, "instance-a")
		_, err := service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		if code == provider.CodeDeletionConfirmationRequired ||
			code == provider.CodeConfirmationInvalid || code == provider.CodeRefreshStale ||
			code == provider.CodeRefreshInProgress {
			continue // These codes are produced by orchestration guards, not a reader.
		}
		var failure *app.AppError
		require.ErrorAs(t, err, &failure, code)
		assert.Equal(t, app.AppErrorCode(code), failure.Code)
		assert.NotContains(t, failure.Detail, "subscription-example")
	}
}

func TestAppErrorIncludesOnlyAllowlistedProviderValidationReason(t *testing.T) {
	t.Parallel()

	service, _ := newProviderRefreshService(t)
	now := time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC)
	source := &fakeProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		fetchErr: provider.NewDataInvalidError(provider.DataInvalidTransactionAmount),
	}
	configureProviderRefreshService(t, service, source, now, "instance-a")

	_, err := service.RefreshProvider(context.Background(), app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, app.AppProviderDataInvalid, failure.Code)
	assert.Equal(
		t,
		"The provider returned invalid data. A transaction amount cannot be represented at the configured currency scale.",
		failure.Detail,
	)
	assert.NotContains(t, failure.Detail, "subscription-example")
}

func TestAppErrorIncludesOnlyAllowlistedRefreshValidationStage(t *testing.T) {
	t.Parallel()

	profile := newMemoryProfile(t, 9)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	profile.currentErr = store.NewInvalidOperationError(
		store.InvalidOperationRefreshPlan,
		errors.New("private planner detail"),
	)

	_, err = service.Refresh(context.Background())
	var failure *app.AppError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, app.AppInvalidOperation, failure.Code)
	assert.Equal(
		t,
		"The requested operation is invalid. Moneyflow rejected its local refresh plan before writing financial data.",
		failure.Detail,
	)
	assert.NotContains(t, failure.Detail, "private planner detail")
}
