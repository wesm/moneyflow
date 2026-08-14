package sqlite

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/store"
)

type codedError int

func (failure codedError) Error() string { return "sensitive driver detail" }
func (failure codedError) Code() int     { return int(failure) }

func TestMapDriverErrorUsesStableSafeCodes(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		code     int
		expected store.ErrorCode
	}{
		"busy":         {5, store.CodeStoreBusy},
		"locked":       {6, store.CodeStoreBusy},
		"corrupt":      {11, store.CodeStoreCorrupt},
		"not database": {26, store.CodeStoreCorrupt},
		"io":           {10, store.CodeStoreError},
		"constraint":   {19, store.CodeStoreError},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mapped := mapDriverError(codedError(test.code), store.CodeStoreError)
			var failure *store.Error
			require.ErrorAs(t, mapped, &failure)
			assert.Equal(t, test.expected, failure.Code)
			assert.NotContains(t, failure.Error(), "sensitive")
			assert.ErrorIs(t, errors.Unwrap(failure), codedError(test.code))
		})
	}
}
