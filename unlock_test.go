package moneyflow

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider/ynab"
)

func TestUnlockOpensLockedYNABCredentials(t *testing.T) {
	root := testRoot(t)
	budget := createLockedYNABProfile(t, root)
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

func TestRenamingAnUnlockedYNABProfileLeavesItUnlocked(t *testing.T) {
	root := testRoot(t)
	budget := createLockedYNABProfile(t, root)
	instance := openTestInstance(t, root, seams{})
	require.NoError(t, instance.Unlock(t.Context(), budget, "example password"))

	renamed := serveAPI(t, instance, http.MethodPost,
		"api/v1/profiles/"+budget+"/rename", `{"version":"1","display_name":"Household"}`)

	require.Equal(t, http.StatusOK, renamed.Code, renamed.Body.String())
	profile := currentProfile(t.Context(), instance, budget)
	assert.Equal(t, "Household", profile.Name)
	assert.Equal(t, StateReady, profile.State)
}

// createLockedYNABProfile creates the YNAB profile Example Budget, whose stored credentials open
// with "example password", and returns its ID.
func createLockedYNABProfile(t *testing.T, root string) string {
	t.Helper()
	budget := createBoundProfile(t, root, "Example Budget", "ynab", t.TempDir())
	vault, err := ynab.NewCredentialVault(home.Paths{Root: filepath.Join(root, "profiles", budget)})
	require.NoError(t, err)
	// The synthetic provider reports the plan ID that createBoundProfile binds the profile to.
	require.NoError(t, vault.Save(ynab.StoredCredentials{
		AccessToken: "synthetic-token", PlanID: "synthetic-ui-scenario", Currency: "USD", Scale: 2,
	}, []byte("example password")))
	return budget
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
