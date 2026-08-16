package monarch

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/wesm/moneyflow/internal/home"
)

const (
	credentialVaultVersion   = uint16(1)
	credentialPayloadVersion = uint16(1)
	credentialVaultFilename  = "credentials.enc"
	credentialVaultMaxBytes  = int64(16 << 10)
	credentialVaultSaltBytes = 16
	credentialVaultKeyBytes  = 32
	credentialVaultKDF       = "argon2id"
	credentialVaultCipher    = "aes-256-gcm" //nolint:gosec // public algorithm identifier.
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

type credentialEnvelope struct {
	Version    uint16                  `json:"version"`
	KDF        string                  `json:"kdf"`
	KDFParams  credentialKDFParameters `json:"kdf_parameters"`
	Cipher     string                  `json:"cipher"`
	Salt       string                  `json:"salt"`
	Nonce      string                  `json:"nonce"`
	Ciphertext string                  `json:"ciphertext"`
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
	path   string
	random io.Reader
	kdf    credentialKDFParameters
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
	return &CredentialVault{
		path: filepath.Join(providerDirectory, credentialVaultFilename), random: random, kdf: kdf,
	}, nil
}

// Path returns the fixed encrypted credential path for diagnostics and hardened file operations.
func (vault *CredentialVault) Path() string { return vault.path }

// Exists reports whether an encrypted credential file is present without following links.
func (vault *CredentialVault) Exists() (bool, error) {
	info, err := os.Lstat(vault.path) //nolint:gosec // fixed caller-owned profile path.
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect monarch credential vault: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("inspect monarch credential vault: target is not a regular file")
	}
	return true, nil
}

// Save encrypts credentials with a user-provided account password and atomically replaces the vault.
func (vault *CredentialVault) Save(credentials StoredCredentials, accountPassword []byte) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if len(accountPassword) == 0 {
		return errors.New("save monarch credentials: account password is empty")
	}
	salt := make([]byte, credentialVaultSaltBytes)
	if _, err := io.ReadFull(vault.random, salt); err != nil {
		return errors.New("save monarch credentials: create salt")
	}
	key := vault.deriveKey(accountPassword, salt)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return errors.New("save monarch credentials: create cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return errors.New("save monarch credentials: create authenticated cipher")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(vault.random, nonce); err != nil {
		return errors.New("save monarch credentials: create nonce")
	}
	payload, err := json.Marshal(credentialPayload{ //nolint:gosec // encrypted immediately below.
		Version: credentialPayloadVersion, Email: credentials.Email,
		Password: credentials.Password, TOTPSecret: credentials.TOTPSecret,
	})
	if err != nil {
		return errors.New("save monarch credentials: encode credentials")
	}
	defer clear(payload)
	ciphertext := aead.Seal(nil, nonce, payload, credentialVaultAAD)
	envelope := credentialEnvelope{
		Version: credentialVaultVersion, KDF: credentialVaultKDF, KDFParams: vault.kdf,
		Cipher:     credentialVaultCipher,
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return errors.New("save monarch credentials: encode vault")
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) > credentialVaultMaxBytes {
		return errors.New("save monarch credentials: encoded vault exceeds maximum size")
	}
	if err = home.WritePrivateFile(vault.path, encoded); err != nil {
		return fmt.Errorf("save monarch credentials: %w", err)
	}
	return nil
}

// Load authenticates and decrypts the vault with the user-provided account password.
func (vault *CredentialVault) Load(accountPassword []byte) (StoredCredentials, error) {
	if len(accountPassword) == 0 {
		return StoredCredentials{}, errors.New("load monarch credentials: account password is empty")
	}
	contents, err := home.ReadPrivateFile(vault.path, credentialVaultMaxBytes)
	if err != nil {
		return StoredCredentials{}, err
	}
	envelope, err := vault.decodeEnvelope(contents)
	if err != nil {
		return StoredCredentials{}, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(envelope.Salt)
	if err != nil || len(salt) != credentialVaultSaltBytes {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	key := vault.deriveKey(accountPassword, salt)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return StoredCredentials{}, ErrCredentialUnlock
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, credentialVaultAAD)
	if err != nil {
		return StoredCredentials{}, ErrCredentialUnlock
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

func (vault *CredentialVault) decodeEnvelope(contents []byte) (credentialEnvelope, error) {
	var envelope credentialEnvelope
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil || requireJSONEOF(decoder) != nil {
		return credentialEnvelope{}, ErrCredentialUnlock
	}
	if envelope.Version != credentialVaultVersion || envelope.KDF != credentialVaultKDF ||
		envelope.KDFParams != vault.kdf || envelope.Cipher != credentialVaultCipher {
		return credentialEnvelope{}, ErrCredentialUnlock
	}
	return envelope, nil
}

func (vault *CredentialVault) deriveKey(accountPassword []byte, salt []byte) []byte {
	return argon2.IDKey(
		accountPassword,
		salt,
		vault.kdf.Time,
		vault.kdf.MemoryKiB,
		vault.kdf.Parallelism,
		credentialVaultKeyBytes,
	)
}

// Delete removes only the encrypted credential vault.
func (vault *CredentialVault) Delete() error {
	return home.RemovePrivateFile(vault.path)
}
