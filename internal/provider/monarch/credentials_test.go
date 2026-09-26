package monarch

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/home"
)

func TestCredentialVaultRoundTripKeepsCredentialsEncrypted(t *testing.T) {
	t.Parallel()
	vault := newTestCredentialVault(t)
	credentials := StoredCredentials{ //nolint:gosec // synthetic test credential.
		Email: "user@example.com", Password: "not-a-real-password",
		TOTPSecret: "JBSWY3DPEHPK3PXP",
	}
	accountPassword := []byte("correct horse battery staple")

	exists, err := vault.Exists()
	require.NoError(t, err)
	assert.False(t, exists)
	require.NoError(t, vault.Save(credentials, accountPassword))
	exists, err = vault.Exists()
	require.NoError(t, err)
	assert.True(t, exists)
	info, err := os.Stat(vault.Path())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	loaded, err := vault.Load(accountPassword)
	require.NoError(t, err)
	assert.Equal(t, credentials, loaded)
	contents, err := os.ReadFile(vault.Path()) //nolint:gosec // test-owned private path.
	require.NoError(t, err)
	for _, secret := range []string{
		credentials.Email, credentials.Password, credentials.TOTPSecret,
		"email", "password", "totp_secret",
	} {
		assert.NotContains(t, strings.ToLower(string(contents)), strings.ToLower(secret))
	}
}

func TestCredentialVaultRejectsWrongPasswordAndTampering(t *testing.T) {
	t.Parallel()
	vault := newTestCredentialVault(t)
	credentials := StoredCredentials{ //nolint:gosec // synthetic test credential.
		Email: "user@example.com", Password: "not-a-real-password",
		TOTPSecret: "JBSWY3DPEHPK3PXP",
	}
	require.NoError(t, vault.Save(credentials, []byte("account-password")))

	_, err := vault.Load([]byte("wrong-password"))
	assert.ErrorIs(t, err, ErrCredentialUnlock)
	assert.ErrorContains(t, err, "account password is incorrect")
	contents, err := os.ReadFile(vault.Path()) //nolint:gosec // test-owned private path.
	require.NoError(t, err)
	contents[len(contents)/2] ^= 1
	require.NoError(t, home.WritePrivateFile(vault.Path(), contents))
	_, err = vault.Load([]byte("account-password"))
	assert.ErrorIs(t, err, ErrCredentialUnlock)
}

func TestCredentialVaultValidatesInputsAndDelete(t *testing.T) {
	t.Parallel()
	vault := newTestCredentialVault(t)
	valid := StoredCredentials{ //nolint:gosec // synthetic test credential.
		Email: "user@example.com", Password: "not-a-real-password",
		TOTPSecret: "JBSWY3DPEHPK3PXP",
	}
	for name, credentials := range map[string]StoredCredentials{
		"email":       {Password: valid.Password, TOTPSecret: valid.TOTPSecret},
		"password":    {Email: valid.Email, TOTPSecret: valid.TOTPSecret},
		"totp secret": {Email: valid.Email, Password: valid.Password},
		"invalid totp": {
			Email: valid.Email, Password: valid.Password, TOTPSecret: "not-base32!",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, vault.Save(credentials, []byte("account-password")))
		})
	}
	assert.Error(t, vault.Save(valid, nil))
	_, err := vault.Load(nil)
	assert.Error(t, err)

	require.NoError(t, vault.Save(valid, []byte("account-password")))
	require.NoError(t, vault.Delete())
	exists, err := vault.Exists()
	require.NoError(t, err)
	assert.False(t, exists)
	_, err = vault.Load([]byte("account-password"))
	assert.True(t, errors.Is(err, os.ErrNotExist))
}

func newTestCredentialVault(t *testing.T) *CredentialVault {
	t.Helper()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	random := bytes.NewReader(bytes.Repeat([]byte{0x42}, 128))
	vault, err := newCredentialVault(paths, random, credentialKDFParameters{
		Time: 1, MemoryKiB: 64, Parallelism: 1,
	})
	require.NoError(t, err)
	return vault
}
