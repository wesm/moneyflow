package mcp

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenStoreCreatesRevealsRotatesAndRereads(t *testing.T) {
	root := canonicalTokenTestRoot(t)
	random := bytes.NewReader(append(bytes.Repeat([]byte{0x11}, 32), bytes.Repeat([]byte{0x22}, 32)...))
	store, err := NewTokenStore(root, random)
	require.NoError(t, err)
	first, err := store.Reveal()
	require.NoError(t, err)
	assert.Len(t, first, httpTokenEncodedLength)
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32)), first)
	assert.Equal(t, filepath.Join(root, "mcp", "http-token"), store.Path())
	assert.NoError(t, store.VerifyAuthorization("Bearer "+first))

	revealed, err := store.Reveal()
	require.NoError(t, err)
	assert.Equal(t, first, revealed)
	second, err := store.Rotate()
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
	assert.ErrorIs(t, store.VerifyAuthorization("Bearer "+first), ErrHTTPTokenInvalid)
	assert.NoError(t, store.VerifyAuthorization("Bearer "+second))
}

func TestTokenStoreRefusesMalformedAndOversizedContent(t *testing.T) {
	root := canonicalTokenTestRoot(t)
	store, err := NewTokenStore(root, bytes.NewReader(bytes.Repeat([]byte{0x33}, 32)))
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(root, "mcp"), 0o700))
	for _, content := range []string{"short", strings.Repeat("a", 44), strings.Repeat("*", 43)} {
		require.NoError(t, os.WriteFile(store.Path(), []byte(content), 0o600))
		_, err = store.Reveal()
		assert.Error(t, err)
	}
}

func TestTokenStoreRejectsNoncanonicalAuthorization(t *testing.T) {
	root := canonicalTokenTestRoot(t)
	store, err := NewTokenStore(root, bytes.NewReader(bytes.Repeat([]byte{0x44}, 32)))
	require.NoError(t, err)
	token, err := store.Reveal()
	require.NoError(t, err)
	for _, value := range []string{
		"", token, "bearer " + token, "Bearer  " + token, "Bearer " + token + "=", "Bearer " + strings.Repeat("a", 43),
	} {
		assert.ErrorIs(t, store.VerifyAuthorization(value), ErrHTTPTokenInvalid, value)
	}
}

func canonicalTokenTestRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return root
}
