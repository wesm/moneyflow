package profilecatalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

func TestSurveyReportsABrokenProfileAndKeepsTheOthers(t *testing.T) {
	ctx := context.Background()
	catalog := newTestCatalog(t, nil)
	healthy := createTestManifestProfile(t, catalog, 0x31, "Example Shop", "local")
	broken := createTestManifestProfile(t, catalog, 0x32, "Acme", "local")
	require.NoError(t, os.WriteFile(filepath.Join(broken.Root, ManifestFilename), []byte("{"), 0o600))

	entries, err := catalog.Survey(ctx)

	require.NoError(t, err)
	require.Len(t, entries, 2)
	byID := map[string]Entry{}
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	assert.NotEqual(t, StatusUnavailable, byID[healthy.ID].Status)
	assert.Empty(t, byID[healthy.ID].Problem)
	assert.Equal(t, StatusUnavailable, byID[broken.ID].Status)
	assert.NotEmpty(t, byID[broken.ID].Problem)
	_, err = catalog.List(ctx)
	assert.Error(t, err)
}

func TestSurveyPreservesManifestMetadataWhenLaterInspectionFails(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		for _, problem := range []string{"recovery", "database", "provider mismatch", "session"} {
			location := "nested"
			if legacy {
				location = "root"
			}
			t.Run(location+"/"+problem, func(t *testing.T) {
				ctx := context.Background()
				catalog := newTestCatalog(t, func(string, string) (bool, error) {
					return false, errors.New("synthetic session inspection failure")
				})
				var existing Entry
				if legacy {
					installTestLegacyProfile(t, catalog)
					var err error
					existing, err = catalog.FinalizeLegacyManifest(ctx, LegacyManifestRequest{
						DisplayName: "Example Profile", ProviderKind: "monarch",
						ProfileID: deterministicProfileID(t, 0x31),
					})
					require.NoError(t, err)
				} else {
					existing = createTestManifestProfile(t, catalog, 0x31, "Example Profile", "monarch")
				}
				switch problem {
				case "recovery":
					require.NoError(t, os.WriteFile(filepath.Join(existing.Root, RecoveryDirectoryName),
						[]byte("invalid recovery directory"), 0o600))
				case "database":
					catalog = rejectingCatalog(t, catalog)
				case "provider mismatch":
					bindTestProfile(t, existing.ProfilePaths(), "ynab")
				case "session":
					bindTestProfile(t, existing.ProfilePaths(), "monarch")
				}

				entries, err := catalog.Survey(ctx)

				require.NoError(t, err)
				require.Len(t, entries, 1)
				assert.Equal(t, existing.ID, entries[0].Key)
				assert.Equal(t, existing.ID, entries[0].ID)
				assert.Equal(t, "Example Profile", entries[0].DisplayName)
				assert.Equal(t, "monarch", entries[0].ProviderKind)
				assert.Equal(t, existing.Root, entries[0].Root)
				assert.Equal(t, StatusUnavailable, entries[0].Status)
				assert.NotEmpty(t, entries[0].Problem)
				_, err = catalog.List(ctx)
				assert.Error(t, err)
			})
		}
	}
}

func TestSurveyReportsAnExclusivelyHeldProfileAsBusy(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	free := createTestManifestProfile(t, catalog, 0x33, "Example Shop", "local")
	held := createTestManifestProfile(t, catalog, 0x34, "Acme", "local")
	lock, err := home.TryLock(held.Root, home.LockProfile, home.LockExclusive)
	require.NoError(t, err)
	defer func() { _ = lock.Release() }()

	entries, err := catalog.Survey(context.Background())

	require.NoError(t, err)
	require.Len(t, entries, 2)
	byKey := entriesByKey(entries)
	assert.Equal(t, StatusUnavailable, byKey[held.Key].Status)
	assert.True(t, strings.HasPrefix(byKey[held.Key].Problem, "profile_busy:"),
		"problem was %q", byKey[held.Key].Problem)
	assert.Equal(t, StatusSetupIncomplete, byKey[free.Key].Status)
	assert.Empty(t, byKey[free.Key].Problem)
}

func TestSurveyReportsAnInvalidLegacyProfileAndKeepsTheOthers(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	healthy := createTestManifestProfile(t, catalog, 0x35, "Example Shop", "local")
	require.NoError(t, os.Mkdir(catalog.paths.LegacyProfile().Database, 0o700))

	entries, err := catalog.Survey(context.Background())

	require.NoError(t, err)
	require.Len(t, entries, 2)
	byKey := entriesByKey(entries)
	legacy := byKey[LegacyKey]
	assert.Equal(t, StatusUnavailable, legacy.Status)
	assert.Equal(t, "Moneyflow", legacy.DisplayName)
	assert.Empty(t, legacy.ID)
	assert.Equal(t, catalog.paths.Root, legacy.Root)
	assert.True(t, strings.HasPrefix(legacy.Problem, "profile_invalid:"),
		"problem was %q", legacy.Problem)
	assert.Empty(t, byKey[healthy.Key].Problem)
	_, err = catalog.List(context.Background())
	assert.Equal(t, CodeProfileInvalid, CodeOf(err))
}

func TestSurveyReportsUnusableProfileDirectoriesAndKeepsTheOthers(t *testing.T) {
	redirectedID := deterministicProfileID(t, 0x7f)
	for name, testCase := range map[string]struct {
		key     string
		id      string
		prepare func(t *testing.T, catalog *Catalog)
	}{
		"malformed ID": {
			key: "named-profile",
			prepare: func(t *testing.T, catalog *Catalog) {
				require.NoError(t, os.MkdirAll(filepath.Join(catalog.paths.Profiles, "named-profile"), 0o700))
			},
		},
		"redirected": {
			key: redirectedID, id: redirectedID,
			prepare: func(t *testing.T, catalog *Catalog) {
				link := filepath.Join(catalog.paths.Profiles, redirectedID)
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Skipf("creating a symlink requires additional platform permission: %v", err)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := newTestCatalog(t, nil)
			healthy := createTestManifestProfile(t, catalog, 0x36, "Example Shop", "local")
			testCase.prepare(t, catalog)

			entries, err := catalog.Survey(context.Background())

			require.NoError(t, err)
			require.Len(t, entries, 2)
			byKey := entriesByKey(entries)
			unusable := byKey[testCase.key]
			assert.Equal(t, StatusUnavailable, unusable.Status)
			assert.Equal(t, testCase.id, unusable.ID)
			assert.Equal(t, filepath.Join(catalog.paths.Profiles, testCase.key), unusable.Root)
			assert.True(t, strings.HasPrefix(unusable.Problem, "profile_invalid:"),
				"problem was %q", unusable.Problem)
			assert.Empty(t, byKey[healthy.Key].Problem)
		})
	}
}

func TestSurveyFailsOnlyWhenTheCatalogItselfIsBusy(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	createTestManifestProfile(t, catalog, 0x37, "Example Shop", "local")
	reader, err := home.TryLock(catalog.Paths().Root, home.LockCatalog, home.LockShared)
	require.NoError(t, err)
	entries, err := catalog.Survey(context.Background())
	require.NoError(t, reader.Release())
	require.NoError(t, err)
	assert.Len(t, entries, 1, "another reader must not block a survey")

	writer, err := home.TryLock(catalog.Paths().Root, home.LockCatalog, home.LockExclusive)
	require.NoError(t, err)
	defer func() { _ = writer.Release() }()
	entries, err = catalog.Survey(context.Background())

	assert.Equal(t, CodeProfileBusy, CodeOf(err))
	assert.Nil(t, entries)
}

func TestCompareEntriesOrdersByNormalizedNameThenKey(t *testing.T) {
	assert.Negative(t, compareEntries(
		Entry{DisplayName: "alpha", Key: "z"}, Entry{DisplayName: "Zulu", Key: "a"}))
	assert.Negative(t, compareEntries(
		Entry{DisplayName: "Same", Key: "a"}, Entry{DisplayName: "same", Key: "b"}))
	assert.Positive(t, compareEntries(
		Entry{DisplayName: "Same", Key: "b"}, Entry{DisplayName: "same", Key: "a"}))
	assert.Zero(t, compareEntries(
		Entry{DisplayName: "Same", Key: "a"}, Entry{DisplayName: "same", Key: "a"}))
}

// A request that ends is not a problem with any profile. A listing fails with the context's
// error instead of reporting a healthy profile as needing recovery.
func TestSurveyAndListFailWhenTheirContextEnds(t *testing.T) {
	catalog := newTestCatalog(t, nil)
	createTestManifestProfile(t, catalog, 0x31, "Example Shop", "local")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()

	for name, list := range map[string]func(context.Context) ([]Entry, error){
		"Survey": catalog.Survey, "List": catalog.List,
	} {
		entries, err := list(canceled)
		assert.ErrorIs(t, err, context.Canceled, name)
		assert.Nil(t, entries, name)

		entries, err = list(expired)
		assert.ErrorIs(t, err, context.DeadlineExceeded, name)
		assert.Nil(t, entries, name)
	}
}
