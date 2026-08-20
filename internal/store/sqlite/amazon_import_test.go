package sqlite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAmazonImportPersistsSettingsLedgerAndHistoryAtomically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	handle := openSeededProfile(t, DefaultOptions)
	before, err := handle.Load(ctx)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 20, 15, 0, 0, 0, time.UTC)
	transaction := before.Committed.Transactions[0]

	commit, err := handle.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-import-test", StartedAt: now.Add(-time.Second), ImportedAt: now,
		CandidateDigest: strings.Repeat("a", 64), ProposedCounts: store.AmazonIDCounts{Sources: 1},
	}, func(state store.AmazonImportState, proposed store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		require.Equal(t, before, state.Snapshot)
		require.Len(t, proposed.SourceIdentities, 1)
		return store.AmazonImportPlan{
			Committed: before.Committed.Clone(), Journal: append([]domain.Operation(nil), before.Journal...),
			Cursor: before.Cursor, KnownDrills: append([]domain.DrillIdentity(nil), before.KnownDrills...),
			Settings: &store.AmazonSettings{Currency: "USD", Scale: 2, CreatedAt: now},
			Items: []store.AmazonOrderItem{{
				LocalTransactionID: transaction.ID, SourceIdentity: proposed.SourceIdentities[0],
				OrderID: "example-order", ASIN: "EXAMPLEASIN", ProductName: "Example Product",
				OrderDate: transaction.Date, Quantity: 1, AmountMinor: transaction.Amount.Minor,
				Currency: "USD", Scale: 2, OrderStatus: "Closed", ShipmentStatus: "Delivered",
				IdentityFingerprint: strings.Repeat("b", 64), FullFingerprint: strings.Repeat("c", 64),
			}},
			History:        store.AmazonImportHistory{FileCount: 1, LogicalRecordCount: 1, InsertedCount: 1},
			SemanticChange: true,
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, before.Revision+1, commit.Revision)
	assert.True(t, commit.SemanticChange)

	state, err := handle.LoadAmazonState(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Settings)
	assert.Equal(t, "USD", string(state.Settings.Currency))
	require.Len(t, state.Items, 1)
	assert.Equal(t, transaction.ID, state.Items[0].LocalTransactionID)
	assert.Equal(t, proposedSourcePattern, state.Items[0].SourceIdentity[:len("amazon_item_")])

	var histories int
	require.NoError(t, handle.database.QueryRowContext(ctx,
		"SELECT count(*) FROM amazon_import_history WHERE import_id = ?", "amazon-import-test").Scan(&histories))
	assert.Equal(t, 1, histories)
}

const proposedSourcePattern = "amazon_item_"

func TestAmazonImportNoOpAppendsHistoryWithoutRevisionChurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	handle := openSeededProfile(t, DefaultOptions)
	before, err := handle.Load(ctx)
	require.NoError(t, err)
	now := time.Date(2026, time.August, 20, 16, 0, 0, 0, time.UTC)

	commit, err := handle.ApplyAmazonImport(ctx, store.AtomicAmazonImportRequest{
		ImportID: "amazon-import-noop", StartedAt: now, ImportedAt: now,
		CandidateDigest: strings.Repeat("d", 64),
	}, func(state store.AmazonImportState, _ store.ProposedAmazonIDs) (store.AmazonImportPlan, error) {
		return store.AmazonImportPlan{
			Committed: state.Snapshot.Committed.Clone(), Journal: append([]domain.Operation(nil), state.Snapshot.Journal...),
			Cursor: state.Snapshot.Cursor, KnownDrills: append([]domain.DrillIdentity(nil), state.Snapshot.KnownDrills...),
			Settings: state.Settings, Items: state.Items, Allocations: state.Allocations,
			History: store.AmazonImportHistory{UnchangedCount: len(state.Items)}, SemanticChange: false,
		}, nil
	})
	if err != nil {
		t.Fatalf("apply no-op Amazon import: %v", errors.Unwrap(err))
	}
	assert.Equal(t, before.Revision, commit.Revision)
	assert.False(t, commit.SemanticChange)
}
