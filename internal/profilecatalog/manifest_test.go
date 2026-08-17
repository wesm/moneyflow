package profilecatalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

const exampleProfileID = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestNewProfileIDUsesInjected128BitRandomness(t *testing.T) {
	t.Parallel()
	random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 16))
	id, err := NewProfileID(random)
	require.NoError(t, err)
	assert.Regexp(t, `^profile_[a-z2-7]{26}$`, id)
	assert.Zero(t, random.Len())
	assert.True(t, ValidProfileID(id))
}

func TestNewProfileIDRejectsNilAndShortRandomness(t *testing.T) {
	t.Parallel()
	_, err := NewProfileID(nil)
	require.Error(t, err)
	_, err = NewProfileID(bytes.NewReader(make([]byte, 15)))
	require.Error(t, err)
}

func TestReadManifestRoundTripsCanonicalVersionOne(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), exampleProfileID)
	require.NoError(t, os.Mkdir(root, 0o700))
	path := filepath.Join(root, ManifestFilename)
	want := validManifest()
	require.NoError(t, writeManifest(path, want))

	got, err := ReadManifest(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	contents, err := os.ReadFile(path) //nolint:gosec // test-owned temporary profile.
	require.NoError(t, err)
	assert.Contains(t, string(contents), `"created_at":"2026-08-17T19:12:34.123456789Z"`)
	assert.NotContains(t, string(contents), "  ")
}

func TestReadManifestRejectsUnknownVersionWithoutTrustingOtherFields(t *testing.T) {
	t.Parallel()
	path := writeManifestFixture(t,
		`{"manifest_version":2,"unexpected":{"shape":true},"display_name":42}`)
	version, err := ProbeManifestVersion(path)
	require.NoError(t, err)
	assert.Equal(t, uint16(2), version)

	_, err = ReadManifest(path)
	assert.Equal(t, CodeManifestUnsupported, CodeOf(err))
}

func TestReadManifestRejectsStructuralJSONViolations(t *testing.T) {
	t.Parallel()
	valid := manifestJSON(t, validManifest())
	tests := map[string]string{
		"duplicate": strings.Replace(valid, `"profile_id":`,
			`"profile_id":"`+exampleProfileID+`","profile_id":`, 1),
		"unknown":  strings.Replace(valid, `}`, `,"extra":true}`, 1),
		"trailing": valid + `{}`,
		"missing":  strings.Replace(valid, `"provider_kind":"monarch",`, "", 1),
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadManifest(writeManifestFixture(t, contents))
			assert.Equal(t, CodeProfileInvalid, CodeOf(err))
		})
	}
}

func TestReadManifestRejectsOversizedFile(t *testing.T) {
	t.Parallel()
	path := writeManifestFixture(t, strings.Repeat("x", ManifestMaximumBytes+1))
	_, err := ReadManifest(path)
	assert.Equal(t, CodeProfileInvalid, CodeOf(err))
}

func TestReadManifestRejectsNoncanonicalValues(t *testing.T) {
	t.Parallel()
	valid := manifestJSON(t, validManifest())
	tests := map[string]string{
		"profile ID":   strings.Replace(valid, exampleProfileID, "profile_AAAAAAAAAAAAAAAAAAAAAAAAAA", 1),
		"trimmed name": strings.Replace(valid, `"Moneyflow"`, `" Moneyflow "`, 1),
		"provider":     strings.Replace(valid, `"monarch"`, `"other"`, 1),
		"timestamp offset": strings.Replace(valid, "2026-08-17T19:12:34.123456789Z",
			"2026-08-17T14:12:34.123456789-05:00", 1),
		"timestamp precision": strings.Replace(valid, "2026-08-17T19:12:34.123456789Z",
			"2026-08-17T19:12:34Z", 1),
		"version string": strings.Replace(valid, `"manifest_version":1`,
			`"manifest_version":"1"`, 1),
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadManifest(writeManifestFixture(t, contents))
			assert.Equal(t, CodeProfileInvalid, CodeOf(err))
		})
	}
}

func TestReadManifestRejectsDirectoryIDMismatch(t *testing.T) {
	t.Parallel()
	other, err := NewProfileID(bytes.NewReader(bytes.Repeat([]byte{0x24}, 16)))
	require.NoError(t, err)
	root := filepath.Join(t.TempDir(), other)
	require.NoError(t, os.Mkdir(root, 0o700))
	path := filepath.Join(root, ManifestFilename)
	require.NoError(t, writeRawPrivate(path, manifestJSON(t, validManifest())))

	_, err = ReadManifest(path)
	assert.Equal(t, CodeProfileInvalid, CodeOf(err))
}

func TestNormalizeDisplayNamePinsLimitsAndCollisionKey(t *testing.T) {
	t.Parallel()
	name, key, err := NormalizeDisplayName("  ＭｏｎｅｙＦｌｏｗ\u2003 HOME  ")
	require.NoError(t, err)
	assert.Equal(t, "ＭｏｎｅｙＦｌｏｗ\u2003 HOME", name)
	assert.Equal(t, "moneyflow home", key)

	_, _, err = NormalizeDisplayName(strings.Repeat("é", 81))
	require.Error(t, err)
	_, _, err = NormalizeDisplayName(strings.Repeat("界", 107))
	require.Error(t, err)
	_, _, err = NormalizeDisplayName("bad\nname")
	require.Error(t, err)
}

func TestWriteManifestRejectsInvalidInputWithoutReplacingExistingFile(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), exampleProfileID)
	require.NoError(t, os.Mkdir(root, 0o700))
	path := filepath.Join(root, ManifestFilename)
	require.NoError(t, writeManifest(path, validManifest()))
	before, err := os.ReadFile(path) //nolint:gosec // test-owned temporary profile.
	require.NoError(t, err)
	invalid := validManifest()
	invalid.DisplayName = ""
	require.Error(t, writeManifest(path, invalid))
	after, err := os.ReadFile(path) //nolint:gosec // test-owned temporary profile.
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func validManifest() Manifest {
	return Manifest{
		ManifestVersion:  ManifestVersion,
		ProfileID:        exampleProfileID,
		DisplayName:      "Moneyflow",
		ProviderKind:     "monarch",
		CreatedAt:        time.Date(2026, 8, 17, 19, 12, 34, 123456789, time.UTC),
		CreatedByVersion: "0.12.0",
	}
}

func manifestJSON(t *testing.T, manifest Manifest) string {
	t.Helper()
	contents, err := marshalManifest(manifest)
	require.NoError(t, err)
	return string(contents)
}

func writeManifestFixture(t *testing.T, contents string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, ManifestFilename)
	require.NoError(t, writeRawPrivate(path, contents))
	return path
}

func writeRawPrivate(path, contents string) error {
	return home.WritePrivateFile(path, []byte(contents))
}
