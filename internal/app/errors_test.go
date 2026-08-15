package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
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
