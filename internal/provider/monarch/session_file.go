package monarch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
)

const maxSessionBytes int64 = 8 << 10

// SessionStore persists Monarch-owned session material outside SQLite.
type SessionStore struct {
	path string
}

// NewSessionStore resolves the fixed provider session path below one Go v2 profile root.
func NewSessionStore(paths home.Paths) (*SessionStore, error) {
	if paths.Root == "" || !filepath.IsAbs(paths.Root) {
		return nil, errors.New("create monarch session store: profile root must be absolute")
	}
	return &SessionStore{
		path: filepath.Join(paths.Root, "providers", providerKind, "session.json"),
	}, nil
}

// Path returns the fixed session path for CLI diagnostics and hardened file operations.
func (store *SessionStore) Path() string { return store.path }

// Save validates and atomically installs one session.
func (store *SessionStore) Save(session Session) error {
	if err := session.Validate(); err != nil {
		return err
	}
	serialized, err := json.Marshal(session)
	if err != nil {
		return errors.New("save monarch session: encode session")
	}
	serialized = append(serialized, '\n')
	if int64(len(serialized)) > maxSessionBytes {
		return errors.New("save monarch session: encoded session exceeds maximum size")
	}
	if err = home.WritePrivateFile(store.path, serialized); err != nil {
		return fmt.Errorf("save monarch session: %w", err)
	}
	return nil
}

// Load returns one validated session and its opaque content fingerprint.
func (store *SessionStore) Load() (Session, provider.SessionFingerprint, error) {
	contents, fingerprint, err := home.ReadPrivateFileWithFingerprint(store.path, maxSessionBytes)
	if err != nil {
		return Session{}, "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var session Session
	if err = decoder.Decode(&session); err != nil {
		return Session{}, "", errors.New("load monarch session: decode session")
	}
	if err = requireJSONEOF(decoder); err != nil {
		return Session{}, "", err
	}
	if err = session.Validate(); err != nil {
		return Session{}, "", err
	}
	return session, provider.SessionFingerprint(fingerprint), nil
}

// Changed reports whether session content differs from a prior fingerprint.
func (store *SessionStore) Changed(previous provider.SessionFingerprint) (bool, error) {
	fingerprint, err := home.PrivateFileFingerprint(store.path, maxSessionBytes)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect monarch session replacement: %w", err)
	}
	return provider.SessionFingerprint(fingerprint) != previous, nil
}

// Delete removes only the provider session file and preserves profile data.
func (store *SessionStore) Delete() error {
	return home.RemovePrivateFile(store.path)
}

// Source caches one loaded session and can explicitly reload after atomic replacement.
type Source struct {
	options Options
	store   *SessionStore

	mu          sync.Mutex
	client      *Client
	fingerprint provider.SessionFingerprint
}

// NewSource constructs a session-backed client source.
func NewSource(options Options, store *SessionStore) (*Source, error) {
	if store == nil {
		return nil, errors.New("create monarch source: session store is nil")
	}
	validated, err := NewClient(options, "", "")
	if err != nil {
		return nil, err
	}
	return &Source{options: validated.options, store: store}, nil
}

// OpenClient returns the cached client or reloads one atomically replaced session.
func (source *Source) OpenClient(
	forceReload bool,
) (*Client, provider.SessionFingerprint, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.client != nil && !forceReload {
		return source.client, source.fingerprint, nil
	}
	session, fingerprint, err := source.store.Load()
	if err != nil {
		return nil, "", provider.NewError(provider.CodeReconnectRequired)
	}
	client, err := NewClient(source.options, session.Token, session.DeviceUUID)
	if err != nil {
		return nil, "", provider.NewError(provider.CodeReconnectRequired)
	}
	source.client = client
	source.fingerprint = fingerprint
	return source.client, source.fingerprint, nil
}

// Changed delegates opaque replacement detection to the hardened session store.
func (source *Source) Changed(previous provider.SessionFingerprint) (bool, error) {
	return source.store.Changed(previous)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("load monarch session: trailing JSON content")
	}
	return nil
}
