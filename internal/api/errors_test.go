package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
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
