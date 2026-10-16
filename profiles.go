package moneyflow

import (
	"context"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// ProfileState says what a host should render for one profile.
type ProfileState string

// Profile states.
const (
	// StateReady means the profile opened and nothing needs the user.
	StateReady ProfileState = "ready"
	// StateNeedsSetup means the profile was created and setup never finished.
	StateNeedsSetup ProfileState = "needs_setup"
	// StateNeedsReconnect means the provider sign-in is missing or expired.
	StateNeedsReconnect ProfileState = "needs_reconnect"
	// StateNeedsUnlock means YNAB credentials are stored but locked.
	StateNeedsUnlock ProfileState = "needs_unlock"
	// StateRefreshFailed means the last refresh failed; Error says why.
	StateRefreshFailed ProfileState = "refresh_failed"
	// StateWriteAttention means a commit to the provider stopped and waits for the user: it is
	// paused, was rate limited, stopped on a local failure such as a busy database, or needs a
	// decision.
	StateWriteAttention ProfileState = "write_attention"
	// StateError means the profile cannot open; Error says why.
	StateError ProfileState = "error"
)

// Profile describes one profile of an instance.
type Profile struct {
	ID string // profile_ + 26 base32 letters
	// Name is the profile's display name. A profile that another process holds exclusively may
	// report its ID as its name, because its manifest cannot be read.
	Name string
	// ProviderKind is monarch, ynab, simplefin, amazon, local or csv; it is empty in StateError
	// when unknown.
	ProviderKind string
	State        ProfileState
	Error        string    // why State is error or refresh_failed; empty otherwise
	LastRefresh  time.Time // zero when the profile never refreshed
	PendingEdits int       // staged edits not yet committed
}

// Profiles returns every profile in Root/profiles, sorted by name. It opens each profile to
// read its state; a profile that cannot open reports StateError. It returns ErrClosed after Close.
// If ctx ends or the instance closes during the call, it returns ctx's error or ErrClosed, not a
// list in which a profile blames the interruption.
func (instance *Instance) Profiles(ctx context.Context) ([]Profile, error) {
	entries, err := instance.list(ctx)
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		profile := instance.describe(ctx, entry)
		// An interruption makes describe blame the profile ("profile registry is closed"), so the
		// caller gets the interruption itself.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := instance.checkOpen(); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (instance *Instance) describe(ctx context.Context, entry profilecatalog.Entry) Profile {
	profile := Profile{ID: entry.ID, Name: entry.DisplayName, ProviderKind: entry.ProviderKind}
	if err := instance.failure(entry.ID); err != nil {
		profile.State, profile.Error = StateError, err.Error()
		return profile
	}
	if problem := entryProblem(entry); problem != "" {
		profile.State, profile.Error = StateError, problem
		return profile
	}
	if entry.Status == profilecatalog.StatusSetupIncomplete {
		profile.State = StateNeedsSetup
		return profile
	}
	lease, err := instance.registry.Acquire(ctx, entry.ID)
	if err != nil {
		profile.State, profile.Error = StateError, err.Error()
		return profile
	}
	defer func() { _ = lease.Release() }()
	service := lease.Service()
	account, err := service.AccountProjection(ctx, app.AccountProjectionRequest{
		Partitions: app.CollectionWindowRequest{Limit: 1},
	})
	if err != nil {
		profile.State, profile.Error = StateError, err.Error()
		return profile
	}
	profile.LastRefresh = account.Provider.LastSuccess
	profile.PendingEdits = account.Pending.ActiveOperations
	profile.State = instance.providerState(ctx, entry, service, account)
	if profile.State == StateRefreshFailed {
		profile.Error = provider.NewError(account.Provider.Code).Error()
	}
	return profile
}

func (instance *Instance) providerState(
	ctx context.Context,
	entry profilecatalog.Entry,
	service *app.Service,
	account app.AccountProjection,
) ProfileState {
	if entry.ProviderKind != "monarch" && entry.ProviderKind != "ynab" &&
		entry.ProviderKind != "simplefin" {
		return StateReady
	}
	status, err := service.ProviderStatus(ctx)
	if err != nil || entry.Status == profilecatalog.StatusReconnect {
		return instance.signInState(entry)
	}
	return statusState(status, account)
}

// statusState says what a provider profile needs from the user, from the provider status it read
// and the state of its provider write.
func statusState(status app.ProviderStatus, account app.AccountProjection) ProfileState {
	switch {
	case status.Code == provider.CodeReconnectRequired,
		account.Write.Phase == store.WritePhaseReconnectRequired:
		return StateNeedsReconnect
	case account.Write.Phase == store.WritePhasePaused,
		account.Write.Phase == store.WritePhaseRateLimited,
		account.Write.Phase == store.WritePhaseAttentionRequired,
		account.Write.Phase == store.WritePhaseReconcileConfirmationRequired,
		account.Write.RunFailure != nil:
		return StateWriteAttention
	case account.Provider.Code != "":
		return StateRefreshFailed
	default:
		return StateReady
	}
}

// signInState separates a YNAB profile whose stored credentials only need a password from a
// profile that must sign in again.
func (instance *Instance) signInState(entry profilecatalog.Entry) ProfileState {
	if entry.ProviderKind == "ynab" {
		if present, err := instance.seams.inspectSession(entry.Root, "ynab"); err == nil && present {
			return StateNeedsUnlock
		}
	}
	return StateNeedsReconnect
}

// failure returns the background failure recordFailure kept for a profile, or nil.
func (instance *Instance) failure(profileID string) error {
	instance.mu.Lock()
	defer instance.mu.Unlock()
	return instance.failures[profileID]
}
