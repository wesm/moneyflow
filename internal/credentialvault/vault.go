// Package credentialvault provides the provider-neutral encrypted envelope used by credential stores.
package credentialvault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"

	"github.com/wesm/moneyflow/internal/home"
)

const (
	envelopeVersion = uint16(1)
	saltBytes       = 16
	keyBytes        = 32
	kdfName         = "argon2id"
	cipherName      = "aes-256-gcm" //nolint:gosec // public algorithm identifier.
)

// ErrUnlock deliberately combines wrong-password, tamper, and malformed-envelope failures.
var ErrUnlock = errors.New("credential vault password is incorrect or the vault was modified")

// Fingerprint is an opaque content fingerprint suitable for replacement detection.
type Fingerprint string

// Options configure deterministic test injection and resource bounds.
type Options struct {
	Random      io.Reader
	Time        uint32
	MemoryKiB   uint32
	Parallelism uint8
	MaxBytes    int64
}

type kdfParameters struct {
	Time        uint32 `json:"time"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Parallelism uint8  `json:"parallelism"`
}

type envelope struct {
	Version    uint16        `json:"version"`
	KDF        string        `json:"kdf"`
	KDFParams  kdfParameters `json:"kdf_parameters"`
	Cipher     string        `json:"cipher"`
	Salt       string        `json:"salt"`
	Nonce      string        `json:"nonce"`
	Ciphertext string        `json:"ciphertext"`
}

// Vault seals arbitrary provider-owned payload bytes at one private path.
type Vault struct {
	path    string
	aad     []byte
	options Options
}

// New validates an absolute path, nonempty authenticated context, and bounded KDF options.
func New(path string, aad []byte, options Options) (*Vault, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, errors.New("create credential vault: path must be absolute")
	}
	if len(aad) == 0 {
		return nil, errors.New("create credential vault: authenticated context is empty")
	}
	if options.Random == nil {
		options.Random = cryptorand.Reader
	}
	if options.Time == 0 || options.Parallelism == 0 ||
		options.MemoryKiB < 8*uint32(options.Parallelism) || options.MaxBytes < 1 {
		return nil, errors.New("create credential vault: encryption options are invalid")
	}
	return &Vault{path: path, aad: bytes.Clone(aad), options: options}, nil
}

// Path returns the fixed provider-owned vault path.
func (vault *Vault) Path() string { return vault.path }

// Exists reports whether the hardened regular file exists.
func (vault *Vault) Exists() (bool, error) {
	info, err := os.Lstat(vault.path) //nolint:gosec // explicit caller-owned path.
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false, errors.New("inspect credential vault: target is not a regular file")
	}
	return true, nil
}

// Seal encrypts plaintext and atomically replaces the owner-only envelope.
func (vault *Vault) Seal(plaintext, password []byte) error {
	if len(password) == 0 {
		return errors.New("seal credential vault: password is empty")
	}
	if len(plaintext) == 0 {
		return errors.New("seal credential vault: plaintext is empty")
	}
	salt := make([]byte, saltBytes)
	if _, err := io.ReadFull(vault.options.Random, salt); err != nil {
		return errors.New("seal credential vault: create salt")
	}
	key := vault.deriveKey(password, salt)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return errors.New("seal credential vault: create cipher")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return errors.New("seal credential vault: create authenticated cipher")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(vault.options.Random, nonce); err != nil {
		return errors.New("seal credential vault: create nonce")
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, vault.aad)
	encoded, err := json.Marshal(envelope{
		Version: envelopeVersion, KDF: kdfName,
		KDFParams: kdfParameters{
			Time: vault.options.Time, MemoryKiB: vault.options.MemoryKiB,
			Parallelism: vault.options.Parallelism,
		},
		Cipher: cipherName, Salt: base64.RawStdEncoding.EncodeToString(salt),
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	})
	if err != nil {
		return errors.New("seal credential vault: encode envelope")
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) > vault.options.MaxBytes {
		return errors.New("seal credential vault: encoded vault exceeds maximum size")
	}
	return home.WritePrivateFile(vault.path, encoded)
}

// Open authenticates and decrypts the envelope into an independent byte slice.
func (vault *Vault) Open(password []byte) ([]byte, error) {
	if len(password) == 0 {
		return nil, errors.New("open credential vault: password is empty")
	}
	contents, err := home.ReadPrivateFile(vault.path, vault.options.MaxBytes)
	if err != nil {
		return nil, err
	}
	decoded, err := vault.decodeEnvelope(contents)
	if err != nil {
		return nil, ErrUnlock
	}
	salt, err := base64.RawStdEncoding.DecodeString(decoded.Salt)
	if err != nil || len(salt) != saltBytes {
		return nil, ErrUnlock
	}
	nonce, err := base64.RawStdEncoding.DecodeString(decoded.Nonce)
	if err != nil {
		return nil, ErrUnlock
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(decoded.Ciphertext)
	if err != nil {
		return nil, ErrUnlock
	}
	key := vault.deriveKey(password, salt)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnlock
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, ErrUnlock
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, vault.aad)
	if err != nil {
		return nil, ErrUnlock
	}
	return plaintext, nil
}

func (vault *Vault) decodeEnvelope(contents []byte) (envelope, error) {
	var decoded envelope
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || requireEOF(decoder) != nil {
		return envelope{}, ErrUnlock
	}
	if decoded.Version != envelopeVersion || decoded.KDF != kdfName ||
		decoded.Cipher != cipherName || decoded.KDFParams != (kdfParameters{
		Time: vault.options.Time, MemoryKiB: vault.options.MemoryKiB,
		Parallelism: vault.options.Parallelism,
	}) {
		return envelope{}, ErrUnlock
	}
	return decoded, nil
}

func (vault *Vault) deriveKey(password, salt []byte) []byte {
	return argon2.IDKey(
		password, salt, vault.options.Time, vault.options.MemoryKiB,
		vault.options.Parallelism, keyBytes,
	)
}

// Fingerprint returns the opaque content fingerprint without decrypting the vault.
func (vault *Vault) Fingerprint() (Fingerprint, error) {
	fingerprint, err := home.PrivateFileFingerprint(vault.path, vault.options.MaxBytes)
	return Fingerprint(fingerprint), err
}

// Delete removes only the encrypted envelope.
func (vault *Vault) Delete() error { return home.RemovePrivateFile(vault.path) }

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("credential vault contains trailing data")
	}
	return nil
}
