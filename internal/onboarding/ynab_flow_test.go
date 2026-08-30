package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/ynab"
)

func TestYNABProtocolVersionAndRemoteChoicesAreCredentialBlind(t *testing.T) {
	t.Parallel()
	assert.Equal(t, uint16(2), ProtocolVersion)
	snapshot := Snapshot{
		ProtocolVersion: ProtocolVersion, AttemptID: "attempt-example", ProfileID: "profile-example",
		StateVersion: 4, State: StateRemoteProfileRequired, ProviderKind: "ynab",
		RemoteProfiles: []RemoteProfileChoice{{
			ChoiceID: "choice_opaque", DisplayName: "Example Budget",
			LastModified: "2026-08-01T00:00:00Z",
		}},
	}
	contents, err := json.Marshal(snapshot)
	require.NoError(t, err)
	assert.Contains(t, string(contents), "choice_opaque")
	assert.Contains(t, string(contents), "Example Budget")
	assert.NotContains(t, string(contents), "plan-secret-id")
	assert.NotContains(t, string(contents), "token-secret")
}

func TestClearSubmitSecretsClearsEveryProviderEnvelope(t *testing.T) {
	t.Parallel()
	request := SubmitRequest{
		MonarchCredentials: &CredentialInput{Password: []byte("monarch-secret")},
		YNABCredentials: &YNABCredentialInput{
			AccessToken: []byte("ynab-secret"), AccountPassword: []byte("vault-secret"),
			Confirmation: []byte("vault-secret"),
		},
	}
	clearSubmitSecrets(&request)
	assert.Equal(t, make([]byte, len("monarch-secret")), request.MonarchCredentials.Password)
	assert.Equal(t, make([]byte, len("ynab-secret")), request.YNABCredentials.AccessToken)
	assert.Equal(t, make([]byte, len("vault-secret")), request.YNABCredentials.AccountPassword)
}

func TestYNABNewCredentialsAutoSelectOnePlanAndImport(t *testing.T) {
	t.Parallel()
	vault := &fakeYNABVault{}
	client := &fakeYNABClient{plans: []ynab.PlanSummary{{ID: "plan-example", Name: "Example Budget"}}, plan: testYNABPlan("plan-example")}
	coordinator, started := newYNABFlowCoordinator(t, vault, client, nil)
	credentials := waitForStableState(t, coordinator, started)
	require.Equal(t, StateCredentialsRequired, credentials.State)
	authenticating, err := coordinator.Submit(context.Background(), ynabCredentialRequest(credentials))
	require.NoError(t, err)
	settings := waitForState(t, coordinator, authenticating, StateSettingsRequired)
	require.Equal(t, StateSettingsRequired, settings.State)
	require.Equal(t, &Settings{Currency: "USD", Scale: 2}, settings.Settings)

	importing, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: settings.ProfileID, AttemptID: settings.AttemptID,
		ExpectedStateVersion: settings.StateVersion, Action: ActionConfirmSettings,
		Settings: &SettingsInput{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	completed := waitForState(t, coordinator, importing, StateComplete)
	assert.Equal(t, 1, completed.Progress.Imported)
	assert.Equal(t, 1, vault.saveCalls)
	assert.Equal(t, "plan-example", vault.credentials.PlanID)
	assert.Equal(t, 1, client.listCalls)
	assert.Equal(t, 1, client.fetchCalls)
}

func TestYNABMultiplePlansExposeOpaqueChoicesAndRejectUnknownChoice(t *testing.T) {
	t.Parallel()
	client := &fakeYNABClient{plans: []ynab.PlanSummary{
		{ID: "plan-private-z", Name: "Zulu Budget"},
		{ID: "plan-private-a", Name: "Alpha Budget"},
	}}
	coordinator, started := newYNABFlowCoordinator(t, &fakeYNABVault{}, client, nil)
	credentials := waitForStableState(t, coordinator, started)
	next, err := coordinator.Submit(context.Background(), ynabCredentialRequest(credentials))
	require.NoError(t, err)
	choices := waitForState(t, coordinator, next, StateRemoteProfileRequired)
	require.Equal(t, StateRemoteProfileRequired, choices.State)
	require.Len(t, choices.RemoteProfiles, 2)
	assert.Equal(t, "Alpha Budget", choices.RemoteProfiles[0].DisplayName)
	assert.NotContains(t, choices.RemoteProfiles[0].ChoiceID, "plan-private")
	encoded, err := json.Marshal(choices)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "plan-private")

	rejected, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: choices.ProfileID, AttemptID: choices.AttemptID,
		ExpectedStateVersion: choices.StateVersion, Action: ActionSelectRemoteProfile,
		RemoteProfileChoiceID: "choice_from_another_attempt",
	})
	require.NoError(t, err)
	failed := waitForState(t, coordinator, rejected, StateFailed)
	assert.Equal(t, string(CodeCredentialInputInvalid), failed.Failure.Code)
	assert.Zero(t, client.fetchCalls)
}

func TestYNABNoPlansFailsWithoutPersistingVault(t *testing.T) {
	t.Parallel()
	vault := &fakeYNABVault{}
	coordinator, started := newYNABFlowCoordinator(t, vault, &fakeYNABClient{}, nil)
	credentials := waitForStableState(t, coordinator, started)
	next, err := coordinator.Submit(context.Background(), ynabCredentialRequest(credentials))
	require.NoError(t, err)
	failed := waitForState(t, coordinator, next, StateFailed)
	assert.Equal(t, string(provider.CodeDataInvalid), failed.Failure.Code)
	assert.Zero(t, vault.saveCalls)
}

func TestYNABVaultBindingMismatchUsesSQLiteAuthority(t *testing.T) {
	t.Parallel()
	opened := newYNABBoundOpenedProfile(t, "plan-bound")
	vault := &fakeYNABVault{exists: true, credentials: ynab.StoredCredentials{
		AccessToken: "token-example", PlanID: "plan-other", Currency: "USD", Scale: 2,
	}}
	coordinator, started := newYNABCoordinator(t, opened, vault, &fakeYNABClient{
		plan: testYNABPlan("plan-other"),
	}, nil)
	unlock := waitForStableState(t, coordinator, started)
	require.Equal(t, StateUnlockRequired, unlock.State)
	next, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: unlock.ProfileID, AttemptID: unlock.AttemptID,
		ExpectedStateVersion: unlock.StateVersion, Action: ActionUnlock,
		Unlock: &UnlockInput{AccountPassword: []byte("account-password")},
	})
	require.NoError(t, err)
	failed := waitForState(t, coordinator, next, StateFailed)
	assert.Equal(t, string(provider.CodeIdentityMismatch), failed.Failure.Code)
	assert.Equal(t, 0, vault.saveCalls)
}

func TestYNABFirstImportRetryReusesSavedVaultAndSnapshot(t *testing.T) {
	t.Parallel()
	vault := &fakeYNABVault{}
	client := &fakeYNABClient{plans: []ynab.PlanSummary{{ID: "plan-example", Name: "Example Budget"}}, plan: testYNABPlan("plan-example")}
	sources := &fakeYNABSourceFactory{errors: []error{provider.NewError(provider.CodeUnavailable), nil}}
	coordinator, started := newYNABFlowCoordinator(t, vault, client, sources)
	credentials := waitForStableState(t, coordinator, started)
	next, err := coordinator.Submit(context.Background(), ynabCredentialRequest(credentials))
	require.NoError(t, err)
	settings := waitForState(t, coordinator, next, StateSettingsRequired)
	next, err = coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: settings.ProfileID, AttemptID: settings.AttemptID,
		ExpectedStateVersion: settings.StateVersion, Action: ActionConfirmSettings,
		Settings: &SettingsInput{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	failed := waitForState(t, coordinator, next, StateFailed)
	require.True(t, failed.Failure.CanRetry)
	require.Equal(t, 1, vault.saveCalls)

	retrying, err := coordinator.Submit(context.Background(), SubmitRequest{
		ProfileID: failed.ProfileID, AttemptID: failed.AttemptID,
		ExpectedStateVersion: failed.StateVersion, Action: ActionRetry,
	})
	require.NoError(t, err)
	completed := waitForState(t, coordinator, retrying, StateComplete)
	assert.Equal(t, 1, completed.Progress.Imported)
	assert.Equal(t, 1, client.fetchCalls)
	assert.Equal(t, 2, sources.calls)
}

type fakeYNABVault struct {
	mu          sync.Mutex
	exists      bool
	credentials ynab.StoredCredentials
	loadErr     error
	saveErr     error
	saveCalls   int
}

func (vault *fakeYNABVault) Exists() (bool, error) { return vault.exists, nil }
func (vault *fakeYNABVault) Load([]byte) (ynab.StoredCredentials, error) {
	return vault.credentials, vault.loadErr
}
func (vault *fakeYNABVault) Save(credentials ynab.StoredCredentials, _ []byte) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	vault.saveCalls++
	vault.credentials = credentials
	vault.exists = vault.saveErr == nil
	return vault.saveErr
}
func (*fakeYNABVault) Delete() error { return nil }

type fakeYNABClient struct {
	mu         sync.Mutex
	plans      []ynab.PlanSummary
	plan       ynab.PlanDocument
	listErr    error
	fetchErr   error
	listCalls  int
	fetchCalls int
}

func (client *fakeYNABClient) ListPlans(context.Context) ([]ynab.PlanSummary, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.listCalls++
	return append([]ynab.PlanSummary(nil), client.plans...), client.listErr
}
func (client *fakeYNABClient) FetchPlan(context.Context, string) (ynab.PlanDocument, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.fetchCalls++
	return client.plan, client.fetchErr
}

type fakeYNABSourceFactory struct {
	mu     sync.Mutex
	errors []error
	calls  int
}

func (factory *fakeYNABSourceFactory) newSource(
	_ ynab.StoredCredentials,
	initial *provider.SnapshotResult,
) (provider.ReaderSource, error) {
	factory.mu.Lock()
	index := factory.calls
	factory.calls++
	var readErr error
	if index < len(factory.errors) {
		readErr = factory.errors[index]
	}
	factory.mu.Unlock()
	return &fakeYNABReaderSource{result: *initial, readErr: readErr}, nil
}

type fakeYNABReaderSource struct {
	result  provider.SnapshotResult
	readErr error
}

func (source *fakeYNABReaderSource) Reader(context.Context, bool) (provider.Reader, provider.SessionFingerprint, error) {
	return fakeYNABReader{result: source.result, err: source.readErr}, "ynab-fingerprint", nil
}
func (*fakeYNABReaderSource) Changed(provider.SessionFingerprint) (bool, error) { return false, nil }

type fakeYNABReader struct {
	result provider.SnapshotResult
	err    error
}

func (reader fakeYNABReader) FetchSnapshot(context.Context, provider.ProgressFunc) (provider.SnapshotResult, error) {
	return reader.result, reader.err
}

func newYNABFlowCoordinator(
	t *testing.T,
	vault *fakeYNABVault,
	client *fakeYNABClient,
	sources *fakeYNABSourceFactory,
) (*Coordinator, Snapshot) {
	t.Helper()
	return newYNABCoordinator(t, newFlowOpenedProfile(t, flowProfilePristine), vault, client, sources)
}

func newYNABCoordinator(
	t *testing.T,
	opened OpenedProfile,
	vault *fakeYNABVault,
	client *fakeYNABClient,
	sources *fakeYNABSourceFactory,
) (*Coordinator, Snapshot) {
	t.Helper()
	if sources == nil {
		sources = &fakeYNABSourceFactory{}
	}
	randomBytes := make([]byte, 512)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	coordinator, err := NewCoordinator(Config{
		Random:      bytes.NewReader(randomBytes),
		Now:         func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
		InstanceID:  "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) { return opened, nil },
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				ProviderKind: "ynab", YNABVault: vault, InstanceID: "ynab-instance",
				Now:           func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
				NewYNABClient: func([]byte) (YNABPlanClient, error) { return client, nil },
				NewYNABSource: sources.newSource,
			}, nil
		},
	})
	require.NoError(t, err)
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: testProfileID, ProviderKind: "ynab", Renderer: "cli",
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = coordinator.Close(context.Background()) })
	return coordinator, started
}

func newYNABBoundOpenedProfile(t *testing.T, planID string) OpenedProfile {
	t.Helper()
	opened := newFlowOpenedProfile(t, flowProfilePristine)
	result, err := ynab.Normalize(testYNABPlan(planID), time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	source := &fakeYNABReaderSource{result: provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: planID}, Snapshot: result,
	}}
	require.NoError(t, opened.Service.ConfigureProvider(appProviderRuntimeForYNAB(source)))
	_, err = opened.Service.RefreshProvider(context.Background(), appRefreshRequest())
	require.NoError(t, err)
	return opened
}

func appProviderRuntimeForYNAB(source provider.ReaderSource) app.ProviderRuntime {
	return app.ProviderRuntime{ReadSource: source, Provider: "ynab", Currency: "USD", Scale: 2,
		Renderer: "cli", InstanceID: "binding-instance", Now: time.Now}
}

func appRefreshRequest() app.ProviderRefreshRequest {
	return app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()}
}

func ynabCredentialRequest(snapshot Snapshot) SubmitRequest {
	return SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: ActionSubmitCredentials,
		YNABCredentials: &YNABCredentialInput{AccessToken: []byte("token-example"),
			AccountPassword: []byte("account-password"), Confirmation: []byte("account-password")},
	}
}

func testYNABPlan(planID string) ynab.PlanDocument {
	no, yes := false, true
	return ynab.PlanDocument{
		ID: planID, Name: "Example Budget",
		CurrencyFormat: ynab.CurrencyFormat{ISOCode: "USD", DecimalDigits: 2},
		Accounts:       []ynab.Account{{ID: "account-example", Name: "Account Name", Type: "checking", OnBudget: &yes, Closed: &no, Deleted: &no}},
		Payees:         []ynab.Payee{{ID: "payee-example", Name: "Example Payee", Deleted: &no}},
		Transactions: []ynab.Transaction{{ID: "transaction-example", Date: "2026-08-30", Amount: -12340,
			Cleared: "cleared", Approved: &yes, AccountID: "account-example", PayeeID: "payee-example", Deleted: &no}},
		ServerKnowledge: 1,
	}
}
