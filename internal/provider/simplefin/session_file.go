package simplefin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
)

const maxSessionBytes = 8 << 10

// SessionStore owns the profile's private, atomically written SimpleFIN credential file.
type SessionStore struct{ path string }

// NewSessionStore prepares the existing private-file boundary for this provider.
func NewSessionStore(paths home.Paths) (*SessionStore, error) {
	if paths.Root == "" || !filepath.IsAbs(paths.Root) {
		return nil, errors.New("SimpleFIN profile root must be absolute")
	}
	directory, err := home.EnsurePrivateSubdirectory(paths.Root, "providers", providerKind)
	if err != nil {
		return nil, err
	}
	return &SessionStore{path: filepath.Join(directory, "session.json")}, nil
}

// Save validates before replacing the saved credential. It does not encrypt it.
func (store *SessionStore) Save(session Session) error {
	if err := session.Validate(); err != nil {
		return err
	}
	value, err := json.Marshal(session)
	if err != nil || len(value) > maxSessionBytes {
		return errors.New("encode SimpleFIN session failed")
	}
	return home.WritePrivateFile(store.path, value)
}

// Load reads a complete validated session and its replacement fingerprint.
func (store *SessionStore) Load() (Session, provider.SessionFingerprint, error) {
	contents, fingerprint, err := home.ReadPrivateFileWithFingerprint(store.path, maxSessionBytes)
	if err != nil {
		return Session{}, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var session Session
	if decoder.Decode(&session) != nil {
		return Session{}, "", errors.New("invalid SimpleFIN session file")
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return Session{}, "", errors.New("invalid SimpleFIN session file")
	}
	if err = session.Validate(); err != nil {
		return Session{}, "", err
	}
	return session, provider.SessionFingerprint(fingerprint), nil
}

// Changed detects replacement or removal without exposing session contents.
func (store *SessionStore) Changed(previous provider.SessionFingerprint) (bool, error) {
	fingerprint, err := home.PrivateFileFingerprint(store.path, maxSessionBytes)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return provider.SessionFingerprint(fingerprint) != previous, err
}

// SessionFilePresent inspects readiness without creating directories or following redirects.
func SessionFilePresent(profileRoot string) (bool, error) {
	if profileRoot == "" || !filepath.IsAbs(profileRoot) {
		return false, errors.New("SimpleFIN profile root must be absolute")
	}
	for _, path := range []string{profileRoot, filepath.Join(profileRoot, "providers"), filepath.Join(profileRoot, "providers", providerKind)} {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, errors.New("SimpleFIN session directory is redirected")
		}
	}
	_, err := home.PrivateFileFingerprint(filepath.Join(profileRoot, "providers", providerKind, "session.json"), maxSessionBytes)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// SourceOptions fixes the binding independently of the credential file being loaded.
type SourceOptions struct {
	ExpectedRemoteID string
	Currency         domain.Currency
	Scale            uint8
	HTTPClient       *http.Client
}

// Source lazily loads a read-only client, allowing cached use without credentials.
type Source struct {
	options     SourceOptions
	sessions    *SessionStore
	mu          sync.Mutex
	client      *Client
	fingerprint provider.SessionFingerprint
}

var _ provider.ReaderSource = (*Source)(nil)

// NewSource configures a reader without loading the session or contacting the provider.
func NewSource(options SourceOptions, sessions *SessionStore) (*Source, error) {
	if sessions == nil {
		return nil, errors.New("SimpleFIN session store is missing")
	}
	if err := (ImportConfig{Currency: options.Currency, Scale: options.Scale}).Validate(); err != nil {
		return nil, err
	}
	return &Source{options: options, sessions: sessions}, nil
}

// Reader opens the current session, checking the original binding before returning it.
func (source *Source) Reader(ctx context.Context, reload bool) (provider.Reader, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if source.client != nil && !reload {
		return source.client, source.fingerprint, nil
	}
	session, fingerprint, err := source.sessions.Load()
	if err != nil {
		return nil, "", provider.NewError(provider.CodeReconnectRequired)
	}
	client, err := NewClient(ClientOptions{AccessURL: session.AccessURL, Import: session.Import, HTTPClient: source.options.HTTPClient})
	if err != nil {
		return nil, "", provider.NewError(provider.CodeReconnectRequired)
	}
	if (source.options.ExpectedRemoteID != "" && source.options.ExpectedRemoteID != client.remoteID) ||
		source.options.Currency != session.Import.Currency || source.options.Scale != session.Import.Scale {
		return nil, fingerprint, provider.NewError(provider.CodeIdentityMismatch)
	}
	source.client, source.fingerprint = client, fingerprint
	return client, fingerprint, nil
}

// Changed delegates replacement detection to the private session store.
func (source *Source) Changed(previous provider.SessionFingerprint) (bool, error) {
	return source.sessions.Changed(previous)
}
