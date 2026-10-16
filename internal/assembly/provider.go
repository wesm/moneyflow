// Package assembly builds the provider runtimes and catalog adapters that the command line and
// the embedding package share.
package assembly

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// MonarchSessionStore is the provider-owned Monarch session surface.
type MonarchSessionStore interface {
	Load() (monarch.Session, provider.SessionFingerprint, error)
	Save(monarch.Session) error
	Delete() error
}

// MonarchCredentialVault is the password-protected Monarch credential surface.
type MonarchCredentialVault interface {
	Exists() (bool, error)
	Load([]byte) (monarch.StoredCredentials, error)
	Save(monarch.StoredCredentials, []byte) error
}

// MonarchRuntime contains the connection dependencies for one Monarch profile.
type MonarchRuntime struct {
	Connector   provider.Connector
	Sessions    MonarchSessionStore
	Credentials MonarchCredentialVault
	ReadSource  provider.ReaderSource
	WriteSource provider.WriterSource
	InstanceID  string
	Now         func() time.Time
}

// MonarchFactory constructs Monarch dependencies below one profile root.
type MonarchFactory func(home.Paths, monarch.ImportConfig) (MonarchRuntime, error)

// YNABFactory constructs the YNAB onboarding runtime below one profile root.
type YNABFactory func(home.Paths) (onboarding.Runtime, error)

// SimpleFINFactory constructs SimpleFIN dependencies below one profile root.
type SimpleFINFactory func(home.Paths) (simplefinonboarding.Runtime, error)

// Factories replaces provider construction in tests. A nil field selects the production runtime.
type Factories struct {
	Monarch   MonarchFactory
	YNAB      YNABFactory
	SimpleFIN SimpleFINFactory
}

func (factories Factories) ynab() YNABFactory {
	if factories.YNAB == nil {
		return NewYNABRuntime
	}
	return factories.YNAB
}

func (factories Factories) simpleFIN() SimpleFINFactory {
	if factories.SimpleFIN == nil {
		return NewSimpleFINRuntime
	}
	return factories.SimpleFIN
}

// NewInstanceID names one provider runtime: the renderer, a dash and 32 random hex digits.
func NewInstanceID(renderer string) (string, error) {
	material := make([]byte, 16)
	if _, err := cryptorand.Read(material); err != nil {
		return "", errors.New("create provider instance identity")
	}
	return renderer + "-" + hex.EncodeToString(material), nil
}

// NewMonarchRuntime builds the production Monarch dependencies. An invalid importConfig selects
// the stored session's import settings.
func NewMonarchRuntime(
	paths home.Paths,
	importConfig monarch.ImportConfig,
) (MonarchRuntime, error) {
	sessions, err := monarch.NewSessionStore(paths)
	if err != nil {
		return MonarchRuntime{}, err
	}
	if importConfig.Validate() != nil {
		if storedSession, _, loadErr := sessions.Load(); loadErr == nil {
			importConfig = storedSession.Import
		}
	}
	options := monarch.Options{
		ImportCurrency: importConfig.Currency,
		ImportScale:    importConfig.Scale,
	}
	connector, err := monarch.NewAuthenticator(options)
	if err != nil {
		return MonarchRuntime{}, err
	}
	source, err := monarch.NewSource(options, sessions)
	if err != nil {
		return MonarchRuntime{}, err
	}
	credentials, err := monarch.NewCredentialVault(paths)
	if err != nil {
		return MonarchRuntime{}, err
	}
	instanceID, err := NewInstanceID("cli")
	if err != nil {
		return MonarchRuntime{}, err
	}
	return MonarchRuntime{
		Connector: connector, Sessions: sessions, Credentials: credentials,
		ReadSource: source, WriteSource: source, InstanceID: instanceID, Now: time.Now,
	}, nil
}

// NewYNABRuntime builds the production YNAB onboarding runtime.
func NewYNABRuntime(paths home.Paths) (onboarding.Runtime, error) {
	vault, err := ynab.NewCredentialVault(paths)
	if err != nil {
		return onboarding.Runtime{}, err
	}
	instanceID, err := NewInstanceID("cli")
	if err != nil {
		return onboarding.Runtime{}, err
	}
	return onboarding.Runtime{
		ProviderKind: "ynab", YNABVault: vault, InstanceID: instanceID, Now: time.Now,
		NewYNABClient: func(token []byte) (onboarding.YNABPlanClient, error) {
			return ynab.NewClient(ynab.ClientOptions{}, string(token))
		},
		NewYNABSource: func(
			credentials ynab.StoredCredentials,
			initial *provider.SnapshotResult,
		) (provider.ReaderSource, provider.WriterSource, error) {
			source, err := ynab.NewSource(ynab.SourceOptions{
				Credentials: credentials, Vault: vault, Now: time.Now, Initial: initial,
			})
			if err != nil {
				return nil, nil, err
			}
			return source, source, nil
		},
	}, nil
}

// NewSimpleFINRuntime builds the production SimpleFIN dependencies.
func NewSimpleFINRuntime(paths home.Paths) (simplefinonboarding.Runtime, error) {
	sessions, err := simplefin.NewSessionStore(paths)
	if err != nil {
		return simplefinonboarding.Runtime{}, err
	}
	id, err := NewInstanceID("simplefin")
	if err != nil {
		return simplefinonboarding.Runtime{}, err
	}
	return simplefinonboarding.Runtime{
		Sessions: sessions, InstanceID: id, Now: time.Now,
		Claim: func(ctx context.Context, input string) (string, error) {
			return simplefin.Claim(ctx, input, nil)
		},
		NewSource: func(
			connection app.ProviderConnectionState,
			session simplefin.Session,
		) (provider.ReaderSource, error) {
			currency, scale := session.Import.Currency, session.Import.Scale
			if connection.Bound {
				currency, scale = connection.Currency, connection.Scale
			}
			return simplefin.NewSource(simplefin.SourceOptions{
				ExpectedRemoteID: connection.RemoteProfileID, Currency: currency, Scale: scale,
			}, sessions)
		},
	}, nil
}

// MonarchOnboardingRuntime adapts a Monarch factory to the onboarding runtime. A nil factory
// selects NewMonarchRuntime, and an invalid initial configuration selects USD with scale 2.
func MonarchOnboardingRuntime(
	paths home.Paths,
	factory MonarchFactory,
	initial monarch.ImportConfig,
) (onboarding.Runtime, error) {
	if factory == nil {
		factory = NewMonarchRuntime
	}
	if initial.Validate() != nil {
		initial = monarch.ImportConfig{Currency: "USD", Scale: 2}
	}
	bootstrap, err := factory(paths, initial)
	if err != nil {
		return onboarding.Runtime{}, err
	}
	if bootstrap.Sessions == nil || bootstrap.Credentials == nil || bootstrap.InstanceID == "" {
		return onboarding.Runtime{}, errors.New("monarch command dependencies are incomplete")
	}
	now := bootstrap.Now
	if now == nil {
		now = time.Now
	}
	return onboarding.Runtime{
		ProviderKind: "monarch", Sessions: bootstrap.Sessions, Credentials: bootstrap.Credentials,
		InstanceID: bootstrap.InstanceID, Now: now,
		NewConnector: func(config monarch.ImportConfig) (provider.Connector, error) {
			runtime, runtimeErr := factory(paths, config)
			if runtimeErr != nil {
				return nil, runtimeErr
			}
			if runtime.Connector == nil {
				return nil, errors.New("monarch connector is unavailable")
			}
			return runtime.Connector, nil
		},
		NewSources: func(config monarch.ImportConfig) (
			provider.ReaderSource,
			provider.WriterSource,
			error,
		) {
			runtime, runtimeErr := factory(paths, config)
			if runtimeErr != nil {
				return nil, nil, runtimeErr
			}
			if runtime.ReadSource == nil || runtime.WriteSource == nil {
				return nil, nil, errors.New("monarch source is unavailable")
			}
			return runtime.ReadSource, runtime.WriteSource, nil
		},
	}, nil
}

// ProviderOnboardingRuntime selects the onboarding runtime for the provider kind in the
// profile's manifest: YNAB, or Monarch for every other kind.
func ProviderOnboardingRuntime(paths home.Paths, factories Factories) (onboarding.Runtime, error) {
	manifestPath := filepath.Join(paths.Root, profilecatalog.ManifestFilename)
	manifest, err := profilecatalog.ReadManifest(manifestPath)
	if err != nil {
		return onboarding.Runtime{}, err
	}
	if manifest.ProviderKind == "ynab" {
		return factories.ynab()(paths)
	}
	usd := monarch.ImportConfig{Currency: "USD", Scale: 2}
	return MonarchOnboardingRuntime(paths, factories.Monarch, usd)
}

// ConfigureProvider installs the reader runtime of a bound Monarch or SimpleFIN profile. A YNAB
// profile stays locked until onboarding or UnlockYNAB supplies its password. Amazon and local
// profiles have no reader runtime.
func ConfigureProvider(
	ctx context.Context,
	service *app.Service,
	paths home.Paths,
	factories Factories,
	renderer string,
) error {
	connection, err := service.ProviderConnection(ctx)
	if err != nil {
		return err
	}
	if !connection.Bound {
		return nil
	}
	switch connection.Kind {
	case "simplefin":
		return configureSimpleFIN(service, paths, factories.simpleFIN(), renderer, connection)
	case "monarch":
		return configureMonarch(service, paths, factories.Monarch, renderer, connection)
	default:
		return nil
	}
}

func configureSimpleFIN(
	service *app.Service,
	paths home.Paths,
	factory SimpleFINFactory,
	renderer string,
	connection app.ProviderConnectionState,
) error {
	runtime, err := factory(paths)
	if err != nil || runtime.NewSource == nil {
		return errors.New("SimpleFIN renderer dependencies are unavailable")
	}
	// Binding supplies settings and expected identity; the source loads credentials lazily.
	source, err := runtime.NewSource(connection, simplefin.Session{})
	if err != nil || source == nil {
		return errors.New("SimpleFIN renderer source is unavailable")
	}
	return service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "simplefin", Currency: connection.Currency,
		Scale: connection.Scale, Renderer: renderer, InstanceID: runtime.InstanceID, Now: runtime.Now,
	})
}

func configureMonarch(
	service *app.Service,
	paths home.Paths,
	factory MonarchFactory,
	renderer string,
	connection app.ProviderConnectionState,
) error {
	production := factory == nil
	if production {
		factory = NewMonarchRuntime
	}
	importConfig := monarch.ImportConfig{Currency: connection.Currency, Scale: connection.Scale}
	runtime, err := factory(paths, importConfig)
	if err != nil {
		return err
	}
	if runtime.ReadSource == nil || runtime.WriteSource == nil {
		return errors.New("monarch renderer source is unavailable")
	}
	if production {
		runtime.InstanceID, err = NewInstanceID(renderer)
		if err != nil {
			return err
		}
	}
	return service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: runtime.ReadSource, WriteSource: runtime.WriteSource, Provider: "monarch",
		Currency: importConfig.Currency, Scale: importConfig.Scale, Renderer: renderer,
		InstanceID: runtime.InstanceID, Now: runtime.Now,
	})
}

// InspectSession reports whether a provider's local session or credential file exists.
func InspectSession(profileRoot string, providerKind string) (bool, error) {
	switch providerKind {
	case "monarch":
		return monarch.SessionFilePresent(profileRoot)
	case "ynab":
		return ynab.CredentialFilePresent(profileRoot)
	case "simplefin":
		return simplefin.SessionFilePresent(profileRoot)
	default:
		return false, nil
	}
}

var (
	// ErrNotYNAB reports an unlock of a profile that is not a connected YNAB profile.
	ErrNotYNAB = errors.New("profile is not a connected YNAB profile")
	// ErrVaultMissing reports a connected YNAB profile without a credential vault.
	ErrVaultMissing = errors.New("YNAB vault is missing")
	// ErrWrongPassword reports a password that does not open the YNAB credential vault.
	ErrWrongPassword = ynab.ErrCredentialUnlock
)

// UnlockRequest unlocks one opened YNAB profile.
type UnlockRequest struct {
	Service *app.Service
	Paths   home.Paths
	// YNAB builds the runtime; nil selects NewYNABRuntime.
	YNAB     YNABFactory
	Password func(context.Context) (string, error)
	Renderer string
}

// UnlockYNAB opens the profile's YNAB credential vault with the password and installs a reader
// and writer runtime. It does no network work.
func UnlockYNAB(ctx context.Context, request UnlockRequest) error {
	connectLock, err := home.TryLock(request.Paths.Root, home.LockProviderConnect, home.LockExclusive)
	if err != nil {
		return fmt.Errorf("unlock YNAB: another connection attempt may be active: %w", err)
	}
	defer func() { _ = connectLock.Release() }()
	connection, err := request.Service.ProviderConnection(ctx)
	if err != nil {
		return err
	}
	if !connection.Bound || connection.Kind != "ynab" {
		return ErrNotYNAB
	}
	factory := request.YNAB
	if factory == nil {
		factory = NewYNABRuntime
	}
	configured, err := factory(request.Paths)
	if err != nil {
		return err
	}
	if configured.YNABVault == nil || configured.NewYNABSource == nil {
		return errors.New("YNAB vault runtime is unavailable")
	}
	exists, err := configured.YNABVault.Exists()
	if err != nil {
		return err
	}
	if !exists {
		return ErrVaultMissing
	}
	password, err := request.Password(ctx)
	if err != nil {
		return err
	}
	secret := []byte(password)
	defer clear(secret)
	credentials, err := configured.YNABVault.Load(secret)
	if err != nil {
		return fmt.Errorf("unlock YNAB vault: %w", err)
	}
	if credentials.PlanID != connection.RemoteProfileID {
		return provider.NewError(provider.CodeIdentityMismatch)
	}
	if credentials.Currency != connection.Currency || credentials.Scale != connection.Scale {
		return provider.NewError(provider.CodeMoneyMismatch)
	}
	readSource, writeSource, err := configured.NewYNABSource(credentials, nil)
	if err != nil {
		return err
	}
	if readSource == nil || writeSource == nil {
		return errors.New("YNAB provider source is unavailable")
	}
	instanceID, err := NewInstanceID(request.Renderer)
	if err != nil {
		return err
	}
	return request.Service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: readSource, WriteSource: writeSource, Provider: "ynab",
		Currency: connection.Currency, Scale: connection.Scale,
		Renderer: request.Renderer, InstanceID: instanceID, Now: configured.Now,
	})
}
