package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

// LoadCSVSettings probes profile kind without loading source rows or memberships.
func (profile *profile) LoadCSVSettings(ctx context.Context) (*store.CSVSettings, error) {
	return loadCSVSettings(ctx, profile.database)
}

func loadCSVSettings(ctx context.Context, query snapshotQueryer) (*store.CSVSettings, error) {
	var value store.CSVSettings
	var created int64
	err := query.QueryRowContext(ctx, `SELECT mapping, currency, scale, created_at_unix_ms FROM csv_settings WHERE singleton = 1`).Scan(&value.Mapping, &value.Currency, &value.Scale, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, loadFailure(err)
	}
	value.CreatedAt = unixMilliTime(created)
	return &value, nil
}

func loadCSVState(ctx context.Context, query snapshotQueryer) (store.CSVState, error) {
	var state store.CSVState
	var err error
	state.Settings, err = loadCSVSettings(ctx, query)
	if err != nil {
		return state, err
	}
	files, err := query.QueryContext(ctx, `SELECT file_key, account_key, digest, completed_at_unix_ms, imported, duplicates, skipped FROM csv_files ORDER BY file_key`)
	if err != nil {
		return state, loadFailure(err)
	}
	defer func() { _ = files.Close() }()
	for files.Next() {
		var file store.CSVFile
		var completed int64
		if err = files.Scan(&file.Key, &file.AccountKey, &file.Digest, &completed, &file.Imported, &file.Duplicates, &file.Skipped); err != nil {
			return state, loadFailure(err)
		}
		file.CompletedAt = unixMilliTime(completed)
		state.Files = append(state.Files, file)
	}
	if err = files.Err(); err != nil {
		return state, loadFailure(err)
	}
	rows, err := query.QueryContext(ctx, `SELECT source_key, local_transaction_id, account_key, occurrence, source_date, amount_minor, currency, scale, merchant, category, notes, metadata_json, merchant_override, category_override, disposition FROM csv_rows ORDER BY source_key`)
	if err != nil {
		return state, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var row store.CSVRow
		var date, metadata string
		if err = rows.Scan(&row.SourceKey, &row.LocalTransactionID, &row.AccountKey, &row.Occurrence, &date, &row.Amount.Minor, &row.Amount.Currency, &row.Amount.Scale, &row.Merchant, &row.Category, &row.Notes, &metadata, &row.MerchantOverride, &row.CategoryOverride, &row.Disposition); err != nil {
			return state, loadFailure(err)
		}
		if row.Date, err = domain.ParseDate(date); err != nil {
			return state, loadFailure(err)
		}
		if err = json.Unmarshal([]byte(metadata), &row.Metadata); err != nil {
			return state, loadFailure(err)
		}
		state.Rows = append(state.Rows, row)
	}
	if err = rows.Err(); err != nil {
		return state, loadFailure(err)
	}
	members, err := query.QueryContext(ctx, `SELECT file_key, source_key FROM csv_file_rows ORDER BY file_key, source_key`)
	if err != nil {
		return state, loadFailure(err)
	}
	defer func() { _ = members.Close() }()
	for members.Next() {
		var member store.CSVFileRow
		if err = members.Scan(&member.FileKey, &member.SourceKey); err != nil {
			return state, loadFailure(err)
		}
		state.Membership = append(state.Membership, member)
	}
	if err = members.Err(); err != nil {
		return state, loadFailure(err)
	}
	return state, nil
}

func replaceCSVState(ctx context.Context, connection *sql.Conn, state store.CSVState) error {
	for _, query := range []string{"DELETE FROM csv_file_rows", "DELETE FROM csv_rows", "DELETE FROM csv_files", "DELETE FROM csv_settings"} {
		if _, err := connection.ExecContext(ctx, query); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	if state.Settings == nil {
		return nil
	}
	s := state.Settings
	if _, err := connection.ExecContext(ctx, `INSERT INTO csv_settings VALUES(1, ?, ?, ?, ?)`, s.Mapping, s.Currency, s.Scale, s.CreatedAt.UnixMilli()); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	files, err := connection.PrepareContext(ctx, `INSERT INTO csv_files VALUES(?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = files.Close() }()
	for _, f := range state.Files {
		if _, err = files.ExecContext(ctx, f.Key, f.AccountKey, f.Digest, f.CompletedAt.UnixMilli(), f.Imported, f.Duplicates, f.Skipped); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	rows, err := connection.PrepareContext(ctx, `INSERT INTO csv_rows VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	for _, r := range state.Rows {
		metadata, err := json.Marshal(r.Metadata)
		if err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
		if _, err = rows.ExecContext(ctx, r.SourceKey, r.LocalTransactionID, r.AccountKey, r.Occurrence, r.Date.String(), r.Amount.Minor, r.Amount.Currency, r.Amount.Scale, r.Merchant, r.Category, r.Notes, string(metadata), r.MerchantOverride, r.CategoryOverride, r.Disposition); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	members, err := connection.PrepareContext(ctx, `INSERT INTO csv_file_rows VALUES(?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = members.Close() }()
	for _, m := range state.Membership {
		if _, err = members.ExecContext(ctx, m.FileKey, m.SourceKey); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func foldCSVOverrides(ctx context.Context, connection *sql.Conn, snapshot domain.ProfileSnapshot, effective domain.CommittedProfile) error {
	settings, err := loadCSVSettings(ctx, connection)
	if err != nil || settings == nil {
		return err
	}
	state, err := loadCSVState(ctx, connection)
	if err != nil {
		return err
	}
	byID := make(map[domain.EntityID]int, len(state.Rows))
	for i, row := range state.Rows {
		byID[row.LocalTransactionID] = i
	}
	current := snapshot.Committed
	for _, operation := range snapshot.Journal[:snapshot.Cursor] {
		profilereplay.VisitAffectedByOperation(current, operation, func(id domain.EntityID) bool {
			index, exists := byID[id]
			if !exists {
				return true
			}
			row := &state.Rows[index]
			switch operation.Type {
			case domain.OperationMerchantLabel, domain.OperationMerchantMerge, domain.OperationMerchantReassign:
				row.MerchantOverride = true
			case domain.OperationCategoryAssign, domain.OperationCategoryCreate, domain.OperationCategoryLabel,
				domain.OperationCategoryMove, domain.OperationCategoryMerge, domain.OperationCategoryDelete,
				domain.OperationGroupLabel, domain.OperationGroupMerge, domain.OperationGroupDelete:
				row.CategoryOverride = true
			case domain.OperationTransactionDelete:
				row.Disposition = "deleted"
			}
			return true
		})
		current, err = profilereplay.ApplyOperation(current, operation)
		if err != nil {
			return err
		}
	}
	if err = store.ValidateCSVState(state, effective); err != nil {
		return err
	}
	return replaceCSVState(ctx, connection, state)
}

// ApplyCSVImport plans against a fresh snapshot under the authoritative write lock.
func (profile *profile) ApplyCSVImport(ctx context.Context, rowCount int, planner store.CSVImportPlanner) (store.CSVImportResult, error) {
	if planner == nil || rowCount < 0 || rowCount > 1_000_000 {
		return store.CSVImportResult{}, errors.New("invalid CSV import request")
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return store.CSVImportResult{}, err
	}
	defer func() { _ = finish(false) }()
	var state store.CSVImportState
	if state.Snapshot, err = loadSnapshot(ctx, connection); err != nil {
		return store.CSVImportResult{}, err
	}
	if state.CSV, err = loadCSVState(ctx, connection); err != nil {
		return store.CSVImportResult{}, err
	}
	var incompatible bool
	if err = connection.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provider_binding) OR EXISTS(SELECT 1 FROM provider_write_batches) OR EXISTS(SELECT 1 FROM amazon_profile_settings) OR EXISTS(SELECT 1 FROM amazon_order_items)`).Scan(&incompatible); err != nil {
		return store.CSVImportResult{}, loadFailure(err)
	}
	if incompatible || state.CSV.Settings == nil && (state.Snapshot.Revision != 0 || len(state.Snapshot.Committed.Transactions) != 0) {
		return store.CSVImportResult{}, errors.New("CSV import requires a CSV profile or a pristine new profile")
	}
	if err = store.ValidateCSVState(state.CSV, state.Snapshot.Committed); err != nil {
		return store.CSVImportResult{}, err
	}
	if state.Allocations, err = loadLabelAllocations(ctx, connection); err != nil {
		return store.CSVImportResult{}, err
	}
	var proposed store.CSVProposedIDs
	for _, demand := range []struct {
		kind  domain.EntityKind
		count int
		ids   *[]domain.EntityID
	}{
		{domain.EntityKindTransaction, rowCount, &proposed.Transactions}, {domain.EntityKindAccount, 1, &proposed.Accounts},
		{domain.EntityKindMerchant, rowCount, &proposed.Merchants}, {domain.EntityKindCategory, rowCount, &proposed.Categories},
	} {
		if *demand.ids, err = proposeEntityIDs(demand.kind, demand.count); err != nil {
			return store.CSVImportResult{}, err
		}
	}
	plan, err := planner(state, proposed)
	if err != nil {
		return store.CSVImportResult{}, err
	}
	if plan.Result.Unchanged {
		plan.Result.Revision = state.Snapshot.Revision
		return plan.Result, nil
	}
	after := plan.State
	if state.CSV.Settings != nil && !reflect.DeepEqual(state.CSV.Settings, after.CSV.Settings) {
		return store.CSVImportResult{}, errors.New("CSV settings are immutable")
	}
	if _, err = profilereplay.Replay(after.Snapshot); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = store.ValidateCSVState(after.CSV, after.Snapshot.Committed); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = applyProviderCommitted(ctx, connection, state.Snapshot.Committed, after.Snapshot.Committed, state.Snapshot.KnownDrills, after.Snapshot.KnownDrills); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = replaceRefreshJournal(ctx, connection, state.Snapshot.Journal, after.Snapshot.Journal); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = replaceLabelAllocations(ctx, connection, state.Allocations, after.Allocations); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = replaceCSVState(ctx, connection, after.CSV); err != nil {
		return store.CSVImportResult{}, err
	}
	next, err := incrementRevision(state.Snapshot.Revision)
	if err != nil {
		return store.CSVImportResult{}, err
	}
	if err = updateJournalState(ctx, connection, state.Snapshot.Revision, next, after.Snapshot.Cursor); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return store.CSVImportResult{}, err
	}
	if err = finish(true); err != nil {
		return store.CSVImportResult{}, err
	}
	plan.Result.Revision = next
	return plan.Result, nil
}
