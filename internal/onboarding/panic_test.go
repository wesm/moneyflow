package onboarding

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// panickingRuntime panics like provider code that a setup job calls.
func panickingRuntime(home.Paths) (Runtime, error) {
	panic("synthetic runtime failure")
}

func TestAPanickingSetupJobIsLoggedWithItsFrames(t *testing.T) {
	opened := newFlowOpenedProfile(t, flowProfilePristine)
	var logged strings.Builder
	coordinator, err := NewCoordinator(Config{
		Now: time.Now, InstanceID: "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) { return opened, nil },
		Runtime:     panickingRuntime,
		Logger:      slog.New(slog.NewTextHandler(&logged, nil)),
	})
	require.NoError(t, err)
	started, err := coordinator.Start(t.Context(), StartRequest{ProfileID: testProfileID})
	require.NoError(t, err)

	final := waitForState(t, coordinator, started, StateFailed)

	require.NotNil(t, final.Failure)
	assert.Equal(t, "Setup stopped unexpectedly.", final.Failure.Message)
	assert.Equal(t, 1, strings.Count(logged.String(), "\n"), logged.String())
	assert.Contains(t, logged.String(), "moneyflow_event=onboarding_job_panicked")
	assert.Contains(t, logged.String(), "moneyflow_cause=internal_error")
	assert.Contains(t, logged.String(), `panic="synthetic runtime failure"`)
	assert.Contains(t, logged.String(), "onboarding.panickingRuntime", "the log locates the panic")
}

func TestCredentialReentryAfterSessionLoadPanicCompletesImport(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	sessions := &fakeSessionStore{panicFirstLoad: true, loadErr: os.ErrNotExist}
	connector := &fakeConnector{
		connectSession: validTestSession("subscription-example", "USD", 2),
	}
	source := newImportTestSource(t)
	coordinator, err := NewCoordinator(Config{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now:    time.Now, InstanceID: "test-instance",
		OpenProfile: func(ctx context.Context, id string) (OpenedProfile, error) {
			handle, openErr := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
			if openErr != nil {
				return OpenedProfile{}, openErr
			}
			service, openErr := app.NewProfileService(ctx, handle)
			if openErr != nil {
				_ = handle.Close()
				return OpenedProfile{}, openErr
			}
			return OpenedProfile{ID: id, Paths: paths, Service: service, Close: handle.Close}, nil
		},
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				Sessions: sessions, Credentials: &fakeCredentialVault{},
				NewConnector: func(monarch.ImportConfig) (provider.Connector, error) {
					return connector, nil
				},
				NewSources: func(monarch.ImportConfig) (provider.ReaderSource, provider.WriterSource, error) {
					return source, source, nil
				},
				InstanceID: "provider-instance", Now: time.Now,
			}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, coordinator.Close(context.Background())) })
	started, err := coordinator.Start(t.Context(), StartRequest{
		ProfileID: testProfileID, Settings: &SettingsInput{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	failed := waitForState(t, coordinator, started, StateFailed)
	require.True(t, failed.Failure.CanReenter)
	restarted, err := coordinator.Submit(t.Context(), SubmitRequest{
		ProfileID: testProfileID, AttemptID: failed.AttemptID,
		ExpectedStateVersion: failed.StateVersion, Action: ActionReauthenticate,
	})
	require.NoError(t, err)
	credentials := waitForState(t, coordinator, restarted, StateCredentialsRequired)
	submitted, err := coordinator.Submit(t.Context(), credentialSubmitRequest(credentials))
	require.NoError(t, err)
	complete := waitForState(t, coordinator, submitted, StateComplete)
	assert.Equal(t, 1, complete.Progress.Imported)
}

func TestAuthenticationPanicAfterReentryRetriesInspection(t *testing.T) {
	for _, providerKind := range []string{"monarch", "ynab"} {
		t.Run(providerKind, func(t *testing.T) {
			paths, err := home.ResolveRoot(t.TempDir(), nil, "")
			require.NoError(t, err)
			var panicAuthentication atomic.Bool
			var sourceCalls atomic.Int32
			runtime := Runtime{ProviderKind: providerKind, InstanceID: "provider-instance", Now: time.Now}
			credentials := credentialSubmitRequest
			if providerKind == "monarch" {
				connector := &fakeConnector{
					identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				}
				source := newImportTestSource(t)
				runtime.Sessions = &fakeSessionStore{
					session: validTestSession("subscription-example", "USD", 2),
				}
				runtime.Credentials = &fakeCredentialVault{}
				runtime.NewConnector = func(monarch.ImportConfig) (provider.Connector, error) {
					if panicAuthentication.Swap(false) {
						panic("synthetic authentication failure")
					}
					return connector, nil
				}
				runtime.NewSources = func(monarch.ImportConfig) (
					provider.ReaderSource, provider.WriterSource, error,
				) {
					if sourceCalls.Add(1) == 1 {
						panic("synthetic source failure")
					}
					return source, source, nil
				}
			} else {
				client := &fakeYNABClient{plan: testYNABPlan("plan-example")}
				sources := &fakeYNABSourceFactory{}
				credentials = ynabCredentialRequest
				runtime.YNABVault = &fakeYNABVault{exists: true, credentials: ynab.StoredCredentials{
					AccessToken: "token-example", PlanID: "plan-example", Currency: "USD", Scale: 2,
				}}
				runtime.NewYNABClient = func([]byte) (YNABPlanClient, error) {
					if panicAuthentication.Swap(false) {
						panic("synthetic authentication failure")
					}
					return client, nil
				}
				runtime.NewYNABSource = func(
					credentials ynab.StoredCredentials,
					initial *provider.SnapshotResult,
				) (provider.ReaderSource, provider.WriterSource, error) {
					if sourceCalls.Add(1) == 1 {
						panic("synthetic source failure")
					}
					return sources.newSource(credentials, initial)
				}
			}
			coordinator, err := NewCoordinator(Config{
				Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
				Now:    time.Now, InstanceID: "test-instance",
				OpenProfile: func(ctx context.Context, id string) (OpenedProfile, error) {
					handle, openErr := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
					if openErr != nil {
						return OpenedProfile{}, openErr
					}
					service, openErr := app.NewProfileService(ctx, handle)
					if openErr != nil {
						_ = handle.Close()
						return OpenedProfile{}, openErr
					}
					return OpenedProfile{ID: id, Paths: paths, Service: service, Close: handle.Close}, nil
				},
				Runtime: func(home.Paths) (Runtime, error) { return runtime, nil },
			})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, coordinator.Close(context.Background())) })
			next, err := coordinator.Start(t.Context(), StartRequest{
				ProfileID: testProfileID, ProviderKind: providerKind,
			})
			require.NoError(t, err)
			unlock := func(snapshot Snapshot) Snapshot {
				t.Helper()
				if providerKind != "ynab" {
					return snapshot
				}
				required := waitForState(t, coordinator, snapshot, StateUnlockRequired)
				submitted, submitErr := coordinator.Submit(t.Context(), SubmitRequest{
					ProfileID: testProfileID, AttemptID: required.AttemptID,
					ExpectedStateVersion: required.StateVersion,
					Action:               ActionUnlock,
					Unlock:               &UnlockInput{AccountPassword: []byte("account-password")},
				})
				require.NoError(t, submitErr)
				return submitted
			}
			failed := waitForState(t, coordinator, unlock(next), StateFailed)
			require.True(t, failed.Failure.CanReenter)
			next, err = coordinator.Submit(t.Context(), SubmitRequest{
				ProfileID: testProfileID, AttemptID: failed.AttemptID,
				ExpectedStateVersion: failed.StateVersion,
				Action:               ActionReauthenticate,
			})
			require.NoError(t, err)
			panicAuthentication.Store(true)
			next, err = coordinator.Submit(t.Context(), credentials(next))
			require.NoError(t, err)
			failed = waitForState(t, coordinator, next, StateFailed)
			require.True(t, failed.Failure.CanRetry)
			next, err = coordinator.Submit(t.Context(), SubmitRequest{
				ProfileID: testProfileID, AttemptID: failed.AttemptID,
				ExpectedStateVersion: failed.StateVersion,
				Action:               ActionRetry,
			})
			require.NoError(t, err)
			require.Equal(t, StateInspect, next.State)
			complete := waitForState(t, coordinator, unlock(next), StateComplete)
			assert.Equal(t, 1, complete.Progress.Imported)
			assert.Equal(t, int32(2), sourceCalls.Load())
		})
	}
}
