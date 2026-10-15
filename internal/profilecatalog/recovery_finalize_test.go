package profilecatalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An interrupted root recovery reserves the profile ID in its marker. Opening the profile must
// not finalize the manifest with another ID, or the recovery could never resume.
func TestActivateLeavesAnInterruptedRootRecoveryResumable(t *testing.T) {
	t.Parallel()
	for _, point := range []RecoveryFaultPoint{
		RecoveryAfterMarkerWrite, RecoveryAfterSchemaInstall, RecoveryAfterVerification,
	} {
		t.Run(string(point), func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)
			makeProfileSchemaOlder(t, catalog.paths.LegacyProfile())
			plan, err := catalog.RecoveryPlan(context.Background(), LegacyKey)
			require.NoError(t, err)
			interrupted := errors.New("synthetic recovery interruption")
			catalog.recoveryFault = func(current RecoveryFaultPoint) error {
				if current == point {
					return interrupted
				}
				return nil
			}
			_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: plan, Confirmed: true})
			require.ErrorIs(t, err, interrupted)
			catalog.recoveryFault = nil

			_, err = catalog.Activate(context.Background(), LegacyKey)
			assert.Equal(t, CodeRecoveryIncomplete, CodeOf(err))
			_, err = catalog.ActivateForProvider(context.Background(), LegacyKey, "monarch")
			assert.Equal(t, CodeRecoveryIncomplete, CodeOf(err))
			_, err = catalog.FinalizeLegacyManifest(context.Background(), LegacyManifestRequest{
				ProviderKind: "local", ProfileID: deterministicProfileID(t, 0x71),
			})
			assert.Equal(t, CodeRecoveryIncomplete, CodeOf(err))
			assert.NoFileExists(t, filepath.Join(catalog.paths.Root, ManifestFilename))

			resumed, err := catalog.RecoveryPlan(context.Background(), LegacyKey)
			require.NoError(t, err)
			require.Equal(t, plan.ProfileID, resumed.ProfileID)
			_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: resumed, Confirmed: true})
			require.NoError(t, err)
			entries, err := catalog.List(context.Background())
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, plan.ProfileID, entries[0].ID)
			assert.Equal(t, StatusSetupIncomplete, entries[0].Status)
		})
	}
}
