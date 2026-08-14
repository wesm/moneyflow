package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/wesm/moneyflow/internal/store"
)

// CurrentSchemaVersion is the only schema version this pre-stability binary opens.
const CurrentSchemaVersion = 2

//go:embed schema/profile.sql
var currentProfileSchema string

var errStartupBusy = errors.New("schema installation lock is busy")

type schemaState uint8

const (
	schemaEmpty schemaState = iota
	schemaCurrent
	schemaOlder
	schemaNewer
)

func ensureCurrentSchema(ctx context.Context, database *sql.DB, options Options) error {
	deadlineContext, cancel := context.WithTimeout(ctx, options.StartupDeadline)
	defer cancel()

	for {
		state, err := inspectSchema(deadlineContext, database)
		if err != nil {
			if isBusy(err) || errors.Is(deadlineContext.Err(), context.DeadlineExceeded) {
				if waitErr := waitForStartup(deadlineContext); waitErr != nil {
					return store.NewError(store.CodeStoreBusy, err)
				}
				continue
			}
			return mapDriverError(err, store.CodeStoreCorrupt)
		}
		switch state {
		case schemaCurrent:
			return nil
		case schemaOlder:
			return store.NewError(store.CodeSchemaIncompatible, errors.New("older schema version"))
		case schemaNewer:
			return store.NewError(store.CodeSchemaNewer, errors.New("newer schema version"))
		case schemaEmpty:
			// Continue below and serialize the one permitted schema installation.
		}

		err = installCurrentSchema(deadlineContext, database, options, currentProfileSchema)
		if err == nil {
			return nil
		}
		if errors.Is(err, errStartupBusy) || isBusy(err) {
			if waitErr := waitForStartup(deadlineContext); waitErr != nil {
				return store.NewError(store.CodeStoreBusy, err)
			}
			continue
		}
		return err
	}
}

func installCurrentSchema(
	ctx context.Context,
	database *sql.DB,
	_ Options,
	schemaSQL string,
) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = connection.Close() }()
	if _, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		if isBusy(err) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %w", errStartupBusy, err)
		}
		return mapDriverError(err, store.CodeStoreError)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	state, err := inspectSchema(ctx, connection)
	if err != nil {
		return mapDriverError(err, store.CodeStoreCorrupt)
	}
	switch state {
	case schemaCurrent:
		return nil
	case schemaOlder:
		return store.NewError(store.CodeSchemaIncompatible, errors.New("older schema version"))
	case schemaNewer:
		return store.NewError(store.CodeSchemaNewer, errors.New("newer schema version"))
	case schemaEmpty:
	}
	if _, err = connection.ExecContext(ctx, schemaSQL); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if _, err = connection.ExecContext(ctx, "COMMIT"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	committed = true
	return nil
}

type schemaQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func inspectSchema(ctx context.Context, queryer schemaQueryer) (schemaState, error) {
	var objectCount int
	if err := queryer.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%'`).Scan(&objectCount); err != nil {
		return schemaEmpty, err
	}
	if objectCount == 0 {
		return schemaEmpty, nil
	}
	var metadataCount int
	if err := queryer.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'schema_metadata'`).Scan(&metadataCount); err != nil {
		return schemaEmpty, err
	}
	if metadataCount != 1 {
		var legacyCount int
		if err := queryer.QueryRowContext(ctx, `
			SELECT count(*) FROM sqlite_schema
			WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&legacyCount); err != nil {
			return schemaEmpty, err
		}
		if legacyCount == 1 {
			return schemaOlder, nil
		}
		return schemaEmpty, store.NewError(store.CodeStoreCorrupt, errors.New("schema metadata is missing"))
	}
	var version int
	if err := queryer.QueryRowContext(ctx,
		"SELECT schema_version FROM schema_metadata WHERE singleton = 1").Scan(&version); err != nil {
		return schemaEmpty, err
	}
	switch {
	case version == CurrentSchemaVersion:
		return schemaCurrent, nil
	case version < CurrentSchemaVersion:
		return schemaOlder, nil
	default:
		return schemaNewer, nil
	}
}

func isBusy(err error) bool {
	var coded sqliteCodedError
	return errors.As(err, &coded) && (coded.Code()&sqlitePrimary == sqliteBusy ||
		coded.Code()&sqlitePrimary == sqliteLocked)
}

func waitForStartup(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
