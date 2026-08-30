package ynab

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
)

func TestCredentialVaultRoundTripFingerprintAndDelete(t *testing.T) {
	t.Parallel()
	vault := newTestCredentialVault(t)
	credentials := StoredCredentials{ //nolint:gosec // synthetic credential.
		AccessToken: "synthetic-access-token", PlanID: "plan-example",
		Currency: "USD", Scale: 2,
	}

	require.NoError(t, vault.Save(credentials, []byte("account-password")))
	loaded, err := vault.Load([]byte("account-password"))
	require.NoError(t, err)
	assert.Equal(t, credentials, loaded)
	fingerprint, err := vault.Fingerprint()
	require.NoError(t, err)
	assert.NotEmpty(t, fingerprint)
	contents, err := os.ReadFile(vault.Path()) //nolint:gosec // test-owned path.
	require.NoError(t, err)
	assert.NotContains(t, string(contents), credentials.AccessToken)
	require.NoError(t, vault.Delete())
	exists, err := vault.Exists()
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestCredentialVaultValidatesCompleteBinding(t *testing.T) {
	t.Parallel()
	vault := newTestCredentialVault(t)
	valid := StoredCredentials{ //nolint:gosec // synthetic credential.
		AccessToken: "synthetic-access-token", PlanID: "plan-example",
		Currency: "USD", Scale: 2,
	}
	for name, credentials := range map[string]StoredCredentials{
		"token":    {PlanID: valid.PlanID, Currency: valid.Currency, Scale: valid.Scale},
		"plan":     {AccessToken: valid.AccessToken, Currency: valid.Currency, Scale: valid.Scale},
		"currency": {AccessToken: valid.AccessToken, PlanID: valid.PlanID, Scale: valid.Scale},
		"scale": {
			AccessToken: valid.AccessToken, PlanID: valid.PlanID,
			Currency: valid.Currency, Scale: 10,
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, vault.Save(credentials, []byte("account-password")))
		})
	}
}

func TestCredentialVaultIsProfileScoped(t *testing.T) {
	t.Parallel()
	first := newTestCredentialVaultAt(t, filepath.Join(t.TempDir(), "first"))
	second := newTestCredentialVaultAt(t, filepath.Join(t.TempDir(), "second"))
	firstCredentials := StoredCredentials{ //nolint:gosec // synthetic credential.
		AccessToken: "synthetic-token-a", PlanID: "plan-a", Currency: domain.Currency("USD"), Scale: 2,
	}
	secondCredentials := StoredCredentials{ //nolint:gosec // synthetic credential.
		AccessToken: "synthetic-token-b", PlanID: "plan-b", Currency: domain.Currency("EUR"), Scale: 2,
	}
	require.NoError(t, first.Save(firstCredentials, []byte("password-a")))
	require.NoError(t, second.Save(secondCredentials, []byte("password-b")))
	loaded, err := first.Load([]byte("password-a"))
	require.NoError(t, err)
	assert.Equal(t, firstCredentials, loaded)
	_, err = first.Load([]byte("password-b"))
	assert.ErrorIs(t, err, ErrCredentialUnlock)
}

func newTestCredentialVault(t *testing.T) *CredentialVault {
	t.Helper()
	return newTestCredentialVaultAt(t, filepath.Join(t.TempDir(), "profile"))
}

func newTestCredentialVaultAt(t *testing.T, root string) *CredentialVault {
	t.Helper()
	paths, err := home.ResolveRoot(root, nil, "")
	require.NoError(t, err)
	vault, err := newCredentialVault(paths, bytes.NewReader(bytes.Repeat([]byte{0x52}, 256)), vaultKDF{
		Time: 1, MemoryKiB: 64, Parallelism: 1,
	})
	require.NoError(t, err)
	return vault
}
