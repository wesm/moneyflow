package home

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	mcpHTTPTokenBytes    = 32
	mcpHTTPTokenTextSize = 43
)

// MCPHTTPTokenPath resolves the one fixed profile-scoped bearer-token path.
func MCPHTTPTokenPath(profileRoot string) (string, error) {
	root, err := canonicalRoot(profileRoot)
	if err != nil {
		return "", fmt.Errorf("resolve MCP HTTP token path: %w", err)
	}
	return filepath.Join(root, "mcp", "http-token"), nil
}

// EnsureMCPHTTPToken returns the existing canonical token or creates it privately once.
func EnsureMCPHTTPToken(profileRoot string, random io.Reader) (string, error) {
	lock, err := acquireMCPTokenLock(profileRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Release() }()
	directory, err := EnsurePrivateSubdirectory(profileRoot, "mcp")
	if err != nil {
		return "", fmt.Errorf("ensure MCP HTTP token: %w", err)
	}
	path := filepath.Join(directory, "http-token")
	if _, statErr := os.Lstat(path); statErr == nil {
		if err = validateMCPTokenFileSecurity(path); err != nil {
			return "", fmt.Errorf("ensure MCP HTTP token: %w", err)
		}
	}
	contents, err := ReadPrivateFile(path, mcpHTTPTokenTextSize+1)
	if err == nil {
		return validateMCPHTTPToken(contents)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("ensure MCP HTTP token: read existing token: %w", err)
	}
	token, err := generateMCPHTTPToken(random)
	if err != nil {
		return "", err
	}
	if err = WritePrivateFile(path, []byte(token)); err != nil {
		return "", fmt.Errorf("ensure MCP HTTP token: write token: %w", err)
	}
	return token, nil
}

// ReadMCPHTTPToken reads and validates the existing private bearer token.
func ReadMCPHTTPToken(profileRoot string) (string, error) {
	lock, err := acquireMCPTokenLock(profileRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Release() }()
	path, err := MCPHTTPTokenPath(profileRoot)
	if err != nil {
		return "", err
	}
	if err = validateMCPTokenFileSecurity(path); err != nil {
		return "", fmt.Errorf("read MCP HTTP token: %w", err)
	}
	contents, err := ReadPrivateFile(path, mcpHTTPTokenTextSize+1)
	if err != nil {
		return "", fmt.Errorf("read MCP HTTP token: %w", err)
	}
	return validateMCPHTTPToken(contents)
}

// RotateMCPHTTPToken atomically replaces the profile-scoped bearer token.
func RotateMCPHTTPToken(profileRoot string, random io.Reader) (string, error) {
	lock, err := acquireMCPTokenLock(profileRoot)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.Release() }()
	directory, err := EnsurePrivateSubdirectory(profileRoot, "mcp")
	if err != nil {
		return "", fmt.Errorf("rotate MCP HTTP token: %w", err)
	}
	token, err := generateMCPHTTPToken(random)
	if err != nil {
		return "", err
	}
	if err = WritePrivateFile(filepath.Join(directory, "http-token"), []byte(token)); err != nil {
		return "", fmt.Errorf("rotate MCP HTTP token: write token: %w", err)
	}
	return token, nil
}

func acquireMCPTokenLock(profileRoot string) (*Lock, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		lock, err := TryLock(profileRoot, LockMCPHTTPToken, LockExclusive)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrLockBusy) || !time.Now().Before(deadline) {
			return nil, fmt.Errorf("acquire MCP HTTP token lock: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func generateMCPHTTPToken(random io.Reader) (string, error) {
	if random == nil {
		random = rand.Reader
	}
	material := make([]byte, mcpHTTPTokenBytes)
	if _, err := io.ReadFull(random, material); err != nil {
		return "", errors.New("generate MCP HTTP token")
	}
	return base64.RawURLEncoding.EncodeToString(material), nil
}

func validateMCPHTTPToken(contents []byte) (string, error) {
	if len(contents) != mcpHTTPTokenTextSize {
		return "", errors.New("MCP HTTP token is malformed")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(contents))
	if err != nil || len(decoded) != mcpHTTPTokenBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != string(contents) {
		return "", errors.New("MCP HTTP token is malformed")
	}
	return string(contents), nil
}
