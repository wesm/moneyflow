package home

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHTTPTokenCreateReadRotateAndValidate(t *testing.T) {
	root := t.TempDir()
	first, err := EnsureMCPHTTPToken(root, strings.NewReader(strings.Repeat("a", 32)))
	require.NoError(t, err)
	assert.Len(t, first, 43)
	decoded, err := base64.RawURLEncoding.DecodeString(first)
	require.NoError(t, err)
	assert.Len(t, decoded, 32)
	repeated, err := EnsureMCPHTTPToken(root, strings.NewReader(strings.Repeat("b", 32)))
	require.NoError(t, err)
	assert.Equal(t, first, repeated)
	read, err := ReadMCPHTTPToken(root)
	require.NoError(t, err)
	assert.Equal(t, first, read)
	rotated, err := RotateMCPHTTPToken(root, strings.NewReader(strings.Repeat("c", 32)))
	require.NoError(t, err)
	assert.NotEqual(t, first, rotated)
}

func TestMCPHTTPTokenRefusesMalformedAndRedirectedContent(t *testing.T) {
	root := t.TempDir()
	directory, err := EnsurePrivateSubdirectory(root, "mcp")
	require.NoError(t, err)
	path := filepath.Join(directory, "http-token")
	require.NoError(t, WritePrivateFile(path, []byte("malformed")))
	_, err = ReadMCPHTTPToken(root)
	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "malformed-token-value")
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(filepath.Join(root, "elsewhere"), path))
	_, err = ReadMCPHTTPToken(root)
	assert.Error(t, err)
}

func TestMCPHTTPTokenConcurrentEnsureReturnsOneValue(t *testing.T) {
	root := t.TempDir()
	values := make(chan string, 8)
	errorsFound := make(chan error, 8)
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := EnsureMCPHTTPToken(root, nil)
			values <- value
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(values)
	close(errorsFound)
	for err := range errorsFound {
		require.NoError(t, err)
	}
	var expected string
	for value := range values {
		if expected == "" {
			expected = value
		}
		assert.Equal(t, expected, value)
	}
}

func TestMCPHTTPTokenConcurrentReadAndRotateRemainCanonical(t *testing.T) {
	root := t.TempDir()
	_, err := EnsureMCPHTTPToken(root, nil)
	require.NoError(t, err)
	var wait sync.WaitGroup
	errorsFound := make(chan error, 16)
	for index := 0; index < 16; index++ {
		wait.Add(1)
		go func(rotate bool) {
			defer wait.Done()
			for range 10 {
				var value string
				var operationErr error
				if rotate {
					value, operationErr = RotateMCPHTTPToken(root, nil)
				} else {
					value, operationErr = ReadMCPHTTPToken(root)
				}
				if operationErr != nil {
					errorsFound <- operationErr
					return
				}
				if len(value) != mcpHTTPTokenTextSize {
					errorsFound <- assert.AnError
					return
				}
			}
		}(index%2 == 0)
	}
	wait.Wait()
	close(errorsFound)
	for operationErr := range errorsFound {
		require.NoError(t, operationErr)
	}
}
