package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// Load returns one fully detached consistent profile snapshot.
func (profile *profile) Load(ctx context.Context) (domain.ProfileSnapshot, error) {
	transaction, err := profile.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return domain.ProfileSnapshot{}, mapDriverError(err, store.CodeStoreError)
	}
	finished := false
	defer func() {
		if !finished {
			_ = transaction.Rollback()
		}
	}()
	snapshot, err := loadSnapshot(ctx, transaction)
	if err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if err = snapshot.Validate(); err != nil {
		return domain.ProfileSnapshot{}, store.NewError(store.CodeStoreCorrupt, err)
	}
	if err = transaction.Commit(); err != nil {
		return domain.ProfileSnapshot{}, mapDriverError(err, store.CodeStoreError)
	}
	finished = true
	return snapshot, nil
}

func loadSnapshot(ctx context.Context, transaction *sql.Tx) (domain.ProfileSnapshot, error) {
	var snapshot domain.ProfileSnapshot
	if err := transaction.QueryRowContext(ctx,
		"SELECT revision, journal_cursor FROM profile_state WHERE singleton = 1").
		Scan(&snapshot.Revision, &snapshot.Cursor); err != nil {
		return domain.ProfileSnapshot{}, loadFailure(err)
	}
	var journalCount int
	if err := transaction.QueryRowContext(ctx, "SELECT count(*) FROM journal_operations").
		Scan(&journalCount); err != nil {
		return domain.ProfileSnapshot{}, loadFailure(err)
	}
	if journalCount != 0 {
		return domain.ProfileSnapshot{}, store.NewError(
			store.CodeSchemaIncompatible,
			errors.New("stored journal requires a supported payload decoder"),
		)
	}

	var err error
	if snapshot.Committed.Accounts, err = loadAccounts(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if snapshot.Committed.Merchants, err = loadMerchants(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if snapshot.Committed.Groups, err = loadGroups(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if snapshot.Committed.Categories, err = loadCategories(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if snapshot.Committed.Transactions, err = loadTransactions(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if snapshot.Committed.ExternalIdentities, err = loadExternalIdentities(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	if snapshot.KnownDrills, err = loadKnownDrills(ctx, transaction); err != nil {
		return domain.ProfileSnapshot{}, err
	}
	return snapshot, nil
}

func loadAccounts(ctx context.Context, transaction *sql.Tx) ([]domain.Account, error) {
	rows, err := transaction.QueryContext(ctx,
		"SELECT id, label, collision_key, retired FROM accounts ORDER BY id")
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Account
	for rows.Next() {
		var value domain.Account
		var retired int
		if err = rows.Scan(&value.ID, &value.Label, &value.CollisionKey, &retired); err != nil {
			return nil, loadFailure(err)
		}
		value.Retired = retired != 0
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func loadMerchants(ctx context.Context, transaction *sql.Tx) ([]domain.Merchant, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT id, label, collision_key, retired, merge_destination_id
		FROM merchants ORDER BY id`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Merchant
	for rows.Next() {
		var value domain.Merchant
		var retired int
		var destination sql.NullString
		if err = rows.Scan(
			&value.ID, &value.Label, &value.CollisionKey, &retired, &destination,
		); err != nil {
			return nil, loadFailure(err)
		}
		value.Retired = retired != 0
		value.MergeDestination = entityIDPointer(destination)
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func loadGroups(ctx context.Context, transaction *sql.Tx) ([]domain.CategoryGroup, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT id, label, collision_key, protected, retired, merge_destination_id
		FROM category_groups ORDER BY id`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.CategoryGroup
	for rows.Next() {
		var value domain.CategoryGroup
		var protected, retired int
		var destination sql.NullString
		if err = rows.Scan(
			&value.ID, &value.Label, &value.CollisionKey, &protected, &retired, &destination,
		); err != nil {
			return nil, loadFailure(err)
		}
		value.Protected = protected != 0
		value.Retired = retired != 0
		value.MergeDestination = entityIDPointer(destination)
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func loadCategories(ctx context.Context, transaction *sql.Tx) ([]domain.Category, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT id, group_id, label, collision_key, protected, retired, merge_destination_id
		FROM categories ORDER BY id`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.Category
	for rows.Next() {
		var value domain.Category
		var protected, retired int
		var destination sql.NullString
		if err = rows.Scan(
			&value.ID, &value.GroupID, &value.Label, &value.CollisionKey,
			&protected, &retired, &destination,
		); err != nil {
			return nil, loadFailure(err)
		}
		value.Protected = protected != 0
		value.Retired = retired != 0
		value.MergeDestination = entityIDPointer(destination)
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func loadTransactions(ctx context.Context, transaction *sql.Tx) ([]domain.TransactionRecord, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT id, provider, provider_id, account_id, merchant_id, category_id,
			transaction_date, amount_minor, currency, scale, notes, hidden, pending, metadata_json
		FROM transactions ORDER BY id`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.TransactionRecord
	for rows.Next() {
		var value domain.TransactionRecord
		var date, currency, metadata string
		var scale, hidden, pending int
		if err = rows.Scan(
			&value.ID, &value.Provider, &value.ProviderID, &value.AccountID, &value.MerchantID,
			&value.CategoryID, &date, &value.Amount.Minor, &currency, &scale, &value.Notes,
			&hidden, &pending, &metadata,
		); err != nil {
			return nil, loadFailure(err)
		}
		if scale < 0 || scale > 9 {
			return nil, store.NewError(store.CodeStoreCorrupt, errors.New("stored money scale is invalid"))
		}
		value.Date, err = domain.ParseDate(date)
		if err != nil {
			return nil, store.NewError(store.CodeStoreCorrupt, err)
		}
		value.Amount.Currency = domain.Currency(currency)
		value.Amount.Scale = uint8(scale)
		value.Hidden = hidden != 0
		value.Pending = pending != 0
		if err = json.Unmarshal([]byte(metadata), &value.Metadata); err != nil {
			return nil, store.NewError(store.CodeStoreCorrupt, err)
		}
		if metadata != "null" && value.Metadata == nil {
			return nil, store.NewError(store.CodeStoreCorrupt, errors.New("stored metadata is not an object"))
		}
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func loadExternalIdentities(
	ctx context.Context,
	transaction *sql.Tx,
) ([]domain.ExternalIdentity, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT entity_type, entity_id, namespace, external_id
		FROM external_identities ORDER BY namespace, external_id`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.ExternalIdentity
	for rows.Next() {
		var value domain.ExternalIdentity
		if err = rows.Scan(
			&value.EntityType, &value.EntityID, &value.Namespace, &value.ExternalID,
		); err != nil {
			return nil, loadFailure(err)
		}
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func loadKnownDrills(ctx context.Context, transaction *sql.Tx) ([]domain.DrillIdentity, error) {
	rows, err := transaction.QueryContext(ctx, `
		SELECT dimension, currency, scale, identity_key
		FROM known_drills ORDER BY dimension, currency, scale, identity_key`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	var result []domain.DrillIdentity
	for rows.Next() {
		var value domain.DrillIdentity
		var scale int
		if err = rows.Scan(&value.Dimension, &value.Currency, &scale, &value.Key); err != nil {
			return nil, loadFailure(err)
		}
		if scale < 0 || scale > 9 {
			return nil, store.NewError(store.CodeStoreCorrupt, errors.New("stored drill scale is invalid"))
		}
		value.Scale = uint8(scale)
		result = append(result, value)
	}
	return result, loadRowsError(rows)
}

func entityIDPointer(value sql.NullString) *domain.EntityID {
	if !value.Valid {
		return nil
	}
	identity := domain.EntityID(value.String)
	return &identity
}

func loadRowsError(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		return loadFailure(err)
	}
	return nil
}

func loadFailure(err error) error {
	return mapDriverError(fmt.Errorf("load profile: %w", err), store.CodeStoreCorrupt)
}
