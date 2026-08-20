// Package amazonimport coordinates renderer-neutral Amazon import attempts.
package amazonimport

import (
	"context"
	"io"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

// ProtocolVersion is the renderer/coordinator state-machine version.
const ProtocolVersion uint16 = 1

// State identifies one import attempt phase.
type State string

// Import attempt states.
const (
	StateSettingsRequired State = "settings_required"
	StateSourceRequired   State = "source_required"
	StateParsing          State = "parsing"
	StateInstalling       State = "installing"
	StateComplete         State = "complete"
	StateFailed           State = "failed"
	StateCanceled         State = "canceled"
)

// Progress is a counts-only status safe for polling and logs.
type Progress struct {
	Phase     string `json:"phase"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
}

// Failure is a coordinate-blind terminal status.
type Failure struct {
	Code   Code   `json:"code"`
	Detail string `json:"detail"`
}

// Snapshot is the credential- and coordinate-blind attempt state.
type Snapshot struct {
	ProtocolVersion uint16                 `json:"protocol_version"`
	AttemptID       string                 `json:"attempt_id"`
	ProfileID       string                 `json:"profile_id"`
	State           State                  `json:"state"`
	StateVersion    uint64                 `json:"state_version"`
	Progress        Progress               `json:"progress"`
	Result          app.AmazonImportResult `json:"result"`
	Failure         Failure                `json:"failure"`
}

// Target is one resolved profile and its atomic import boundary.
type Target struct {
	ProfileID string
	Root      string
	Import    ImportFunc
	Close     func() error
}

// ImportFunc applies a parsed candidate atomically.
type ImportFunc func(context.Context, app.AmazonImportRequest) (app.AmazonImportResult, error)

// ResolveTargetFunc opens the exact target without reading an import source.
type ResolveTargetFunc func(context.Context, string) (Target, error)

// DiscoverFunc discovers bounded directory inputs.
type DiscoverFunc func(context.Context, string, amazon.Limits) ([]amazon.SourceFile, error)

// ParseFunc parses bounded staged or directory inputs.
type ParseFunc func(context.Context, []amazon.SourceFile, amazon.Settings, amazon.Limits, amazon.ObserveFunc) (amazon.Candidate, error)

// Config supplies all coordinator side-effect boundaries.
type Config struct {
	InstanceID    string
	Now           func() time.Time
	Random        io.Reader
	Limits        amazon.Limits
	ResolveTarget ResolveTargetFunc
	Discover      DiscoverFunc
	Parse         ParseFunc
}

// StartRequest creates one profile-bound attempt.
type StartRequest struct {
	ProfileID     string
	Settings      amazon.Settings
	TaxonomyClone *app.TaxonomyClone
}

// Upload is one bounded browser-supplied CSV stream.
type Upload struct {
	RelativeName string
	Reader       io.Reader
}

// StageRequest streams private files into one attempt.
type StageRequest struct {
	ProfileID            string
	AttemptID            string
	ExpectedStateVersion uint64
	Files                []Upload
}

// ExecuteRequest parses and atomically installs a staged attempt.
type ExecuteRequest struct {
	ProfileID            string
	AttemptID            string
	ExpectedStateVersion uint64
}

// StatusRequest reads one coordinate-blind attempt status.
type StatusRequest struct {
	ProfileID string
	AttemptID string
}

// CancelRequest cancels a not-yet-installing attempt.
type CancelRequest struct {
	ProfileID            string
	AttemptID            string
	ExpectedStateVersion uint64
}

// DirectoryRequest runs the synchronous CLI/TUI directory workflow.
type DirectoryRequest struct {
	ProfileID     string
	Directory     string
	Settings      amazon.Settings
	TaxonomyClone *app.TaxonomyClone
	Observe       func(Progress)
}
