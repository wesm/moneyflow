package profilecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportReservesInterruptedLegacyRecoveryIdentity(t *testing.T) {
	for _, test := range []struct {
		name       string
		point      RecoveryFaultPoint
		directory  bool
		wantStatus Status
	}{
		{"marker written", RecoveryAfterMarkerWrite, false, StatusNeedsRecovery},
		{"main moved", RecoveryAfterMainMove, false, StatusNeedsRecovery},
		{"database directory", RecoveryAfterMainMove, true, StatusUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			catalog := newTestCatalog(t, nil)
			installTestLegacyProfile(t, catalog)
			makeProfileSchemaOlder(t, catalog.paths.LegacyProfile())
			plan, err := catalog.RecoveryPlan(t.Context(), LegacyKey)
			require.NoError(t, err)
			interrupted := errors.New("synthetic recovery interruption")
			catalog.recoveryFault = func(current RecoveryFaultPoint) error {
				if current == test.point {
					return interrupted
				}
				return nil
			}
			_, err = catalog.Recreate(t.Context(), RecoveryRequest{Plan: plan, Confirmed: true})
			require.ErrorIs(t, err, interrupted)
			require.NoFileExists(t, filepath.Join(catalog.paths.Root, ManifestFilename))
			if test.directory {
				require.NoError(t, os.Mkdir(catalog.paths.LegacyProfile().Database, 0o700))
			}
			entries, err := catalog.Survey(t.Context())
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, LegacyKey, entries[0].Key)
			assert.Empty(t, entries[0].ID, "recovery has not finalized the legacy manifest")
			assert.Equal(t, "Moneyflow", entries[0].DisplayName)
			assert.Equal(t, test.wantStatus, entries[0].Status)

			_, err = catalog.Import(t.Context(), ImportRequest{
				Manifest: importManifest(plan.ProfileID, "Different Profile"),
				Populate: installPristine,
			})

			assert.Equal(t, CodeProfileExists, CodeOf(err))
			assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, plan.ProfileID))
			if test.directory {
				id := deterministicProfileID(t, 0x62)
				_, err = catalog.Import(t.Context(), ImportRequest{
					Manifest: importManifest(id, "Moneyflow"), Populate: installPristine,
				})
				assert.Equal(t, CodeProfileNameConflict, CodeOf(err))
				assert.NoDirExists(t, filepath.Join(catalog.paths.Profiles, id))
				require.NoError(t, os.Remove(catalog.paths.LegacyProfile().Database))
			}
			catalog.recoveryFault = nil
			resumed, err := catalog.RecoveryPlan(t.Context(), LegacyKey)
			require.NoError(t, err)
			require.Equal(t, plan.ProfileID, resumed.ProfileID)
			_, err = catalog.Recreate(t.Context(), RecoveryRequest{Plan: resumed, Confirmed: true})
			require.NoError(t, err)
			entries, err = catalog.List(t.Context())
			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, plan.ProfileID, entries[0].ID)
			assert.Equal(t, catalog.paths.Root, entries[0].Root)
		})
	}
}
