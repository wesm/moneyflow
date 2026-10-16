package moneyflow

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/uitest"
	uitestruntime "github.com/wesm/moneyflow/internal/uitest/runtime"
)

// sessionPresent makes every bound provider profile look signed in.
func sessionPresent(string, string) (bool, error) { return true, nil }

// sessionAbsent makes every bound provider profile look signed out.
func sessionAbsent(string, string) (bool, error) { return false, nil }

// createLocalProfile creates a local profile that holds moneyflow's demo transactions.
func createLocalProfile(t *testing.T, root, name string) string {
	t.Helper()
	return createProfileWith(t, root, name, "local",
		func(ctx context.Context, entry profilecatalog.Entry) error {
			committed, err := fixture.CommittedProfile(fixture.Demo())
			if err != nil {
				return err
			}
			profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
			if err != nil {
				return err
			}
			_, err = profile.CreateSeededProfile(ctx, committed)
			return errors.Join(err, profile.Close())
		})
}

// syntheticMonarch serves Monarch profiles from the synthetic provider in providerRoot, on the
// provider's own clock.
func syntheticMonarch(providerRoot string) assembly.MonarchFactory {
	return func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		source, err := uitest.OpenProvider(providerRoot)
		if err != nil {
			return assembly.MonarchRuntime{}, err
		}
		return assembly.MonarchRuntime{
			ReadSource: source, WriteSource: source, InstanceID: "test-" + cryptorand.Text(),
			Now: source.Now,
		}, nil
	}
}

// createDrivenMonarchProfile creates a Monarch profile bound to the synthetic provider in
// providerRoot, then lets drive change the profile and the provider before the profile closes.
func createDrivenMonarchProfile(
	t *testing.T,
	root, name, providerRoot string,
	drive func(context.Context, *uitestruntime.Runtime) error,
) string {
	t.Helper()
	return createProfileWith(t, root, name, "monarch",
		func(ctx context.Context, entry profilecatalog.Entry) error {
			run, err := uitestruntime.OpenProfile(ctx, entry.ProfilePaths(), providerRoot, "web")
			if err != nil {
				return err
			}
			return errors.Join(drive(ctx, run), run.Close())
		})
}

// failRefresh makes the provider fail the next refresh and runs it.
func failRefresh(ctx context.Context, run *uitestruntime.Runtime) error {
	if err := run.Provider.SetRefreshFault(uitest.RefreshFail); err != nil {
		return err
	}
	return refreshExpectingFailure(ctx, run)
}

func refreshExpectingFailure(ctx context.Context, run *uitestruntime.Runtime) error {
	_, err := run.Service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	return expectFailure(err)
}

// signedOutSource is a provider whose saved sign-in no longer works.
type signedOutSource struct{ *uitest.Provider }

func (signedOutSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeReconnectRequired)
}

func (signedOutSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeReconnectRequired)
}

// rateLimitedSource is a provider that rate limits every write.
type rateLimitedSource struct{ *uitest.Provider }

func (rateLimitedSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeRateLimited)
}

// refreshWhileSignedOut runs a refresh that finds the provider sign-in expired.
func refreshWhileSignedOut(ctx context.Context, run *uitestruntime.Runtime) error {
	if err := useSources(run, signedOutSource{run.Provider}, run.Provider); err != nil {
		return err
	}
	return refreshExpectingFailure(ctx, run)
}

// commitWhileSignedOut stages an edit and commits it while the provider sign-in is expired.
func commitWhileSignedOut(ctx context.Context, run *uitestruntime.Runtime) error {
	return commitAgainst(ctx, run, signedOutSource{run.Provider})
}

// commitWhileRateLimited stages an edit and commits it while the provider rate limits writes.
func commitWhileRateLimited(ctx context.Context, run *uitestruntime.Runtime) error {
	return commitAgainst(ctx, run, rateLimitedSource{run.Provider})
}

// commitAgainst stages an edit and commits it to write, which must fail the write.
func commitAgainst(
	ctx context.Context,
	run *uitestruntime.Runtime,
	write provider.WriterSource,
) error {
	session := uitestruntime.FilteredSession()
	if err := run.Stage(ctx, session, "hide", ""); err != nil {
		return err
	}
	if err := useSources(run, run.Provider, write); err != nil {
		return err
	}
	return expectFailure(run.Commit(ctx, session))
}

// useSources makes the profile read from read and write to write.
func useSources(
	run *uitestruntime.Runtime,
	read provider.ReaderSource,
	write provider.WriterSource,
) error {
	return run.Service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: read, WriteSource: write, Provider: "monarch", Currency: "USD", Scale: 2,
		Renderer: renderer, InstanceID: "test-sources", Now: run.Provider.Now,
	})
}

// commitRejected stages an edit that the provider rejects when the profile commits it.
func commitRejected(ctx context.Context, run *uitestruntime.Runtime) error {
	session := uitestruntime.FilteredSession()
	if err := run.Stage(ctx, session, "hide", ""); err != nil {
		return err
	}
	if err := run.Provider.SetFault(uitest.FaultReject); err != nil {
		return err
	}
	return expectFailure(run.Commit(ctx, session))
}

// expectFailure turns the provider failure a driven profile needs into success, and an unexpected
// success into an error.
func expectFailure(err error) error {
	if err == nil {
		return errors.New("the operation succeeded; the test wants a provider failure")
	}
	return nil
}

// commitWithoutWriting stages and commits an edit and returns before its provider write runs. A
// profile that closes then holds a batch that is writing under the lease of a process that ended.
func commitWithoutWriting(ctx context.Context, run *uitestruntime.Runtime) error {
	session := uitestruntime.FilteredSession()
	if err := run.Stage(ctx, session, "hide", ""); err != nil {
		return err
	}
	review, err := run.Service.Review(ctx, run.Service.Revision(), app.ReviewWindow{})
	if err != nil {
		return err
	}
	_, err = run.Service.Commit(ctx, app.CommitRequest{
		ExpectedRevision: review.Revision, ReviewedRevision: review.Revision,
		State: session.ViewState(), Selection: app.EmptySelection(),
	})
	return err
}

// abandonWrite leaves a committed write unfinished, then moves the clock past the end of its
// lease, which nothing renewed.
func abandonWrite(ctx context.Context, run *uitestruntime.Runtime) error {
	if err := commitWithoutWriting(ctx, run); err != nil {
		return err
	}
	return run.Provider.Advance(time.Hour)
}

// pauseWrite stages and commits an edit, then pauses its provider write before it runs.
func pauseWrite(ctx context.Context, run *uitestruntime.Runtime) error {
	if err := commitWithoutWriting(ctx, run); err != nil {
		return err
	}
	status, err := run.Service.ProviderWriteStatus(ctx)
	if err != nil {
		return err
	}
	_, err = run.Service.PauseProviderWrite(ctx, status.Version)
	return err
}

// reconcileWithRemovals stops a rejected write and reconciles it against a provider snapshot
// that drops most transactions, which the profile holds until the user confirms the removals.
func reconcileWithRemovals(ctx context.Context, run *uitestruntime.Runtime) error {
	if err := commitRejected(ctx, run); err != nil {
		return err
	}
	status, err := run.Service.ProviderWriteStatus(ctx)
	if err != nil {
		return err
	}
	if err = run.Provider.SetRefreshFault(uitest.RefreshRemove); err != nil {
		return err
	}
	_, err = run.Service.StopAndReconcileProviderWrite(ctx, app.ProviderWriteReconcileRequest{
		ExpectedVersion: status.Version, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	return expectFailure(err)
}

// kindSource serves the synthetic provider's transactions as the data of another provider kind.
type kindSource struct {
	*uitest.Provider
	kind string
}

func (source kindSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return source, "scenario-session", nil
}

func (source kindSource) FetchSnapshot(
	ctx context.Context,
	request provider.FetchRequest,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	result, err := source.Provider.FetchSnapshot(ctx, request, progress)
	result.Identity.Kind = source.kind
	return result, err
}

// createBoundProfile creates a profile of a YNAB or SimpleFIN kind that refreshed once from the
// synthetic provider. No instance has unlocked or loaded its credentials.
func createBoundProfile(t *testing.T, root, name, kind, providerRoot string) string {
	t.Helper()
	return createProfileWith(t, root, name, kind,
		func(ctx context.Context, entry profilecatalog.Entry) error {
			source, err := uitest.OpenProvider(providerRoot)
			if err != nil {
				return err
			}
			profile, err := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
			if err != nil {
				return err
			}
			service, err := app.NewProfileService(ctx, profile)
			if err == nil {
				err = service.ConfigureProvider(app.ProviderRuntime{
					ReadSource: kindSource{source, kind}, Provider: kind, Currency: "USD", Scale: 2,
					Renderer: renderer, InstanceID: "test-" + kind, Now: source.Now,
				})
			}
			if err == nil {
				_, err = service.RefreshProvider(ctx, app.ProviderRefreshRequest{
					Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
				})
			}
			return errors.Join(err, profile.Close())
		})
}

// syntheticSimpleFIN serves SimpleFIN profiles from the synthetic provider in providerRoot.
func syntheticSimpleFIN(providerRoot string) assembly.SimpleFINFactory {
	return func(home.Paths) (simplefinonboarding.Runtime, error) {
		source, err := uitest.OpenProvider(providerRoot)
		if err != nil {
			return simplefinonboarding.Runtime{}, err
		}
		return simplefinonboarding.Runtime{
			InstanceID: "test-simplefin", Now: source.Now,
			NewSource: func(app.ProviderConnectionState, simplefin.Session) (provider.ReaderSource, error) {
				return kindSource{source, "simplefin"}, nil
			},
		}, nil
	}
}

// currentProfile returns one profile as Profiles reports it, or a zero Profile.
func currentProfile(ctx context.Context, instance *Instance, profileID string) Profile {
	profiles, err := instance.Profiles(ctx)
	if err != nil {
		return Profile{}
	}
	for _, profile := range profiles {
		if profile.ID == profileID {
			return profile
		}
	}
	return Profile{}
}

func TestProfilesReportEachState(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	shop := createLocalProfile(t, root, "Example Shop")
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	budget := createProfileWith(t, root, "Example Budget", "ynab", nil)
	instance := openTestInstance(t, root, seams{
		factories: assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})

	profiles, err := instance.Profiles(t.Context())

	require.NoError(t, err)
	require.Len(t, profiles, 3)
	assert.Equal(t, []string{acme, budget, shop},
		[]string{profiles[0].ID, profiles[1].ID, profiles[2].ID})
	assert.Equal(t, "Acme", profiles[0].Name)
	assert.Equal(t, "monarch", profiles[0].ProviderKind)
	assert.Equal(t, StateNeedsReconnect, profiles[0].State)
	assert.Equal(t, 1, profiles[0].PendingEdits)
	assert.False(t, profiles[0].LastRefresh.IsZero())
	assert.Equal(t, StateNeedsSetup, profiles[1].State)
	assert.Equal(t, StateReady, profiles[2].State)
	assert.Equal(t, "local", profiles[2].ProviderKind)
	assert.True(t, profiles[2].LastRefresh.IsZero())
}

func TestASignedInProviderProfileIsReady(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})

	profile := currentProfile(t.Context(), instance, acme)

	assert.Equal(t, StateReady, profile.State)
	assert.Empty(t, profile.Error)
}

func TestABrokenProfileReportsAnErrorWhileOthersWork(t *testing.T) {
	root := testRoot(t)
	broken := createLocalProfile(t, root, "Acme")
	// A broken profile has no readable name, so it sorts by its ID, ahead of Zeta Shop.
	shop := createLocalProfile(t, root, "Zeta Shop")
	manifest := filepath.Join(root, "profiles", broken, profilecatalog.ManifestFilename)
	require.NoError(t, home.WritePrivateFile(manifest, []byte("{}")))
	instance := openTestInstance(t, root, seams{})

	failed := currentProfile(t.Context(), instance, broken)
	working := currentProfile(t.Context(), instance, shop)
	listed := serveAPI(t, instance, http.MethodGet, "api/v1/profiles", "")

	assert.Equal(t, StateError, failed.State)
	assert.NotEmpty(t, failed.Error)
	assert.Equal(t, StateReady, working.State)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	assert.Contains(t, listed.Body.String(), `"unavailable"`)
}

func TestAProfileAnotherProcessHoldsReportsBusy(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	lock, err := home.TryLockExisting(
		filepath.Join(root, "profiles", shop), home.LockProfile, home.LockExclusive,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, lock.Release()) }()
	instance := openTestInstance(t, root, seams{})

	profile := currentProfile(t.Context(), instance, shop)
	health := serveAPI(t, instance, http.MethodGet, "api/v1/profiles/"+shop+"/health", "")

	assert.Equal(t, StateError, profile.State)
	assert.Contains(t, profile.Error, "busy")
	assert.Equal(t, http.StatusServiceUnavailable, health.Code, health.Body.String())
	assert.Contains(t, health.Body.String(), `"code":"profile_busy"`)
}

func TestRemoveProfileDeletesItsFiles(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	require.Equal(t, StateReady, currentProfile(t.Context(), instance, shop).State)

	response := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+shop+"/remove", `{"version":"1"}`)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	profiles, err := instance.Profiles(t.Context())
	require.NoError(t, err)
	assert.Empty(t, profiles)
	assert.NoDirExists(t, filepath.Join(root, "profiles", shop))
}

func TestProfilesReportWhatAProviderProfileNeeds(t *testing.T) {
	for name, scenario := range map[string]struct {
		drive func(context.Context, *uitestruntime.Runtime) error
		want  ProfileState
	}{
		"a refresh that finds the sign-in out": {drive: refreshWhileSignedOut, want: StateNeedsReconnect},
		"a commit that finds the sign-in out":  {drive: commitWhileSignedOut, want: StateNeedsReconnect},
		// Nothing in an instance resumes a rate-limited write; a client must ask for it.
		"a rate-limited commit":               {drive: commitWhileRateLimited, want: StateWriteAttention},
		"a rejected commit":                   {drive: commitRejected, want: StateWriteAttention},
		"a paused commit":                     {drive: pauseWrite, want: StateWriteAttention},
		"a reconcile that needs confirmation": {drive: reconcileWithRemovals, want: StateWriteAttention},
		// Until its lease expires, the process that committed may still be writing.
		"a commit whose writer still holds its lease": {drive: commitWithoutWriting, want: StateReady},
		// Nothing in an instance resumes a write whose lease expired; a client must ask for it.
		"a commit whose writer ended and whose lease expired": {
			drive: abandonWrite, want: StateWriteAttention,
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, providerRoot := testRoot(t), t.TempDir()
			acme := createDrivenMonarchProfile(t, root, "Acme", providerRoot, scenario.drive)
			instance := openTestInstance(t, root, seams{
				inspectSession: sessionPresent,
				factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
			})

			profile := currentProfile(t.Context(), instance, acme)

			assert.Equal(t, scenario.want, profile.State)
			assert.Empty(t, profile.Error)
			assert.False(t, profile.LastRefresh.IsZero())
		})
	}
}

func TestProfilesReportAWriteRunThatStoppedBeforeItsBatchParked(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	instance := openInstance(t, Options{
		Root: root, APIBasePath: testBasePath,
		SQLite: SQLiteOptions{MutationBusyTimeout: 20 * time.Millisecond},
	}, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})
	entry, err := instance.lookup(t.Context(), acme)
	require.NoError(t, err)
	lease, err := instance.registry.Acquire(t.Context(), acme)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, lease.Release()) })
	service := lease.Service()
	revision := service.Revision()
	_, err = service.Commit(t.Context(), app.CommitRequest{
		ExpectedRevision: revision, ReviewedRevision: revision,
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	require.NoError(t, err)
	// Another process holds the profile's database, so the write run stops with its batch still
	// writing.
	database, err := sql.Open("sqlite", entry.ProfilePaths().Database)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	connection, err := database.Conn(t.Context())
	require.NoError(t, err)
	_, err = connection.ExecContext(t.Context(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	_, err = service.RunProviderWrite(t.Context())
	require.Error(t, err)
	_, err = connection.ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, connection.Close())

	profile := currentProfile(t.Context(), instance, acme)

	assert.Equal(t, StateWriteAttention, profile.State)
}

func TestAFailedRefreshReportsItsCode(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	acme := createDrivenMonarchProfile(t, root, "Acme", providerRoot, failRefresh)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})

	profile := currentProfile(t.Context(), instance, acme)

	assert.Equal(t, StateRefreshFailed, profile.State)
	assert.Contains(t, profile.Error, string(provider.CodeUnavailable))
}

func TestAYNABProfileNeedsAPasswordOnlyWhenItsCredentialsAreStored(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	budget := createBoundProfile(t, root, "Example Budget", "ynab", providerRoot)
	for name, scenario := range map[string]struct {
		inspect profilecatalog.SessionPresence
		want    ProfileState
	}{
		"stored credentials": {inspect: sessionPresent, want: StateNeedsUnlock},
		"no credentials":     {inspect: sessionAbsent, want: StateNeedsReconnect},
	} {
		t.Run(name, func(t *testing.T) {
			instance := openTestInstance(t, root, seams{inspectSession: scenario.inspect})

			profile := currentProfile(t.Context(), instance, budget)

			assert.Equal(t, scenario.want, profile.State)
			assert.Empty(t, profile.Error)
		})
	}
}

func TestOnlyAYNABProfileWithItsOwnStoredCredentialsNeedsUnlock(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	budget := createBoundProfile(t, root, "Example Budget", "ynab", providerRoot)
	acme := createMonarchProfile(t, root, "Acme", providerRoot)
	// YNAB credentials are stored at both profiles' own roots, and nothing else is stored, so the
	// Monarch profile is signed out. Only a check that asks about YNAB credentials at a YNAB
	// profile's own root finds any.
	stored := map[string]bool{
		filepath.Join(root, "profiles", budget): true,
		filepath.Join(root, "profiles", acme):   true,
	}
	instance := openTestInstance(t, root, seams{
		inspectSession: func(profileRoot, providerKind string) (bool, error) {
			return providerKind == "ynab" && stored[profileRoot], nil
		},
		factories: assembly.Factories{Monarch: syntheticMonarch(providerRoot)},
	})

	assert.Equal(t, StateNeedsUnlock, currentProfile(t.Context(), instance, budget).State)
	assert.Equal(t, StateNeedsReconnect, currentProfile(t.Context(), instance, acme).State)
}

func TestASimpleFINProfileWithoutASessionNeedsReconnect(t *testing.T) {
	root, providerRoot := testRoot(t), t.TempDir()
	bank := createBoundProfile(t, root, "Example Bank", "simplefin", providerRoot)
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionAbsent,
		factories:      assembly.Factories{SimpleFIN: syntheticSimpleFIN(providerRoot)},
	})

	profile := currentProfile(t.Context(), instance, bank)

	assert.Equal(t, StateNeedsReconnect, profile.State)
	assert.Equal(t, "simplefin", profile.ProviderKind)
}

func TestAProfileWhoseProviderCannotStartReportsAnError(t *testing.T) {
	root := testRoot(t)
	acme := createMonarchProfile(t, root, "Acme", t.TempDir())
	unavailable := func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
		return assembly.MonarchRuntime{}, errors.New("synthetic provider failure")
	}
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: unavailable},
	})

	profile := currentProfile(t.Context(), instance, acme)

	assert.Equal(t, StateError, profile.State)
	assert.Contains(t, profile.Error, "synthetic provider failure")
}

func TestProfilesReportABackgroundFailure(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	require.Equal(t, StateReady, currentProfile(t.Context(), instance, shop).State)

	instance.recordFailure(shop, errors.New("synthetic background failure"))

	profile := currentProfile(t.Context(), instance, shop)
	assert.Equal(t, StateError, profile.State)
	assert.Equal(t, "synthetic background failure", profile.Error)
}

// heldOpen holds the first Monarch profile open until Release, which keeps one Profiles call
// inside its description of that profile.
type heldOpen struct {
	next     assembly.MonarchFactory
	held     atomic.Bool
	started  chan struct{}
	release  chan struct{}
	released sync.Once
}

func (hold *heldOpen) build(
	paths home.Paths,
	config monarch.ImportConfig,
) (assembly.MonarchRuntime, error) {
	if hold.held.CompareAndSwap(false, true) {
		close(hold.started)
		<-hold.release
	}
	return hold.next(paths, config)
}

// Release lets the held open finish. Later calls do nothing.
func (hold *heldOpen) Release() { hold.released.Do(func() { close(hold.release) }) }

// openWithHeldOpen opens an instance with one Monarch profile whose first open waits for Release.
// The test releases it before the instance closes.
func openWithHeldOpen(t *testing.T) (*Instance, *heldOpen) {
	t.Helper()
	root, providerRoot := testRoot(t), t.TempDir()
	createMonarchProfile(t, root, "Acme", providerRoot)
	hold := &heldOpen{
		next: syntheticMonarch(providerRoot), started: make(chan struct{}), release: make(chan struct{}),
	}
	instance := openTestInstance(t, root, seams{
		inspectSession: sessionPresent,
		factories:      assembly.Factories{Monarch: hold.build},
	})
	t.Cleanup(hold.Release)
	return instance, hold
}

// profilesResult is what one Profiles call returned.
type profilesResult struct {
	profiles []Profile
	err      error
}

// startProfiles runs Profiles in the background and returns once it waits for the held open. It
// fails the test at once if Profiles finishes without reaching it.
func startProfiles(
	ctx context.Context,
	t *testing.T,
	instance *Instance,
	hold *heldOpen,
) <-chan profilesResult {
	t.Helper()
	done := make(chan profilesResult, 1)
	go func() {
		profiles, err := instance.Profiles(ctx)
		done <- profilesResult{profiles: profiles, err: err}
	}()
	select {
	case <-hold.started:
	case result := <-done:
		t.Fatalf("Profiles finished without opening the profile: %v, %v", result.profiles, result.err)
	}
	return done
}

func TestProfilesFailWhenTheContextIsAlreadyDone(t *testing.T) {
	root := testRoot(t)
	createLocalProfile(t, root, "Example Shop")
	instance := openTestInstance(t, root, seams{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	profiles, err := instance.Profiles(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, profiles)
}

func TestProfilesReturnTheContextErrorWhenTheContextEndsMidCall(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	instance, hold := openWithHeldOpen(t)
	done := startProfiles(ctx, t, instance, hold)
	cancel()
	hold.Release()

	result := <-done

	require.ErrorIs(t, result.err, context.Canceled)
	assert.Nil(t, result.profiles, "no list blames the profile for the cancellation")
}

func TestProfilesReturnErrClosedWhenTheInstanceClosesMidCall(t *testing.T) {
	instance, hold := openWithHeldOpen(t)
	done := startProfiles(t.Context(), t, instance, hold)
	closed := make(chan error, 1)
	go func() { closed <- instance.Close() }()
	// Close marks the instance closed before it ends the instance's context.
	<-instance.ctx.Done()
	assert.ErrorIs(t, instance.checkOpen(), ErrClosed, "the instance is closed while Profiles waits")
	hold.Release()

	// Both calls end before any assertion can stop the test, so neither outlives its temporary
	// directories.
	result, closeErr := <-done, <-closed

	require.ErrorIs(t, result.err, ErrClosed)
	assert.Nil(t, result.profiles, "no list blames the profile for the close")
	require.NoError(t, closeErr)
}
