package api

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/store"
)

func TestSafeErrorDoesNotExposeCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("private query contents")
	err := newSafeError(CodeInvalidViewState, "The view URL is invalid.", cause)
	assert.Equal(t, "The view URL is invalid.", err.Error())
	assert.Equal(t, cause, errors.Unwrap(err))
	var safe *SafeError
	require.True(t, errors.As(err, &safe))
	assert.Equal(t, CodeInvalidViewState, safe.Code)
}

func TestProblemMapsEveryOnboardingCodeToStableSafeEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code   onboarding.Code
		status int
	}{
		{onboarding.CodeOnboardingStale, http.StatusConflict},
		{onboarding.CodeOnboardingExpired, http.StatusNotFound},
		{onboarding.CodeOnboardingCanceled, http.StatusConflict},
		{onboarding.CodeOnboardingLocalOnly, http.StatusConflict},
		{onboarding.CodeCredentialUnlockFailed, http.StatusUnprocessableEntity},
		{onboarding.CodeCredentialInputInvalid, http.StatusUnprocessableEntity},
		{onboarding.CodeProviderConnectInProgress, http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		problem := problemFromOnboardingError(onboarding.ErrorForCode(test.code))
		assert.Equal(t, test.status, problem.Status)
		assert.Equal(t, string(test.code), problem.Code)
		assert.NotContains(t, problem.Detail, "credential contents")
	}
}

func TestProblemMapsPersistentFailuresToStableSafeEnvelopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code   app.AppErrorCode
		status int
	}{
		{app.AppRevisionConflict, http.StatusConflict},
		{app.AppInvalidOperation, http.StatusUnprocessableEntity},
		{app.AppInvalidTarget, http.StatusConflict},
		{app.AppSelectionStale, http.StatusConflict},
		{app.AppStoreBusy, http.StatusServiceUnavailable},
		{app.AppStoreError, http.StatusInternalServerError},
		{app.AppJournalFull, http.StatusConflict},
		{app.AppProviderReconnectRequired, http.StatusConflict},
		{app.AppProviderIdentityMismatch, http.StatusConflict},
		{app.AppProviderSnapshotUnstable, http.StatusServiceUnavailable},
		{app.AppProviderRefreshInProgress, http.StatusServiceUnavailable},
		{app.AppProviderDeletionConfirmationRequired, http.StatusConflict},
		{app.AppProviderConfirmationInvalid, http.StatusConflict},
		{app.AppProviderRefreshStale, http.StatusConflict},
		{app.AppProviderRateLimited, http.StatusServiceUnavailable},
		{app.AppProviderUnavailable, http.StatusServiceUnavailable},
		{app.AppProviderMoneyMismatch, http.StatusUnprocessableEntity},
		{app.AppProviderDataInvalid, http.StatusUnprocessableEntity},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			failure := &app.AppError{
				Code: test.code, Detail: "The request could not use private diagnostic data.",
				CurrentRevision: 17, Selection: app.EmptySelection(),
			}
			problem := problemFromError(failure)
			assert.Equal(t, test.status, problem.Status)
			assert.Equal(t, string(test.code), problem.Code)
			assert.Equal(t, "17", problem.CurrentRevision)
			assert.NotContains(t, problem.Detail, "/private/profile.sqlite")
			if test.code == app.AppSelectionStale {
				require.NotNil(t, problem.Selection)
				assert.Equal(t, "cleared", problem.Selection.Kind)
			} else {
				assert.Nil(t, problem.Selection)
			}
		})
	}
}

func TestProblemReasonEnumListsEveryStoreReason(t *testing.T) {
	t.Parallel()

	field, found := reflect.TypeOf(Problem{}).FieldByName("Reason")
	require.True(t, found)
	var documented []store.InvalidOperationReason
	for _, value := range strings.Split(field.Tag.Get("enum"), ",") {
		documented = append(documented, store.InvalidOperationReason(value))
	}

	assert.ElementsMatch(t, store.InvalidOperationReasons(), documented)
}

func TestProblemReportsAReasonOnlyForAnInvalidOperation(t *testing.T) {
	t.Parallel()

	reasoned := store.NewInvalidOperationError(store.InvalidOperationTaxonomyLabelCollision, nil)
	for _, test := range []struct {
		code app.AppErrorCode
		want string
	}{
		{app.AppInvalidOperation, "taxonomy_label_collision"},
		{app.AppStoreError, ""},
		{app.AppProviderDataInvalid, ""},
	} {
		failure := &app.AppError{Code: test.code, Detail: "Safe detail."}

		problem := problemFromError(errors.Join(failure, reasoned))

		assert.Equal(t, test.want, problem.Reason, test.code)
	}
}
