// Package sqlite implements the private pure-Go SQLite profile store.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // Register the pure-Go database/sql driver.

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

const driverName = "sqlite"

// Options controls bounded SQLite connection and startup behavior.
type Options struct {
	MaxOpenConnections  int
	MutationBusyTimeout time.Duration
	StartupDeadline     time.Duration
	Now                 func() time.Time
}

// DefaultOptions favors durable financial writes and bounded concurrent access.
var DefaultOptions = Options{
	MaxOpenConnections:  3,
	MutationBusyTimeout: 5 * time.Second,
	StartupDeadline:     60 * time.Second,
	Now:                 time.Now,
}

type profile struct {
	database  *sql.DB
	closeOnce sync.Once
	closeErr  error
}

var _ store.Profile = (*profile)(nil)

// Open prepares, installs or validates, and opens one isolated v2 profile.
func Open(ctx context.Context, paths home.Paths, options Options) (store.Profile, error) {
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if err := home.PrepareDatabase(paths); err != nil {
		return nil, store.NewError(store.CodeStoreError, err)
	}
	database, err := sql.Open(driverName, dataSourceName(paths.Database, options))
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	database.SetMaxOpenConns(options.MaxOpenConnections)
	database.SetMaxIdleConns(options.MaxOpenConnections)
	opened := &profile{database: database}
	success := false
	defer func() {
		if !success {
			_ = database.Close()
		}
	}()
	if err = database.PingContext(ctx); err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	if err = verifyConnection(ctx, database, options); err != nil {
		return nil, err
	}
	if err = ensureCurrentSchema(ctx, database, options); err != nil {
		return nil, err
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

func validateOptions(options Options) error {
	if options.MaxOpenConnections <= 0 {
		return errors.New("open SQLite profile: max open connections must be positive")
	}
	if options.MutationBusyTimeout < time.Millisecond {
		return errors.New("open SQLite profile: busy timeout must be at least one millisecond")
	}
	if options.StartupDeadline <= 0 {
		return errors.New("open SQLite profile: startup deadline must be positive")
	}
	return nil
}

func dataSourceName(databasePath string, options Options) string {
	path := filepath.ToSlash(databasePath)
	if volume := filepath.VolumeName(databasePath); volume != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Add("_pragma", "busy_timeout("+strconv.FormatInt(options.MutationBusyTimeout.Milliseconds(), 10)+")")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func verifyConnection(ctx context.Context, database *sql.DB, options Options) error {
	checks := []struct {
		pragma string
		want   int
	}{
		{"foreign_keys", 1}, {"synchronous", 2},
		{"busy_timeout", int(options.MutationBusyTimeout.Milliseconds())},
	}
	for _, check := range checks {
		var actual int
		if err := database.QueryRowContext(ctx, "PRAGMA "+check.pragma).Scan(&actual); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
		if actual != check.want {
			return store.NewError(store.CodeStoreError,
				fmt.Errorf("SQLite pragma %s is %d", check.pragma, actual))
		}
	}
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if !strings.EqualFold(journalMode, "wal") {
		return store.NewError(store.CodeStoreError,
			fmt.Errorf("SQLite journal mode is %s", journalMode))
	}
	return nil
}

func validateSchema(ctx context.Context, database *sql.DB) error {
	required := []string{
		"schema_metadata", "profile_state", "accounts", "merchants", "category_groups",
		"categories", "transactions", "external_identities", "known_drills",
		"journal_operations", "operation_payloads", "operation_targets",
		"provider_binding", "provider_refresh_state", "provider_refresh_lease",
		"provider_label_allocations",
	}
	for _, table := range required {
		var count int
		if err := database.QueryRowContext(ctx, `
			SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			return mapDriverError(err, store.CodeStoreCorrupt)
		}
		if count != 1 {
			return store.NewError(store.CodeStoreCorrupt, fmt.Errorf("required table is missing"))
		}
	}
	return nil
}

func quickCheck(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(ctx, "PRAGMA quick_check")
	if err != nil {
		return mapDriverError(err, store.CodeStoreCorrupt)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var result string
		if err = rows.Scan(&result); err != nil {
			return mapDriverError(err, store.CodeStoreCorrupt)
		}
		if result != "ok" {
			return store.NewError(store.CodeStoreCorrupt, errors.New("SQLite quick check failed"))
		}
	}
	if err = rows.Err(); err != nil {
		return mapDriverError(err, store.CodeStoreCorrupt)
	}
	return nil
}

func (profile *profile) Close() error {
	profile.closeOnce.Do(func() {
		if _, err := profile.database.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			profile.closeErr = mapDriverError(err, store.CodeStoreError)
		}
		if err := profile.database.Close(); err != nil && profile.closeErr == nil {
			profile.closeErr = mapDriverError(err, store.CodeStoreError)
		}
	})
	return profile.closeErr
}

func (profile *profile) CurrentRevision(ctx context.Context) (uint64, error) {
	var revision uint64
	if err := profile.database.QueryRowContext(ctx,
		"SELECT revision FROM profile_state WHERE singleton = 1").Scan(&revision); err != nil {
		return 0, mapDriverError(err, store.CodeStoreError)
	}
	return revision, nil
}
