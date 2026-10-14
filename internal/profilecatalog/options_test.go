package profilecatalog

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestCatalogUsesTheSuppliedSQLiteOptions(t *testing.T) {
	paths, err := home.ResolveCatalogRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	catalog, err := New(Config{
		Paths: paths, Random: bytes.NewReader(bytes.Repeat([]byte{0x55}, 1024)),
		Now: time.Now, Version: "test", SQLite: sqlite.Options{MaxOpenConnections: 1},
	})
	require.NoError(t, err)
	_, err = catalog.Create(
		context.Background(), CreateRequest{DisplayName: "Example Shop", ProviderKind: "local"},
	)
	require.ErrorContains(t, err, "busy timeout must be at least one millisecond")
}

func TestCatalogOperationsOpenDatabasesWithTheSuppliedSQLiteOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy := LegacyManifestRequest{DisplayName: "Example Shop", ProviderKind: "local"}
	tests := []struct {
		name string
		run  func(t *testing.T) error
	}{
		{"list inspects each profile", func(t *testing.T) error {
			base := newTestCatalog(t, nil)
			createTestManifestProfile(t, base, 0x31, "Household", "local")
			_, err := rejectingCatalog(t, base).List(ctx)
			return err
		}},
		{"cancel inspects the new profile", func(t *testing.T) error {
			base := newTestCatalog(t, nil)
			entry := createTestManifestProfile(t, base, 0x24, "Canceled", "monarch")
			_, err := rejectingCatalog(t, base).CancelNewProfile(ctx, entry.ID)
			return err
		}},
		{"finalizing a legacy profile that has a manifest inspects it", func(t *testing.T) error {
			base := newTestCatalog(t, nil)
			installTestLegacyProfile(t, base)
			_, err := base.FinalizeLegacyManifest(ctx, legacy)
			require.NoError(t, err)
			_, err = rejectingCatalog(t, base).FinalizeLegacyManifest(ctx, legacy)
			return err
		}},
		{"recovery installs the replacement database", func(t *testing.T) error {
			base := newTestCatalog(t, nil)
			entry := createTestManifestProfile(t, base, 0x50, "Recoverable", "monarch")
			makeProfileSchemaOlder(t, entry.ProfilePaths())
			interruptRecovery(t, base, entry, RecoveryAfterMainMove)
			return planAndRecreate(t, rejectingCatalog(t, base), entry)
		}},
		{"recovery inspects the original database", func(t *testing.T) error {
			base := newTestCatalog(t, nil)
			entry := createTestManifestProfile(t, base, 0x51, "Recoverable", "monarch")
			makeProfileSchemaOlder(t, entry.ProfilePaths())
			interruptRecovery(t, base, entry, RecoveryAfterMarkerWrite)
			setProfileSchemaVersion(t, entry.ProfilePaths(), sqlite.CurrentSchemaVersion+1)
			return planAndRecreate(t, rejectingCatalog(t, base), entry)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.run(t)
			require.Error(t, err)
			// A catalog error hides its cause from Error(), so read the whole chain.
			var causes []string
			for cause := err; cause != nil; cause = errors.Unwrap(cause) {
				causes = append(causes, cause.Error())
			}
			require.Contains(
				t, strings.Join(causes, ": "), "busy timeout must be at least one millisecond",
			)
		})
	}
}

// rejectingCatalog returns a catalog over base's directory whose SQLite options fail
// validation. New accepts them because MaxOpenConnections is set, and every database open
// that receives them returns an error.
func rejectingCatalog(t *testing.T, base *Catalog) *Catalog {
	t.Helper()
	catalog, err := New(Config{
		Paths: base.Paths(), Random: bytes.NewReader(bytes.Repeat([]byte{0x66}, 1024)),
		Now: time.Now, Version: "test", SQLite: sqlite.Options{MaxOpenConnections: 1},
	})
	require.NoError(t, err)
	return catalog
}

// planAndRecreate plans a recovery of entry and confirms it, as a user would.
func planAndRecreate(t *testing.T, catalog *Catalog, entry Entry) error {
	t.Helper()
	plan, err := catalog.RecoveryPlan(context.Background(), entry.ID)
	require.NoError(t, err)
	_, err = catalog.Recreate(context.Background(), RecoveryRequest{Plan: plan, Confirmed: true})
	return err
}

// interruptRecovery starts a recovery of entry and stops it at point, leaving the unfinished
// recovery on disk for another catalog to resume.
func interruptRecovery(t *testing.T, catalog *Catalog, entry Entry, point RecoveryFaultPoint) {
	t.Helper()
	stop := errors.New("injected recovery stop")
	catalog.recoveryFault = func(observed RecoveryFaultPoint) error {
		if observed == point {
			return stop
		}
		return nil
	}
	defer func() { catalog.recoveryFault = nil }()
	require.ErrorIs(t, planAndRecreate(t, catalog, entry), stop)
}
