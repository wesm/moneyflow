package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

// OpenTransferSource opens only an existing compatible database without installation.
// The caller must hold the exclusive profile lifecycle lock until Close returns.
func OpenTransferSource(ctx context.Context, paths home.Paths, options Options) (store.Profile, error) {
	if err := validateMaintenancePaths(paths); err != nil {
		return nil, err
	}
	if err := validateInspectionSidecars(paths); err != nil {
		return nil, err
	}
	database, pinned, err := openMaintenanceDatabase(ctx, paths, options, store.CodeStoreCorrupt, true)
	if err != nil {
		return nil, err
	}
	opened := &profile{database: database, readOnlyFile: pinned}
	success := false
	defer func() {
		if !success {
			_ = opened.Close()
		}
	}()
	state, err := inspectSchema(ctx, database)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreCorrupt)
	}
	if state != schemaCurrent {
		return nil, store.NewError(store.CodeSchemaIncompatible, errors.New("export requires a source-compatible binary"))
	}
	if err = validateSchema(ctx, database); err != nil {
		return nil, err
	}
	if err = quickCheck(ctx, database); err != nil {
		return nil, err
	}
	success = true
	return opened, nil
}

// LoadProfileTransfer reads eligibility and all financial records in one transaction.
func (profile *profile) LoadProfileTransfer(ctx context.Context) (store.ProfileTransfer, error) {
	tx, err := profile.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return store.ProfileTransfer{}, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = tx.Rollback() }()
	var state store.ProfileTransfer
	if state.Snapshot, err = loadSnapshot(ctx, tx); err != nil {
		return state, err
	}
	refresh, err := loadRefreshState(ctx, tx)
	if err != nil {
		return state, err
	}
	if state.Provider, err = loadProviderStateForWrite(ctx, tx, state.Snapshot.Revision, refresh); err != nil {
		return state, err
	}
	if state.YNABSplits, err = loadYNABTransactionSplits(ctx, tx); err != nil {
		return state, err
	}
	if state.AmazonSettings, err = loadAmazonSettings(ctx, tx); err != nil {
		return state, err
	}
	if state.AmazonItems, err = loadAmazonItems(ctx, tx); err != nil {
		return state, err
	}
	if state.CSV, err = loadCSVState(ctx, tx); err != nil {
		return state, err
	}
	return state, mapDriverError(tx.Commit(), store.CodeStoreError)
}

// InstallProfileTransfer atomically installs financial records into a pristine profile.
// Revision starts at one; no operational counters, journal, or leases are copied.
func (profile *profile) InstallProfileTransfer(ctx context.Context, state store.ProfileTransfer) error {
	if state.Snapshot.Cursor != 0 || len(state.Snapshot.Journal) != 0 {
		return store.NewError(store.CodeInvalidOperation, errors.New("transfer contains journal operations"))
	}
	if err := state.Snapshot.Validate(); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	if err := store.ValidateCSVState(state.CSV, state.Snapshot.Committed); err != nil {
		return store.NewError(store.CodeInvalidOperation, err)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = finish(false) }()
	populated, err := profilePopulated(ctx, connection)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if populated {
		return store.NewError(store.CodeInvalidOperation, errors.New("transfer requires a pristine profile"))
	}
	if err = insertSeed(ctx, connection, state.Snapshot.Committed, state.Snapshot.KnownDrills); err != nil {
		return mapDriverError(err, store.CodeInvalidOperation)
	}
	if state.Provider.Binding != nil {
		if err = persistRefreshBinding(ctx, connection, state.Provider.Binding); err != nil {
			return err
		}
	}
	if err = replaceLabelAllocations(ctx, connection, nil, state.Provider.Allocations); err != nil {
		return err
	}
	if err = replaceProviderIdentityLineage(ctx, connection, state.Provider.Lineage); err != nil {
		return err
	}
	if err = replaceYNABTransactionSplits(ctx, connection, state.YNABSplits); err != nil {
		return err
	}
	if err = replaceProviderWriteRestrictions(ctx, connection, state.Provider.WriteRestrictions); err != nil {
		return err
	}
	if err = replaceAmazonSettings(ctx, connection, nil, state.AmazonSettings); err != nil {
		return err
	}
	if err = replaceAmazonItems(ctx, connection, nil, state.AmazonItems); err != nil {
		return err
	}
	if err = replaceCSVState(ctx, connection, state.CSV); err != nil {
		return err
	}
	if _, err = connection.ExecContext(ctx, "UPDATE profile_state SET revision = 1 WHERE singleton = 1"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return finish(true)
}
