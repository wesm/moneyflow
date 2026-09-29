package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

const (
	maxJournalOperations = 10_000
	maxJournalTargets    = 1_000_000
)

var errJournalFull = errors.New("pending journal capacity exceeded")

func (profile *profile) Append(
	ctx context.Context,
	expectedRevision uint64,
	operation domain.Operation,
) (uint64, error) {
	if len(operation.Targets) > maxJournalTargets {
		return 0, store.NewError(store.CodeJournalFull, errJournalFull)
	}
	if err := operation.ValidateDraft(); err != nil {
		return 0, store.NewError(store.CodeInvalidOperation, err)
	}
	if err := validateJournalCapacity(0, 0, len(operation.Targets)); err != nil {
		return 0, store.NewError(store.CodeJournalFull, err)
	}
	if operation.CreatedRevision != expectedRevision {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("operation creation revision does not match expectation"),
		)
	}
	if operation.CreatedAt != time.UnixMilli(operation.CreatedAt.UnixMilli()).UTC() {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("operation creation time is not millisecond precise"),
		)
	}
	payload, err := encodeOperationPayload(operation)
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
	var targetCount int
	if err = connection.QueryRowContext(ctx, "SELECT count(*) FROM operation_targets").Scan(
		&targetCount,
	); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	if err = validateJournalCapacity(cursor, targetCount, len(operation.Targets)); err != nil {
		return 0, store.NewError(store.CodeJournalFull, err)
	}
	var sequence int64
	if err = connection.QueryRowContext(ctx,
		"SELECT coalesce(max(sequence), 0) + 1 FROM journal_operations").Scan(&sequence); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	operation.Sequence = sequence
	if err = insertOperation(ctx, connection, operation, payload); err != nil {
		return 0, err
	}
	next, err := incrementRevision(current)
	if err != nil {
		return 0, err
	}
	if err = updateJournalState(ctx, connection, current, next, cursor+1); err != nil {
		return 0, err
	}
	if err = finish(true); err != nil {
		return 0, err
	}
	return next, nil
}

func validateJournalCapacity(operationCount, targetCount, newTargetCount int) error {
	if operationCount < 0 || targetCount < 0 || newTargetCount < 0 {
		return errors.New("journal capacity counts are negative")
	}
	if operationCount >= maxJournalOperations || newTargetCount > maxJournalTargets ||
		targetCount > maxJournalTargets-newTargetCount {
		return errJournalFull
	}
	return nil
}

func truncateRedoTail(
	ctx context.Context,
	connection *sql.Conn,
	cursor, count int,
) error {
	if cursor >= count {
		return nil
	}
	if _, err := connection.ExecContext(ctx, `
		DELETE FROM journal_operations
		WHERE id IN (
			SELECT id FROM journal_operations
			ORDER BY sequence LIMIT -1 OFFSET ?
		)`, cursor); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	return nil
}

func (profile *profile) MoveCursor(
	ctx context.Context,
	expectedRevision uint64,
	direction int,
) (uint64, error) {
	if direction != -1 && direction != 1 {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("cursor direction must be negative or positive one"),
		)
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
	nextCursor := cursor + direction
	if nextCursor < 0 || nextCursor > count {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			errors.New("cursor cannot move past the journal boundary"),
		)
	}
	next, err := incrementRevision(current)
	if err != nil {
		return 0, err
	}
	if err = updateJournalState(ctx, connection, current, next, nextCursor); err != nil {
		return 0, err
	}
	if err = finish(true); err != nil {
		return 0, err
	}
	return next, nil
}

type immediateFinish func(bool) error

func (profile *profile) beginImmediate(
	ctx context.Context,
) (*sql.Conn, immediateFinish, error) {
	connection, err := profile.database.Conn(ctx)
	if err != nil {
		return nil, nil, mapDriverError(err, store.CodeStoreError)
	}
	if _, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		_ = connection.Close()
		return nil, nil, mapDriverError(err, store.CodeStoreError)
	}
	finished := false
	finish := func(commit bool) error {
		if finished {
			return nil
		}
		finished = true
		var transactionErr error
		if commit {
			_, transactionErr = connection.ExecContext(ctx, "COMMIT")
			if transactionErr != nil {
				_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
			}
		} else {
			_, transactionErr = connection.ExecContext(context.Background(), "ROLLBACK")
		}
		closeErr := connection.Close()
		if transactionErr != nil {
			return mapDriverError(transactionErr, store.CodeStoreError)
		}
		if closeErr != nil {
			return mapDriverError(closeErr, store.CodeStoreError)
		}
		return nil
	}
	return connection, finish, nil
}

func loadJournalState(
	ctx context.Context,
	connection *sql.Conn,
) (uint64, int, int, error) {
	var revision uint64
	var cursor, count int
	if err := connection.QueryRowContext(ctx, `
		SELECT revision, journal_cursor,
			(SELECT count(*) FROM journal_operations)
		FROM profile_state WHERE singleton = 1`).Scan(&revision, &cursor, &count); err != nil {
		return 0, 0, 0, mapDriverError(err, store.CodeStoreError)
	}
	if cursor < 0 || cursor > count {
		return 0, 0, 0, store.NewError(
			store.CodeStoreCorrupt,
			errors.New("journal cursor is outside the operation count"),
		)
	}
	return revision, cursor, count, nil
}

func insertOperation(
	ctx context.Context,
	connection *sql.Conn,
	operation domain.Operation,
	payload []byte,
) error {
	createdRevision, err := sqliteInteger(operation.CreatedRevision)
	if err != nil {
		return err
	}
	if _, err = connection.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?)`,
		operation.ID,
		operation.Sequence,
		operation.Type,
		operation.PayloadVersion,
		createdRevision,
		operation.CreatedAt.UnixMilli(),
	); err != nil {
		return mapDriverError(err, store.CodeInvalidOperation)
	}
	if _, err = connection.ExecContext(ctx, `
		INSERT INTO operation_payloads(operation_id, payload_version, payload_json)
		VALUES (?, ?, ?)`,
		operation.ID,
		operation.PayloadVersion,
		string(payload),
	); err != nil {
		return mapDriverError(err, store.CodeInvalidOperation)
	}
	for ordinal, target := range operation.Targets {
		if _, err = connection.ExecContext(ctx, `
			INSERT INTO operation_targets(operation_id, ordinal, entity_id)
			VALUES (?, ?, ?)`, operation.ID, ordinal, target); err != nil {
			return mapDriverError(err, store.CodeInvalidOperation)
		}
	}
	return nil
}

func updateJournalState(
	ctx context.Context,
	connection *sql.Conn,
	current, next uint64,
	cursor int,
) error {
	currentInteger, err := sqliteInteger(current)
	if err != nil {
		return err
	}
	nextInteger, err := sqliteInteger(next)
	if err != nil {
		return err
	}
	result, err := connection.ExecContext(ctx, `
		UPDATE profile_state SET revision = ?, journal_cursor = ?
		WHERE singleton = 1 AND revision = ?`, nextInteger, cursor, currentInteger)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if affected != 1 {
		return store.NewError(store.CodeStoreCorrupt, errors.New("profile state compare-and-advance failed"))
	}
	return nil
}

func incrementRevision(current uint64) (uint64, error) {
	if current >= math.MaxInt64 {
		return 0, store.NewError(store.CodeStoreCorrupt, errors.New("profile revision is exhausted"))
	}
	return current + 1, nil
}

func sqliteInteger(value uint64) (int64, error) {
	if value > math.MaxInt64 {
		return 0, store.NewError(
			store.CodeInvalidOperation,
			fmt.Errorf("value exceeds SQLite integer range"),
		)
	}
	return int64(value), nil
}

func revisionConflict(observed, current uint64) error {
	return store.NewRevisionError(
		store.CodeRevisionConflict,
		observed,
		current,
		errors.New("authoritative profile revision compare failed"),
	)
}
