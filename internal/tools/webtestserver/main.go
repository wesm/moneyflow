// Command webtestserver composes the production browser handlers with a synthetic provider.
// It exists only for browser integration tests and requires a per-run marker capability.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/version"
	webserver "github.com/wesm/moneyflow/internal/web"
)

const (
	syntheticRemotePrefix      = "synthetic-subscription-"
	isolatedRootMarkerFilename = ".moneyflow-webtest-root"
	isolatedRootMarkerLimit    = 4096
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "web test server:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("webtestserver", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var root, rootToken, listen, basePath string
	var recoveryProfile bool
	flags.StringVar(&root, "home", "", "temporary profile catalog root")
	flags.StringVar(&rootToken, "root-token", "", "per-run isolated root capability")
	flags.StringVar(&listen, "listen", "127.0.0.1:0", "loopback listen address")
	flags.StringVar(&basePath, "base-path", "/", "browser base path")
	flags.BoolVar(&recoveryProfile, "recovery-profile", false, "install a corrupt recovery fixture")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("parse arguments")
	}
	if err := requireIsolatedRoot(root, rootToken); err != nil {
		return err
	}
	if !strings.HasPrefix(listen, "127.0.0.1:") && !strings.HasPrefix(listen, "[::1]:") {
		return errors.New("listen address must be loopback")
	}

	catalogPaths, err := home.ResolveCatalogRoot(root, nil, "")
	if err != nil {
		return fmt.Errorf("resolve catalog: %w", err)
	}
	runtimes := newSyntheticRuntimes()
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: catalogPaths, Random: cryptorand.Reader, Now: time.Now, Version: version.Version,
		InspectSession: runtimes.sessionPresent,
	})
	if err != nil {
		return fmt.Errorf("create catalog: %w", err)
	}
	if recoveryProfile {
		if err = installRecoveryProfile(ctx, catalog); err != nil {
			return err
		}
	}

	registry, err := webserver.NewProfileRegistry(webserver.ProfileRegistryConfig{
		Open: func(openContext context.Context, profileID string) (webserver.RegistryProfile, error) {
			return openRegistryProfile(openContext, catalog, profileID)
		},
	})
	if err != nil {
		return fmt.Errorf("create profile registry: %w", err)
	}
	coordinator, err := onboarding.NewCoordinator(onboarding.Config{
		Random: cryptorand.Reader, Now: time.Now, InstanceID: "webtestserver",
		OpenProfile: registry.OnboardingOpener(), Runtime: runtimes.runtime,
	})
	if err != nil {
		return fmt.Errorf("create onboarding coordinator: %w", err)
	}
	amazonCoordinator, err := amazonimport.New(amazonimport.Config{
		InstanceID: "webtestserver-amazon", Random: cryptorand.Reader, Now: time.Now,
		Limits: amazon.ProductionLimits, Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
		ResolveTarget: func(targetContext context.Context, profileID string) (amazonimport.Target, error) {
			entry, resolveErr := catalog.Resolve(targetContext, profileID)
			if resolveErr != nil {
				return amazonimport.Target{}, resolveErr
			}
			if entry.ProviderKind != "amazon" {
				return amazonimport.Target{}, errors.New("selected profile is not an Amazon profile")
			}
			return openAmazonTarget(targetContext, catalog, entry)
		},
	})
	if err != nil {
		return fmt.Errorf("create Amazon import coordinator: %w", err)
	}
	origin, err := api.ResolveOrigin(listen, basePath, "")
	if err != nil {
		return fmt.Errorf("resolve origin: %w", err)
	}
	application, err := webserver.NewServer(webserver.ServerConfig{
		Resolver: registry, Catalog: catalog, Evictor: registry, Onboarding: coordinator,
		AmazonImports: amazonCoordinator,
		BasePath:      basePath, Version: version.Version, Origin: origin,
	})
	if err != nil {
		return fmt.Errorf("compose web server: %w", err)
	}
	handler := testControlHandler(application.Handler(), runtimes)
	httpServer := application.HTTPServer(listen, os.Stderr)
	httpServer.Handler = handler
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.ListenAndServe() }()

	stopContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	var runErr error
	select {
	case <-stopContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		runErr = httpServer.Shutdown(shutdownContext)
		cancel()
		if serveErr := <-serveErrors; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = errors.Join(runErr, serveErr)
		}
	case serveErr := <-serveErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = serveErr
		}
	}
	cleanupContext, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelCleanup()
	return errors.Join(runErr, coordinator.Close(cleanupContext), registry.Close(cleanupContext))
}

func openAmazonTarget(
	_ context.Context,
	catalog *profilecatalog.Catalog,
	entry profilecatalog.Entry,
) (amazonimport.Target, error) {
	lifecycle, err := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return amazonimport.Target{}, err
	}
	if err = catalog.ValidateEntry(entry); err != nil {
		_ = lifecycle.Release()
		return amazonimport.Target{}, err
	}
	return amazonimport.Target{
		ProfileID: entry.ID, Root: entry.Root, Close: lifecycle.Release,
		Import: func(ctx context.Context, request app.AmazonImportRequest) (app.AmazonImportResult, error) {
			profile, openErr := sqlite.Open(ctx, entry.ProfilePaths(), sqlite.DefaultOptions)
			if openErr != nil {
				return app.AmazonImportResult{}, openErr
			}
			defer func() { _ = profile.Close() }()
			return app.ImportAmazonProfile(ctx, profile, request)
		},
	}, nil
}

func requireIsolatedRoot(root string, token string) error {
	if root == "" || token == "" {
		return errors.New("isolated profile root and marker token are required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return errors.New("resolve isolated profile root")
	}
	info, err := os.Lstat(absolute)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("isolated profile root must be a real directory")
	}
	if err = home.EnsurePrivateDirectory(absolute); err != nil {
		return errors.New("isolated profile root must be an owner-only directory")
	}
	marker, err := home.ReadPrivateFile(
		filepath.Join(absolute, isolatedRootMarkerFilename), isolatedRootMarkerLimit,
	)
	if err != nil {
		return errors.New("isolated profile root marker is invalid")
	}
	if subtle.ConstantTimeCompare(marker, []byte(token)) != 1 {
		return errors.New("isolated profile root marker does not match")
	}
	return nil
}

func openRegistryProfile(
	ctx context.Context,
	catalog *profilecatalog.Catalog,
	profileID string,
) (webserver.RegistryProfile, error) {
	entry, err := catalog.Resolve(ctx, profileID)
	if err != nil {
		return webserver.RegistryProfile{}, err
	}
	if entry.ID != profileID {
		return webserver.RegistryProfile{}, errors.New("resolved profile identity differs")
	}
	if err = catalog.ValidateEntry(entry); err != nil {
		return webserver.RegistryProfile{}, err
	}
	paths := entry.ProfilePaths()
	lock, err := home.TryLockExisting(paths.Root, home.LockProfile, home.LockShared)
	if err != nil {
		return webserver.RegistryProfile{}, err
	}
	handle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	if err != nil {
		_ = lock.Release()
		return webserver.RegistryProfile{}, err
	}
	service, err := app.NewProfileService(ctx, handle)
	if err != nil {
		_ = handle.Close()
		_ = lock.Release()
		return webserver.RegistryProfile{}, err
	}
	var once sync.Once
	var closeErr error
	return webserver.RegistryProfile{
		ID: profileID, Paths: paths, Service: service,
		Close: func() error {
			once.Do(func() { closeErr = errors.Join(handle.Close(), lock.Release()) })
			return closeErr
		},
	}, nil
}

func installRecoveryProfile(ctx context.Context, catalog *profilecatalog.Catalog) error {
	entry, err := catalog.Create(ctx, profilecatalog.CreateRequest{
		DisplayName: "Recovery Profile", ProviderKind: "monarch",
	})
	if err != nil {
		return fmt.Errorf("create recovery profile: %w", err)
	}
	if err = os.WriteFile(entry.ProfilePaths().Database, []byte("synthetic corrupt database"), 0o600); err != nil {
		return fmt.Errorf("corrupt recovery profile: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if removeErr := os.Remove(entry.ProfilePaths().Database + suffix); removeErr != nil &&
			!errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove recovery sidecar: %w", removeErr)
		}
	}
	return nil
}

type syntheticRuntimes struct {
	mu       sync.Mutex
	profiles map[string]*syntheticProfile
}

type syntheticProfile struct {
	mu          sync.Mutex
	root        string
	session     *monarch.Session
	credentials *monarch.StoredCredentials
	vaultKey    string
	expired     bool
	hidden      map[string]bool
}

func newSyntheticRuntimes() *syntheticRuntimes {
	return &syntheticRuntimes{profiles: make(map[string]*syntheticProfile)}
}

func (runtimes *syntheticRuntimes) forRoot(root string) *syntheticProfile {
	runtimes.mu.Lock()
	defer runtimes.mu.Unlock()
	if existing := runtimes.profiles[root]; existing != nil {
		return existing
	}
	created := &syntheticProfile{root: root}
	runtimes.profiles[root] = created
	return created
}

func (runtimes *syntheticRuntimes) sessionPresent(root, providerKind string) (bool, error) {
	if providerKind != "monarch" {
		return false, nil
	}
	profile := runtimes.forRoot(root)
	profile.mu.Lock()
	defer profile.mu.Unlock()
	return profile.session != nil, nil
}

func (runtimes *syntheticRuntimes) runtime(paths home.Paths) (onboarding.Runtime, error) {
	profile := runtimes.forRoot(paths.Root)
	return onboarding.Runtime{
		Sessions:    &syntheticSessions{profile: profile},
		Credentials: &syntheticVault{profile: profile},
		NewConnector: func(config monarch.ImportConfig) (provider.Connector, error) {
			return &syntheticConnector{profile: profile, config: config}, nil
		},
		NewSource: func(monarch.ImportConfig) (provider.Source, error) {
			return &syntheticSource{profile: profile}, nil
		},
		InstanceID: "webtestserver", Now: time.Now,
	}, nil
}

func (runtimes *syntheticRuntimes) expire(profileID string) bool {
	runtimes.mu.Lock()
	defer runtimes.mu.Unlock()
	for root, profile := range runtimes.profiles {
		if filepath.Base(root) != profileID {
			continue
		}
		profile.mu.Lock()
		profile.expired = true
		profile.mu.Unlock()
		return true
	}
	return false
}

type syntheticSessions struct{ profile *syntheticProfile }

func (store *syntheticSessions) Load() (monarch.Session, provider.SessionFingerprint, error) {
	store.profile.mu.Lock()
	defer store.profile.mu.Unlock()
	if store.profile.session == nil {
		return monarch.Session{}, "", os.ErrNotExist
	}
	return *store.profile.session, "synthetic-session", nil
}

func (store *syntheticSessions) Save(session monarch.Session) error {
	store.profile.mu.Lock()
	defer store.profile.mu.Unlock()
	sessionCopy := session
	store.profile.session = &sessionCopy
	return nil
}

func (store *syntheticSessions) Delete() error {
	store.profile.mu.Lock()
	defer store.profile.mu.Unlock()
	store.profile.session = nil
	return nil
}

type syntheticVault struct{ profile *syntheticProfile }

func (vault *syntheticVault) Exists() (bool, error) {
	vault.profile.mu.Lock()
	defer vault.profile.mu.Unlock()
	return vault.profile.credentials != nil, nil
}

func (vault *syntheticVault) Load(password []byte) (monarch.StoredCredentials, error) {
	vault.profile.mu.Lock()
	defer vault.profile.mu.Unlock()
	if vault.profile.credentials == nil || string(password) != vault.profile.vaultKey {
		return monarch.StoredCredentials{}, monarch.ErrCredentialUnlock
	}
	return *vault.profile.credentials, nil
}

func (vault *syntheticVault) Save(credentials monarch.StoredCredentials, password []byte) error {
	vault.profile.mu.Lock()
	defer vault.profile.mu.Unlock()
	credentialsCopy := credentials
	vault.profile.credentials = &credentialsCopy
	vault.profile.vaultKey = string(password)
	return nil
}

type syntheticConnector struct {
	profile *syntheticProfile
	config  monarch.ImportConfig
}

func (connector *syntheticConnector) Connect(
	_ context.Context,
	_ provider.Credentials,
	_ provider.ChallengeResponder,
) (provider.Session, error) {
	connector.profile.mu.Lock()
	defer connector.profile.mu.Unlock()
	connector.profile.expired = false
	now := time.Now().UTC()
	return monarch.Session{
		Version: 2, Token: "synthetic-session-token", DeviceUUID: "synthetic-device",
		RemoteProfileID: syntheticRemotePrefix + filepath.Base(connector.profile.root),
		Import:          connector.config, IssuedAt: now, ValidatedAt: now,
	}, nil
}

func (connector *syntheticConnector) Validate(
	_ context.Context,
	_ provider.Session,
) (provider.ProfileIdentity, error) {
	connector.profile.mu.Lock()
	defer connector.profile.mu.Unlock()
	if connector.profile.expired {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeReconnectRequired)
	}
	return syntheticIdentity(connector.profile), nil
}

type syntheticSource struct{ profile *syntheticProfile }

func (source *syntheticSource) Reader(
	_ context.Context,
	_ bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	source.profile.mu.Lock()
	defer source.profile.mu.Unlock()
	if source.profile.expired {
		return nil, "", provider.NewError(provider.CodeReconnectRequired)
	}
	return &syntheticReader{profile: source.profile}, "synthetic-session", nil
}

func (source *syntheticSource) Writer(
	_ context.Context,
	_ bool,
) (provider.Writer, provider.SessionFingerprint, error) {
	source.profile.mu.Lock()
	defer source.profile.mu.Unlock()
	if source.profile.expired {
		return nil, "", provider.NewError(provider.CodeReconnectRequired)
	}
	return &syntheticWriter{profile: source.profile}, "synthetic-session", nil
}

func (source *syntheticSource) Changed(provider.SessionFingerprint) (bool, error) {
	return false, nil
}

type syntheticReader struct{ profile *syntheticProfile }

type syntheticWriter struct{ profile *syntheticProfile }

func (writer *syntheticWriter) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	writer.profile.mu.Lock()
	defer writer.profile.mu.Unlock()
	if writer.profile.expired {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeReconnectRequired)
	}
	return syntheticIdentity(writer.profile), nil
}

func (writer *syntheticWriter) UpdateTransaction(
	_ context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	writer.profile.mu.Lock()
	defer writer.profile.mu.Unlock()
	if writer.profile.expired {
		return provider.TransactionUpdateResult{}, provider.NewError(provider.CodeReconnectRequired)
	}
	result := provider.TransactionUpdateResult{TransactionExternalID: update.TransactionExternalID}
	if update.MerchantName.Present {
		result.MerchantExternalID = provider.Some("merchant")
		result.MerchantLabel = provider.Some(update.MerchantName.Value)
	}
	if update.CategoryExternalID.Present {
		result.CategoryExternalID = provider.Some(update.CategoryExternalID.Value)
	}
	if update.Hidden.Present {
		result.Hidden = provider.Some(update.Hidden.Value)
		if writer.profile.hidden == nil {
			writer.profile.hidden = make(map[string]bool)
		}
		writer.profile.hidden[update.TransactionExternalID] = update.Hidden.Value
	}
	return result, nil
}

func (writer *syntheticWriter) DeleteTransaction(
	_ context.Context,
	externalID string,
) (provider.TransactionDeleteResult, error) {
	writer.profile.mu.Lock()
	defer writer.profile.mu.Unlock()
	if writer.profile.expired {
		return provider.TransactionDeleteResult{}, provider.NewError(provider.CodeReconnectRequired)
	}
	return provider.TransactionDeleteResult{TransactionExternalID: externalID}, nil
}

func (reader *syntheticReader) ProbeIdentity(context.Context) (provider.ProfileIdentity, error) {
	reader.profile.mu.Lock()
	defer reader.profile.mu.Unlock()
	if reader.profile.expired {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeReconnectRequired)
	}
	return syntheticIdentity(reader.profile), nil
}

func (reader *syntheticReader) FetchSnapshot(
	ctx context.Context,
	progress provider.ProgressFunc,
) (domain.ImportSnapshot, error) {
	reader.profile.mu.Lock()
	expired := reader.profile.expired
	root := reader.profile.root
	hidden := make(map[string]bool, len(reader.profile.hidden))
	for externalID, value := range reader.profile.hidden {
		hidden[externalID] = value
	}
	reader.profile.mu.Unlock()
	if expired {
		return domain.ImportSnapshot{}, provider.NewError(provider.CodeReconnectRequired)
	}
	if progress != nil {
		progress(provider.Progress{Partition: "visible", Fetched: 2, Total: 4, Attempt: 1, Pass: 1})
	}
	select {
	case <-ctx.Done():
		return domain.ImportSnapshot{}, ctx.Err()
	case <-time.After(900 * time.Millisecond):
	}
	if progress != nil {
		progress(provider.Progress{Partition: "visible", Fetched: 4, Total: 4, Attempt: 1, Pass: 1})
	}
	return syntheticSnapshot(root, hidden)
}

func syntheticIdentity(profile *syntheticProfile) provider.ProfileIdentity {
	return provider.ProfileIdentity{
		Kind: "monarch", RemoteID: syntheticRemotePrefix + filepath.Base(profile.root),
	}
}

func syntheticSnapshot(root string, hidden map[string]bool) (domain.ImportSnapshot, error) {
	date, err := domain.ParseDate("2026-08-18")
	if err != nil {
		return domain.ImportSnapshot{}, err
	}
	profileKey := filepath.Base(root)
	snapshot := domain.ImportSnapshot{
		ObservedAt: time.Now().UTC(),
		Accounts: []domain.ImportEntity{{
			Kind: domain.EntityKindAccount, ExternalID: "account", Label: "Account Name",
		}},
		Merchants: []domain.ImportEntity{{
			Kind: domain.EntityKindMerchant, ExternalID: "merchant", Label: "Example Merchant",
		}},
		Groups: []domain.ImportEntity{{
			Kind: domain.EntityKindGroup, ExternalID: "group", Label: "Example Group",
		}},
		Categories: []domain.ImportEntity{{
			Kind: domain.EntityKindCategory, ExternalID: "category", ParentExternalID: "group",
			Label: "Example Category",
		}},
	}
	for index := range 4 {
		externalID := fmt.Sprintf("%s-transaction-%d", profileKey, index)
		snapshot.Transactions = append(snapshot.Transactions, domain.ImportTransaction{
			ExternalID:        externalID,
			AccountExternalID: "account", MerchantExternalID: "merchant",
			CategoryExternalID: "category", Date: date,
			Amount: domain.Money{Minor: int64(-1000 - index), Currency: "USD", Scale: 2},
			Hidden: hidden[externalID],
		})
	}
	return snapshot, snapshot.Validate()
}

func testControlHandler(next http.Handler, runtimes *syntheticRuntimes) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		const prefix = "/__moneyflow_test/profiles/"
		if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, prefix) &&
			strings.HasSuffix(request.URL.Path, "/expire") {
			profileID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, prefix), "/expire")
			if !profilecatalog.ValidProfileID(profileID) || !runtimes.expire(profileID) {
				http.Error(response, "profile not found", http.StatusNotFound)
				return
			}
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}
