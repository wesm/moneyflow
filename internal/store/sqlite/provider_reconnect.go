package sqlite

import (
	"context"

	"github.com/wesm/moneyflow/internal/store"
)

const clearProviderReconnectFailureSQL = `
	UPDATE provider_refresh_state SET status_code = ''
	WHERE singleton = 1 AND status_code = 'provider_reconnect_required'`

// ClearProviderReconnectFailure clears an authenticated reconnect without changing cache freshness.
func (profile *profile) ClearProviderReconnectFailure(ctx context.Context) error {
	_, err := profile.database.ExecContext(ctx, clearProviderReconnectFailureSQL)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}
