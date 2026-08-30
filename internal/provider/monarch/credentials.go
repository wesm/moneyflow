package monarch

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/credentialvault"
	"github.com/wesm/moneyflow/internal/home"
)

const (
	credentialPayloadVersion = uint16(1)
	credentialVaultFilename  = "credentials.enc"
	credentialVaultMaxBytes  = int64(16 << 10)
)

var (
	// ErrCredentialUnlock deliberately combines wrong-password and tamper failures.
	ErrCredentialUnlock = errors.New(
		"unlock monarch credentials: account password is incorrect or credential vault was modified",
	)
	credentialVaultAAD = []byte("moneyflow-monarch-credentials-v1")
)

type credentialKDFParameters struct {
	Time        uint32 `json:"time"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Parallelism uint8  `json:"parallelism"`
}

var defaultCredentialKDFParameters = credentialKDFParameters{
	Time: 3, MemoryKiB: 64 * 1024, Parallelism: 4,
}

type credentialPayload struct {
	Version    uint16 `json:"version"`
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
}

// StoredCredentials are the Monarch login values protected by one account password.
type StoredCredentials struct {
	Email      string
	Password   string
	TOTPSecret string
}

// Validate rejects incomplete or padded credential values before encryption.
func (credentials StoredCredentials) Validate() error {
	if credentials.Email == "" || strings.TrimSpace(credentials.Email) != credentials.Email {
		return errors.New("validate monarch credentials: email is invalid")
	}
	if credentials.Password == "" {
		return errors.New("validate monarch credentials: password is empty")
	}
	if credentials.TOTPSecret == "" ||
		NormalizeTOTPSecret(credentials.TOTPSecret) != credentials.TOTPSecret {
		return errors.New("validate monarch credentials: TOTP secret is invalid")
	}
	if _, err := GenerateTOTPCode(credentials.TOTPSecret, time.Unix(0, 0).UTC()); err != nil {
		return errors.New("validate monarch credentials: TOTP secret is invalid")
	}
	return nil
}

// CredentialVault persists password-encrypted Monarch credentials outside SQLite and sessions.
type CredentialVault struct {
	sealed *credentialvault.Vault
}

// NewCredentialVault resolves the fixed provider credential path below a Go v2 profile root.
func NewCredentialVault(paths home.Paths) (*CredentialVault, error) {
	return newCredentialVault(paths, cryptorand.Reader, defaultCredentialKDFParameters)
}

func newCredentialVault(
	paths home.Paths,
	random io.Reader,
	kdf credentialKDFParameters,
) (*CredentialVault, error) {
	if paths.Root == "" || !filepath.IsAbs(paths.Root) {
		return nil, errors.New("create monarch credential vault: profile root must be absolute")
	}
	if random == nil || kdf.Time == 0 || kdf.MemoryKiB < 8*uint32(kdf.Parallelism) ||
		kdf.Parallelism == 0 {
		return nil, errors.New("create monarch credential vault: encryption options are invalid")
	}
	providerDirectory, err := home.EnsurePrivateSubdirectory(paths.Root, "providers", providerKind)
	if err != nil {
		return nil, fmt.Errorf("create monarch credential vault: %w", err)
	}
	sealed, err := credentialvault.New(
		filepath.Join(providerDirectory, credentialVaultFilename),
		credentialVaultAAD,
		credentialvault.Options{
			Random: random, Time: kdf.Time, MemoryKiB: kdf.MemoryKiB,
			Parallelism: kdf.Parallelism, MaxBytes: credentialVaultMaxBytes,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create monarch credential vault: %w", err)
	}
	return &CredentialVault{sealed: sealed}, nil
}

// Path returns the fixed encrypted credential path for diagnostics and hardened file operations.
func (vault *CredentialVault) Path() string { return vault.sealed.Path() }

// Exists reports whether an encrypted credential file is present without following links.
func (vault *CredentialVault) Exists() (bool, error) { return vault.sealed.Exists() }

// Save encrypts credentials with a user-provided account password and atomically replaces the vault.
func (vault *CredentialVault) Save(credentials StoredCredentials, accountPassword []byte) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if len(accountPassword) == 0 {
		return errors.New("save monarch credentials: account password is empty")
	}
	payload, err := json.Marshal(credentialPayload{ //nolint:gosec // encrypted immediately below.
		Version: credentialPayloadVersion, Email: credentials.Email,
		Password: credentials.Password, TOTPSecret: credentials.TOTPSecret,
	})
	if err != nil {
		return errors.New("save monarch credentials: encode credentials")
	}
	defer clear(payload)
	if err = vault.sealed.Seal(payload, accountPassword); err != nil {
		return fmt.Errorf("save monarch credentials: %w", err)
	}
	return nil
}

// Load authenticates and decrypts the vault with the user-provided account password.
func (vault *CredentialVault) Load(accountPassword []byte) (StoredCredentials, error) {
	if len(accountPassword) == 0 {
		return StoredCredentials{}, errors.New("load monarch credentials: account password is empty")
	}
	plaintext, err := vault.sealed.Open(accountPassword)
	if err != nil {
		if errors.Is(err, credentialvault.ErrUnlock) {
			return StoredCredentials{}, ErrCredentialUnlock
		}
		return StoredCredentials{}, err
	}
	defer clear(plaintext)
	var payload credentialPayload
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&payload); err != nil || payload.Version != credentialPayloadVersion ||
		requireJSONEOF(decoder) != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	credentials := StoredCredentials{
		Email: payload.Email, Password: payload.Password, TOTPSecret: payload.TOTPSecret,
	}
	if err = credentials.Validate(); err != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	return credentials, nil
}

// Delete removes only the encrypted credential vault.
func (vault *CredentialVault) Delete() error {
	return vault.sealed.Delete()
}
