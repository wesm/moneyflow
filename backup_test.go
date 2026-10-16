package moneyflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/uitest"
)

func TestBackupVerifyAndRestoreKeepEveryProfile(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	require.NoError(t, home.WritePrivateFile(
		filepath.Join(root, "profiles", acme, "providers", "monarch", "session.json"),
		[]byte(`{"synthetic":true}`)))
	shop := createLocalProfile(t, root, "Example Shop")
	factories := assembly.Factories{Monarch: syntheticMonarch(providerRoot)}
	instance := openTestInstance(t, root, seams{factories: factories})
	require.Equal(t, StateReady, currentProfile(t.Context(), instance, acme).State)
	backup := filepath.Join(t.TempDir(), "backup")

	require.NoError(t, instance.Backup(t.Context(), backup))
	require.NoError(t, Verify(t.Context(), backup))
	require.NoError(t, instance.Close())
	restored := openTestInstance(t, backup, seams{factories: factories})

	assert.NoDirExists(t, filepath.Join(backup, "profiles", acme, "providers"))
	restoredAcme := currentProfile(t.Context(), restored, acme)
	assert.Equal(t, StateNeedsReconnect, restoredAcme.State)
	assert.Equal(t, 1, restoredAcme.PendingEdits)
	assert.Equal(t, StateReady, currentProfile(t.Context(), restored, shop).State)
}

func TestARestoredBackupDoesNotClaimAFullRefreshItsDatabaseLacks(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	// Past Monarch's 30 days for complete history.
	advanceProvider(t, providerRoot)
	source, err := uitest.OpenProvider(providerRoot)
	require.NoError(t, err)
	factories := assembly.Factories{Monarch: sharedMonarch(source)}
	var instance *Instance
	var refreshed *httptest.ResponseRecorder
	// A complete-history refresh commits and records itself while the backup copies Acme.
	instance = openTestInstance(t, root, seams{
		inspectSession: sessionPresent, factories: factories,
		betweenBackupCopies: func() {
			body := manualRefresh()
			body.CompleteHistory = true
			refreshed = serveAPI(t, instance, http.MethodPost,
				"api/v1/profiles/"+acme+"/provider/refresh", encode(t, body))
		},
	})
	backup := filepath.Join(t.TempDir(), "backup")

	require.NoError(t, instance.Backup(t.Context(), backup))

	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
	require.NoError(t, instance.Close())
	restored := openTestInstance(t, backup, seams{
		inspectSession: sessionPresent, factories: factories,
	})
	response := serveAPI(t, restored, http.MethodGet,
		"api/v1/profiles/"+acme+"/provider/status", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var status api.ProviderStatusResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &status))
	assert.Equal(t, "full", status.RefreshDue, "the next refresh reads complete history")
}

func TestVerifyWritesNothingAndRejectsCredentials(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	backup := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, instance.Backup(t.Context(), backup))
	before := fileDigests(t, backup)

	require.NoError(t, Verify(t.Context(), backup))

	assert.Equal(t, before, fileDigests(t, backup))
	assert.NoFileExists(t, filepath.Join(backup, "profiles", shop, "moneyflow.db-shm"))
	credentials := filepath.Join(backup, "profiles", shop, "providers", "ynab", "credentials.enc")
	require.NoError(t, home.WritePrivateFile(credentials, []byte("synthetic")))
	require.ErrorContains(t, Verify(t.Context(), backup), "providers does not belong in a backup")
}

func TestVerifyRejectsARenamedProfileDirectory(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	backup := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, instance.Backup(t.Context(), backup))
	renamed := "profile_" + strings.Repeat("a", 26)
	require.NoError(t, os.Rename(
		filepath.Join(backup, "profiles", shop), filepath.Join(backup, "profiles", renamed)))

	err := Verify(t.Context(), backup)

	require.ErrorContains(t, err, "verify backup: profile "+renamed)
	require.ErrorContains(t, err, string(profilecatalog.CodeProfileInvalid))
}

func TestVerifyRejectsADamagedProfile(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, profileRoot string){
		"database": func(t *testing.T, profileRoot string) {
			require.NoError(t, home.WritePrivateFile(
				filepath.Join(profileRoot, "moneyflow.db"), []byte("synthetic damage")))
		},
		"directory in place of a file": func(t *testing.T, profileRoot string) {
			_, err := home.EnsurePrivateSubdirectory(profileRoot, "provider-refresh.json")
			require.NoError(t, err)
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := testRoot(t)
			shop := createLocalProfile(t, root, "Example Shop")
			instance := openTestInstance(t, root, seams{})
			backup := filepath.Join(t.TempDir(), "backup")
			require.NoError(t, instance.Backup(t.Context(), backup))
			damage(t, filepath.Join(backup, "profiles", shop))

			require.ErrorContains(t, Verify(t.Context(), backup), "verify backup: profile "+shop)
		})
	}
}

// linkOrSkip makes the symbolic link link to target, or skips the test on a platform that cannot
// create one: Windows needs a privilege or developer mode for it.
func linkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
}

// TestVerifyRejectsWhatBackupDoesNotWrite damages a backup in a way that Backup never does. Each
// case returns the error text that must name the entry it added or replaced.
func TestVerifyRejectsWhatBackupDoesNotWrite(t *testing.T) {
	for name, damage := range map[string]func(t *testing.T, backup, profile string) string{
		"a file at the root": func(t *testing.T, backup, _ string) string {
			require.NoError(t, home.WritePrivateFile(
				filepath.Join(backup, "credentials.json"), []byte("synthetic")))
			return "credentials.json does not belong in a backup"
		},
		"a directory at the root": func(t *testing.T, backup, _ string) string {
			_, err := home.EnsurePrivateSubdirectory(backup, "providers")
			require.NoError(t, err)
			return "providers does not belong in a backup"
		},
		"a symbolic link in place of profiles": func(t *testing.T, backup, _ string) string {
			moved := filepath.Join(t.TempDir(), "profiles")
			require.NoError(t, os.Rename(filepath.Join(backup, "profiles"), moved))
			linkOrSkip(t, moved, filepath.Join(backup, "profiles"))
			return "profiles is a symbolic link"
		},
		"a symbolic link in place of a profile": func(t *testing.T, backup, profile string) string {
			copied := filepath.Join(t.TempDir(), profile)
			require.NoError(t, os.Rename(filepath.Join(backup, "profiles", profile), copied))
			linkOrSkip(t, copied, filepath.Join(backup, "profiles", profile))
			return filepath.Join("profiles", profile) + " is a symbolic link"
		},
		"a file in profiles": func(t *testing.T, backup, _ string) string {
			require.NoError(t, home.WritePrivateFile(
				filepath.Join(backup, "profiles", "notes.txt"), []byte("synthetic")))
			return filepath.Join("profiles", "notes.txt") + " is not a directory"
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := testRoot(t)
			shop := createLocalProfile(t, root, "Example Shop")
			instance := openTestInstance(t, root, seams{})
			backup := filepath.Join(t.TempDir(), "backup")
			require.NoError(t, instance.Backup(t.Context(), backup))
			entry := damage(t, backup, shop)

			require.ErrorContains(t, Verify(t.Context(), backup), "verify backup: "+entry)
		})
	}
}

// completeAudit holds two synthetic audit records. tornAudit is the start of a third, as a copy
// can read it while a provider write appends it.
const (
	completeAudit = `{"version":1,"time":"2026-01-15T00:00:00Z"}` + "\n" +
		`{"version":1,"time":"2026-01-15T00:00:01Z"}` + "\n"
	tornAudit = `{"version":1,"ti`
)

func TestBackupLeavesOutATornFinalAuditRecord(t *testing.T) {
	for name, complete := range map[string]string{
		"after complete records": completeAudit,
		"alone":                  "",
	} {
		t.Run(name, func(t *testing.T) {
			root := testRoot(t)
			shop := createLocalProfile(t, root, "Example Shop")
			require.NoError(t, home.WritePrivateFile(
				filepath.Join(root, "profiles", shop, "audit.jsonl"), []byte(complete+tornAudit)))
			instance := openTestInstance(t, root, seams{})
			backup := filepath.Join(t.TempDir(), "backup")

			require.NoError(t, instance.Backup(t.Context(), backup))

			copied, err := os.ReadFile(filepath.Clean(
				filepath.Join(backup, "profiles", shop, "audit.jsonl")))
			require.NoError(t, err)
			assert.Equal(t, complete, string(copied))
			require.NoError(t, Verify(t.Context(), backup))
		})
	}
}

func TestVerifyRejectsATornFinalAuditRecord(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	backup := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, instance.Backup(t.Context(), backup))
	audit := filepath.Join(backup, "profiles", shop, "audit.jsonl")
	require.NoError(t, home.WritePrivateFile(audit, []byte(completeAudit+tornAudit)))

	err := Verify(t.Context(), backup)

	require.ErrorContains(t, err, "verify backup: profile "+shop+": audit.jsonl")
}

func TestBackupSkipsABrokenProfileAndKeepsTheOthers(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	broken := createLocalProfile(t, root, "Acme")
	manifest := filepath.Join(root, "profiles", broken, profilecatalog.ManifestFilename)
	require.NoError(t, home.WritePrivateFile(manifest, []byte("{}")))
	destination := filepath.Join(t.TempDir(), "backup")

	existing := instance.Backup(t.Context(), t.TempDir())
	partial := instance.Backup(t.Context(), destination)

	require.ErrorContains(t, existing, "already exists")
	require.ErrorContains(t, partial, "skipped profile "+broken)
	require.NoError(t, Verify(t.Context(), destination))
	assert.FileExists(t, filepath.Join(destination, "profiles", shop, "moneyflow.db"))
	assert.NoDirExists(t, filepath.Join(destination, "profiles", broken))
}

func TestABackupLeavesADestinationThatAppearsAfterItsCheck(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	createMonarchProfile(t, root, "Acme", providerRoot)
	destination := filepath.Join(t.TempDir(), "backup")
	other := filepath.Join(destination, "other-backup.txt")
	var appears sync.Once
	instance := openTestInstance(t, root, seams{
		factories: assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
		// Backup checks that the destination is absent, then surveys the catalog, and the survey
		// checks each provider profile's session. So another call that creates the destination
		// now creates it after the check and before Backup does.
		inspectSession: func(profileRoot, providerKind string) (bool, error) {
			appears.Do(func() {
				assert.NoError(t, os.Mkdir(destination, 0o700))
				assert.NoError(t, os.WriteFile(other, []byte("another backup"), 0o600))
			})
			return assembly.InspectSession(profileRoot, providerKind)
		},
	})

	err := instance.Backup(t.Context(), destination)

	require.ErrorContains(t, err, destination+" already exists")
	entries, readErr := os.ReadDir(destination)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "the backup added nothing and removed nothing")
	contents, readErr := os.ReadFile(filepath.Clean(other))
	require.NoError(t, readErr)
	assert.Equal(t, "another backup", string(contents))
}

func TestBackupRemovesThePartialCopyOfAProfileItCannotCopy(t *testing.T) {
	root := testRoot(t)
	acme := createLocalProfile(t, root, "Acme")
	shop := createLocalProfile(t, root, "Example Shop")
	// A directory where the refresh record belongs fails the copy after the database copy.
	_, err := home.EnsurePrivateSubdirectory(
		filepath.Join(root, "profiles", acme), "provider-refresh.json")
	require.NoError(t, err)
	instance := openTestInstance(t, root, seams{})
	destination := filepath.Join(t.TempDir(), "backup")

	err = instance.Backup(t.Context(), destination)

	require.ErrorContains(t, err, "skipped profile "+acme)
	require.NoError(t, Verify(t.Context(), destination))
	assert.NoDirExists(t, filepath.Join(destination, "profiles", acme))
	assert.FileExists(t, filepath.Join(destination, "profiles", shop, "moneyflow.db"))
}

// startHeldBackup opens an instance with a local profile, Acme, and a Monarch profile, Example
// Shop, whose first open waits for the returned hold's Release. It starts a backup to destination
// and returns once the backup copied Acme and waits to open Example Shop. It fails the test at
// once if the backup finishes first.
func startHeldBackup(
	ctx context.Context,
	t *testing.T,
	destination string,
) (*Instance, *heldOpen, <-chan error) {
	t.Helper()
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createLocalProfile(t, root, "Acme")
	createMonarchProfile(t, root, "Example Shop", providerRoot)
	hold := &heldOpen{
		next:    syntheticMonarch(providerRoot),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	instance := openTestInstance(t, root, seams{factories: assembly.Factories{Monarch: hold.build}})
	t.Cleanup(hold.Release)
	done := make(chan error, 1)
	go func() { done <- instance.Backup(ctx, destination) }()
	select {
	case <-hold.started:
	case err := <-done:
		t.Fatalf("Backup finished without opening Example Shop: %v", err)
	}
	assert.FileExists(t, filepath.Join(destination, "profiles", acme, "moneyflow.db"),
		"the backup copied Acme first")
	return instance, hold, done
}

func TestABackupInterruptedByItsContextLeavesNoDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	destination := filepath.Join(t.TempDir(), "backup")
	_, hold, done := startHeldBackup(ctx, t, destination)

	cancel()
	hold.Release()

	require.ErrorIs(t, <-done, context.Canceled)
	assert.NoDirExists(t, destination)
}

func TestABackupInterruptedByCloseLeavesNoDestination(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "backup")
	instance, hold, done := startHeldBackup(t.Context(), t, destination)
	closed := make(chan error, 1)
	go func() { closed <- instance.Close() }()
	<-instance.ctx.Done() // Close marks the instance closed before it cancels this context.

	hold.Release()

	// Both calls end before any assertion can stop the test, so neither outlives its temporary
	// directories.
	backupErr, closeErr := <-done, <-closed
	require.ErrorIs(t, backupErr, ErrClosed)
	require.NoError(t, closeErr)
	assert.NoDirExists(t, destination)
}

func TestEveryMethodReturnsErrClosedAfterClose(t *testing.T) {
	instance := openTestInstance(t, testRoot(t), seams{})
	require.NoError(t, instance.Close())
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "host", Version: "1"}, nil)
	profileID := "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"

	profiles, profilesErr := instance.Profiles(t.Context())
	_, importErr := instance.ImportProfiles(t.Context(), t.TempDir())

	require.ErrorIs(t, profilesErr, ErrClosed)
	assert.Nil(t, profiles)
	require.ErrorIs(t, importErr, ErrClosed)
	require.ErrorIs(t, instance.RegisterTools(server, ToolOptions{Classes: []Class{ClassRead}}),
		ErrClosed)
	require.ErrorIs(t, instance.RunScheduler(t.Context()), ErrClosed)
	require.ErrorIs(t, instance.Unlock(t.Context(), profileID, "example password"), ErrClosed)
	require.ErrorIs(t, instance.Backup(t.Context(), filepath.Join(t.TempDir(), "backup")), ErrClosed)
	require.ErrorIs(t, instance.Backup(t.Context(), t.TempDir()), ErrClosed,
		"ErrClosed even when the destination exists")
}
