package moneyflow

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider/ynab"
)

func TestUnlockOpensLockedYNABCredentials(t *testing.T) {
	root := testRoot(t)
	budget := createBoundProfile(t, root, "Example Budget", "ynab", t.TempDir())
	vault, err := ynab.NewCredentialVault(home.Paths{Root: filepath.Join(root, "profiles", budget)})
	require.NoError(t, err)
	// The synthetic provider reports the plan ID that createBoundProfile binds the profile to.
	require.NoError(t, vault.Save(ynab.StoredCredentials{
		AccessToken: "synthetic-token", PlanID: "synthetic-ui-scenario", Currency: "USD", Scale: 2,
	}, []byte("example password")))
	instance := openTestInstance(t, root, seams{})
	require.Equal(t, StateNeedsUnlock, currentProfile(t.Context(), instance, budget).State)

	wrong := instance.Unlock(t.Context(), budget, "wrong password")
	locked := currentProfile(t.Context(), instance, budget).State
	right := instance.Unlock(t.Context(), budget, "example password")

	require.ErrorIs(t, wrong, ErrUnlockFailed)
	assert.Equal(t, StateNeedsUnlock, locked)
	require.NoError(t, right)
	assert.Equal(t, StateReady, currentProfile(t.Context(), instance, budget).State)
}

func TestUnlockRejectsProfilesWithoutLockedCredentials(t *testing.T) {
	root := testRoot(t)
	shop := createLocalProfile(t, root, "Example Shop")
	budget := createBoundProfile(t, root, "Example Budget", "ynab", t.TempDir())
	instance := openTestInstance(t, root, seams{})

	local := instance.Unlock(t.Context(), shop, "example password")
	withoutVault := instance.Unlock(t.Context(), budget, "example password")
	unknown := instance.Unlock(t.Context(), unknownProfileID, "example password")

	require.ErrorIs(t, local, ErrNotLocked)
	require.ErrorIs(t, withoutVault, ErrNotLocked)
	require.ErrorIs(t, unknown, ErrProfileNotFound)
}
