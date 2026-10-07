package uitest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
)

// Fault controls the next update at the synthetic provider boundary.
type Fault string

// Faults affect one provider update; the consumed fault is persisted before returning.
const (
	FaultNone       Fault = ""
	FaultReject     Fault = "reject"
	FaultUnknown    Fault = "unknown"
	FaultBlock      Fault = "block"
	FaultBlockAfter Fault = "block-after"
)

// Call records one actual provider invocation, independently of the local journal.
type Call struct {
	Method  string                      `json:"method"`
	Target  string                      `json:"target,omitempty"`
	Update  *provider.TransactionUpdate `json:"update,omitempty"`
	Outcome string                      `json:"outcome"`
}

// ProviderState is the durable simulator state, separate from Moneyflow's database.
type ProviderState struct {
	Now          time.Time                           `json:"now"`
	Catalog      domain.ImportSnapshot               `json:"catalog"`
	Transactions map[string]domain.ImportTransaction `json:"transactions"`
	Calls        []Call                              `json:"calls"`
	Fault        Fault                               `json:"fault"`
}

// Provider implements synthetic reads, writes and exact-transaction recovery.
type Provider struct {
	mu      sync.Mutex
	path    string
	state   ProviderState
	release chan struct{}
}

// OpenProvider opens the same remote state after restart; only a new root is seeded.
func OpenProvider(root string) (*Provider, error) {
	path := filepath.Join(root, "provider-state.json")
	source := &Provider{path: path}
	data, err := home.ReadPrivateFile(path, 16<<20)
	if err == nil {
		if err = json.Unmarshal(data, &source.state); err != nil {
			return nil, fmt.Errorf("decode simulator: %w", err)
		}
		if err = source.snapshotLocked().Validate(); err != nil {
			return nil, fmt.Errorf("validate simulator: %w", err)
		}
		return source, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read simulator: %w", err)
	}
	fixture := Fixture()
	source.state = ProviderState{Now: fixture.ObservedAt, Catalog: fixture, Transactions: make(map[string]domain.ImportTransaction)}
	source.state.Catalog.Transactions = nil
	for _, row := range fixture.Transactions {
		source.state.Transactions[row.ExternalID] = row
	}
	if err = source.saveLocked(); err != nil {
		return nil, err
	}
	return source, nil
}

func (source *Provider) saveLocked() error {
	data, err := json.Marshal(source.state)
	if err != nil {
		return fmt.Errorf("encode simulator: %w", err)
	}
	return home.WritePrivateFile(source.path, data)
}

// State returns an independently owned observation.
func (source *Provider) State() ProviderState {
	source.mu.Lock()
	defer source.mu.Unlock()
	state := source.state
	state.Catalog = state.Catalog.Clone()
	state.Transactions = maps.Clone(state.Transactions)
	state.Calls = slices.Clone(state.Calls)
	for i := range state.Calls {
		if state.Calls[i].Update != nil {
			state.Calls[i].Update = new(*state.Calls[i].Update)
		}
	}
	return state
}

// Now supplies logical provider time without replacing wall-clock deadlines.
func (source *Provider) Now() time.Time {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.state.Now
}

// Advance moves the persisted scenario clock at an explicit checkpoint.
func (source *Provider) Advance(duration time.Duration) error {
	source.mu.Lock()
	defer source.mu.Unlock()
	if duration < 0 {
		return errors.New("scenario clock cannot move backwards")
	}
	source.state.Now = source.state.Now.Add(duration)
	return source.saveLocked()
}

// SetFault arms one update; it cannot replace a currently blocked invocation.
func (source *Provider) SetFault(fault Fault) error {
	source.mu.Lock()
	defer source.mu.Unlock()
	switch fault {
	case FaultNone, FaultReject, FaultUnknown, FaultBlock, FaultBlockAfter:
	default:
		return fmt.Errorf("unknown scenario fault %q", fault)
	}
	if source.release != nil {
		return errors.New("release the blocked provider call first")
	}
	source.state.Fault = fault
	return source.saveLocked()
}

// Release allows a deliberately delayed call to finish.
func (source *Provider) Release() {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.release != nil {
		close(source.release)
		source.release = nil
	}
}

func (source *Provider) snapshotLocked() domain.ImportSnapshot {
	snapshot := source.state.Catalog.Clone()
	snapshot.ObservedAt = source.state.Now
	for _, id := range slices.Sorted(maps.Keys(source.state.Transactions)) {
		snapshot.Transactions = append(snapshot.Transactions, source.state.Transactions[id])
	}
	return snapshot
}

// Reader opens the synthetic provider, never a network client.
func (source *Provider) Reader(context.Context, bool) (provider.Reader, provider.SessionFingerprint, error) {
	return source, "scenario-session", nil
}

// Writer opens the synthetic provider, never a credential file.
func (source *Provider) Writer(context.Context, bool) (provider.Writer, provider.SessionFingerprint, error) {
	return source, "scenario-session", nil
}

// Changed reports whether the caller has the scenario's session generation.
func (*Provider) Changed(previous provider.SessionFingerprint) (bool, error) {
	return previous != "scenario-session", nil
}

// ProbeIdentity records the identity check required by the production worker.
func (source *Provider) ProbeIdentity(ctx context.Context) (provider.ProfileIdentity, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return provider.ProfileIdentity{}, err
	}
	source.state.Calls = append(source.state.Calls, Call{Method: "identity", Outcome: "ok"})
	return provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-ui-scenario"}, source.saveLocked()
}

// FetchSnapshot records full downloads so tests can enforce their request budget.
func (source *Provider) FetchSnapshot(ctx context.Context, _ provider.FetchRequest, _ provider.ProgressFunc) (provider.SnapshotResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return provider.SnapshotResult{}, err
	}
	source.state.Calls = append(source.state.Calls, Call{Method: "snapshot", Outcome: "ok"})
	return provider.SnapshotResult{Identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "synthetic-ui-scenario"}, Snapshot: source.snapshotLocked()}, source.saveLocked()
}

func (source *Provider) resultLocked(row domain.ImportTransaction) provider.TransactionUpdateResult {
	result := provider.TransactionUpdateResult{TransactionExternalID: row.ExternalID,
		MerchantExternalID: provider.Some(row.MerchantExternalID), Hidden: provider.Some(row.Hidden)}
	for _, merchant := range source.state.Catalog.Merchants {
		if merchant.ExternalID == row.MerchantExternalID {
			result.MerchantLabel = provider.Some(merchant.Label)
			break
		}
	}
	if row.CategoryExternalID == "" {
		result.CategoryCleared = true
	} else {
		result.CategoryExternalID = provider.Some(row.CategoryExternalID)
	}
	return result
}

// ReadTransaction observes only the requested row, including its date binding.
func (source *Provider) ReadTransaction(ctx context.Context, id string, date domain.Date) (provider.TransactionUpdateResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return provider.TransactionUpdateResult{}, err
	}
	row, exists := source.state.Transactions[id]
	if !exists || row.Date != date {
		source.state.Calls = append(source.state.Calls, Call{Method: "readback", Target: id, Outcome: "not-found"})
		return provider.TransactionUpdateResult{}, errors.Join(provider.NewWriteFailure(provider.WriteTargetNotFound), source.saveLocked())
	}
	source.state.Calls = append(source.state.Calls, Call{Method: "readback", Target: id, Outcome: "ok"})
	return source.resultLocked(row), source.saveLocked()
}

// DeleteTransaction implements absolute, idempotent deletion.
func (source *Provider) DeleteTransaction(ctx context.Context, id string) (provider.TransactionDeleteResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return provider.TransactionDeleteResult{}, err
	}
	_, exists := source.state.Transactions[id]
	delete(source.state.Transactions, id)
	source.state.Calls = append(source.state.Calls, Call{Method: "delete", Target: id, Outcome: "applied"})
	return provider.TransactionDeleteResult{TransactionExternalID: id, AlreadyAbsent: !exists}, source.saveLocked()
}

// UpdateTransaction changes only requested fields and consumes one injected fault.
func (source *Provider) UpdateTransaction(ctx context.Context, update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
	source.mu.Lock()
	if err := ctx.Err(); err != nil {
		source.mu.Unlock()
		return provider.TransactionUpdateResult{}, err
	}
	row, exists := source.state.Transactions[update.TransactionExternalID]
	if !exists {
		source.state.Calls = append(source.state.Calls, Call{Method: "update", Target: update.TransactionExternalID, Update: new(update), Outcome: "not-found"})
		err := source.saveLocked()
		source.mu.Unlock()
		return provider.TransactionUpdateResult{}, errors.Join(provider.NewWriteFailure(provider.WriteTargetNotFound), err)
	}
	fault := source.state.Fault
	source.state.Fault = FaultNone
	index := len(source.state.Calls)
	source.state.Calls = append(source.state.Calls, Call{Method: "update", Target: row.ExternalID, Update: new(update), Outcome: "started"})
	if fault == FaultReject {
		source.state.Calls[index].Outcome = "rejected"
		err := source.saveLocked()
		source.mu.Unlock()
		return provider.TransactionUpdateResult{}, errors.Join(provider.NewWriteFailure(provider.WriteRejected), err)
	}
	if fault == FaultBlock {
		if err := source.waitLocked(ctx, index); err != nil {
			source.mu.Unlock()
			return provider.TransactionUpdateResult{}, err
		}
	}
	if update.CategoryExternalID.Present {
		if !slices.ContainsFunc(source.state.Catalog.Categories, func(entity domain.ImportEntity) bool { return entity.ExternalID == update.CategoryExternalID.Value }) {
			source.state.Calls[index].Outcome = "rejected"
			err := source.saveLocked()
			source.mu.Unlock()
			return provider.TransactionUpdateResult{}, errors.Join(provider.NewWriteFailure(provider.WriteRejected), err)
		}
		row.CategoryExternalID = update.CategoryExternalID.Value
	}
	if update.ClearCategory {
		row.CategoryExternalID = ""
	}
	if update.Hidden.Present {
		row.Hidden = update.Hidden.Value
	}
	if update.MerchantExternalID.Present {
		if !slices.ContainsFunc(source.state.Catalog.Merchants, func(entity domain.ImportEntity) bool { return entity.ExternalID == update.MerchantExternalID.Value }) {
			source.state.Calls[index].Outcome = "rejected"
			err := source.saveLocked()
			source.mu.Unlock()
			return provider.TransactionUpdateResult{}, errors.Join(provider.NewWriteFailure(provider.WriteRejected), err)
		}
		row.MerchantExternalID = update.MerchantExternalID.Value
	}
	if update.MerchantName.Present {
		merchantIndex := slices.IndexFunc(source.state.Catalog.Merchants, func(entity domain.ImportEntity) bool { return entity.Label == update.MerchantName.Value })
		if merchantIndex < 0 {
			source.state.Catalog.Merchants = append(source.state.Catalog.Merchants, domain.ImportEntity{Kind: domain.EntityKindMerchant, ExternalID: fmt.Sprintf("scenario-merchant-%d", len(source.state.Catalog.Merchants)), Label: update.MerchantName.Value})
			merchantIndex = len(source.state.Catalog.Merchants) - 1
		}
		row.MerchantExternalID = source.state.Catalog.Merchants[merchantIndex].ExternalID
	}
	source.state.Transactions[row.ExternalID] = row
	source.state.Calls[index].Outcome = "applied"
	if fault == FaultUnknown {
		source.state.Calls[index].Outcome = "unknown"
	}
	if err := source.saveLocked(); err != nil {
		source.mu.Unlock()
		return provider.TransactionUpdateResult{}, err
	}
	if fault == FaultBlockAfter {
		if err := source.waitLocked(ctx, index); err != nil {
			source.mu.Unlock()
			return provider.TransactionUpdateResult{}, errors.Join(provider.NewWriteFailure(provider.WriteOutcomeUnknown), err)
		}
	}
	result := source.resultLocked(row)
	source.mu.Unlock()
	if fault == FaultUnknown {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	return result, nil
}

// waitLocked releases the state mutex while a caller owns the blocked invocation.
func (source *Provider) waitLocked(ctx context.Context, index int) error {
	release := make(chan struct{})
	source.release = release
	if err := source.saveLocked(); err != nil {
		source.release = nil
		return err
	}
	source.mu.Unlock()
	select {
	case <-release:
	case <-ctx.Done():
	}
	source.mu.Lock()
	if source.release == release {
		source.release = nil
	}
	if err := ctx.Err(); err != nil {
		if source.state.Calls[index].Outcome == "started" {
			source.state.Calls[index].Outcome = "canceled"
		}
		return errors.Join(err, source.saveLocked())
	}
	return nil
}
