package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func TestProviderConnectHasNoReplaceFlag(t *testing.T) {
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
	})
	command.SetArgs([]string{"provider", "connect", "monarch", "--replace"})
	err := command.Execute()
	require.ErrorContains(t, err, "unknown flag: --replace")
}

func TestProviderConnectCreatesCurrentSchemaAndBindsPristineProfile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 22, 0, 0, 0, time.UTC)
	connector := &fakeMonarchConnector{session: testMonarchSession(now, "subscription-example")}
	source := &commandProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: commandProviderSnapshot(t, now),
	}
	prompts := &recordingPrompt{answers: []string{
		"user@example.com", "not-a-real-password",
	}}
	stdout, stderr, err := executeProviderCommand(
		t, connector, source, prompts.Prompt, "provider", "connect", "monarch",
	)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.NotContains(t, stdout, "user@example.com")
	assert.NotContains(t, stdout, "not-a-real-password")
	assert.Contains(t, stdout, "Imported 1 posted transaction")
	assert.Equal(t, []bool{false, true}, prompts.secretFlags())

	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	profileHandle, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profileHandle.Close()) })
	state, err := profileHandle.ProviderState(context.Background())
	require.NoError(t, err)
	require.NotNil(t, state.Binding)
	assert.Equal(t, "subscription-example", state.Binding.RemoteProfileID)
	loaded, err := profileHandle.Load(context.Background())
	require.NoError(t, err)
	assert.Len(t, loaded.Committed.Transactions, 1)
}

func TestProviderConnectRefusesJournalOnlyAndPopulatedProfiles(t *testing.T) {
	now := time.Date(2026, time.August, 15, 22, 15, 0, 0, time.UTC)
	for _, test := range []struct {
		name  string
		setup func(testing.TB, home.Paths)
	}{
		{name: "journal only", setup: appendJournalOnlyIntent},
		{name: "populated", setup: seedCommandProfile},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "profile")
			t.Setenv("MONEYFLOW_HOME", root)
			paths, err := home.ResolveRoot(root, nil, "")
			require.NoError(t, err)
			test.setup(t, paths)
			connector := &fakeMonarchConnector{
				session: testMonarchSession(now, "subscription-example"),
			}
			prompts := &recordingPrompt{answers: []string{"must-not-be-read"}}

			_, _, err = executeProviderCommand(
				t, connector, &commandProviderSource{}, prompts.Prompt,
				"provider", "connect", "monarch",
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), paths.Database)
			assert.Contains(t, err.Error(), "move or remove")
			assert.Zero(t, connector.connectCalls)
			assert.Empty(t, prompts.calls)
		})
	}
}

func TestProviderConnectPromptsForMFAWithoutEchoingSecrets(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 22, 30, 0, 0, time.UTC)
	connector := &fakeMonarchConnector{
		session: testMonarchSession(now, "subscription-example"), requireMFA: true,
	}
	source := &commandProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: commandProviderSnapshot(t, now),
	}
	prompts := &recordingPrompt{answers: []string{
		"user@example.com", "not-a-real-password", "000000",
	}}

	stdout, stderr, err := executeProviderCommand(
		t, connector, source, prompts.Prompt, "provider", "connect", "monarch",
	)
	require.NoError(t, err)
	assert.Equal(t, []bool{false, true, true}, prompts.secretFlags())
	assert.Equal(t, provider.Credentials{
		Login: "user@example.com", Password: "not-a-real-password",
	}, connector.credentials)
	for _, secret := range []string{"user@example.com", "not-a-real-password", "000000"} {
		assert.NotContains(t, stdout, secret)
		assert.NotContains(t, stderr, secret)
	}
}

func TestProviderConnectRetriesRetainedValidSessionWithoutPrompts(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 22, 45, 0, 0, time.UTC)
	saveCommandSession(t, root, testMonarchSession(now, "subscription-example"))
	connector := &fakeMonarchConnector{}
	source := &commandProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: commandProviderSnapshot(t, now),
	}
	prompts := &recordingPrompt{answers: []string{"must-not-be-read"}}

	_, _, err := executeProviderCommand(
		t, connector, source, prompts.Prompt, "provider", "connect", "monarch",
	)
	require.NoError(t, err)
	assert.Zero(t, connector.connectCalls)
	assert.Equal(t, 1, connector.validateCalls)
	assert.Empty(t, prompts.calls)
}

func TestProviderConnectRetainedIdentityMismatchDoesNotPromptOrReplaceSession(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 22, 50, 0, 0, time.UTC)
	original := testMonarchSession(now, "subscription-example")
	original.Token = "original-token"
	saveCommandSession(t, root, original)
	connector := &fakeMonarchConnector{
		validateErr: provider.NewError(provider.CodeIdentityMismatch),
	}
	prompts := &recordingPrompt{answers: []string{"must-not-be-read"}}

	_, _, err := executeProviderCommand(
		t, connector, &commandProviderSource{}, prompts.Prompt,
		"provider", "connect", "monarch",
	)
	assertProviderCommandCode(t, err, provider.CodeIdentityMismatch)
	assert.Zero(t, connector.connectCalls)
	assert.Empty(t, prompts.calls)
	assert.Equal(t, "original-token", loadCommandSession(t, root).Token)
}

func TestProviderConnectSameHouseholdReconnectAndDifferentHouseholdRefusal(t *testing.T) {
	now := time.Date(2026, time.August, 15, 23, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		remoteID  string
		wantErr   provider.ErrorCode
		wantToken string
	}{
		{name: "same household", remoteID: "subscription-example", wantToken: "replacement-token"},
		{name: "different household", remoteID: "subscription-other", wantErr: provider.CodeIdentityMismatch, wantToken: "original-token"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "profile")
			t.Setenv("MONEYFLOW_HOME", root)
			bindCommandProfile(t, root, now)
			original := testMonarchSession(now, "subscription-example")
			original.Token = "original-token"
			saveCommandSession(t, root, original)
			connector := &fakeMonarchConnector{
				session:     testMonarchSession(now.Add(time.Minute), test.remoteID),
				validateErr: provider.NewError(provider.CodeReconnectRequired),
			}
			connector.session.Token = "replacement-token"
			source := &commandProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: test.remoteID},
				snapshot: commandProviderSnapshot(t, now.Add(time.Minute)),
			}
			prompts := &recordingPrompt{answers: []string{
				"user@example.com", "not-a-real-password",
			}}

			_, _, err := executeProviderCommand(
				t, connector, source, prompts.Prompt, "provider", "connect", "monarch",
			)
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				assertProviderCommandCode(t, err, test.wantErr)
			}
			stored := loadCommandSession(t, root)
			assert.Equal(t, test.wantToken, stored.Token)
		})
	}
}

func TestProviderConnectInitialImportFailureKeepsValidatedSessionAndPristineProfile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 23, 15, 0, 0, time.UTC)
	connector := &fakeMonarchConnector{session: testMonarchSession(now, "subscription-example")}
	source := &commandProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		fetchErr: provider.NewError(provider.CodeUnavailable),
	}
	prompts := &recordingPrompt{answers: []string{
		"user@example.com", "not-a-real-password",
	}}

	_, _, err := executeProviderCommand(
		t, connector, source, prompts.Prompt, "provider", "connect", "monarch",
	)
	assertProviderCommandCode(t, err, provider.CodeUnavailable)
	stored := loadCommandSession(t, root)
	assert.Equal(t, "subscription-example", stored.RemoteProfileID)
	paths, pathErr := home.ResolveRoot(root, nil, "")
	require.NoError(t, pathErr)
	profileHandle, openErr := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, profileHandle.Close()) })
	state, stateErr := profileHandle.ProviderState(context.Background())
	require.NoError(t, stateErr)
	assert.True(t, state.Pristine)
	assert.Nil(t, state.Binding)
}

func TestProviderDisconnectRemovesOnlySessionAndPreservesSQLiteState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 23, 30, 0, 0, time.UTC)
	bindCommandProfile(t, root, now)
	saveCommandSession(t, root, testMonarchSession(now, "subscription-example"))

	_, _, err := executeProviderCommand(
		t, &fakeMonarchConnector{}, &commandProviderSource{}, nil,
		"provider", "disconnect", "monarch",
	)
	require.NoError(t, err)
	paths, pathErr := home.ResolveRoot(root, nil, "")
	require.NoError(t, pathErr)
	sessions, sessionErr := monarch.NewSessionStore(paths)
	require.NoError(t, sessionErr)
	_, _, sessionErr = sessions.Load()
	assert.ErrorIs(t, sessionErr, os.ErrNotExist)
	profileHandle, openErr := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, openErr)
	t.Cleanup(func() { require.NoError(t, profileHandle.Close()) })
	state, stateErr := profileHandle.ProviderState(context.Background())
	require.NoError(t, stateErr)
	require.NotNil(t, state.Binding)
	loaded, loadErr := profileHandle.Load(context.Background())
	require.NoError(t, loadErr)
	assert.Len(t, loaded.Committed.Transactions, 1)
}

func TestProviderConnectImportReopenAndBrowseOffline(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	t.Setenv("MONEYFLOW_HOME", root)
	now := time.Date(2026, time.August, 15, 23, 40, 0, 0, time.UTC)
	connector := &fakeMonarchConnector{session: testMonarchSession(now, "subscription-example")}
	source := &commandProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: commandProviderSnapshot(t, now),
	}
	prompts := &recordingPrompt{answers: []string{"user@example.com", "not-a-real-password"}}
	_, _, err := executeProviderCommand(
		t, connector, source, prompts.Prompt, "provider", "connect", "monarch",
	)
	require.NoError(t, err)
	_, _, err = executeProviderCommand(
		t, &fakeMonarchConnector{}, &commandProviderSource{}, nil,
		"provider", "disconnect", "monarch",
	)
	require.NoError(t, err)

	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	reopened, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	service, err := app.NewProfileService(context.Background(), reopened)
	require.NoError(t, err)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	projection, err := service.ProjectView(state, app.EmptySelection(), app.WindowRequest{})
	require.NoError(t, err)
	assert.Len(t, projection.DetailRows, 1)
	providerState, err := reopened.ProviderState(context.Background())
	require.NoError(t, err)
	require.NotNil(t, providerState.Binding)
	assert.Equal(t, uint64(1), providerState.Refresh.Generation)
}

func executeProviderCommand(
	t *testing.T,
	connector provider.Connector,
	source provider.Source,
	prompt PromptFunc,
	args ...string,
) (string, string, error) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command := newRootCommand(IOStreams{
		In: strings.NewReader(""), Out: &stdout, Err: &stderr, Prompt: prompt,
		OpenMonarch: func(paths home.Paths) (MonarchCommandRuntime, error) {
			sessions, err := monarch.NewSessionStore(paths)
			if err != nil {
				return MonarchCommandRuntime{}, err
			}
			return MonarchCommandRuntime{
				Connector: connector, Sessions: sessions, Source: source, InstanceID: "cli-test",
			}, nil
		},
	})
	command.SetArgs(args)
	err := command.Execute()
	return stdout.String(), stderr.String(), err
}

type fakeMonarchConnector struct {
	session       monarch.Session
	connectErr    error
	validateErr   error
	requireMFA    bool
	connectCalls  int
	validateCalls int
	credentials   provider.Credentials
}

func (connector *fakeMonarchConnector) Connect(
	ctx context.Context,
	credentials provider.Credentials,
	respond provider.ChallengeResponder,
) (provider.Session, error) {
	connector.connectCalls++
	connector.credentials = credentials
	if connector.requireMFA {
		_, err := respond(ctx, provider.Challenge{Kind: "mfa", Prompt: "Enter code"})
		if err != nil {
			return nil, err
		}
	}
	return connector.session, connector.connectErr
}

func (connector *fakeMonarchConnector) Validate(
	_ context.Context,
	session provider.Session,
) (provider.ProfileIdentity, error) {
	connector.validateCalls++
	if connector.validateErr != nil {
		err := connector.validateErr
		connector.validateErr = nil
		return provider.ProfileIdentity{}, err
	}
	monarchSession, ok := session.(monarch.Session)
	if !ok {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeReconnectRequired)
	}
	return provider.ProfileIdentity{
		Kind: "monarch", RemoteID: monarchSession.RemoteProfileID,
	}, nil
}

type commandProviderSource struct {
	mu          sync.Mutex
	identity    provider.ProfileIdentity
	snapshot    domain.ImportSnapshot
	fetchErr    error
	fingerprint provider.SessionFingerprint
}

func (source *commandProviderSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.fingerprint == "" {
		source.fingerprint = "synthetic-session-fingerprint"
	}
	return (*commandProviderReader)(source), source.fingerprint, nil
}

func (source *commandProviderSource) Changed(previous provider.SessionFingerprint) (bool, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return previous != source.fingerprint, nil
}

type commandProviderReader commandProviderSource

func (reader *commandProviderReader) ProbeIdentity(
	context.Context,
) (provider.ProfileIdentity, error) {
	source := (*commandProviderSource)(reader)
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.identity, nil
}

func (reader *commandProviderReader) FetchSnapshot(
	context.Context,
	provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	source := (*commandProviderSource)(reader)
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.snapshot.Clone(), source.fetchErr
}

type recordingPrompt struct {
	answers []string
	calls   []promptCall
}

type promptCall struct {
	label  string
	secret bool
}

func (prompt *recordingPrompt) Prompt(
	_ context.Context,
	label string,
	secret bool,
) (string, error) {
	prompt.calls = append(prompt.calls, promptCall{label: label, secret: secret})
	if len(prompt.answers) == 0 {
		return "", errors.New("unexpected prompt")
	}
	answer := prompt.answers[0]
	prompt.answers = prompt.answers[1:]
	return answer, nil
}

func (prompt *recordingPrompt) secretFlags() []bool {
	result := make([]bool, len(prompt.calls))
	for index, call := range prompt.calls {
		result[index] = call.secret
	}
	return result
}

func testMonarchSession(at time.Time, remoteID string) monarch.Session {
	return monarch.Session{
		Version: 1, Token: "synthetic-session-token",
		DeviceUUID:      "00000000-0000-4000-8000-000000000000",
		RemoteProfileID: remoteID, IssuedAt: at, ValidatedAt: at,
	}
}

func commandProviderSnapshot(t testing.TB, observedAt time.Time) domain.ImportSnapshot {
	t.Helper()
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	return domain.ImportSnapshot{
		ObservedAt: observedAt,
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account-example", Label: "Account Name",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant-example", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group-example", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category-example",
			ParentExternalID: "group-example", Label: "Example Category",
		}},
		Transactions: []domain.ImportTransaction{{
			ExternalID: "transaction-example", AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date: date, Amount: domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
		}},
	}
}

func seedCommandProfile(t testing.TB, paths home.Paths) {
	t.Helper()
	profileHandle, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profileHandle.CreateSeededProfile(context.Background(), committed)
	require.NoError(t, err)
	require.NoError(t, profileHandle.Close())
}

func appendJournalOnlyIntent(t testing.TB, paths home.Paths) {
	t.Helper()
	profileHandle, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	operation := domain.Operation{
		ID: "operation_pending", Type: domain.OperationGroupCreate, PayloadVersion: 1,
		CreatedAt: time.Date(2026, time.August, 15, 22, 0, 0, 0, time.UTC),
		Targets:   []domain.EntityID{"group_pending"}, Create: &domain.CreatePayload{
			EntityType: string(domain.EntityKindGroup), EntityID: "group_pending",
			Label: "Pending Group", CollisionKey: "pending group",
		},
	}
	_, err = profileHandle.Append(context.Background(), 0, operation)
	require.NoError(t, err)
	require.NoError(t, profileHandle.Close())
}

func bindCommandProfile(t testing.TB, root string, now time.Time) {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	profileHandle, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := newProviderBoundServiceForCommand(t, profileHandle, now)
	require.NoError(t, err)
	_, err = service.RefreshProvider(context.Background(), providerRefreshRequest())
	require.NoError(t, err)
	require.NoError(t, profileHandle.Close())
}

func newProviderBoundServiceForCommand(
	t testing.TB,
	profileHandle store.Profile,
	now time.Time,
) (*app.Service, error) {
	t.Helper()
	service, err := app.NewProfileService(context.Background(), profileHandle)
	if err != nil {
		return nil, err
	}
	source := &commandProviderSource{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
		snapshot: commandProviderSnapshot(t, now),
	}
	err = service.ConfigureProvider(app.ProviderRuntime{
		Source: source, Provider: "monarch", Renderer: "cli", InstanceID: "seed-cli",
		Now: func() time.Time { return now }, Random: &commandRandomReader{},
	})
	return service, err
}

func providerRefreshRequest() app.ProviderRefreshRequest {
	return app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	}
}

type commandRandomReader struct{ value byte }

func (reader *commandRandomReader) Read(buffer []byte) (int, error) {
	reader.value++
	for index := range buffer {
		buffer[index] = reader.value
	}
	return len(buffer), nil
}

func saveCommandSession(t testing.TB, root string, session monarch.Session) {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	sessions, err := monarch.NewSessionStore(paths)
	require.NoError(t, err)
	require.NoError(t, sessions.Save(session))
}

func loadCommandSession(t testing.TB, root string) monarch.Session {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	sessions, err := monarch.NewSessionStore(paths)
	require.NoError(t, err)
	session, _, err := sessions.Load()
	require.NoError(t, err)
	return session
}

func assertProviderCommandCode(t testing.TB, err error, code provider.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	providerCode, ok := provider.CodeOf(err)
	if !ok {
		var appError *app.AppError
		require.ErrorAs(t, err, &appError)
		assert.Equal(t, app.AppErrorCode(code), appError.Code)
		return
	}
	assert.Equal(t, code, providerCode)
}

var _ io.Reader = (*commandRandomReader)(nil)
