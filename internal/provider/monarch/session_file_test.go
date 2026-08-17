package monarch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestSessionStoreRoundTripContainsNoCredentials(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	session := validSession()
	require.NoError(t, store.Save(session))
	loaded, fingerprint, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, session, loaded)
	assert.NotEmpty(t, fingerprint)

	serialized, err := os.ReadFile(store.Path()) //nolint:gosec // test-owned temporary path.
	require.NoError(t, err)
	lower := strings.ToLower(string(serialized))
	for _, forbidden := range []string{"email", "login", "password", "mfa", "totp", "secret"} {
		assert.NotContains(t, lower, forbidden)
	}
}

func TestSessionStoreRejectsInvalidAndOversizedFiles(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	require.NoError(t, home.WritePrivateFile(store.Path(), []byte(`{"version":99}`)))
	_, _, err := store.Load()
	assert.Error(t, err)

	require.NoError(t, home.WritePrivateFile(
		store.Path(),
		[]byte(strings.Repeat("x", int(maxSessionBytes+1))),
	))
	_, _, err = store.Load()
	assert.ErrorContains(t, err, "maximum size")
}

func TestSessionStoreRejectsRedirectedRootAndIntermediateDirectory(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target, err := home.ResolveRoot(filepath.Join(base, "target"), nil, "")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(target.Root, 0o700))
	redirect := filepath.Join(base, "redirect")
	if err = os.Symlink(target.Root, redirect); err != nil {
		t.Skipf("creating a symlink requires additional platform permission: %v", err)
	}
	_, err = NewSessionStore(home.Paths{
		Root: redirect, Database: filepath.Join(redirect, "moneyflow.db"),
	})
	assert.ErrorContains(t, err, "redirect")

	profile, err := home.ResolveRoot(filepath.Join(base, "profile"), nil, "")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(profile.Root, 0o700))
	require.NoError(t, os.Symlink(target.Root, filepath.Join(profile.Root, "providers")))
	_, err = NewSessionStore(profile)
	assert.ErrorContains(t, err, "symbolic link")
}

func TestSessionStoreDetectsReplacementAndDeletePreservesProfile(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	require.NoError(t, store.Save(validSession()))
	_, first, err := store.Load()
	require.NoError(t, err)
	changed, err := store.Changed(first)
	require.NoError(t, err)
	assert.False(t, changed)

	replacement := validSession()
	replacement.Token = "replacement-token"
	require.NoError(t, store.Save(replacement))
	changed, err = store.Changed(first)
	require.NoError(t, err)
	assert.True(t, changed)
	require.NoError(t, store.Delete())
	_, err = os.Stat(store.Path())
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Dir(filepath.Dir(filepath.Dir(store.Path()))))
	assert.NoError(t, err, "disconnect must preserve the profile root")
}

func TestSourceReloadsAtomicallyReplacedSessionOnce(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	require.NoError(t, store.Save(validSession()))
	options := testClientOptions(t, "")
	options.ImportCurrency = "USD"
	options.ImportScale = 2
	source, err := NewSource(options, store)
	require.NoError(t, err)
	firstClient, firstFingerprint, err := source.OpenClient(false)
	require.NoError(t, err)
	assert.Equal(t, "session-token", firstClient.authorization)

	replacement := validSession()
	replacement.Token = "replacement-token"
	require.NoError(t, store.Save(replacement))
	changed, err := source.Changed(firstFingerprint)
	require.NoError(t, err)
	assert.True(t, changed)
	cachedClient, _, err := source.OpenClient(false)
	require.NoError(t, err)
	assert.Equal(t, "session-token", cachedClient.authorization)
	reloadedClient, secondFingerprint, err := source.OpenClient(true)
	require.NoError(t, err)
	assert.Equal(t, "replacement-token", reloadedClient.authorization)
	assert.NotEqual(t, firstFingerprint, secondFingerprint)
}

func TestSourceRejectsPersistedImportConfigurationDifferentFromExpected(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	session := validSession()
	session.Import = ImportConfig{Currency: "JPY", Scale: 0}
	require.NoError(t, store.Save(session))
	options := testClientOptions(t, "")
	options.ImportCurrency = "USD"
	options.ImportScale = 2
	source, err := NewSource(options, store)
	require.NoError(t, err)

	_, _, err = source.OpenClient(false)
	assertProviderCode(t, err, provider.CodeReconnectRequired)
}

func TestSourceAppliesNonPersistedTransactionRange(t *testing.T) {
	t.Parallel()

	store := newTestSessionStore(t)
	require.NoError(t, store.Save(validSession()))
	source, err := NewSource(testClientOptions(t, ""), store)
	require.NoError(t, err)
	ranger, ok := any(source).(interface {
		SetTransactionRange(string, string) error
	})
	require.True(t, ok)
	require.NoError(t, ranger.SetTransactionRange("2026-08-01", "2026-08-15"))

	client, _, err := source.OpenClient(false)
	require.NoError(t, err)
	assert.Equal(t, "2026-08-01", client.options.TransactionStartDate)
	assert.Equal(t, "2026-08-15", client.options.TransactionEndDate)
	assert.Equal(t, ImportConfig{Currency: "USD", Scale: 2}, validSession().Import)
}

func TestSessionImplementsOpaqueProviderContract(t *testing.T) {
	t.Parallel()

	var session provider.Session = validSession()
	assert.Equal(t, providerKind, session.ProviderKind())
}

func newTestSessionStore(t *testing.T) *SessionStore {
	t.Helper()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	store, err := NewSessionStore(paths)
	require.NoError(t, err)
	return store
}

func validSession() Session {
	return Session{
		Version:         sessionVersion,
		Token:           "session-token",
		DeviceUUID:      "00000000-0000-4000-8000-000000000001",
		RemoteProfileID: "subscription-a",
		Import:          ImportConfig{Currency: "USD", Scale: 2},
		IssuedAt:        time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
		ValidatedAt:     time.Date(2026, time.August, 15, 12, 1, 0, 0, time.UTC),
	}
}
