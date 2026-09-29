// Package ynab implements the YNAB provider adapter.
package ynab

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wesm/moneyflow/internal/credentialvault"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
)

const (
	providerKind            = "ynab"
	vaultFilename           = "credentials.enc"
	vaultPayloadVersion     = uint16(1)
	vaultMaximumBytes       = int64(16 << 10)
	defaultVaultTime        = uint32(3)
	defaultVaultMemoryKiB   = uint32(64 * 1024)
	defaultVaultParallelism = uint8(4)
)

var (
	vaultAAD = []byte("moneyflow-ynab-credentials-v1")
	// ErrCredentialUnlock deliberately combines wrong-password and tamper failures.
	ErrCredentialUnlock = errors.New(
		"unlock YNAB credentials: account password is incorrect or credential vault was modified",
	)
)

type vaultKDF struct {
	Time        uint32
	MemoryKiB   uint32
	Parallelism uint8
}

type credentialPayload struct {
	Version     uint16          `json:"version"`
	AccessToken string          `json:"access_token"`
	PlanID      string          `json:"plan_id"`
	Currency    domain.Currency `json:"currency"`
	Scale       uint8           `json:"scale"`
}

// StoredCredentials are the YNAB token and selected plan interpretation protected by a password.
type StoredCredentials struct {
	AccessToken string
	PlanID      string
	Currency    domain.Currency
	Scale       uint8
}

// Validate requires a complete selected plan before persistence.
func (credentials StoredCredentials) Validate() error {
	if credentials.AccessToken == "" || strings.TrimSpace(credentials.AccessToken) != credentials.AccessToken {
		return errors.New("validate YNAB credentials: access token is invalid")
	}
	if credentials.PlanID == "" || strings.TrimSpace(credentials.PlanID) != credentials.PlanID {
		return errors.New("validate YNAB credentials: plan identity is invalid")
	}
	if !domain.IsValidCurrency(credentials.Currency) || credentials.Scale > 9 {
		return errors.New("validate YNAB credentials: money interpretation is invalid")
	}
	return nil
}

// CredentialVault persists one password-encrypted YNAB token and plan binding outside SQLite.
type CredentialVault struct {
	sealed *credentialvault.Vault
}

// CredentialFilePresent checks the provider-owned vault path without creating or modifying
// profile directories. Catalog listing uses this local-only probe.
func CredentialFilePresent(profileRoot string) (bool, error) {
	if profileRoot == "" || !filepath.IsAbs(profileRoot) {
		return false, errors.New("inspect YNAB credentials: profile root must be absolute")
	}
	components := []string{
		profileRoot,
		filepath.Join(profileRoot, "providers"),
		filepath.Join(profileRoot, "providers", providerKind),
	}
	for _, directory := range components {
		info, err := os.Lstat(directory)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect YNAB credential directory: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, errors.New("inspect YNAB credentials: directory is redirected")
		}
	}
	path := filepath.Join(components[len(components)-1], vaultFilename)
	if _, err := home.PrivateFileFingerprint(path, vaultMaximumBytes); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("inspect YNAB credentials: %w", err)
	}
	return true, nil
}

// NewCredentialVault resolves the fixed provider credential path below a Go v2 profile root.
func NewCredentialVault(paths home.Paths) (*CredentialVault, error) {
	return newCredentialVault(paths, cryptorand.Reader, vaultKDF{
		Time: defaultVaultTime, MemoryKiB: defaultVaultMemoryKiB,
		Parallelism: defaultVaultParallelism,
	})
}

func newCredentialVault(paths home.Paths, random io.Reader, kdf vaultKDF) (*CredentialVault, error) {
	if paths.Root == "" || !filepath.IsAbs(paths.Root) {
		return nil, errors.New("create YNAB credential vault: profile root must be absolute")
	}
	providerDirectory, err := home.EnsurePrivateSubdirectory(paths.Root, "providers", providerKind)
	if err != nil {
		return nil, fmt.Errorf("create YNAB credential vault: %w", err)
	}
	sealed, err := credentialvault.New(
		filepath.Join(providerDirectory, vaultFilename), vaultAAD,
		credentialvault.Options{
			Random: random, Time: kdf.Time, MemoryKiB: kdf.MemoryKiB,
			Parallelism: kdf.Parallelism, MaxBytes: vaultMaximumBytes,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create YNAB credential vault: %w", err)
	}
	return &CredentialVault{sealed: sealed}, nil
}

// Path returns the fixed encrypted credential path.
func (vault *CredentialVault) Path() string { return vault.sealed.Path() }

// Exists reports whether an encrypted credential file is present.
func (vault *CredentialVault) Exists() (bool, error) { return vault.sealed.Exists() }

// Save validates and seals a complete YNAB credential payload.
func (vault *CredentialVault) Save(credentials StoredCredentials, accountPassword []byte) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(credentialPayload{ //nolint:gosec // encrypted immediately below.
		Version: vaultPayloadVersion, AccessToken: credentials.AccessToken,
		PlanID: credentials.PlanID, Currency: credentials.Currency, Scale: credentials.Scale,
	})
	if err != nil {
		return errors.New("save YNAB credentials: encode credentials")
	}
	defer clear(payload)
	if err = vault.sealed.Seal(payload, accountPassword); err != nil {
		return fmt.Errorf("save YNAB credentials: %w", err)
	}
	return nil
}

// Load authenticates, validates, and returns the sealed YNAB credentials.
func (vault *CredentialVault) Load(accountPassword []byte) (StoredCredentials, error) {
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
	if err = decoder.Decode(&payload); err != nil || payload.Version != vaultPayloadVersion ||
		requireJSONEOF(decoder) != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	credentials := StoredCredentials{
		AccessToken: payload.AccessToken, PlanID: payload.PlanID,
		Currency: payload.Currency, Scale: payload.Scale,
	}
	if err = credentials.Validate(); err != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	return credentials, nil
}

// Fingerprint returns an opaque provider session fingerprint.
func (vault *CredentialVault) Fingerprint() (provider.SessionFingerprint, error) {
	fingerprint, err := vault.sealed.Fingerprint()
	return provider.SessionFingerprint(fingerprint), err
}

// Delete removes only the encrypted YNAB credential vault.
func (vault *CredentialVault) Delete() error { return vault.sealed.Delete() }

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("YNAB credential payload contains trailing data")
	}
	return nil
}
