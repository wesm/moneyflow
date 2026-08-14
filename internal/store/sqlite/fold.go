package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/wesm/moneyflow/internal/domain"
	profilereplay "github.com/wesm/moneyflow/internal/replay"
	"github.com/wesm/moneyflow/internal/store"
)

// Fold atomically replaces the committed rows changed by the active journal prefix.
func (profile *profile) Fold(
	ctx context.Context,
	expectedRevision uint64,
	plan store.FoldPlan,
) (uint64, error) {
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = finish(false) }()

	current, cursor, _, err := loadJournalState(ctx, connection)
	if err != nil {
		return 0, err
	}
	if current != expectedRevision {
		return 0, revisionConflict(expectedRevision, current)
	}
	if err = plan.Validate(expectedRevision); err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	snapshot, err := loadSnapshot(ctx, connection)
	if err != nil {
		return 0, err
	}
	if err = snapshot.Validate(); err != nil {
		return 0, store.NewError(store.CodeStoreCorrupt, err)
	}
	activeIDs := make([]string, cursor)
	for index := range cursor {
		activeIDs[index] = snapshot.Journal[index].ID
	}
	if !slices.Equal(activeIDs, plan.ActiveOperationIDs) {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("fold active operation prefix changed"),
		)
	}
	replayed, err := profilereplay.Replay(snapshot)
	if err != nil {
		return 0, store.NewError(store.CodeStoreCorrupt, err)
	}
	authoritativeDrills, err := profilereplay.KnownDrillsForFold(
		snapshot.KnownDrills,
		replayed.Effective,
		snapshot.Journal[:cursor],
	)
	if err != nil {
		return 0, store.NewError(store.CodeStoreCorrupt, err)
	}
	if !reflect.DeepEqual(plan.Effective, replayed.Effective) ||
		!reflect.DeepEqual(plan.KnownDrills, authoritativeDrills) {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("fold plan does not match authoritative journal replay"),
		)
	}
	if err = validateFoldShape(snapshot, plan); err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	if err = applyFold(ctx, connection, snapshot.Committed, plan.Effective); err != nil {
		return 0, err
	}
	if err = insertFoldKnownDrills(ctx, connection, snapshot.KnownDrills, plan.KnownDrills); err != nil {
		return 0, err
	}
	if _, err = connection.ExecContext(ctx, "DELETE FROM journal_operations"); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	next, err := incrementRevision(current)
	if err != nil {
		return 0, err
	}
	if err = updateJournalState(ctx, connection, current, next, 0); err != nil {
		return 0, err
	}
	if err = finish(true); err != nil {
		return 0, err
	}
	return next, nil
}

func validateFoldShape(snapshot domain.ProfileSnapshot, plan store.FoldPlan) error {
	if !reflect.DeepEqual(snapshot.Committed.Accounts, plan.Effective.Accounts) {
		return errors.New("fold cannot change accounts")
	}
	if !reflect.DeepEqual(
		snapshot.Committed.ExternalIdentities,
		plan.Effective.ExternalIdentities,
	) {
		return errors.New("fold cannot change external identities")
	}
	if err := requireExistingEntityIDs(snapshot.Committed.Merchants, plan.Effective.Merchants); err != nil {
		return err
	}
	if err := requireExistingEntityIDs(snapshot.Committed.Groups, plan.Effective.Groups); err != nil {
		return err
	}
	if err := requireExistingEntityIDs(snapshot.Committed.Categories, plan.Effective.Categories); err != nil {
		return err
	}
	if err := requireExactTransactionIDs(
		snapshot.Committed.Transactions,
		plan.Effective.Transactions,
	); err != nil {
		return err
	}
	known := make(map[string]struct{}, len(plan.KnownDrills))
	for _, identity := range plan.KnownDrills {
		key, _ := identity.CanonicalKey()
		known[key] = struct{}{}
	}
	for _, identity := range snapshot.KnownDrills {
		key, _ := identity.CanonicalKey()
		if _, ok := known[key]; !ok {
			return errors.New("fold cannot remove a known drill identity")
		}
	}
	return nil
}

type foldEntity interface {
	domain.Merchant | domain.CategoryGroup | domain.Category
}

func requireExistingEntityIDs[T foldEntity](before, after []T) error {
	afterIDs := make(map[domain.EntityID]struct{}, len(after))
	for _, value := range after {
		afterIDs[foldEntityID(value)] = struct{}{}
	}
	for _, value := range before {
		if _, ok := afterIDs[foldEntityID(value)]; !ok {
			return errors.New("fold cannot remove an entity")
		}
	}
	return nil
}

func foldEntityID[T foldEntity](value T) domain.EntityID {
	switch entity := any(value).(type) {
	case domain.Merchant:
		return entity.ID
	case domain.CategoryGroup:
		return entity.ID
	case domain.Category:
		return entity.ID
	default:
		return ""
	}
}

func requireExactTransactionIDs(before, after []domain.TransactionRecord) error {
	if len(before) != len(after) {
		return errors.New("fold cannot add or remove transactions")
	}
	for index := range before {
		if before[index].ID != after[index].ID {
			return errors.New("fold cannot change transaction identities")
		}
	}
	return nil
}

func applyFold(
	ctx context.Context,
	connection *sql.Conn,
	before, after domain.CommittedProfile,
) error {
	if err := releaseFoldCollisionKeys(ctx, connection, before, after); err != nil {
		return err
	}
	statements, err := prepareFoldStatements(ctx, connection)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer statements.close()
	if err = foldGroups(ctx, statements, before.Groups, after.Groups); err != nil {
		return err
	}
	if err = foldMerchants(ctx, statements, before.Merchants, after.Merchants); err != nil {
		return err
	}
	if err = foldCategories(ctx, statements, before.Categories, after.Categories); err != nil {
		return err
	}
	return foldTransactions(ctx, statements, before.Transactions, after.Transactions)
}

func releaseFoldCollisionKeys(
	ctx context.Context,
	connection *sql.Conn,
	before, after domain.CommittedProfile,
) error {
	merchantAfter := make(map[domain.EntityID]domain.Merchant, len(after.Merchants))
	for _, value := range after.Merchants {
		merchantAfter[value.ID] = value
	}
	for _, value := range before.Merchants {
		next := merchantAfter[value.ID]
		if !value.Retired && (next.Retired || next.CollisionKey != value.CollisionKey) {
			if err := releaseFoldCollisionKey(ctx, connection, "merchants", value.ID); err != nil {
				return err
			}
		}
	}
	groupAfter := make(map[domain.EntityID]domain.CategoryGroup, len(after.Groups))
	for _, value := range after.Groups {
		groupAfter[value.ID] = value
	}
	for _, value := range before.Groups {
		next := groupAfter[value.ID]
		if !value.Retired && (next.Retired || next.CollisionKey != value.CollisionKey) {
			if err := releaseFoldCollisionKey(ctx, connection, "category_groups", value.ID); err != nil {
				return err
			}
		}
	}
	categoryAfter := make(map[domain.EntityID]domain.Category, len(after.Categories))
	for _, value := range after.Categories {
		categoryAfter[value.ID] = value
	}
	for _, value := range before.Categories {
		next := categoryAfter[value.ID]
		if !value.Retired && (next.Retired || next.CollisionKey != value.CollisionKey) {
			if err := releaseFoldCollisionKey(ctx, connection, "categories", value.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func releaseFoldCollisionKey(
	ctx context.Context,
	connection *sql.Conn,
	table string,
	id domain.EntityID,
) error {
	query := "UPDATE " + table + " SET collision_key = ? WHERE id = ?" //nolint:gosec // table is an internal constant.
	if _, err := connection.ExecContext(ctx, query, "\x1fmoneyflow-fold:"+string(id), id); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

type foldStatements struct {
	insertMerchant    *sql.Stmt
	updateMerchant    *sql.Stmt
	insertGroup       *sql.Stmt
	updateGroup       *sql.Stmt
	insertCategory    *sql.Stmt
	updateCategory    *sql.Stmt
	updateTransaction *sql.Stmt
}

func prepareFoldStatements(ctx context.Context, connection *sql.Conn) (*foldStatements, error) {
	statements := &foldStatements{}
	queries := []struct {
		destination **sql.Stmt
		query       string
	}{
		{&statements.insertMerchant, `
			INSERT INTO merchants(
				id, label, collision_key, retired, protected, merge_destination_id
			) VALUES (?, ?, ?, ?, 0, ?)`},
		{&statements.updateMerchant, `
			UPDATE merchants
			SET label = ?, collision_key = ?, retired = ?, merge_destination_id = ?
			WHERE id = ?`},
		{&statements.insertGroup, `
			INSERT INTO category_groups(
				id, label, collision_key, retired, protected, merge_destination_id
			) VALUES (?, ?, ?, ?, ?, ?)`},
		{&statements.updateGroup, `
			UPDATE category_groups
			SET label = ?, collision_key = ?, retired = ?, protected = ?, merge_destination_id = ?
			WHERE id = ?`},
		{&statements.insertCategory, `
			INSERT INTO categories(
				id, group_id, label, collision_key, retired, protected, merge_destination_id
			) VALUES (?, ?, ?, ?, ?, ?, ?)`},
		{&statements.updateCategory, `
			UPDATE categories
			SET group_id = ?, label = ?, collision_key = ?, retired = ?, protected = ?,
				merge_destination_id = ?
			WHERE id = ?`},
		{&statements.updateTransaction, `
			UPDATE transactions SET
				provider = ?, provider_id = ?, account_id = ?, merchant_id = ?, category_id = ?,
				transaction_date = ?, amount_minor = ?, currency = ?, scale = ?, notes = ?,
				hidden = ?, pending = ?, metadata_json = ?
			WHERE id = ?`},
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

func (statements *foldStatements) close() {
	for _, statement := range []*sql.Stmt{
		statements.insertMerchant,
		statements.updateMerchant,
		statements.insertGroup,
		statements.updateGroup,
		statements.insertCategory,
		statements.updateCategory,
		statements.updateTransaction,
	} {
		if statement != nil {
			_ = statement.Close()
		}
	}
}

func foldMerchants(
	ctx context.Context,
	statements *foldStatements,
	before, after []domain.Merchant,
) error {
	existing := make(map[domain.EntityID]domain.Merchant, len(before))
	for _, value := range before {
		existing[value.ID] = value
	}
	for _, value := range after {
		if _, found := existing[value.ID]; found {
			continue
		}
		_, err := statements.insertMerchant.ExecContext(
			ctx, value.ID, value.Label, value.CollisionKey, booleanInteger(value.Retired),
			nullableEntityID(value.MergeDestination),
		)
		if err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	for _, value := range after {
		previous, found := existing[value.ID]
		if !found || reflect.DeepEqual(previous, value) {
			continue
		}
		if _, err := statements.updateMerchant.ExecContext(
			ctx, value.Label, value.CollisionKey, booleanInteger(value.Retired),
			nullableEntityID(value.MergeDestination), value.ID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func foldGroups(
	ctx context.Context,
	statements *foldStatements,
	before, after []domain.CategoryGroup,
) error {
	existing := make(map[domain.EntityID]domain.CategoryGroup, len(before))
	for _, value := range before {
		existing[value.ID] = value
	}
	for _, value := range after {
		if _, found := existing[value.ID]; found {
			continue
		}
		_, err := statements.insertGroup.ExecContext(
			ctx, value.ID, value.Label, value.CollisionKey, booleanInteger(value.Retired),
			booleanInteger(value.Protected), nullableEntityID(value.MergeDestination),
		)
		if err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	for _, value := range after {
		previous, found := existing[value.ID]
		if !found || reflect.DeepEqual(previous, value) {
			continue
		}
		if _, err := statements.updateGroup.ExecContext(
			ctx, value.Label, value.CollisionKey, booleanInteger(value.Retired),
			booleanInteger(value.Protected), nullableEntityID(value.MergeDestination), value.ID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func foldCategories(
	ctx context.Context,
	statements *foldStatements,
	before, after []domain.Category,
) error {
	existing := make(map[domain.EntityID]domain.Category, len(before))
	for _, value := range before {
		existing[value.ID] = value
	}
	for _, value := range after {
		if _, found := existing[value.ID]; found {
			continue
		}
		_, err := statements.insertCategory.ExecContext(
			ctx, value.ID, value.GroupID, value.Label, value.CollisionKey,
			booleanInteger(value.Retired), booleanInteger(value.Protected),
			nullableEntityID(value.MergeDestination),
		)
		if err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	for _, value := range after {
		previous, found := existing[value.ID]
		if !found || reflect.DeepEqual(previous, value) {
			continue
		}
		if _, err := statements.updateCategory.ExecContext(
			ctx, value.GroupID, value.Label, value.CollisionKey, booleanInteger(value.Retired),
			booleanInteger(value.Protected), nullableEntityID(value.MergeDestination), value.ID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func foldTransactions(
	ctx context.Context,
	statements *foldStatements,
	before, after []domain.TransactionRecord,
) error {
	for index, value := range after {
		if reflect.DeepEqual(before[index], value) {
			continue
		}
		metadata, err := json.Marshal(value.Metadata)
		if err != nil {
			return store.NewError(store.CodeInvalidOperation, err)
		}
		if _, err = statements.updateTransaction.ExecContext(
			ctx,
			value.Provider,
			value.ProviderID,
			value.AccountID,
			value.MerchantID,
			value.CategoryID,
			value.Date.String(),
			value.Amount.Minor,
			value.Amount.Currency,
			value.Amount.Scale,
			value.Notes,
			booleanInteger(value.Hidden),
			booleanInteger(value.Pending),
			string(metadata),
			value.ID,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func insertFoldKnownDrills(
	ctx context.Context,
	connection *sql.Conn,
	existing, desired []domain.DrillIdentity,
) error {
	known := make(map[string]struct{}, len(existing))
	for _, identity := range existing {
		key, _ := identity.CanonicalKey()
		known[key] = struct{}{}
	}
	statement, err := connection.PrepareContext(ctx, `
		INSERT OR IGNORE INTO known_drills(dimension, currency, scale, identity_key)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = statement.Close() }()
	for _, identity := range desired {
		key, _ := identity.CanonicalKey()
		if _, found := known[key]; found {
			continue
		}
		if _, err = statement.ExecContext(
			ctx, identity.Dimension, identity.Currency, identity.Scale, identity.Key,
		); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}
