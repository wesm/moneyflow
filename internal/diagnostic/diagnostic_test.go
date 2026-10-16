package diagnostic

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/store"
)

// A formatted wrapper repeats its storage error's safe text, and the storage error keeps its
// diagnostic out of Error(), so the description has to keep unwrapping past both.
func TestDescribeCauseKeepsADiagnosticBehindAFormattedStorageWrapper(t *testing.T) {
	t.Parallel()
	storage := store.NewError(store.CodeStoreError, errors.New("disk-private-diagnostic"))
	cases := map[string]error{
		"formatted wrapper": fmt.Errorf("save view: %w", storage),
		"joined errors": errors.Join(errors.New("first-diagnostic"),
			fmt.Errorf("save view: %w", storage)),
	}
	for name, cause := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			described := DescribeCause(cause)

			assert.Contains(t, described, "save view: store_error:")
			assert.Contains(t, described, "disk-private-diagnostic")
			assert.Equal(t, 1, strings.Count(described, "save view:"))
		})
	}
}
