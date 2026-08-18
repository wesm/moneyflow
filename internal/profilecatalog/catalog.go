package profilecatalog

import (
	"errors"
	"io"
	"path/filepath"
	"time"

	"github.com/wesm/moneyflow/internal/home"
)

const (
	// LegacyKey identifies a manifest-less root profile only within one catalog process.
	LegacyKey = "legacy"
	// RecoveryDirectoryName is the fixed profile-local backup container.
	RecoveryDirectoryName = "recovery"
	// RecoveryMarkerFilename identifies an unfinished supported recovery.
	RecoveryMarkerFilename = "recovery-in-progress.json"
)

// SessionPresence checks only whether a provider's local session file exists.
type SessionPresence func(profileRoot, providerKind string) (bool, error)

// Config supplies deterministic catalog dependencies and paths.
type Config struct {
	Paths          home.CatalogPaths
	Random         io.Reader
	Now            func() time.Time
	Version        string
	InspectSession SessionPresence
	RecoveryFault  func(RecoveryFaultPoint) error
}

// Catalog owns filesystem discovery and profile lifecycle operations.
type Catalog struct {
	paths          home.CatalogPaths
	random         io.Reader
	now            func() time.Time
	version        string
	inspectSession SessionPresence
	recoveryFault  func(RecoveryFaultPoint) error
}

// New validates and constructs one profile catalog.
func New(config Config) (*Catalog, error) {
	if config.Paths.Root == "" || config.Paths.Profiles == "" ||
		!filepath.IsAbs(config.Paths.Root) || !filepath.IsAbs(config.Paths.Profiles) ||
		filepath.Clean(config.Paths.Profiles) != filepath.Join(config.Paths.Root, "profiles") {
		return nil, newError(CodeProfileInvalid, errors.New("catalog paths are invalid"))
	}
	if config.Random == nil || config.Now == nil || config.Version == "" {
		return nil, newError(CodeProfileInvalid, errors.New("catalog dependencies are incomplete"))
	}
	return &Catalog{
		paths: config.Paths, random: config.Random, now: config.Now,
		version: config.Version, inspectSession: config.InspectSession,
		recoveryFault: config.RecoveryFault,
	}, nil
}

// Paths returns the resolved catalog layout.
func (catalog *Catalog) Paths() home.CatalogPaths { return catalog.paths }

// Status is one local-only selector state.
type Status string

// Local profile statuses.
const (
	StatusReady               Status = "ready"
	StatusReconnect           Status = "reconnect"
	StatusSetupIncomplete     Status = "setup_incomplete"
	StatusLocalOnly           Status = "local_only"
	StatusNeedsRecovery       Status = "needs_recovery"
	StatusRequiresNewer       Status = "requires_newer_moneyflow"
	StatusManifestUnsupported Status = "manifest_unsupported"
)

// Entry is one discovered profile without any financial contents.
type Entry struct {
	Key          string
	ID           string
	DisplayName  string
	ProviderKind string
	Root         string
	Status       Status
}

// ProfilePaths returns the fixed database layout for this entry.
func (entry Entry) ProfilePaths() home.Paths {
	return home.Paths{Root: entry.Root, Database: filepath.Join(entry.Root, "moneyflow.db")}
}
