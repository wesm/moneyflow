package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// CreateSeededProfile atomically populates only a pristine revision-zero profile.
func (profile *profile) CreateSeededProfile(
	ctx context.Context,
	committed domain.CommittedProfile,
) (uint64, error) {
	if err := committed.Validate(); err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	knownDrills, err := seededKnownDrills(committed)
	if err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	connection, err := profile.database.Conn(ctx)
	if err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = connection.Close() }()
	if _, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	committedTransaction := false
	defer func() {
		if !committedTransaction {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	populated, err := profilePopulated(ctx, connection)
	if err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	if populated {
		return 0, store.NewError(store.CodeInvalidOperation, errors.New("seed requires a pristine profile"))
	}
	if err = insertSeed(ctx, connection, committed, knownDrills); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	result, err := connection.ExecContext(ctx, `
		UPDATE profile_state SET revision = 1
		WHERE singleton = 1 AND revision = 0 AND journal_cursor = 0`)
	if err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	if affected != 1 {
		return 0, store.NewError(store.CodeInvalidOperation, errors.New("seed profile state is not pristine"))
	}
	if _, err = connection.ExecContext(ctx, "COMMIT"); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	committedTransaction = true
	return 1, nil
}

func profilePopulated(ctx context.Context, connection *sql.Conn) (bool, error) {
	var revision, cursor, rowCount, sentinelGroups, sentinelCategories int64
	err := connection.QueryRowContext(ctx, `
		SELECT revision, journal_cursor,
			(SELECT count(*) FROM accounts) +
			(SELECT count(*) FROM merchants) +
			(SELECT count(*) FROM category_groups WHERE id <> ?) +
			(SELECT count(*) FROM categories WHERE id <> ?) +
			(SELECT count(*) FROM transactions) +
			(SELECT count(*) FROM external_identities) +
			(SELECT count(*) FROM known_drills) +
			(SELECT count(*) FROM journal_operations) +
			(SELECT count(*) FROM operation_payloads) +
			(SELECT count(*) FROM operation_targets),
			(SELECT count(*) FROM category_groups
			 WHERE id = ? AND label = 'Uncategorized' AND collision_key = 'uncategorized'
			   AND retired = 0 AND protected = 1 AND merge_destination_id IS NULL),
			(SELECT count(*) FROM categories
			 WHERE id = ? AND group_id = ? AND label = 'Uncategorized'
			   AND collision_key = 'uncategorized' AND retired = 0 AND protected = 1
			   AND merge_destination_id IS NULL)
		FROM profile_state WHERE singleton = 1`,
		domain.UncategorizedGroupID,
		domain.UncategorizedCategoryID,
		domain.UncategorizedGroupID,
		domain.UncategorizedCategoryID,
		domain.UncategorizedGroupID,
	).Scan(&revision, &cursor, &rowCount, &sentinelGroups, &sentinelCategories)
	return revision != 0 || cursor != 0 || rowCount != 0 ||
		sentinelGroups != 1 || sentinelCategories != 1, err
}

func insertSeed(
	ctx context.Context,
	connection *sql.Conn,
	profile domain.CommittedProfile,
	knownDrills []domain.DrillIdentity,
) error {
	statements, err := prepareSeedStatements(ctx, connection)
	if err != nil {
		return err
	}
	defer statements.close()
	for _, account := range profile.Accounts {
		if _, err = statements.account.ExecContext(ctx,
			account.ID, account.Label, account.CollisionKey, booleanInteger(account.Retired)); err != nil {
			return err
		}
	}
	for _, merchant := range profile.Merchants {
		if _, err = statements.merchant.ExecContext(ctx,
			merchant.ID, merchant.Label, merchant.CollisionKey, booleanInteger(merchant.Retired),
			nullableEntityID(merchant.MergeDestination)); err != nil {
			return err
		}
	}
	for _, group := range profile.Groups {
		if group.ID == domain.UncategorizedGroupID {
			continue
		}
		if _, err = statements.group.ExecContext(ctx,
			group.ID, group.Label, group.CollisionKey, booleanInteger(group.Retired),
			booleanInteger(group.Protected), nullableEntityID(group.MergeDestination)); err != nil {
			return err
		}
	}
	for _, category := range profile.Categories {
		if category.ID == domain.UncategorizedCategoryID {
			continue
		}
		if _, err = statements.category.ExecContext(ctx,
			category.ID, category.GroupID, category.Label, category.CollisionKey,
			booleanInteger(category.Retired), booleanInteger(category.Protected),
			nullableEntityID(category.MergeDestination)); err != nil {
			return err
		}
	}
	for _, transaction := range profile.Transactions {
		metadata, err := json.Marshal(transaction.Metadata)
		if err != nil {
			return fmt.Errorf("encode seed transaction metadata: %w", err)
		}
		if _, err = statements.transaction.ExecContext(ctx,
			transaction.ID, transaction.Provider, transaction.ProviderID, transaction.AccountID,
			transaction.MerchantID, transaction.CategoryID, transaction.Date.String(),
			transaction.Amount.Minor, transaction.Amount.Currency, transaction.Amount.Scale,
			transaction.Notes, booleanInteger(transaction.Hidden), booleanInteger(transaction.Pending),
			string(metadata)); err != nil {
			return err
		}
	}
	for _, identity := range profile.ExternalIdentities {
		if _, err = statements.externalIdentity.ExecContext(ctx,
			identity.EntityType, identity.EntityID, identity.Namespace, identity.ExternalID); err != nil {
			return err
		}
	}
	for _, identity := range knownDrills {
		if _, err = statements.knownDrill.ExecContext(ctx,
			identity.Dimension, identity.Currency, identity.Scale, identity.Key); err != nil {
			return err
		}
	}
	return nil
}

type seedStatements struct {
	account          *sql.Stmt
	merchant         *sql.Stmt
	group            *sql.Stmt
	category         *sql.Stmt
	transaction      *sql.Stmt
	externalIdentity *sql.Stmt
	knownDrill       *sql.Stmt
}

func prepareSeedStatements(ctx context.Context, connection *sql.Conn) (*seedStatements, error) {
	statements := &seedStatements{}
	queries := []struct {
		destination **sql.Stmt
		query       string
	}{
		{&statements.account,
			"INSERT INTO accounts(id, label, collision_key, retired) VALUES (?, ?, ?, ?)"},
		{&statements.merchant, `
			INSERT INTO merchants(
				id, label, collision_key, retired, protected, merge_destination_id
			) VALUES (?, ?, ?, ?, 0, ?)`},
		{&statements.group, `
			INSERT INTO category_groups(
				id, label, collision_key, retired, protected, merge_destination_id
			) VALUES (?, ?, ?, ?, ?, ?)`},
		{&statements.category, `
			INSERT INTO categories(
				id, group_id, label, collision_key, retired, protected, merge_destination_id
			) VALUES (?, ?, ?, ?, ?, ?, ?)`},
		{&statements.transaction, `
			INSERT INTO transactions(
				id, provider, provider_id, account_id, merchant_id, category_id,
				transaction_date, amount_minor, currency, scale, notes, hidden, pending, metadata_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`},
		{&statements.externalIdentity, `
			INSERT INTO external_identities(entity_type, entity_id, namespace, external_id)
			VALUES (?, ?, ?, ?)`},
		{&statements.knownDrill, `
			INSERT INTO known_drills(dimension, currency, scale, identity_key)
			VALUES (?, ?, ?, ?)`},
	}
	for _, candidate := range queries {
		prepared, err := connection.PrepareContext(ctx, candidate.query)
		if err != nil {
			statements.close()
			return nil, err
		}
		*candidate.destination = prepared
	}
	return statements, nil
}

func (statements *seedStatements) close() {
	for _, statement := range []*sql.Stmt{
		statements.account,
		statements.merchant,
		statements.group,
		statements.category,
		statements.transaction,
		statements.externalIdentity,
		statements.knownDrill,
	} {
		if statement != nil {
			_ = statement.Close()
		}
	}
}

func seededKnownDrills(profile domain.CommittedProfile) ([]domain.DrillIdentity, error) {
	categories := make(map[domain.EntityID]domain.Category, len(profile.Categories))
	for _, category := range profile.Categories {
		categories[category.ID] = category
	}
	known := make(map[string]domain.DrillIdentity, len(profile.Transactions)*4)
	for _, transaction := range profile.Transactions {
		category, exists := categories[transaction.CategoryID]
		if !exists {
			return nil, fmt.Errorf("seed known drills: category %q is missing", transaction.CategoryID)
		}
		for _, identity := range []domain.DrillIdentity{
			{Dimension: domain.DimensionAccount, Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale, Key: string(transaction.AccountID)},
			{Dimension: domain.DimensionMerchant, Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale, Key: string(transaction.MerchantID)},
			{Dimension: domain.DimensionCategory, Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale, Key: string(transaction.CategoryID)},
			{Dimension: domain.DimensionGroup, Currency: transaction.Amount.Currency, Scale: transaction.Amount.Scale, Key: string(category.GroupID)},
		} {
			known[drillSortKey(identity)] = identity
		}
	}
	result := make([]domain.DrillIdentity, 0, len(known))
	for _, identity := range known {
		result = append(result, identity)
	}
	slices.SortFunc(result, compareDrillIdentities)
	return result, nil
}

func compareDrillIdentities(left, right domain.DrillIdentity) int {
	return compareStrings(drillSortKey(left), drillSortKey(right))
}

func drillSortKey(identity domain.DrillIdentity) string {
	return fmt.Sprintf("%s\x00%s\x00%03d\x00%s",
		identity.Dimension, identity.Currency, identity.Scale, identity.Key)
}

func compareStrings(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func booleanInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableEntityID(value *domain.EntityID) any {
	if value == nil {
		return nil
	}
	return *value
}
