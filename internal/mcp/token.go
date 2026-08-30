package mcp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wesm/moneyflow/internal/home"
)

const (
	httpTokenBytes         = 32
	httpTokenEncodedLength = 43
	maxAuthorizationBytes  = len("Bearer ") + httpTokenEncodedLength
)

// ErrHTTPTokenInvalid deliberately collapses missing, malformed, and mismatched credentials.
var ErrHTTPTokenInvalid = errors.New("MCP HTTP token is invalid") //nolint:revive // product name

// TokenStore owns one profile-scoped private streamable-HTTP bearer token.
type TokenStore struct {
	profileRoot string
	random      io.Reader
}

// NewTokenStore validates one profile root without creating token state.
func NewTokenStore(profileRoot string, random io.Reader) (*TokenStore, error) {
	if profileRoot == "" || !filepath.IsAbs(profileRoot) {
		return nil, errors.New("new MCP token store: profile root must be absolute") //nolint:revive // product name
	}
	if random == nil {
		random = rand.Reader
	}
	return &TokenStore{profileRoot: profileRoot, random: random}, nil
}

// Path returns the token path for user-facing startup guidance. It does not reveal token bytes.
func (store *TokenStore) Path() string {
	if store == nil {
		return ""
	}
	return filepath.Join(store.profileRoot, "mcp", "http-token")
}

// Reveal returns the current canonical token, creating it on first use.
func (store *TokenStore) Reveal() (string, error) {
	if store == nil {
		return "", errors.New("reveal MCP token: token store is nil") //nolint:revive // product name
	}
	lock, err := home.TryLockExisting(store.profileRoot, home.LockMCPHTTPToken, home.LockExclusive)
	if err != nil {
		return "", fmt.Errorf("reveal MCP token: %w", err) //nolint:revive // product name
	}
	defer func() { _ = lock.Release() }()
	value, err := store.read()
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return store.create()
}

// Rotate atomically replaces the token and returns the new explicit reveal value.
func (store *TokenStore) Rotate() (string, error) {
	if store == nil {
		return "", errors.New("rotate MCP token: token store is nil") //nolint:revive // product name
	}
	lock, err := home.TryLockExisting(store.profileRoot, home.LockMCPHTTPToken, home.LockExclusive)
	if err != nil {
		return "", fmt.Errorf("rotate MCP token: %w", err) //nolint:revive // product name
	}
	defer func() { _ = lock.Release() }()
	return store.create()
}

// VerifyAuthorization rereads the current token and compares one exact Bearer credential.
func (store *TokenStore) VerifyAuthorization(value string) error {
	if store == nil || len(value) != maxAuthorizationBytes || !strings.HasPrefix(value, "Bearer ") {
		return ErrHTTPTokenInvalid
	}
	candidate, err := decodeCanonicalToken(strings.TrimPrefix(value, "Bearer "))
	if err != nil {
		return ErrHTTPTokenInvalid
	}
	currentText, err := store.read()
	if err != nil {
		return ErrHTTPTokenInvalid
	}
	current, err := decodeCanonicalToken(currentText)
	if err != nil || subtle.ConstantTimeCompare(candidate, current) != 1 {
		return ErrHTTPTokenInvalid
	}
	return nil
}

func (store *TokenStore) create() (string, error) {
	directory, err := home.EnsurePrivateSubdirectory(store.profileRoot, "mcp")
	if err != nil {
		return "", fmt.Errorf("write MCP token: %w", err) //nolint:revive // product name
	}
	secret := make([]byte, httpTokenBytes)
	if _, err = io.ReadFull(store.random, secret); err != nil {
		return "", fmt.Errorf("write MCP token: read randomness: %w", err) //nolint:revive // product name
	}
	value := base64.RawURLEncoding.EncodeToString(secret)
	if err = home.WritePrivateFile(filepath.Join(directory, "http-token"), []byte(value)); err != nil {
		return "", fmt.Errorf("write MCP token: %w", err) //nolint:revive // product name
	}
	return value, nil
}

func (store *TokenStore) read() (string, error) {
	contents, err := home.ReadPrivateFile(store.Path(), httpTokenEncodedLength)
	if err != nil {
		return "", fmt.Errorf("read MCP token: %w", err) //nolint:revive // product name
	}
	value := string(contents)
	if _, err = decodeCanonicalToken(value); err != nil {
		return "", errors.New("read MCP token: stored token is malformed") //nolint:revive // product name
	}
	return value, nil
}

func decodeCanonicalToken(value string) ([]byte, error) {
	if len(value) != httpTokenEncodedLength {
		return nil, ErrHTTPTokenInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != httpTokenBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != value {
		return nil, ErrHTTPTokenInvalid
	}
	return decoded, nil
}
