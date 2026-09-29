package simplefin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestSessionSurvivesReopenAndSourceChecksBinding(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	present, err := SessionFilePresent(paths.Root)
	require.NoError(t, err)
	require.False(t, present)
	sessions, err := NewSessionStore(paths)
	require.NoError(t, err)
	session := Session{Version: 1, AccessURL: "https://user:synthetic-secret@example.com/simplefin", Import: ImportConfig{Currency: "USD", Scale: 2}} // #nosec G101 -- synthetic credential in temporary profile; no network I/O.
	require.NoError(t, sessions.Save(session))
	present, err = SessionFilePresent(paths.Root)
	require.NoError(t, err)
	require.True(t, present)
	reopened, err := NewSessionStore(paths)
	require.NoError(t, err)
	loaded, fingerprint, err := reopened.Load()
	require.NoError(t, err)
	require.Equal(t, session, loaded)
	require.NotContains(t, fmt.Sprintf("%v %+v %#v", loaded, loaded, loaded), "synthetic-secret")
	source, err := NewSource(SourceOptions{Currency: "USD", Scale: 2}, reopened)
	require.NoError(t, err)
	reader, _, err := source.Reader(t.Context(), false)
	require.NoError(t, err)
	identity, err := reader.(*Client).ProbeIdentity(t.Context())
	require.NoError(t, err)
	bound, err := NewSource(SourceOptions{ExpectedRemoteID: identity.RemoteID, Currency: "USD", Scale: 2}, reopened)
	require.NoError(t, err)
	_, _, err = bound.Reader(t.Context(), false)
	require.NoError(t, err)
	session.AccessURL = "https://different:synthetic-secret@example.com/simplefin"
	require.NoError(t, reopened.Save(session))
	changed, err := bound.Changed(fingerprint)
	require.NoError(t, err)
	require.True(t, changed)
	_, _, err = bound.Reader(t.Context(), true)
	code, _ := provider.CodeOf(err)
	require.Equal(t, provider.CodeIdentityMismatch, code)
	_, writable := any(bound).(provider.WriterSource)
	require.False(t, writable)
}

func TestSessionValidationAndLazyMissingCredentials(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	sessions, err := NewSessionStore(paths)
	require.NoError(t, err)
	source, err := NewSource(SourceOptions{Currency: "USD", Scale: 2}, sessions)
	require.NoError(t, err)
	_, _, err = source.Reader(t.Context(), false)
	code, _ := provider.CodeOf(err)
	require.Equal(t, provider.CodeReconnectRequired, code)
	for _, session := range []Session{
		{Version: 1, AccessURL: "invalid", Import: ImportConfig{Currency: "USD", Scale: 2}},
		// #nosec G101 -- Reserved example credentials exercise session validation.
		{Version: 2, AccessURL: "https://user:secret@example.com", Import: ImportConfig{Currency: "USD", Scale: 2}},
		// #nosec G101 -- Reserved example credentials exercise session validation.
		{Version: 1, AccessURL: "https://user:secret@example.com", Import: ImportConfig{Currency: "points", Scale: 2}},
		// #nosec G101 -- Reserved example credentials exercise session validation.
		{Version: 1, AccessURL: "https://user:secret@example.com", Import: ImportConfig{Currency: "USD", Scale: 10}},
	} {
		require.Error(t, sessions.Save(session))
	}
	present, err := SessionFilePresent(paths.Root)
	require.NoError(t, err)
	require.False(t, present)
}
