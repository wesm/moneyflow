package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

// SchemaStatus is the shallow install-only classification of one profile database.
type SchemaStatus string

const (
	// SchemaEmpty means the database is missing or contains no application schema.
	SchemaEmpty SchemaStatus = "empty"
	// SchemaCurrent means the exact installed schema is compatible with this binary.
	SchemaCurrent SchemaStatus = "current"
	// SchemaOlder means the installed schema predates this binary and cannot be migrated.
	SchemaOlder SchemaStatus = "older"
	// SchemaNewer means the installed schema requires a newer Moneyflow binary.
	SchemaNewer SchemaStatus = "newer"
)

// Inspection contains only local profile lifecycle facts needed by the catalog.
type Inspection struct {
	Schema       SchemaStatus
	Pristine     bool
	Bound        bool
	ProviderKind string
}

// ErrMaintenanceWouldOverwrite prevents pristine installation over an existing database.
var ErrMaintenanceWouldOverwrite = errors.New("profile maintenance would overwrite a database")

// InspectProfile classifies a profile without loading its financial rows.
func InspectProfile(
	ctx context.Context,
	paths home.Paths,
	options Options,
) (Inspection, error) {
	if err := validateMaintenancePaths(paths); err != nil {
		return Inspection{}, err
	}
	if err := home.PreparePrivateRoot(paths.Root); err != nil {
		return Inspection{}, store.NewError(store.CodeStoreError, err)
	}
	info, err := os.Lstat(paths.Database)
	if errors.Is(err, os.ErrNotExist) {
		return Inspection{Schema: SchemaEmpty, Pristine: true}, nil
	}
	if err != nil {
		return Inspection{}, store.NewError(store.CodeStoreError, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Inspection{}, store.NewError(
			store.CodeStoreCorrupt, errors.New("profile database is not a regular file"),
		)
	}
	if info.Size() == 0 {
		return Inspection{Schema: SchemaEmpty, Pristine: true}, nil
	}

	database, pinned, err := openMaintenanceDatabase(
		ctx, paths, options, store.CodeStoreCorrupt, true,
	)
	if err != nil {
		return Inspection{}, err
	}
	defer func() { _ = errors.Join(database.Close(), pinned.Close()) }()
	state, err := inspectSchema(ctx, database)
	if err != nil {
		return Inspection{}, mapDriverError(err, store.CodeStoreCorrupt)
	}
	inspection := Inspection{Schema: exportedSchemaStatus(state)}
	if state != schemaCurrent {
		return inspection, nil
	}
	if err = validateSchema(ctx, database); err != nil {
		return Inspection{}, err
	}
	populated, err := profilePopulated(ctx, database)
	if err != nil {
		return Inspection{}, mapDriverError(err, store.CodeStoreCorrupt)
	}
	inspection.Pristine = !populated
	var providerKind string
	err = database.QueryRowContext(ctx,
		"SELECT kind FROM provider_binding WHERE singleton = 1",
	).Scan(&providerKind)
	switch {
	case err == nil:
		inspection.Bound = true
		inspection.ProviderKind = providerKind
	case errors.Is(err, sql.ErrNoRows):
	default:
		return Inspection{}, mapDriverError(err, store.CodeStoreCorrupt)
	}
	return inspection, nil
}

// CheckpointProfile truncates an existing database's write-ahead log without schema validation.
func CheckpointProfile(ctx context.Context, paths home.Paths, options Options) error {
	if err := validateMaintenancePaths(paths); err != nil {
		return err
	}
	if err := home.PreparePrivateRoot(paths.Root); err != nil {
		return store.NewError(store.CodeStoreError, err)
	}
	info, err := os.Lstat(paths.Database)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return store.NewError(store.CodeStoreError, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return store.NewError(store.CodeStoreCorrupt, errors.New("profile database is not regular"))
	}
	if info.Size() == 0 {
		return nil
	}
	database, pinned, err := openMaintenanceDatabase(
		ctx, paths, options, store.CodeStoreCorrupt, false,
	)
	if err != nil {
		return err
	}
	if _, err = database.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		err = mapDriverError(err, store.CodeStoreError)
	}
	return errors.Join(err, database.Close(), pinned.Close())
}

// InstallPristineProfile installs the exact current schema only into a missing or empty database.
func InstallPristineProfile(ctx context.Context, paths home.Paths, options Options) error {
	if err := validateMaintenancePaths(paths); err != nil {
		return err
	}
	info, err := os.Lstat(paths.Database)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return store.NewError(store.CodeStoreError, err)
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular():
		return store.NewError(store.CodeStoreCorrupt, errors.New("profile database is not regular"))
	case info.Size() != 0:
		return ErrMaintenanceWouldOverwrite
	}
	handle, err := Open(ctx, paths, options)
	if err != nil {
		return err
	}
	if err = handle.Close(); err != nil {
		return err
	}
	inspection, err := InspectProfile(ctx, paths, options)
	if err != nil {
		return err
	}
	if inspection.Schema != SchemaCurrent || !inspection.Pristine {
		return store.NewError(
			store.CodeStoreCorrupt,
			errors.New("installed profile did not verify as current and pristine"),
		)
	}
	return nil
}

func openMaintenanceDatabase(
	ctx context.Context,
	paths home.Paths,
	options Options,
	fallback store.ErrorCode,
	readOnly bool,
) (*sql.DB, *os.File, error) {
	if err := validateOptions(options); err != nil {
		return nil, nil, err
	}
	pinned, err := home.OpenPrivateFile(paths.Database)
	if err != nil {
		return nil, nil, store.NewError(fallback, err)
	}
	dsn := dataSourceName(paths.Database, options)
	if readOnly {
		dsn = inspectionDataSourceName(paths.Database, options)
	}
	database, err := sql.Open(driverName, dsn)
	if err != nil {
		_ = pinned.Close()
		return nil, nil, mapDriverError(err, fallback)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	if err = database.PingContext(ctx); err != nil {
		_ = database.Close()
		_ = pinned.Close()
		return nil, nil, mapDriverError(err, fallback)
	}
	openedInfo, err := pinned.Stat()
	if err != nil {
		_ = database.Close()
		_ = pinned.Close()
		return nil, nil, store.NewError(fallback, err)
	}
	pathInfo, err := os.Stat(paths.Database)
	if err != nil || !os.SameFile(openedInfo, pathInfo) {
		_ = database.Close()
		_ = pinned.Close()
		return nil, nil, store.NewError(fallback, errors.New("profile database changed while opening"))
	}
	return database, pinned, nil
}

func inspectionDataSourceName(databasePath string, options Options) string {
	path := filepath.ToSlash(databasePath)
	if volume := filepath.VolumeName(databasePath); volume != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	query := url.Values{}
	query.Set("mode", "ro")
	if _, err := os.Lstat(databasePath + "-wal"); errors.Is(err, os.ErrNotExist) {
		query.Set("immutable", "1")
	}
	query.Add("_pragma", "query_only(1)")
	query.Add("_pragma", "busy_timeout("+strconv.FormatInt(
		options.MutationBusyTimeout.Milliseconds(), 10,
	)+")")
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

func validateMaintenancePaths(paths home.Paths) error {
	if paths.Root == "" || paths.Database == "" ||
		!filepath.IsAbs(paths.Root) || !filepath.IsAbs(paths.Database) {
		return errors.New("profile maintenance: paths must be absolute")
	}
	if filepath.Clean(paths.Database) != filepath.Join(filepath.Clean(paths.Root), "moneyflow.db") {
		return errors.New("profile maintenance: database is outside the selected root")
	}
	return nil
}

func exportedSchemaStatus(state schemaState) SchemaStatus {
	switch state {
	case schemaEmpty:
		return SchemaEmpty
	case schemaCurrent:
		return SchemaCurrent
	case schemaOlder:
		return SchemaOlder
	case schemaNewer:
		return SchemaNewer
	default:
		panic(fmt.Sprintf("unknown schema state %d", state))
	}
}
