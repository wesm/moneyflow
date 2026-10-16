package moneyflow

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// createLegacyProfile writes a profile the way moneyflow did before profiles had IDs: a
// database at the root of the data directory and no profile.json.
func createLegacyProfile(t *testing.T, root string) {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(fixture.Demo())
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(t.Context(), committed)
	require.NoError(t, errors.Join(err, profile.Close()))
}

// requireNewerManifest makes a profile's profile.json claim a manifest version this moneyflow
// does not know.
func requireNewerManifest(t *testing.T, root, profileID string) {
	t.Helper()
	path := filepath.Join(root, "profiles", profileID, profilecatalog.ManifestFilename)
	contents, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	manifest := map[string]any{}
	require.NoError(t, json.Unmarshal(contents, &manifest))
	manifest["manifest_version"] = profilecatalog.ManifestVersion + 1
	contents, err = json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, home.WritePrivateFile(path, contents))
}

func TestImportCopiesEveryProfileAndLeavesTheSourceAlone(t *testing.T) {
	source, providerRoot := testRoot(t), t.TempDir()
	shop := createLocalProfile(t, source, "Example Shop")
	acme := createMonarchProfile(t, source, "Acme", providerRoot)
	session := filepath.Join("providers", "monarch", "session.json")
	require.NoError(t, home.WritePrivateFile(
		filepath.Join(source, "profiles", acme, session), []byte(`{"synthetic":true}`)))
	require.NoError(t, home.WritePrivateFile(
		filepath.Join(source, "profiles", shop, "audit.jsonl"), []byte(`{"synthetic":true}`+"\n")))
	createLegacyProfile(t, source)
	before := fileDigests(t, source)
	root := testRoot(t)
	instance := openTestInstance(t, root, seams{
		factories: assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})

	report, err := instance.ImportProfiles(t.Context(), source)

	require.NoError(t, err)
	assert.Empty(t, report.Skipped)
	require.Len(t, report.Imported, 3)
	byName := map[string]ImportedProfile{}
	for _, imported := range report.Imported {
		byName[imported.Name] = imported
	}
	assert.Equal(t, ImportedProfile{ID: acme, Name: "Acme", ProviderKind: "monarch"}, byName["Acme"])
	assert.Equal(t, ImportedProfile{ID: shop, Name: "Example Shop", ProviderKind: "local"},
		byName["Example Shop"])
	assert.True(t, profilecatalog.ValidProfileID(byName["Moneyflow"].ID))
	assert.Equal(t, "local", byName["Moneyflow"].ProviderKind)
	copied, err := os.ReadFile(filepath.Clean(filepath.Join(root, "profiles", acme, session)))
	require.NoError(t, err)
	assert.JSONEq(t, `{"synthetic":true}`, string(copied))
	for _, profileID := range []string{shop, acme} {
		copiedFiles := fileDigests(t, filepath.Join(root, "profiles", profileID))
		sourceFiles := fileDigests(t, filepath.Join(source, "profiles", profileID))
		delete(copiedFiles, "moneyflow.db")
		delete(sourceFiles, "moneyflow.db")
		assert.Equal(t, sourceFiles, copiedFiles, "every file but the database is copied as it is")
	}
	assert.Equal(t, 1, currentProfile(t.Context(), instance, acme).PendingEdits)
	assert.Equal(t, StateReady, currentProfile(t.Context(), instance, byName["Moneyflow"].ID).State)
	assert.Equal(t, before, fileDigests(t, source))
}

func TestImportSkipsProfilesItCannotCopy(t *testing.T) {
	root := testRoot(t)
	createLocalProfile(t, root, "Example Shop")
	source := testRoot(t)
	taken := createLocalProfile(t, source, "Example Shop")
	acme := createLocalProfile(t, source, "Acme")
	newer := createLocalProfile(t, source, "Example Merchant")
	requireNewerManifest(t, source, newer)
	busy := createLocalProfile(t, source, "Example Budget")
	open := createLocalProfile(t, source, "Example Ledger")
	broken := createLocalProfile(t, source, "Example Store")
	require.NoError(t, home.WritePrivateFile(
		filepath.Join(source, "profiles", broken, profilecatalog.ManifestFilename), []byte("{}")))
	// A legacy database that was never set up: the catalog lists it, but it cannot be copied.
	require.NoError(t, home.WritePrivateFile(filepath.Join(source, "moneyflow.db"), nil))
	lock, err := home.TryLockExisting(
		filepath.Join(source, "profiles", busy), home.LockProfile, home.LockExclusive,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	// Standalone moneyflow holds the profiles it has open with a shared lock.
	shared, err := home.TryLockExisting(
		filepath.Join(source, "profiles", open), home.LockProfile, home.LockShared,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, shared.Release()) }()
	instance := openTestInstance(t, root, seams{})

	first, firstErr := instance.ImportProfiles(t.Context(), source)
	second, secondErr := instance.ImportProfiles(t.Context(), source)

	require.NoError(t, firstErr)
	require.Equal(t, []ImportedProfile{{ID: acme, Name: "Acme", ProviderKind: "local"}},
		first.Imported)
	reasons := map[string]SkippedProfile{}
	for _, skipped := range first.Skipped {
		reasons[skipped.ID] = skipped
	}
	assert.Equal(t, SkipNameTaken, reasons[taken].Reason)
	assert.Equal(t, SkipUnsupported, reasons[newer].Reason)
	assert.Equal(t, SkipInUse, reasons[busy].Reason)
	assert.Equal(t, "Quit moneyflow and try again.", reasons[busy].Detail)
	assert.Equal(t, SkippedProfile{
		ID: open, Name: "Example Ledger", Reason: SkipInUse, Detail: "Quit moneyflow and try again.",
	}, reasons[open])
	assert.Equal(t, SkipUnsupported, reasons[broken].Reason)
	assert.Equal(t, SkipUnsupported, reasons[""].Reason, "the legacy database")
	require.NoError(t, secondErr)
	assert.Empty(t, second.Imported)
	assert.Contains(t, second.Skipped,
		SkippedProfile{ID: acme, Name: "Acme", Reason: SkipExists})
}

func TestImportRejectsAMissingSource(t *testing.T) {
	instance := openTestInstance(t, testRoot(t), seams{})

	_, err := instance.ImportProfiles(t.Context(), filepath.Join(t.TempDir(), "missing"))

	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestImportRejectsARelativeSource(t *testing.T) {
	instance := openTestInstance(t, testRoot(t), seams{})
	source := testRoot(t)
	createLocalProfile(t, source, "Example Shop")
	t.Chdir(filepath.Dir(source))

	report, err := instance.ImportProfiles(t.Context(), filepath.Base(source))

	require.Error(t, err, "a relative source would depend on the host's working directory")
	assert.Empty(t, report.Imported)
}

func TestImportWaitsForAProfileListingInsteadOfSkipping(t *testing.T) {
	root, source := testRoot(t), testRoot(t)
	createMonarchProfile(t, root, "Acme", t.TempDir())
	shop := createLocalProfile(t, source, "Example Shop")
	instance, hold := openWithHeldSurvey(t, root)
	listed := make(chan error, 1)
	go func() { _, err := instance.list(t.Context()); listed <- err }()
	<-hold.started
	var report ImportReport
	imported := make(chan error, 1)
	go func() {
		var err error
		report, err = instance.ImportProfiles(t.Context(), source)
		imported <- err
	}()

	waitForCatalogWriter(t, instance, imported)
	hold.Release()

	require.NoError(t, <-listed)
	require.NoError(t, <-imported)
	assert.Empty(t, report.Skipped)
	assert.Equal(t, []ImportedProfile{{ID: shop, Name: "Example Shop", ProviderKind: "local"}},
		report.Imported)
}

func TestCloseStopsAnImportThatWaitsForTheCatalog(t *testing.T) {
	root, source := testRoot(t), testRoot(t)
	createMonarchProfile(t, root, "Acme", t.TempDir())
	shop := createLocalProfile(t, source, "Example Shop")
	instance, hold := openWithHeldSurvey(t, root)
	listed := make(chan error, 1)
	go func() { _, err := instance.list(t.Context()); listed <- err }()
	<-hold.started
	imported := make(chan error, 1)
	go func() {
		_, err := instance.ImportProfiles(t.Context(), source)
		imported <- err
	}()
	waitForCatalogWriter(t, instance, imported)
	closed := make(chan error, 1)
	go func() { closed <- instance.Close() }()
	<-instance.ctx.Done() // Close marks the instance closed before it cancels this context.

	hold.Release()

	require.NoError(t, <-listed)
	require.ErrorIs(t, <-imported, ErrClosed)
	require.NoError(t, <-closed)
	assert.NoDirExists(t, filepath.Join(root, "profiles", shop))
}

func TestImportSkipsAProfileWhileAnotherProcessHoldsTheCatalog(t *testing.T) {
	root, source := testRoot(t), testRoot(t)
	instance := openTestInstance(t, root, seams{})
	shop := createLocalProfile(t, source, "Example Shop")
	lock, err := home.TryLock(root, home.LockCatalog, home.LockShared)
	require.NoError(t, err)

	held, heldErr := instance.ImportProfiles(t.Context(), source)
	require.NoError(t, lock.Release())
	released, releasedErr := instance.ImportProfiles(t.Context(), source)

	require.NoError(t, heldErr)
	assert.Empty(t, held.Imported)
	assert.Equal(t, []SkippedProfile{{
		ID: shop, Name: "Example Shop", Reason: SkipInUse, Detail: "Quit moneyflow and try again.",
	}}, held.Skipped)
	require.NoError(t, releasedErr)
	assert.Equal(t, []ImportedProfile{{ID: shop, Name: "Example Shop", ProviderKind: "local"}},
		released.Imported, "the import works once the other process lets go")
}

// activateLegacyProfile gives the legacy profile at root an ID, as moneyflow does when it first
// opens one, and returns the ID.
func activateLegacyProfile(t *testing.T, root string) string {
	t.Helper()
	paths, err := home.ResolveCatalogRoot(root, nil, "")
	require.NoError(t, err)
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: cryptorand.Reader, Now: time.Now, Version: "test",
	})
	require.NoError(t, err)
	entry, err := catalog.Activate(t.Context(), "")
	require.NoError(t, err)
	return entry.ID
}

func TestImportKeepsTheIDOfAProfileAtTheSourceRoot(t *testing.T) {
	// A legacy profile keeps its manifest at the root of its data directory, which may have any
	// name, even one that looks like another profile's ID, and which a host may spell with a
	// trailing separator.
	directory, err := profilecatalog.NewProfileID(cryptorand.Reader)
	require.NoError(t, err)
	source := filepath.Join(t.TempDir(), directory)
	createLegacyProfile(t, source)
	legacy := activateLegacyProfile(t, source)
	instance := openTestInstance(t, testRoot(t), seams{})

	report, err := instance.ImportProfiles(t.Context(), source+string(filepath.Separator))

	require.NoError(t, err)
	assert.Equal(t, []ImportedProfile{{ID: legacy, Name: "Moneyflow", ProviderKind: "local"}},
		report.Imported)
}
