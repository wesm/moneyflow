package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider/ynab"
)

func TestRequireTemporaryRootAcceptsOwnedChildAndRejectsOutsideDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	marker := filepath.Join(root, isolatedRootMarkerFilename)
	require.NoError(t, os.WriteFile(marker, []byte("test-token"), 0o600))
	require.NoError(t, requireIsolatedRoot(root, "test-token"))
	assert.ErrorContains(t, requireIsolatedRoot(root, "wrong-token"), "marker")
	assert.ErrorContains(t, requireIsolatedRoot(t.TempDir(), "test-token"), "marker")
}

func TestSyntheticRuntimeExercisesYNABBudgetSelectionAndImport(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runtimes := newSyntheticRuntimes()
	runtime, err := runtimes.runtime(home.Paths{Root: root})
	require.NoError(t, err)
	require.NotNil(t, runtime.YNABVault)
	require.NotNil(t, runtime.NewYNABClient)
	require.NotNil(t, runtime.NewYNABSource)

	client, err := runtime.NewYNABClient([]byte("synthetic-token"))
	require.NoError(t, err)
	plans, err := client.ListPlans(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, "Example Budget", plans[0].Name)

	plan, err := client.FetchPlan(context.Background(), plans[0].ID)
	require.NoError(t, err)
	assert.Equal(t, "USD", plan.CurrencyFormat.ISOCode)
	assert.Equal(t, 2, *plan.CurrencyFormat.DecimalDigits)

	credentials := ynab.StoredCredentials{
		AccessToken: "synthetic-token", PlanID: plan.ID, Currency: "USD", Scale: 2,
	}
	require.NoError(t, runtime.YNABVault.Save(credentials, []byte("vault-password")))
	present, err := runtimes.sessionPresent(root, "ynab")
	require.NoError(t, err)
	assert.True(t, present)
}
