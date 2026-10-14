package profilecatalog

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

func TestProfileRemovalRejectsRedirectedProfilesDirectory(t *testing.T) {
	snapshotContents := func(t *testing.T, root string) map[string][sha256.Size]byte {
		t.Helper()
		snapshot := make(map[string][sha256.Size]byte)
		require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if entry.IsDir() {
				snapshot[relative] = [sha256.Size]byte{}
				return nil
			}
			contents, err := os.ReadFile(path) //nolint:gosec // path belongs to the test-owned temporary fixture.
			if err != nil {
				return err
			}
			snapshot[relative] = sha256.Sum256(contents)
			return nil
		}))
		return snapshot
	}
	for _, operation := range []string{"remove", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			catalog := newTestCatalog(t, nil)
			external := newTestCatalog(t, nil)
			entry := createTestManifestProfile(t, external, 0x37, "Example Profile", "local")
			if err := os.Symlink(external.paths.Profiles, catalog.paths.Profiles); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			before := snapshotContents(t, external.paths.Root)

			var err error
			switch operation {
			case "remove":
				err = catalog.Remove(context.Background(), entry.ID)
			case "cancel":
				var removed bool
				removed, err = catalog.CancelNewProfile(context.Background(), entry.ID)
				assert.False(t, removed)
			}

			assert.Equal(t, CodeProfileInvalid, CodeOf(err))
			assert.Equal(t, before, snapshotContents(t, external.paths.Root))
			lock, lockErr := home.TryLockExisting(entry.Root, home.LockProfile, home.LockShared)
			if assert.NoError(t, lockErr, "external profile must remain available") {
				require.NoError(t, lock.Release())
			}
		})
	}
}
