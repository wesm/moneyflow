package credentialvault_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/credentialvault"
	"github.com/wesm/moneyflow/internal/home"
)

func TestVaultSealOpenFingerprintAndDelete(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credentials.enc")
	vault := newVault(t, path)
	plaintext := []byte(`{"version":1,"value":"synthetic-secret"}`)
	password := []byte("account-password")

	exists, err := vault.Exists()
	require.NoError(t, err)
	assert.False(t, exists)
	require.NoError(t, vault.Seal(plaintext, password))
	decoded, err := vault.Open(password)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decoded)
	decoded[0] = 'x'
	again, err := vault.Open(password)
	require.NoError(t, err)
	assert.Equal(t, plaintext, again)
	fingerprint, err := vault.Fingerprint()
	require.NoError(t, err)
	assert.NotEmpty(t, fingerprint)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.NoError(t, vault.Delete())
	exists, err = vault.Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestVaultRejectsWrongPasswordTamperAndMalformedEnvelope(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "credentials.enc")
	vault := newVault(t, path)
	require.NoError(t, vault.Seal([]byte("synthetic"), []byte("right-password")))

	_, err := vault.Open([]byte("wrong-password"))
	assert.ErrorIs(t, err, credentialvault.ErrUnlock)
	contents, err := os.ReadFile(path) //nolint:gosec // test-owned path.
	require.NoError(t, err)
	contents[len(contents)/2] ^= 1
	require.NoError(t, home.WritePrivateFile(path, contents))
	_, err = vault.Open([]byte("right-password"))
	assert.ErrorIs(t, err, credentialvault.ErrUnlock)

	for name, invalid := range map[string][]byte{
		"truncated":     []byte(`{"version":1`),
		"trailing json": []byte(`{} {}`),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, home.WritePrivateFile(path, invalid))
			_, openErr := vault.Open([]byte("right-password"))
			assert.ErrorIs(t, openErr, credentialvault.ErrUnlock)
		})
	}
}

func TestVaultValidatesBoundsAndRejectsRedirectedTarget(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	vault := newVault(t, filepath.Join(directory, "credentials.enc"))
	assert.Error(t, vault.Seal(bytes.Repeat([]byte{'x'}, 17<<10), []byte("password")))
	assert.Error(t, vault.Seal([]byte("payload"), nil))
	_, err := vault.Open(nil)
	assert.Error(t, err)

	if err = os.Symlink(filepath.Join(directory, "elsewhere"), vault.Path()); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	assert.Error(t, vault.Seal([]byte("payload"), []byte("password")))
}

func TestVaultRejectsInvalidConstruction(t *testing.T) {
	t.Parallel()
	_, err := credentialvault.New("relative", []byte("aad"), credentialvault.Options{})
	assert.Error(t, err)
	_, err = credentialvault.New(filepath.Join(t.TempDir(), "vault"), nil, credentialvault.Options{})
	assert.Error(t, err)
	assert.False(t, errors.Is(err, credentialvault.ErrUnlock))
}

func newVault(t *testing.T, path string) *credentialvault.Vault {
	t.Helper()
	vault, err := credentialvault.New(path, []byte("moneyflow-test-v1"), credentialvault.Options{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 256)),
		Time:   1, MemoryKiB: 64, Parallelism: 1, MaxBytes: 16 << 10,
	})
	require.NoError(t, err)
	return vault
}
