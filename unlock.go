package moneyflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/assembly"
)

var (
	// ErrUnlockFailed reports a password that does not unlock the profile.
	ErrUnlockFailed = errors.New("the password did not unlock this profile")
	// ErrNotLocked reports an unlock of a profile without locked provider credentials.
	ErrNotLocked = errors.New("this profile has no locked provider credentials")
)

// Unlock unlocks a YNAB profile's stored credentials with a password the host supplies. Moneyflow
// never stores the password. It returns ErrUnlockFailed for a wrong password, ErrNotLocked for a
// profile that is not a connected YNAB profile or has no stored credentials, ErrProfileNotFound
// for an ID that names no profile, and ErrClosed after Close.
func (instance *Instance) Unlock(ctx context.Context, profileID, password string) error {
	entry, err := instance.lookup(ctx, profileID)
	if err != nil {
		return err
	}
	lease, err := instance.registry.Acquire(ctx, entry.ID)
	if err != nil {
		return fmt.Errorf("unlock profile %s: %w", profileID, err)
	}
	defer func() { _ = lease.Release() }()
	err = assembly.UnlockYNAB(ctx, assembly.UnlockRequest{
		Service: lease.Service(), Paths: entry.ProfilePaths(), YNAB: instance.seams.factories.YNAB,
		Password: func(context.Context) (string, error) { return password, nil },
		Renderer: renderer,
	})
	switch {
	case errors.Is(err, assembly.ErrWrongPassword):
		return ErrUnlockFailed
	case errors.Is(err, assembly.ErrNotYNAB), errors.Is(err, assembly.ErrVaultMissing):
		return ErrNotLocked
	case err != nil:
		return fmt.Errorf("unlock profile %s: %w", profileID, err)
	default:
		return nil
	}
}
