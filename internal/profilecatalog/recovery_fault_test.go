package profilecatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestRecoveryFaultsRollForwardFromEveryDurableBoundary(t *testing.T) {
	t.Parallel()
	points := []RecoveryFaultPoint{
		RecoveryAfterMarkerWrite,
		RecoveryAfterWALMove,
		RecoveryAfterSHMMove,
		RecoveryAfterMainMove,
		RecoveryAfterEmptyCreate,
		RecoveryAfterSchemaInstall,
		RecoveryAfterVerification,
		RecoveryAfterMarkerRemoval,
	}
	for index, point := range points {
		point := point
		t.Run(string(point), func(t *testing.T) {
			t.Parallel()
			catalog := newTestCatalog(t, nil)
			entry := createTestManifestProfile(t, catalog, byte(0x50+index), "Recoverable", "monarch") //nolint:gosec // Bounded test index.
			makeProfileSchemaOlder(t, entry.ProfilePaths())
			sessionPath := filepath.Join(entry.Root, "providers", "monarch", "session.json")
			vaultPath := filepath.Join(entry.Root, "providers", "monarch", "credentials.enc")
			require.NoError(t, home.WritePrivateFile(sessionPath, []byte("session-example")))
			require.NoError(t, home.WritePrivateFile(vaultPath, []byte("vault-example")))
			fault := errors.New("injected recovery stop")
			catalog.recoveryFault = func(observed RecoveryFaultPoint) error {
				if observed == point {
					return fault
				}
				return nil
			}

			plan, err := catalog.RecoveryPlan(context.Background(), entry.ID)
			require.NoError(t, err)
			_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: plan, Confirmed: true})
			require.ErrorIs(t, err, fault)

			catalog.recoveryFault = nil
			for range 2 {
				if activeRecovery(entry.Root) {
					plan, planErr := catalog.RecoveryPlan(context.Background(), entry.ID)
					require.NoError(t, planErr)
					_, err = catalog.Recreate(
						context.Background(), RecoveryRequest{Plan: plan, Confirmed: true},
					)
					require.NoError(t, err)
				}
			}
			inspection, err := sqlite.InspectProfile(
				context.Background(), entry.ProfilePaths(), sqlite.DefaultOptions,
			)
			require.NoError(t, err)
			assert.Equal(t, sqlite.SchemaCurrent, inspection.Schema)
			assert.True(t, inspection.Pristine)
			assert.Equal(t, []byte("session-example"), mustReadFile(t, sessionPath))
			assert.Equal(t, []byte("vault-example"), mustReadFile(t, vaultPath))
			assert.False(t, activeRecovery(entry.Root))
			backups, err := os.ReadDir(filepath.Join(entry.Root, RecoveryDirectoryName))
			require.NoError(t, err)
			require.Len(t, backups, 1)
			backupPaths := home.Paths{
				Root: filepath.Join(entry.Root, RecoveryDirectoryName, backups[0].Name()),
			}
			backupPaths.Database = filepath.Join(backupPaths.Root, "moneyflow.db")
			backupInspection, err := sqlite.InspectProfile(
				context.Background(), backupPaths, sqlite.DefaultOptions,
			)
			require.NoError(t, err)
			assert.Equal(t, sqlite.SchemaOlder, backupInspection.Schema)
		})
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path) //nolint:gosec // Test reads a temp path it created.
	require.NoError(t, err)
	return contents
}
