package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/tui"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func TestOpenProfileCreatesAndReopensEmptyPersistentProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "profile")

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	assert.False(t, opened.Demo)
	assert.Equal(t, paths.Database, opened.Path)
	assert.Equal(t, uint64(0), opened.Service.Revision())
	result, err := opened.Service.Query(app.NewSession())
	require.NoError(t, err)
	assert.Empty(t, result.AggregateRows)
	require.NoError(t, opened.Close())
	require.NoError(t, opened.Close(), "profile cleanup must be idempotent")
	_, err = os.Stat(opened.Path)
	require.NoError(t, err, "persistent profile must survive command shutdown")

	reopened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	assert.Equal(t, uint64(0), reopened.Service.Revision())
	require.NoError(t, reopened.Close())
}

func TestOpenProfileFinalizesAndUsesExistingRootLevelProfile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "profile")
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	require.NoError(t, sqlite.InstallPristineProfile(ctx, paths, sqlite.DefaultOptions))
	assert.NoFileExists(t, filepath.Join(root, profilecatalog.ManifestFilename))

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	assert.Equal(t, paths.Database, opened.Path)
	require.NoError(t, opened.Close())
	manifest, err := profilecatalog.ReadManifest(filepath.Join(root, profilecatalog.ManifestFilename))
	require.NoError(t, err)
	assert.Equal(t, "Moneyflow", manifest.DisplayName)
}

func TestOpenProfileUsesSolePersistentCatalogEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "catalog")
	catalog := newCommandTestCatalog(t, root)
	entry, err := catalog.Create(ctx, profilecatalog.CreateRequest{
		DisplayName: "Primary", ProviderKind: "local",
	})
	require.NoError(t, err)

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	assert.Equal(t, entry.ProfilePaths().Database, opened.Path)
	require.NoError(t, opened.Close())
}

func TestOpenProfileHoldsSharedLifecycleLockUntilClose(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "catalog")
	opened, err := openProfile(context.Background(), ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)

	_, err = home.TryLock(opened.Paths.Root, home.LockProfile, home.LockExclusive)
	assert.ErrorIs(t, err, home.ErrLockBusy)
	require.NoError(t, opened.Close())
	exclusive, err := home.TryLock(opened.Paths.Root, home.LockProfile, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, exclusive.Release())
}

func TestOpenProfileRejectsAmbiguousPersistentCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "catalog")
	catalog := newCommandTestCatalog(t, root)
	first, err := catalog.Create(ctx, profilecatalog.CreateRequest{
		DisplayName: "First", ProviderKind: "local",
	})
	require.NoError(t, err)
	second, err := catalog.Create(ctx, profilecatalog.CreateRequest{
		DisplayName: "Second", ProviderKind: "local",
	})
	require.NoError(t, err)
	firstBefore := commandProfileRevision(t, first.ProfilePaths())
	secondBefore := commandProfileRevision(t, second.ProfilePaths())

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	assert.Nil(t, opened.Service)
	require.Error(t, err)
	assert.Equal(t, profilecatalog.CodeProfileAmbiguous, profilecatalog.CodeOf(err))
	assert.NotContains(t, err.Error(), first.DisplayName)
	assert.NotContains(t, err.Error(), second.DisplayName)
	assert.Equal(t, firstBefore, commandProfileRevision(t, first.ProfilePaths()))
	assert.Equal(t, secondBefore, commandProfileRevision(t, second.ProfilePaths()))
}

func TestOpenProfileExplainsHowToRecoverAnIncompatiblePreviewSchema(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "profile")
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	database, err := sql.Open("sqlite", paths.Database)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx,
		"UPDATE schema_metadata SET schema_version = 2 WHERE singleton = 1")
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err = openProfile(ctx, ProfileOptions{ExplicitHome: root})
	assert.Nil(t, opened.Service)
	var storageFailure *store.Error
	require.ErrorAs(t, err, &storageFailure)
	assert.Equal(t, store.CodeSchemaIncompatible, storageFailure.Code)
	assert.ErrorContains(t, err, "profile directory "+strconv.Quote(paths.Root))
	assert.ErrorContains(t, err, "does not migrate preview profiles")
	assert.ErrorContains(t, err, "move the complete directory to a backup location")
	assert.ErrorContains(t, err, "rerun the command")
}

func TestOpenProfileRejectsPriorV3ProviderBindingShapeBeforeServiceLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "profile")
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	database, err := sql.Open("sqlite", paths.Database)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		ALTER TABLE provider_binding DROP COLUMN currency;
		ALTER TABLE provider_binding DROP COLUMN scale;
		UPDATE schema_metadata SET schema_version = 3 WHERE singleton = 1;
	`)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err = openProfile(ctx, ProfileOptions{ExplicitHome: root})
	assert.Nil(t, opened.Service)
	var storageFailure *store.Error
	require.ErrorAs(t, err, &storageFailure)
	assert.Equal(t, store.CodeSchemaIncompatible, storageFailure.Code)
	assert.ErrorContains(t, err, "profile directory "+strconv.Quote(paths.Root))
	assert.ErrorContains(t, err, "move the complete directory to a backup location")
	assert.NotContains(t, err.Error(), "load service")
}

func TestOpenProfileExplainsHowToRecoverAnUnsupportedJournalPayload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "profile")
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)

	opened, err := openProfile(ctx, ProfileOptions{ExplicitHome: root})
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	database, err := sql.Open("sqlite", paths.Database)
	require.NoError(t, err)
	_, err = database.ExecContext(ctx, `
		INSERT INTO journal_operations(
			id, sequence, operation_type, payload_version, creation_revision, created_at_unix_ms
		) VALUES ('operation_unsupported', 1, 'transaction.hide-toggle', 2, 0, 1);
		INSERT INTO operation_payloads(operation_id, payload_version, payload_json)
		VALUES ('operation_unsupported', 2, '{}');
		INSERT INTO operation_targets(operation_id, ordinal, entity_id)
		VALUES ('operation_unsupported', 0, 'transaction_a');
		UPDATE profile_state SET revision = 1, journal_cursor = 1 WHERE singleton = 1;
	`)
	require.NoError(t, err)
	require.NoError(t, database.Close())

	opened, err = openProfile(ctx, ProfileOptions{ExplicitHome: root})
	assert.Nil(t, opened.Service)
	var storageFailure *store.Error
	require.ErrorAs(t, err, &storageFailure)
	assert.Equal(t, store.CodeSchemaIncompatible, storageFailure.Code)
	assert.ErrorContains(t, err, "load service")
	assert.ErrorContains(t, err, "profile directory "+strconv.Quote(paths.Root))
	assert.ErrorContains(t, err, "move the complete directory to a backup location")
}

func TestOpenProfileDemoSeedsUniquePrivateTemporaryProfileAndCleansIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	first, err := openProfile(ctx, ProfileOptions{Demo: true})
	require.NoError(t, err)
	second, err := openProfile(ctx, ProfileOptions{Demo: true})
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	t.Cleanup(func() { _ = second.Close() })

	assert.True(t, first.Demo)
	assert.NotEqual(t, first.Path, second.Path)
	assert.Equal(t, uint64(1), first.Service.Revision())
	result, err := first.Service.Query(app.NewSession())
	require.NoError(t, err)
	assert.NotEmpty(t, result.AggregateRows)
	root := filepath.Dir(first.Path)
	info, err := os.Stat(root)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0), info.Mode().Perm()&0o077)

	require.NoError(t, first.Close())
	require.NoError(t, first.Close())
	_, err = os.Stat(root)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Dir(second.Path))
	require.NoError(t, err, "closing one demo must not remove another demo")
}

func TestOpenTemporaryContractProfileUsesEmptySQLiteAndCleansIt(t *testing.T) {
	t.Parallel()
	opened, err := openTemporaryContractProfile(context.Background())
	require.NoError(t, err)
	assert.False(t, opened.Demo)
	assert.Equal(t, uint64(0), opened.Service.Revision())
	root := filepath.Dir(opened.Path)
	_, err = os.Stat(root)
	require.NoError(t, err)
	require.NoError(t, opened.Close())
	require.NoError(t, opened.Close())
	_, err = os.Stat(root)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestOpenProfilePersistsPendingJournalAcrossRestart(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	require.NoError(t, profile.Close())

	transactionID := domain.EntityID(transactions[0].ID)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	first := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		RunTUI: func(
			commandContext context.Context,
			service *app.Service,
			_ app.Session,
			_ tui.Options,
			_ IOStreams,
		) error {
			mutation, mutationErr := service.Mutate(commandContext, app.MutationRequest{
				Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
				State: state, Selection: app.EmptySelection(),
				Target: &app.RowTarget{
					Kind: app.IdentityTransaction, Identity: string(transactionID),
				},
			})
			require.NoError(t, mutationErr)
			assert.Equal(t, 1, mutation.Pending.ActiveOperations)
			return nil
		},
	})
	first.SetArgs([]string{"tui"})
	require.NoError(t, first.Execute())

	second := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		RunTUI: func(
			commandContext context.Context,
			service *app.Service,
			_ app.Session,
			_ tui.Options,
			_ IOStreams,
		) error {
			review, reviewErr := service.Review(
				commandContext, service.Revision(), app.ReviewWindow{Limit: 20},
			)
			require.NoError(t, reviewErr)
			require.Len(t, review.Operations, 1)
			assert.Equal(t, domain.OperationTransactionHide, review.Operations[0].Type)
			return nil
		},
	})
	second.SetArgs([]string{"tui"})
	require.NoError(t, second.Execute())
}

func TestCommandsOpenExpectedProfileAndAlwaysCloseIt(t *testing.T) {
	t.Parallel()
	fixturePath := filepath.Join("..", "..", "testdata", "parity", "transactions.json")
	for _, test := range []struct {
		name string
		args []string
		want ProfileOptions
	}{
		{name: "tui", args: []string{"tui"}, want: ProfileOptions{}},
		{name: "tui demo", args: []string{"tui", "--demo"}, want: ProfileOptions{Demo: true}},
		{
			name: "tui fixture", args: []string{"tui", "--fixture", fixturePath},
			want: ProfileOptions{Demo: true, FixturePath: fixturePath},
		},
		{name: "web demo", args: []string{"web", "--demo", "--open=false"}, want: ProfileOptions{Demo: true}},
		{
			name: "web fixture", args: []string{"web", "--fixture", fixturePath, "--open=false"},
			want: ProfileOptions{Demo: true, FixturePath: fixturePath},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), profileContextKey{}, test.name)
			service, err := app.NewService(nil)
			require.NoError(t, err)
			var got ProfileOptions
			var closes int
			streams := IOStreams{
				In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
				OpenProfile: func(openCtx context.Context, options ProfileOptions) (OpenedProfile, error) {
					assert.Equal(t, test.name, openCtx.Value(profileContextKey{}))
					got = options
					return OpenedProfile{Service: service, Close: func() error { closes++; return nil }}, nil
				},
				RunTUI: func(context.Context, *app.Service, app.Session, tui.Options, IOStreams) error { return nil },
				RunWeb: func(context.Context, *app.Service, WebOptions, IOStreams) error { return nil },
			}
			command := newRootCommand(streams)
			command.SetContext(ctx)
			command.SetArgs(test.args)
			require.NoError(t, command.Execute())
			assert.Equal(t, test.want, got)
			assert.Equal(t, 1, closes)
		})
	}
}

func TestTUICommandUsesEmptySQLiteProfileFromEnvironment(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	var rows int
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		RunTUI: func(
			_ context.Context,
			service *app.Service,
			_ app.Session,
			_ tui.Options,
			_ IOStreams,
		) error {
			result, err := service.Query(app.NewSession())
			require.NoError(t, err)
			rows = len(result.AggregateRows)
			return nil
		},
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"tui"})
	require.NoError(t, command.Execute())
	assert.Zero(t, rows)
	_, err := os.Stat(filepath.Join(root, "moneyflow.db"))
	require.NoError(t, err)
}

func TestCommandClosesProfileWhenRunnerFails(t *testing.T) {
	t.Parallel()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	runnerFailure := errors.New("runner failed")
	var closes int
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
			return OpenedProfile{Service: service, Close: func() error { closes++; return nil }}, nil
		},
		RunTUI: func(context.Context, *app.Service, app.Session, tui.Options, IOStreams) error {
			return runnerFailure
		},
	})
	command.SetArgs([]string{"tui"})
	err = command.Execute()
	assert.ErrorIs(t, err, runnerFailure)
	assert.Equal(t, 1, closes)
}

type profileContextKey struct{}

func newCommandTestCatalog(t *testing.T, root string) *profilecatalog.Catalog {
	t.Helper()
	paths, err := home.ResolveCatalogRoot(root, nil, "")
	require.NoError(t, err)
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths,
		Random: bytes.NewReader(append(
			bytes.Repeat([]byte{0x61}, 16), bytes.Repeat([]byte{0x62}, 16)...,
		)),
		Now: func() time.Time {
			return time.Date(2026, 8, 17, 20, 0, 0, 0, time.UTC)
		},
		Version: "test",
	})
	require.NoError(t, err)
	return catalog
}

func commandProfileRevision(t *testing.T, paths home.Paths) uint64 {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(paths.Database))
	require.NoError(t, err)
	var revision uint64
	require.NoError(t, database.QueryRow(
		"SELECT revision FROM profile_state WHERE singleton = 1",
	).Scan(&revision))
	require.NoError(t, database.Close())
	return revision
}
