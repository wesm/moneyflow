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

// CurrentSchemaVersion is the newest schema understood by this binary.
const CurrentSchemaVersion = 1

//go:embed schema/0001_profile.sql
var profileSchemaV1 string

type migration struct {
	version int
	sql     string
}

var migrations = []migration{{version: 1, sql: profileSchemaV1}}

var errMigrationBusy = errors.New("migration lock is busy")

func migrate(
	ctx context.Context,
	database *sql.DB,
	options Options,
	candidates []migration,
) error {
	deadlineContext, cancel := context.WithTimeout(ctx, options.MigrationDeadline)
	defer cancel()

	for {
		version, err := readSchemaVersion(deadlineContext, database)
		if err != nil {
			if isBusy(err) {
				if waitErr := waitForMigration(deadlineContext); waitErr != nil {
					return store.NewError(store.CodeStoreBusy, err)
				}
				continue
			}
			return mapDriverError(err, store.CodeMigrationFailed)
		}
		if version > CurrentSchemaVersion {
			return store.NewError(store.CodeSchemaNewer, fmt.Errorf("schema version %d", version))
		}
		if version == CurrentSchemaVersion {
			return nil
		}

		err = applyPendingMigrations(deadlineContext, database, options, candidates, version)
		if err == nil {
			return nil
		}
		if errors.Is(err, errMigrationBusy) || isBusy(err) {
			if waitErr := waitForMigration(deadlineContext); waitErr != nil {
				return store.NewError(store.CodeStoreBusy, err)
			}
			continue
		}
		var failure *store.Error
		if errors.As(err, &failure) {
			return err
		}
		return store.NewError(store.CodeMigrationFailed, err)
	}
}

func applyPendingMigrations(
	ctx context.Context,
	database *sql.DB,
	options Options,
	candidates []migration,
	knownVersion int,
) error {
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	if _, err = connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		if isBusy(err) || errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %w", errMigrationBusy, err)
		}
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = connection.ExecContext(context.Background(), "ROLLBACK")
		}
	}()

	version, err := readSchemaVersion(ctx, connection)
	if err != nil {
		return err
	}
	if version > CurrentSchemaVersion {
		return store.NewError(store.CodeSchemaNewer, fmt.Errorf("schema version %d", version))
	}
	if version < knownVersion {
		return errors.New("schema version moved backwards")
	}
	for _, candidate := range candidates {
		if candidate.version <= version {
			continue
		}
		if _, err = connection.ExecContext(ctx, candidate.sql); err != nil {
			return fmt.Errorf("apply schema version %d: %w", candidate.version, err)
		}
		if _, err = connection.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, applied_at_unix_ms) VALUES (?, ?)",
			candidate.version, options.Now().UnixMilli()); err != nil {
			return fmt.Errorf("record schema version %d: %w", candidate.version, err)
		}
		version = candidate.version
	}
	if _, err = connection.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

type schemaQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readSchemaVersion(ctx context.Context, queryer schemaQueryer) (int, error) {
	var exists int
	if err := queryer.QueryRowContext(ctx, `
		SELECT count(*) FROM sqlite_schema
		WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return 0, nil
	}
	var version int
	if err := queryer.QueryRowContext(ctx,
		"SELECT coalesce(max(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func isBusy(err error) bool {
	var coded sqliteCodedError
	return errors.As(err, &coded) && (coded.Code()&sqlitePrimary == sqliteBusy ||
		coded.Code()&sqlitePrimary == sqliteLocked)
}

func waitForMigration(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
