package api

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
