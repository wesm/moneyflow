package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/profiletransfer"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestProfileTransferCommandRoundTrip(t *testing.T) {
	sourceHome := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", sourceHome)
	catalog := newCommandTestCatalog(t, sourceHome)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{DisplayName: "Source", ProviderKind: "local"})
	require.NoError(t, err)
	source, err := sqlite.Open(t.Context(), entry.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions := fixture.Generate(42, 32)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = source.CreateSeededProfile(t.Context(), committed)
	require.NoError(t, err)
	_, err = source.Append(t.Context(), 1, domain.Operation{ID: "operation-export-test", Type: domain.OperationTransactionHide, PayloadVersion: 1, CreatedRevision: 1, CreatedAt: time.Unix(100, 0).UTC(), Targets: []domain.EntityID{committed.Transactions[0].ID}, HideToggle: &domain.HideTogglePayload{}})
	require.NoError(t, err)
	require.NoError(t, source.Close())
	blockedOutput := filepath.Join(t.TempDir(), "blocked.jsonl")
	_, _, err = executeCommand(t, "profile", "export", "--profile", "Source", "--output", blockedOutput)
	require.ErrorContains(t, err, "active edits")
	require.NoFileExists(t, blockedOutput)
	source, err = sqlite.Open(t.Context(), entry.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = source.MoveCursor(t.Context(), 2, -1)
	require.NoError(t, err)
	require.NoError(t, source.Close())
	before, err := os.ReadFile(entry.ProfilePaths().Database) //nolint:gosec // Synthetic profile.
	require.NoError(t, err)
	t.Chdir(t.TempDir())
	stdout, stderr, err := executeCommand(t, "profile", "export", "--profile", "Source", "--output", "saved.jsonl")
	require.NoError(t, err)
	require.Contains(t, stdout, "transactions")
	require.Contains(t, stdout, "excluded inactive redo operations: 1")
	require.Contains(t, stderr, "unencrypted")
	_, _, err = executeCommand(t, "profile", "export", "--profile", "Source", "--output", "saved.jsonl")
	require.Error(t, err)
	after, err := os.ReadFile(entry.ProfilePaths().Database) //nolint:gosec // Synthetic profile.
	require.NoError(t, err)
	require.Equal(t, before, after)
	destinationHome := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", destinationHome)
	stdout, stderr, err = executeCommand(t, "profile", "import", "--input", "saved.jsonl", "--name", "Imported")
	require.NoError(t, err)
	require.Contains(t, stdout, "Imported")
	require.Contains(t, stderr, "other profile commands must wait")
	destinationCatalog := newCommandTestCatalog(t, destinationHome)
	imported, err := destinationCatalog.Resolve(t.Context(), "Imported")
	require.NoError(t, err)
	require.NotEqual(t, entry.ID, imported.ID)
	target, err := sqlite.Open(t.Context(), imported.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	loaded, err := target.Load(t.Context())
	require.NoError(t, err)
	require.ElementsMatch(t, committed.Transactions, loaded.Committed.Transactions)
	require.ElementsMatch(t, committed.Categories, loaded.Committed.Categories)
	require.Empty(t, loaded.Journal)
	require.NoError(t, target.Close())
	require.NoDirExists(t, filepath.Join(imported.Root, "providers"))
	_, _, err = executeCommand(t, "profile", "import", "--input", "saved.jsonl", "--name", "Imported")
	require.Error(t, err)
}

func TestProfileTransferCommandRejectsMissingAndBusySource(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	out := filepath.Join(t.TempDir(), "export.jsonl")
	for _, args := range [][]string{
		{"profile", "export", "--output", out},
		{"profile", "export", "--profile", "Missing", "--output", out},
	} {
		_, _, err := executeCommand(t, args...)
		require.Error(t, err)
		require.NoFileExists(t, out)
	}
	catalog := newCommandTestCatalog(t, root)
	entries, err := catalog.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, entries)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{DisplayName: "Busy", ProviderKind: "local"})
	require.NoError(t, err)
	lock, err := home.TryLock(entry.Root, home.LockProfile, home.LockShared)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	_, _, err = executeCommand(t, "profile", "export", "--profile", "Busy", "--output", out)
	require.Error(t, err)
	require.NoFileExists(t, out)
}

func TestProfileTransferCommandFailedPopulationStaysInvisible(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	var document profiletransfer.Document
	document.Header = profiletransfer.Header{Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: "test", ProviderKind: "local", ExportedAt: time.Unix(100, 0).UTC(), SourceName: "Example"}
	document.State.Snapshot.Committed = domain.CommittedProfile{
		Groups:     []domain.CategoryGroup{{ID: domain.UncategorizedGroupID, Label: domain.UncategorizedLabel, CollisionKey: domain.UncategorizedCollisionKey, Protected: true}},
		Categories: domain.ProtectedCategories(),
	}
	// The codec checks shape; SQLite owns the positive batch-version constraint.
	document.State.Provider.Lineage = []store.ProviderIdentityLineage{{Kind: domain.EntityKindMerchant, Namespace: "example", ExternalID: "example", PriorLocalID: "prior", CurrentLocalID: "current", ProviderLabel: "Example", Disposition: "alias", BatchVersion: 0}}
	var encoded bytes.Buffer
	require.NoError(t, profiletransfer.Encode(&encoded, document))
	input := filepath.Join(t.TempDir(), "input.jsonl")
	require.NoError(t, os.WriteFile(input, encoded.Bytes(), 0600))
	_, _, err := executeCommand(t, "profile", "import", "--input", input, "--name", "Rejected")
	require.Error(t, err)
	catalog := newCommandTestCatalog(t, root)
	entries, err := catalog.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, entries)
	children, err := os.ReadDir(catalog.Paths().Profiles)
	require.NoError(t, err)
	require.Empty(t, children)
}

func TestProfileRenameCommandRenamesInPlace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	catalog := newCommandTestCatalog(t, root)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Moneyflow", ProviderKind: "monarch",
	})
	require.NoError(t, err)

	stdout, _, err := executeCommand(t, "profile", "rename", "--profile", "moneyflow", "--name", "Monarch")

	require.NoError(t, err)
	require.Contains(t, stdout, `Renamed "Moneyflow" to "Monarch" (`+entry.ID+`).`)
	renamed, err := catalog.Resolve(t.Context(), "Monarch")
	require.NoError(t, err)
	require.Equal(t, entry.ID, renamed.ID)
}

func TestProfileRenameCommandRenamesAManifestlessRootProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	catalog := newCommandTestCatalog(t, root)
	require.NoError(t, sqlite.InstallPristineProfile(
		t.Context(), catalog.Paths().LegacyProfile(), sqlite.DefaultOptions,
	))

	_, _, err := executeCommand(t, "profile", "rename", "--profile", "Local", "--name", "Example Budget")

	require.NoError(t, err)
	renamed, err := catalog.Resolve(t.Context(), "Example Budget")
	require.NoError(t, err)
	require.Equal(t, catalog.Paths().Root, renamed.Root)
	require.NotEmpty(t, renamed.ID)
}

func TestProfileRenameCommandRejectsMissingFlagsAndBusyProfile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	for _, args := range [][]string{
		{"profile", "rename", "--name", "Monarch"},
		{"profile", "rename", "--profile", "Moneyflow"},
	} {
		_, _, err := executeCommand(t, args...)
		require.ErrorContains(t, err, "profile rename requires --profile and --name")
	}
	catalog := newCommandTestCatalog(t, root)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Moneyflow", ProviderKind: "monarch",
	})
	require.NoError(t, err)
	lock, err := home.TryLock(entry.Root, home.LockProfile, home.LockShared)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()

	_, _, err = executeCommand(t, "profile", "rename", "--profile", "Moneyflow", "--name", "Monarch")

	require.ErrorContains(t, err, "close other Moneyflow processes using this profile")
	unchanged, err := catalog.Resolve(t.Context(), "Moneyflow")
	require.NoError(t, err)
	require.Equal(t, entry.ID, unchanged.ID)
}

func TestProfileRenameCommandRefusesAnInterruptedRootRecovery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", root)
	paths, err := home.ResolveCatalogRoot(root, nil, "")
	require.NoError(t, err)
	legacy := paths.LegacyProfile()
	require.NoError(t, sqlite.InstallPristineProfile(t.Context(), legacy, sqlite.DefaultOptions))
	database, err := sql.Open("sqlite", legacy.Database)
	require.NoError(t, err)
	_, err = database.ExecContext(t.Context(),
		"UPDATE schema_metadata SET schema_version = ? WHERE singleton = 1",
		sqlite.CurrentSchemaVersion-1)
	require.NoError(t, err)
	require.NoError(t, database.Close())
	interrupted := errors.New("synthetic recovery interruption")
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: rand.Reader, Now: time.Now, Version: "test",
		RecoveryFault: func(point profilecatalog.RecoveryFaultPoint) error {
			if point == profilecatalog.RecoveryAfterSchemaInstall {
				return interrupted
			}
			return nil
		},
	})
	require.NoError(t, err)
	plan, err := catalog.RecoveryPlan(t.Context(), profilecatalog.LegacyKey)
	require.NoError(t, err)
	_, err = catalog.Recreate(t.Context(), profilecatalog.RecoveryRequest{Plan: plan, Confirmed: true})
	require.ErrorIs(t, err, interrupted)

	_, _, err = executeCommand(t, "profile", "rename", "--profile", "Unknown profile", "--name", "Monarch")

	require.ErrorContains(t, err, "finish this profile's recovery first")
	require.ErrorContains(t, err, "moneyflow tui")
	require.NoFileExists(t, filepath.Join(paths.Root, profilecatalog.ManifestFilename))
}
