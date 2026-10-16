package moneyflow

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	uitestruntime "github.com/wesm/moneyflow/internal/uitest/runtime"
)

// unknownProfileID is a well-formed profile ID that names no profile.
const unknownProfileID = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"

// logBuffer collects an instance's log output.
type logBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (logged *logBuffer) Write(data []byte) (int, error) {
	logged.mu.Lock()
	defer logged.mu.Unlock()
	return logged.buffer.Write(data)
}

func (logged *logBuffer) String() string {
	logged.mu.Lock()
	defer logged.mu.Unlock()
	return logged.buffer.String()
}

// openLoggedInstance opens a test instance whose log the returned buffer collects.
func openLoggedInstance(t *testing.T, root string, testSeams seams) (*Instance, *logBuffer) {
	t.Helper()
	logged := &logBuffer{}
	options := Options{
		Root: root, APIBasePath: testBasePath, Logger: slog.New(slog.NewTextHandler(logged, nil)),
	}
	return openInstance(t, options, testSeams), logged
}

// addProfile creates a profile through the instance's API and returns its ID.
func addProfile(t *testing.T, instance *Instance, name, kind string) string {
	t.Helper()
	response := serveAPI(t, instance, http.MethodPost, "api/v1/profiles",
		`{"version":"1","display_name":"`+name+`","provider_kind":"`+kind+`"}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var created api.ProfileResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	return created.Profile.ID
}

func createProfileWith(
	t *testing.T,
	root, name, kind string,
	populate func(context.Context, profilecatalog.Entry) error,
) string {
	t.Helper()
	paths, err := home.ResolveCatalogRoot(root, nil, "")
	require.NoError(t, err)
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: cryptorand.Reader, Now: time.Now, Version: "test",
	})
	require.NoError(t, err)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: name, ProviderKind: kind, Populate: populate,
	})
	require.NoError(t, err)
	return entry.ID
}

// createMonarchProfile creates a Monarch profile bound to the synthetic provider in
// providerRoot, with one staged edit.
func createMonarchProfile(t *testing.T, root, name, providerRoot string) string {
	t.Helper()
	return createProfileWith(t, root, name, "monarch",
		func(ctx context.Context, entry profilecatalog.Entry) error {
			run, err := uitestruntime.OpenProfile(ctx, entry.ProfilePaths(), providerRoot, "web")
			if err != nil {
				return err
			}
			err = run.Stage(ctx, uitestruntime.FilteredSession(), "hide", "")
			return errors.Join(err, run.Close())
		})
}

func panickingMonarch(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
	panic("synthetic factory failure")
}

func TestTheAPILogsWhyAProfileCouldNotOpen(t *testing.T) {
	instance, logged := openLoggedInstance(t, testRoot(t), seams{})

	serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+unknownProfileID+"/health", "")

	assert.Contains(t, logged.String(), "the profile could not be opened: profile not found")
}

func TestAProfileThatCannotOpenAnswersNotFound(t *testing.T) {
	root := testRoot(t)
	instance := openTestInstance(t, root, seams{})
	broken := addProfile(t, instance, "Acme", "local")
	manifest := filepath.Join(root, "profiles", broken, profilecatalog.ManifestFilename)
	require.NoError(t, home.WritePrivateFile(manifest, []byte("{}")))

	for _, profileID := range []string{unknownProfileID, broken} {
		health := serveAPI(t, instance, http.MethodGet,
			"api/v1/profiles/"+profileID+"/health", "")

		assert.Equal(t, http.StatusNotFound, health.Code, health.Body.String())
		assert.Contains(t, health.Body.String(), `"code":"not_found"`)
	}
	_, err := instance.registry.Acquire(t.Context(), unknownProfileID)
	assert.ErrorIs(t, err, ErrProfileNotFound, "host-facing methods still match the root error")
}

func TestAnActivatedProfileAtTheRootIsNotListedAndAnswersNotFound(t *testing.T) {
	root := testRoot(t)
	createLegacyProfile(t, root)
	legacy := activateLegacyProfile(t, root)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})

	profiles, err := instance.Profiles(t.Context())
	_, resolveErr := instance.resolve(t.Context(), legacy)
	health := serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+legacy+"/health", "")

	require.NoError(t, err)
	require.Len(t, profiles, 1, "Remove, Backup and Verify handle only Root/profiles")
	assert.Equal(t, shop, profiles[0].ID)
	assert.Equal(t, profilecatalog.CodeProfileNotFound, profilecatalog.CodeOf(resolveErr))
	assert.Equal(t, http.StatusNotFound, health.Code, health.Body.String())
	assert.Contains(t, health.Body.String(), `"code":"not_found"`)
}

func TestAProfileAnswersBusyWhileAnotherHolderLocksTheCatalog(t *testing.T) {
	root := testRoot(t)
	instance := openTestInstance(t, root, seams{})
	acme := addProfile(t, instance, "Acme", "local")
	lock, err := home.TryLock(root, home.LockCatalog, home.LockExclusive)
	require.NoError(t, err)

	health := serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+acme+"/health", "")

	assert.Equal(t, http.StatusServiceUnavailable, health.Code, health.Body.String())
	assert.Contains(t, health.Body.String(), `"code":"profile_busy"`)
	require.NoError(t, lock.Release())
}

func TestAPanicWhileOpeningAProfileIsRecoveredAndRecorded(t *testing.T) {
	root := testRoot(t)
	acme := createMonarchProfile(t, root, "Acme", t.TempDir())
	instance, logged := openLoggedInstance(t, root, seams{
		factories: assembly.Factories{Monarch: panickingMonarch},
	})

	health := serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+acme+"/health", "")

	assert.Equal(t, http.StatusNotFound, health.Code, health.Body.String())
	assert.Contains(t, health.Body.String(), `"code":"not_found"`)
	instance.mu.Lock()
	failure := instance.failures[acme]
	instance.mu.Unlock()
	require.ErrorIs(t, failure, errOpenPanicked)
	assert.NotContains(t, failure.Error(), "synthetic factory failure")
	reported := currentProfile(t.Context(), instance, acme)
	assert.Equal(t, StateError, reported.State)
	assert.Equal(t, "opening the profile stopped unexpectedly", reported.Error)
	assert.NotContains(t, reported.Error, "synthetic factory failure")
	assert.Contains(t, logged.String(), "synthetic factory failure")
	assert.Contains(t, logged.String(), "moneyflow.panickingMonarch", "the log locates the panic")
	profileRoot := filepath.Join(root, "profiles", acme)
	assert.NoFileExists(t, filepath.Join(profileRoot, "moneyflow.db-wal"), "the store is closed")
	lock, err := home.TryLockExisting(profileRoot, home.LockProfile, home.LockExclusive)
	require.NoError(t, err, "the profile lock is released")
	require.NoError(t, lock.Release())
}

// firstInspection holds the first session inspection until Release, which keeps one catalog read
// in progress.
type firstInspection struct {
	held     atomic.Bool
	started  chan struct{}
	release  chan struct{}
	released sync.Once
}

func (hold *firstInspection) inspect(profileRoot, providerKind string) (bool, error) {
	if hold.held.CompareAndSwap(false, true) {
		close(hold.started)
		<-hold.release
	}
	return assembly.InspectSession(profileRoot, providerKind)
}

// Release lets the held inspection finish. Later calls do nothing.
func (hold *firstInspection) Release() { hold.released.Do(func() { close(hold.release) }) }

// openWithHeldSurvey opens an instance whose first catalog read waits for Release. The test
// releases it before the instance closes.
func openWithHeldSurvey(t *testing.T, root string) (*Instance, *firstInspection) {
	t.Helper()
	hold := &firstInspection{started: make(chan struct{}), release: make(chan struct{})}
	instance := openTestInstance(t, root, seams{inspectSession: hold.inspect})
	t.Cleanup(hold.Release)
	return instance, hold
}

// waitForCatalogWriter returns once a caller waits for the catalog write lock. It fails the test
// at once if done delivers first, because that caller finished without waiting.
func waitForCatalogWriter(t *testing.T, instance *Instance, done <-chan error) {
	t.Helper()
	for instance.catalogMu.TryRLock() {
		instance.catalogMu.RUnlock()
		select {
		case err := <-done:
			t.Fatalf("finished without waiting for the catalog lock: %v", err)
		default:
			runtime.Gosched()
		}
	}
}

// catalogCalls makes every catalog call the API reaches, except List, on one profile.
var catalogCalls = map[string]func(context.Context, lockedCatalog, string) error{
	"create": func(ctx context.Context, catalog lockedCatalog, _ string) error {
		_, err := catalog.Create(ctx, profilecatalog.CreateRequest{
			DisplayName: "Beta", ProviderKind: "local",
		})
		return err
	},
	"cancel a new profile": func(ctx context.Context, catalog lockedCatalog, profileID string) error {
		_, err := catalog.CancelNewProfile(ctx, profileID)
		return err
	},
	"activate": func(ctx context.Context, catalog lockedCatalog, profileID string) error {
		_, err := catalog.ActivateForProvider(ctx, profileID, "monarch")
		return err
	},
	"preview recovery": func(ctx context.Context, catalog lockedCatalog, profileID string) error {
		_, err := catalog.RecoveryPlan(ctx, profileID)
		return err
	},
	"recreate": func(ctx context.Context, catalog lockedCatalog, profileID string) error {
		_, err := catalog.Recreate(ctx, profilecatalog.RecoveryRequest{
			Plan:      profilecatalog.RecoveryPlan{ProfileKey: profileID, ProfileID: profileID},
			Confirmed: true,
		})
		return err
	},
	"remove": func(ctx context.Context, catalog lockedCatalog, profileID string) error {
		return catalog.Remove(ctx, profileID)
	},
}

func TestCatalogCallsWaitForASurveyInsteadOfAnsweringBusy(t *testing.T) {
	for name, call := range catalogCalls {
		t.Run(name, func(t *testing.T) {
			root := testRoot(t)
			acme := createMonarchProfile(t, root, "Acme", t.TempDir())
			instance, hold := openWithHeldSurvey(t, root)
			listed := make(chan *httptest.ResponseRecorder, 1)
			go func() { listed <- serveAPI(t, instance, http.MethodGet, "api/v1/profiles", "") }()
			<-hold.started
			called := make(chan error, 1)
			go func() { called <- call(t.Context(), lockedCatalog{instance: instance}, acme) }()

			waitForCatalogWriter(t, instance, called)
			hold.Release()

			list := <-listed
			assert.Equal(t, http.StatusOK, list.Code, list.Body.String())
			assert.NotErrorIs(t, <-called, home.ErrLockBusy, "a call in the same process waits")
		})
	}
}

func TestCloseWaitsForASurveyInProgress(t *testing.T) {
	root := testRoot(t)
	acme := createMonarchProfile(t, root, "Acme", t.TempDir())
	instance, hold := openWithHeldSurvey(t, root)
	listed := make(chan *httptest.ResponseRecorder, 1)
	go func() { listed <- serveAPI(t, instance, http.MethodGet, "api/v1/profiles", "") }()
	<-hold.started
	closed := make(chan error, 1)
	go func() { closed <- instance.Close() }()

	waitForCatalogWriter(t, instance, closed)
	hold.Release()

	list := <-listed
	assert.Equal(t, http.StatusOK, list.Code, list.Body.String())
	require.NoError(t, <-closed)
	_, err := instance.list(t.Context())
	assert.ErrorIs(t, err, ErrClosed)
	for name, call := range catalogCalls {
		err := call(t.Context(), lockedCatalog{instance: instance}, acme)
		assert.ErrorIs(t, err, ErrClosed, name)
		assert.Equal(t, profilecatalog.CodeProfileBusy, profilecatalog.CodeOf(err),
			"%s: the API answers 503 profile_busy", name)
	}
}

func TestAProfileOpenAfterCloseAnswersBusyNotNotFound(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	require.NoError(t, instance.Close())

	// The API refuses every request that arrives after Close, so open and resolve the profile
	// directly, as the registry and the Amazon routes do for a request that the API admitted
	// before Close.
	_, err := instance.openProfile(t.Context(), shop)
	_, resolveErr := instance.resolve(t.Context(), shop)

	require.ErrorIs(t, err, ErrClosed)
	assert.ErrorIs(t, err, api.ErrProfileUnavailable, "the API answers 503 profile_busy")
	assert.NotErrorIs(t, err, api.ErrProfileNotFound, "not 404: the profile still exists")
	assert.ErrorIs(t, resolveErr, ErrClosed, "a resolve after Close still reports ErrClosed")
	assert.ErrorIs(t, resolveErr, api.ErrProfileUnavailable, "the API answers 503 profile_busy")
}

func TestAnOpenProfileAnswersBusyOnceACallSeesTheInstanceClosed(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	var instance *Instance
	var edit string
	var profilesErr error
	answers := map[string]*httptest.ResponseRecorder{}
	// A host that sees the instance closed and then sends a request sends it after Close started.
	instance = openTestInstance(t, root, seams{beforeShutdown: func() {
		_, profilesErr = instance.Profiles(t.Context())
		answers["read"] = serveAPI(t, instance, http.MethodGet,
			"api/v1/profiles/"+shop+"/health", "")
		answers["write"] = serveAPI(t, instance, http.MethodPost,
			"api/v1/profiles/"+shop+"/mutations", edit)
	}})
	// Reading the view opens the profile, and the registry keeps it open until Close ends.
	view := projectView(t, instance, shop)
	require.NotEmpty(t, view.AggregateRows)
	edit = encode(t, api.MutationBody{
		Version: api.MutationSchemaVersion, ExpectedRevision: view.Revision,
		Query: view.CanonicalQuery, Selection: view.Selection, Action: app.ActionEditMerchant,
		Target: &api.TransitionTarget{
			Kind: app.IdentityAggregate, Identity: view.AggregateRows[0].Identity,
		},
		Input:  api.MutationInput{Scope: string(app.EditScopeEntity), Label: "Example Merchant"},
		Window: api.Window{Limit: 200},
	})

	require.NoError(t, instance.Close())

	require.ErrorIs(t, profilesErr, ErrClosed, "the call saw the instance closed")
	for name, answer := range answers {
		assert.Equal(t, http.StatusServiceUnavailable, answer.Code,
			"%s: %s", name, answer.Body.String())
		assert.Contains(t, answer.Body.String(), `"code":"profile_busy"`, name)
	}
	after := projectView(t, openTestInstance(t, root, seams{}), shop)
	assert.Equal(t, view.Revision, after.Revision, "the write changed nothing")
}
