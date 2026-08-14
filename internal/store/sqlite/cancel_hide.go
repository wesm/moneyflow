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

// CancelHide removes exact targets from every originating active hide operation.
func (profile *profile) CancelHide(
	ctx context.Context,
	expectedRevision uint64,
	targets []domain.EntityID,
) (uint64, error) {
	if err := validateHideCancellationTargets(targets); err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	encodedTargets, err := json.Marshal(targets)
	if err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	connection, finish, err := profile.beginImmediate(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = finish(false) }()

	current, cursor, count, err := loadJournalState(ctx, connection)
	if err != nil {
		return 0, err
	}
	if current != expectedRevision {
		return 0, revisionConflict(expectedRevision, current)
	}
	if err = truncateRedoTail(ctx, connection, cursor, count); err != nil {
		return 0, err
	}
	covered, err := countCoveredHideTargets(ctx, connection, encodedTargets)
	if err != nil {
		return 0, err
	}
	if covered != len(targets) {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("hide cancellation targets do not all have active effects"),
		)
	}
	if err = deleteCoveredHideTargets(ctx, connection, encodedTargets); err != nil {
		return 0, err
	}
	if err = resequenceHideTargets(ctx, connection); err != nil {
		return 0, err
	}
	removed, err := deleteEmptyHideOperations(ctx, connection)
	if err != nil {
		return 0, err
	}
	if removed > cursor {
		return 0, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("hide cancellation removed more operations than the active cursor"),
		)
	}
	next, err := incrementRevision(current)
	if err != nil {
		return 0, err
	}
	if err = updateJournalState(ctx, connection, current, next, cursor-removed); err != nil {
		return 0, err
	}
	if err = finish(true); err != nil {
		return 0, err
	}
	return next, nil
}

func resequenceHideTargets(ctx context.Context, connection *sql.Conn) error {
	statements := []string{
		`CREATE TEMP TABLE cancel_hide_remaining_targets (
			operation_id TEXT NOT NULL,
			ordinal INTEGER NOT NULL,
			entity_id TEXT NOT NULL,
			PRIMARY KEY(operation_id, ordinal)
		) STRICT`,
		`INSERT INTO cancel_hide_remaining_targets(operation_id, ordinal, entity_id)
		SELECT targets.operation_id,
			row_number() OVER (
				PARTITION BY targets.operation_id ORDER BY targets.ordinal
			) - 1,
			targets.entity_id
		FROM operation_targets AS targets
		JOIN journal_operations AS operations ON operations.id = targets.operation_id
		WHERE operations.operation_type = 'transaction.hide-toggle'`,
		`DELETE FROM operation_targets WHERE operation_id IN (
			SELECT id FROM journal_operations
			WHERE operation_type = 'transaction.hide-toggle'
		)`,
		`INSERT INTO operation_targets(operation_id, ordinal, entity_id)
		SELECT operation_id, ordinal, entity_id
		FROM cancel_hide_remaining_targets
		ORDER BY operation_id, ordinal`,
		`DROP TABLE cancel_hide_remaining_targets`,
	}
	for _, statement := range statements {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}

func validateHideCancellationTargets(targets []domain.EntityID) error {
	if len(targets) == 0 {
		return errors.New("hide cancellation targets are empty")
	}
	for index, target := range targets {
		if target == "" {
			return errors.New("hide cancellation target is empty")
		}
		if index > 0 && targets[index-1] >= target {
			return errors.New("hide cancellation targets must be strictly sorted and unique")
		}
	}
	return nil
}

func countCoveredHideTargets(
	ctx context.Context,
	connection *sql.Conn,
	encodedTargets []byte,
) (int, error) {
	var covered int
	if err := connection.QueryRowContext(ctx, `
		SELECT count(DISTINCT targets.entity_id)
		FROM operation_targets AS targets
		JOIN journal_operations AS operations ON operations.id = targets.operation_id
		WHERE operations.operation_type = ?
			AND targets.entity_id IN (SELECT value FROM json_each(?))`,
		domain.OperationTransactionHide,
		string(encodedTargets),
	).Scan(&covered); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	return covered, nil
}

func deleteCoveredHideTargets(
	ctx context.Context,
	connection *sql.Conn,
	encodedTargets []byte,
) error {
	if _, err := connection.ExecContext(ctx, `
		DELETE FROM operation_targets
		WHERE entity_id IN (SELECT value FROM json_each(?))
			AND operation_id IN (
				SELECT id FROM journal_operations WHERE operation_type = ?
			)`,
		string(encodedTargets),
		domain.OperationTransactionHide,
	); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func deleteEmptyHideOperations(ctx context.Context, connection *sql.Conn) (int, error) {
	result, err := connection.ExecContext(ctx, `
		DELETE FROM journal_operations
		WHERE operation_type = ? AND NOT EXISTS (
			SELECT 1 FROM operation_targets WHERE operation_id = journal_operations.id
		)`, domain.OperationTransactionHide)
	if err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	if removed < 0 || int64(int(removed)) != removed {
		return 0, store.NewError(
			store.CodeStoreCorrupt,
			fmt.Errorf("hide cancellation removed invalid operation count %d", removed),
		)
	}
	return int(removed), nil
}
