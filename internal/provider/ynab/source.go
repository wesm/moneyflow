package ynab

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/provider"
)

// SourceOptions configure one unlocked YNAB profile runtime.
type SourceOptions struct {
	Client      ClientOptions
	Credentials StoredCredentials
	Vault       *CredentialVault
	Now         func() time.Time
	Initial     *provider.SnapshotResult
}

// Source creates readers and writers from one unlocked vault payload.
type Source struct {
	options     SourceOptions
	fingerprint provider.SessionFingerprint
	initialMu   sync.Mutex
	initial     *provider.SnapshotResult
}

// NewSource validates an unlocked runtime and captures its vault generation.
func NewSource(options SourceOptions) (*Source, error) {
	if options.Vault == nil || options.Credentials.Validate() != nil {
		return nil, errors.New("create YNAB source: credentials are invalid")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	fingerprint, err := options.Vault.Fingerprint()
	if err != nil {
		return nil, err
	}
	var initial *provider.SnapshotResult
	if options.Initial != nil {
		cloned := *options.Initial
		cloned.Snapshot = options.Initial.Snapshot.Clone()
		initial = &cloned
	}
	return &Source{options: options, fingerprint: fingerprint, initial: initial}, nil
}

// Reader returns a reader over the already unlocked access token.
func (source *Source) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	if err := source.validateVault(); err != nil {
		return nil, source.fingerprint, err
	}
	client, err := NewClient(source.options.Client, source.options.Credentials.AccessToken)
	if err != nil {
		return nil, source.fingerprint, err
	}
	source.initialMu.Lock()
	client.validate = source.validateVault
	initial := source.initial
	source.initial = nil
	source.initialMu.Unlock()
	return &reader{
		client: client, planID: source.options.Credentials.PlanID, now: source.options.Now,
		initial: initial, source: source,
	}, source.fingerprint, nil
}

// Writer never decrypts or reloads a changed vault; explicit unlock creates a new Source.
func (source *Source) Writer(context.Context, bool) (provider.Writer, provider.SessionFingerprint, error) {
	if err := source.validateVault(); err != nil {
		return nil, source.fingerprint, err
	}
	client, err := NewClient(source.options.Client, source.options.Credentials.AccessToken)
	if err != nil {
		return nil, source.fingerprint, err
	}
	credentials := source.options.Credentials
	return &transactionWriter{
		client: client, planID: credentials.PlanID, currency: credentials.Currency, scale: credentials.Scale,
		now: source.options.Now, validate: source.validateVault,
	}, source.fingerprint, nil
}

// Changed reports atomic vault replacement or deletion.
func (source *Source) Changed(previous provider.SessionFingerprint) (bool, error) {
	fingerprint, err := source.options.Vault.Fingerprint()
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return fingerprint != previous, nil
}

func (source *Source) validateVault() error {
	changed, err := source.Changed(source.fingerprint)
	if err != nil || changed {
		return provider.NewError(provider.CodeReconnectRequired)
	}
	return nil
}

type reader struct {
	source  *Source
	client  *Client
	planID  string
	now     func() time.Time
	initial *provider.SnapshotResult
}

func (reader *reader) FetchSnapshot(
	ctx context.Context,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	if err := reader.source.validateVault(); err != nil {
		return provider.SnapshotResult{}, err
	}
	if reader.initial != nil {
		result := *reader.initial
		result.Snapshot = reader.initial.Snapshot.Clone()
		reader.initial = nil
		if progress != nil {
			progress(provider.Progress{Partition: "all", Fetched: len(result.Snapshot.Transactions), Total: len(result.Snapshot.Transactions), Attempt: 1})
		}
		if err := reader.source.validateVault(); err != nil {
			return provider.SnapshotResult{}, err
		}
		return result, nil
	}
	plan, err := reader.client.FetchPlan(ctx, reader.planID)
	if err != nil {
		return provider.SnapshotResult{}, err
	}
	// The plan declares its money interpretation even when there are no rows.
	// Row-level checks alone would let an empty budget bypass the immutable binding.
	credentials := reader.source.options.Credentials
	if plan.CurrencyFormat.ISOCode != string(credentials.Currency) ||
		*plan.CurrencyFormat.DecimalDigits != int(credentials.Scale) {
		return provider.SnapshotResult{}, provider.NewError(provider.CodeMoneyMismatch)
	}
	if progress != nil {
		progress(provider.Progress{
			Partition: "all", Fetched: len(plan.Transactions), Total: len(plan.Transactions), Attempt: 1,
		})
	}
	snapshot, err := Normalize(plan, reader.now().UTC())
	if err != nil {
		return provider.SnapshotResult{}, err
	}
	if err := reader.source.validateVault(); err != nil {
		return provider.SnapshotResult{}, err
	}
	return provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: providerKind, RemoteID: plan.ID},
		Snapshot: snapshot,
	}, nil
}

var _ provider.ReaderSource = (*Source)(nil)
var _ provider.WriterSource = (*Source)(nil)
