package onboarding

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestConfigurationPrecedenceIsBindingThenSessionThenInput(t *testing.T) {
	tests := []struct {
		name                    string
		binding, session, input *monarch.ImportConfig
		want                    monarch.ImportConfig
	}{
		{"binding", importConfig("USD", 2), importConfig("EUR", 2), importConfig("GBP", 2), *importConfig("USD", 2)},
		{"session", nil, importConfig("EUR", 2), importConfig("GBP", 2), *importConfig("EUR", 2)},
		{"input", nil, nil, importConfig("GBP", 2), *importConfig("GBP", 2)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectImportConfig(test.binding, test.session, test.input)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestValidSessionWithoutVaultContinuesToImport(t *testing.T) {
	sessions := &fakeSessionStore{session: validTestSession("subscription-example", "USD", 2)}
	vault := &fakeCredentialVault{exists: false}
	connector := &fakeConnector{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
	}
	coordinator, started := newFlowCoordinator(t, flowProfilePristine, sessions, vault, connector, nil)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateImporting, final.State)
	assert.Equal(t, &Settings{Currency: "USD", Scale: 2}, final.Settings)
	assert.Equal(t, 1, connector.validateCalls)
	assert.Zero(t, vault.existsCalls)
}

func TestSessionConfigurationPrecedesExplicitInput(t *testing.T) {
	sessions := &fakeSessionStore{session: validTestSession("subscription-example", "EUR", 2)}
	connector := &fakeConnector{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
	}
	coordinator, started := newFlowCoordinator(
		t, flowProfilePristine, sessions, &fakeCredentialVault{}, connector,
		&SettingsInput{Currency: "GBP", Scale: 2},
	)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateFailed, final.State)
	require.NotNil(t, final.Failure)
	assert.Equal(t, string(CodeCredentialInputInvalid), final.Failure.Code)
}

func TestLocalOnlyProfileStopsBeforeSessionInspection(t *testing.T) {
	sessions := &fakeSessionStore{loadErr: errors.New("must not load")}
	coordinator, started := newFlowCoordinator(
		t, flowProfileLocalOnly, sessions, &fakeCredentialVault{}, &fakeConnector{}, nil,
	)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateLocalOnly, final.State)
	require.NotNil(t, final.Failure)
	assert.Equal(t, string(CodeOnboardingLocalOnly), final.Failure.Code)
	assert.Zero(t, sessions.loadCalls)
}

func TestExpiredSessionRoutesToUnlockWhenVaultExists(t *testing.T) {
	sessions := &fakeSessionStore{session: validTestSession("subscription-example", "USD", 2)}
	connector := &fakeConnector{validateErr: provider.NewError(provider.CodeReconnectRequired)}
	vault := &fakeCredentialVault{exists: true}
	coordinator, started := newFlowCoordinator(t, flowProfilePristine, sessions, vault, connector, nil)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateUnlockRequired, final.State)
	assert.Equal(t, 1, vault.existsCalls)
}

func TestAbsentSessionRoutesToCredentialsAfterSettings(t *testing.T) {
	sessions := &fakeSessionStore{loadErr: os.ErrNotExist}
	vault := &fakeCredentialVault{exists: false}
	coordinator, started := newFlowCoordinator(
		t, flowProfilePristine, sessions, vault, &fakeConnector{},
		&SettingsInput{Currency: "USD", Scale: 2},
	)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateCredentialsRequired, final.State)
	assert.Equal(t, &Settings{Currency: "USD", Scale: 2}, final.Settings)
}

func TestAbsentSessionRequestsSettingsBeforeCredentials(t *testing.T) {
	coordinator, started := newFlowCoordinator(
		t, flowProfilePristine, &fakeSessionStore{loadErr: os.ErrNotExist},
		&fakeCredentialVault{}, &fakeConnector{}, nil,
	)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateSettingsRequired, final.State)
	assert.Nil(t, final.Settings)
}

func TestIdentityMismatchStopsWithoutChangingBoundProfile(t *testing.T) {
	sessions := &fakeSessionStore{session: validTestSession("subscription-other", "USD", 2)}
	connector := &fakeConnector{
		identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-other"},
	}
	coordinator, started := newFlowCoordinator(t, flowProfileBound, sessions, &fakeCredentialVault{}, connector, nil)

	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateIdentityMismatch, final.State)
	require.NotNil(t, final.Failure)
	assert.Equal(t, string(provider.CodeIdentityMismatch), final.Failure.Code)
}

func TestExpiredStableAttemptReleasesProfileAndProviderLock(t *testing.T) {
	clock := &testClock{now: time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)}
	opened := newFlowOpenedProfile(t, flowProfilePristine)
	profileRoot := opened.Paths.Root
	closeProfile := opened.Close
	closeCalls := 0
	opened.Close = func() error {
		closeCalls++
		return closeProfile()
	}
	coordinator, err := NewCoordinator(Config{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now:    clock.Now, InstanceID: "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) { return opened, nil },
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				Sessions:    &fakeSessionStore{loadErr: os.ErrNotExist},
				Credentials: &fakeCredentialVault{},
				NewConnector: func(monarch.ImportConfig) (provider.Connector, error) {
					return &fakeConnector{identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"}}, nil
				},
				NewSource:  func(monarch.ImportConfig) (provider.Source, error) { return pendingProviderSource{}, nil },
				InstanceID: "provider-instance", Now: clock.Now,
			}, nil
		},
	})
	require.NoError(t, err)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: testProfileID})
	require.NoError(t, err)
	assert.Equal(t, StateSettingsRequired, waitForStableState(t, coordinator, started).State)

	clock.Advance(31 * time.Minute)
	_, err = coordinator.Status(context.Background(), StatusRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
	})
	assert.Equal(t, CodeOnboardingExpired, CodeOf(err))
	assert.Equal(t, 1, closeCalls)
	lock, err := home.TryLock(profileRoot, home.LockProviderConnect, home.LockExclusive)
	require.NoError(t, err)
	require.NoError(t, lock.Release())
}

func TestRuntimeRequiresSourceFactoryBeforeSessionValidation(t *testing.T) {
	opened := newFlowOpenedProfile(t, flowProfilePristine)
	coordinator, err := NewCoordinator(Config{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now:    time.Now, InstanceID: "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) { return opened, nil },
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				Sessions: &fakeSessionStore{}, Credentials: &fakeCredentialVault{},
				NewConnector: func(monarch.ImportConfig) (provider.Connector, error) {
					return &fakeConnector{}, nil
				},
				InstanceID: "provider-instance",
			}, nil
		},
	})
	require.NoError(t, err)
	started, err := coordinator.Start(context.Background(), StartRequest{ProfileID: testProfileID})
	require.NoError(t, err)
	final := waitForStableState(t, coordinator, started)
	assert.Equal(t, StateFailed, final.State)
	assert.Equal(t, genericFailureCode, final.Failure.Code)
	_, err = coordinator.Cancel(context.Background(), CancelRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: final.StateVersion,
	})
	require.NoError(t, err)
}

type flowProfileKind uint8

const (
	flowProfilePristine flowProfileKind = iota + 1
	flowProfileLocalOnly
	flowProfileBound
)

func newFlowCoordinator(
	t *testing.T,
	kind flowProfileKind,
	sessions *fakeSessionStore,
	vault *fakeCredentialVault,
	connector *fakeConnector,
	settings *SettingsInput,
) (*Coordinator, Snapshot) {
	t.Helper()
	opened := newFlowOpenedProfile(t, kind)
	coordinator, err := NewCoordinator(Config{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 128)),
		Now: func() time.Time {
			return time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
		},
		InstanceID: "test-instance",
		OpenProfile: func(context.Context, string) (OpenedProfile, error) {
			return opened, nil
		},
		Runtime: func(home.Paths) (Runtime, error) {
			return Runtime{
				Sessions: sessions, Credentials: vault,
				NewConnector: func(monarch.ImportConfig) (provider.Connector, error) {
					return connector, nil
				},
				NewSource:  func(monarch.ImportConfig) (provider.Source, error) { return pendingProviderSource{}, nil },
				InstanceID: "provider-instance", Now: time.Now,
			}, nil
		},
	})
	require.NoError(t, err)
	started, err := coordinator.Start(context.Background(), StartRequest{
		ProfileID: testProfileID, Settings: settings,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		latest, statusErr := coordinator.Status(context.Background(), StatusRequest{
			ProfileID: testProfileID, AttemptID: started.AttemptID,
		})
		if statusErr == nil {
			_, _ = coordinator.Cancel(context.Background(), CancelRequest{
				ProfileID: testProfileID, AttemptID: started.AttemptID,
				ExpectedStateVersion: latest.StateVersion,
			})
		}
	})
	return coordinator, started
}

func waitForStableState(t *testing.T, coordinator *Coordinator, snapshot Snapshot) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, err := coordinator.Status(context.Background(), StatusRequest{
			ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		})
		require.NoError(t, err)
		if current.State != StateInspect && current.State != StateValidateSession {
			return current
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("onboarding attempt did not reach a stable state")
	return Snapshot{}
}

func importConfig(currency domain.Currency, scale uint8) *monarch.ImportConfig {
	return &monarch.ImportConfig{Currency: currency, Scale: scale}
}

func validTestSession(remoteID string, currency domain.Currency, scale uint8) monarch.Session {
	issued := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	return monarch.Session{
		Version: 2, Token: "synthetic-token", DeviceUUID: "synthetic-device",
		RemoteProfileID: remoteID, Import: monarch.ImportConfig{Currency: currency, Scale: scale},
		IssuedAt: issued, ValidatedAt: issued.Add(time.Minute),
	}
}

type fakeSessionStore struct {
	mu        sync.Mutex
	session   monarch.Session
	loadErr   error
	saveErr   error
	loadCalls int
	saveCalls int
	order     *[]string
}

func (store *fakeSessionStore) Load() (monarch.Session, provider.SessionFingerprint, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loadCalls++
	return store.session, "session-fingerprint", store.loadErr
}

func (store *fakeSessionStore) Save(session monarch.Session) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.saveCalls++
	if store.order != nil {
		*store.order = append(*store.order, "session")
	}
	store.session = session
	return store.saveErr
}

func (*fakeSessionStore) Delete() error { return nil }

type fakeCredentialVault struct {
	mu          sync.Mutex
	exists      bool
	existsErr   error
	existsCalls int
	credentials monarch.StoredCredentials
	loadErr     error
	saveErr     error
	loadCalls   int
	saveCalls   int
	order       *[]string
}

func (vault *fakeCredentialVault) Exists() (bool, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	vault.existsCalls++
	return vault.exists, vault.existsErr
}

func (vault *fakeCredentialVault) Load([]byte) (monarch.StoredCredentials, error) {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	vault.loadCalls++
	return vault.credentials, vault.loadErr
}

func (vault *fakeCredentialVault) Save(
	credentials monarch.StoredCredentials,
	_ []byte,
) error {
	vault.mu.Lock()
	defer vault.mu.Unlock()
	vault.saveCalls++
	if vault.order != nil {
		*vault.order = append(*vault.order, "vault")
	}
	vault.credentials = credentials
	return vault.saveErr
}

type fakeConnector struct {
	mu             sync.Mutex
	identity       provider.ProfileIdentity
	validateErr    error
	validateCalls  int
	connectSession provider.Session
	connectErr     error
	connectCalls   int
	credentials    provider.Credentials
	challenge      *provider.Challenge
	challengeReply string
}

func (connector *fakeConnector) Connect(
	ctx context.Context,
	credentials provider.Credentials,
	respond provider.ChallengeResponder,
) (provider.Session, error) {
	connector.mu.Lock()
	connector.connectCalls++
	connector.credentials = credentials
	challenge := connector.challenge
	connector.mu.Unlock()
	if challenge != nil {
		reply, err := respond(ctx, *challenge)
		if err != nil {
			return nil, err
		}
		connector.mu.Lock()
		connector.challengeReply = reply
		connector.mu.Unlock()
	}
	return connector.connectSession, connector.connectErr
}

func (connector *fakeConnector) Validate(
	context.Context,
	provider.Session,
) (provider.ProfileIdentity, error) {
	connector.mu.Lock()
	defer connector.mu.Unlock()
	connector.validateCalls++
	return connector.identity, connector.validateErr
}

func newFlowOpenedProfile(t *testing.T, kind flowProfileKind) OpenedProfile {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	handle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, handle)
	require.NoError(t, err)
	opened := OpenedProfile{ID: testProfileID, Paths: paths, Service: service, Close: handle.Close}

	switch kind {
	case flowProfilePristine:
	case flowProfileLocalOnly:
		revision, appendErr := handle.Append(ctx, 0, domain.Operation{
			ID: "operation_local_only", Type: domain.OperationGroupCreate,
			PayloadVersion: 1, CreatedRevision: 0,
			CreatedAt: time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC),
			Targets:   []domain.EntityID{"group_local"},
			Create: &domain.CreatePayload{
				EntityType: "group", EntityID: "group_local",
				Label: "Local Group", CollisionKey: "local group",
			},
		})
		require.NoError(t, appendErr)
		assert.Equal(t, uint64(1), revision)
	case flowProfileBound:
		source := newTestBoundSource(t)
		require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
			Source: source, Provider: "monarch", Currency: "USD", Scale: 2,
			Renderer: "cli", InstanceID: "binding-instance",
		}))
		_, refreshErr := service.RefreshProvider(ctx, app.ProviderRefreshRequest{
			Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
		})
		require.NoError(t, refreshErr)
	default:
		t.Fatalf("unknown flow profile kind %d", kind)
	}
	return opened
}

type testBoundSource struct {
	snapshot domain.ImportSnapshot
}

func newTestBoundSource(t *testing.T) *testBoundSource {
	t.Helper()
	date, err := domain.ParseDate("2026-08-17")
	require.NoError(t, err)
	return &testBoundSource{snapshot: domain.ImportSnapshot{
		ObservedAt: time.Date(2026, time.August, 17, 11, 0, 0, 0, time.UTC),
		Accounts:   []domain.ImportEntity{{Kind: domain.EntityKindAccount, ExternalID: "account-example", Label: "Account Name"}},
		Merchants:  []domain.ImportEntity{{Kind: domain.EntityKindMerchant, ExternalID: "merchant-example", Label: "Example Merchant"}},
		Groups:     []domain.ImportEntity{{Kind: domain.EntityKindGroup, ExternalID: "group-example", Label: "Example Group"}},
		Categories: []domain.ImportEntity{{Kind: domain.EntityKindCategory, ExternalID: "category-example", ParentExternalID: "group-example", Label: "Example Category"}},
		Transactions: []domain.ImportTransaction{{
			ExternalID: "transaction-example", AccountExternalID: "account-example",
			MerchantExternalID: "merchant-example", CategoryExternalID: "category-example",
			Date: date, Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
		}},
	}}
}

func (source *testBoundSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return (*testBoundReader)(source), "binding-session", nil
}

func (*testBoundSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeWriteUnsupported)
}

func (*testBoundSource) Changed(provider.SessionFingerprint) (bool, error) { return false, nil }

type testBoundReader testBoundSource

func (*testBoundReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"}, nil
}

func (reader *testBoundReader) FetchSnapshot(
	context.Context,
	provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	return reader.snapshot.Clone(), nil
}

type pendingProviderSource struct{}

func (pendingProviderSource) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	return pendingProviderReader{}, "pending-session", nil
}

func (pendingProviderSource) Writer(
	context.Context,
	bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	return nil, "", provider.NewError(provider.CodeWriteUnsupported)
}

func (pendingProviderSource) Changed(provider.SessionFingerprint) (bool, error) {
	return false, nil
}

type pendingProviderReader struct{}

func (pendingProviderReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	return provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"}, nil
}

func (pendingProviderReader) FetchSnapshot(
	ctx context.Context,
	_ provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	<-ctx.Done()
	return domain.ImportSnapshot{}, ctx.Err()
}
