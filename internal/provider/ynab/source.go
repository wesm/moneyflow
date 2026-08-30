package ynab

import (
	"context"
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/provider"
)

// SourceOptions configure one unlocked YNAB profile runtime.
type SourceOptions struct {
	Client      ClientOptions
	Credentials StoredCredentials
	Vault       *CredentialVault
	Now         func() time.Time
}

// Source creates readers from one unlocked vault payload.
type Source struct {
	options     SourceOptions
	fingerprint provider.SessionFingerprint
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
	return &Source{options: options, fingerprint: fingerprint}, nil
}

// Reader returns a reader over the already unlocked access token.
func (source *Source) Reader(
	context.Context,
	bool,
) (provider.Reader, provider.SessionFingerprint, error) {
	client, err := NewClient(source.options.Client, source.options.Credentials.AccessToken)
	if err != nil {
		return nil, source.fingerprint, err
	}
	return &reader{
		client: client, planID: source.options.Credentials.PlanID, now: source.options.Now,
	}, source.fingerprint, nil
}

// Changed reports atomic vault replacement or deletion.
func (source *Source) Changed(previous provider.SessionFingerprint) (bool, error) {
	fingerprint, err := source.options.Vault.Fingerprint()
	if err != nil {
		return true, err
	}
	return fingerprint != previous, nil
}

type reader struct {
	client *Client
	planID string
	now    func() time.Time
}

func (reader *reader) FetchSnapshot(
	ctx context.Context,
	progress provider.ProgressFunc,
) (provider.SnapshotResult, error) {
	plan, err := reader.client.FetchPlan(ctx, reader.planID)
	if err != nil {
		return provider.SnapshotResult{}, err
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
	return provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: providerKind, RemoteID: plan.ID},
		Snapshot: snapshot,
	}, nil
}

var _ provider.ReaderSource = (*Source)(nil)
