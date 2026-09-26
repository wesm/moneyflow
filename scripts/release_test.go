package scripts

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestReleaseBinary exercises the extracted archive, not a separately built test binary.
func TestReleaseBinary(t *testing.T) {
	binary := os.Getenv("MONEYFLOW_RELEASE_TEST_BINARY")
	if binary == "" {
		t.Skip("set MONEYFLOW_RELEASE_TEST_BINARY to an extracted release binary")
	}
	binary, err := filepath.Abs(binary)
	require.NoError(t, err)
	version := os.Getenv("MONEYFLOW_RELEASE_TEST_VERSION")
	require.NotEmpty(t, version)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "version").CombinedOutput() //nolint:gosec // Explicitly selected release binary under test.
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "moneyflow "+version+" (commit ")
	require.NotContains(t, string(output), "unknown")

	// The public CLI requires a nonzero port; reserve an available one for this smoke run.
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	listenAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	command := exec.CommandContext(ctx, binary, "web", "--demo", "--open=false", "--listen", listenAddress) //nolint:gosec // Explicitly selected release binary; synthetic demo only.
	command.Dir = t.TempDir()
	// Keep demo files inside the test fixture even when CommandContext kills the child.
	scratch := t.TempDir()
	command.Env = append(os.Environ(), "MONEYFLOW_HOME="+scratch,
		"TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	command.Stderr = os.Stderr
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	t.Cleanup(func() { cancel(); _ = command.Wait() })
	scanner := bufio.NewScanner(stdout)
	require.True(t, scanner.Scan(), "release web server did not print its address")
	address, ok := strings.CutPrefix(scanner.Text(), "Moneyflow web: ")
	require.True(t, ok, scanner.Text())
	client := &http.Client{Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	require.NoError(t, err)
	request.Header.Set("Accept", "text/html")
	response, err := client.Do(request)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, "GET %s: %s", address, body)
	asset := regexp.MustCompile(`src="\./(assets/[^" ]+\.js)"`).FindSubmatch(body)
	require.Len(t, asset, 2, "release must serve the embedded application")
	assetURL, err := response.Request.URL.Parse("/" + string(asset[1]))
	require.NoError(t, err)
	assetResponse, err := client.Get(assetURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, assetResponse.Body.Close()) })
	require.Equal(t, http.StatusOK, assetResponse.StatusCode)
	require.Contains(t, assetResponse.Header.Get("Content-Type"), "javascript")
}

func TestReleaseBuildRejectsInvalidVersion(t *testing.T) {
	for _, version := range []string{"", "1.2", "v1.2.3", "../release", "1.2.3;false"} {
		t.Run(version, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), "bash", "release-build.sh", version, filepath.Join(t.TempDir(), "out")) //nolint:gosec // Deliberate invalid arguments to the fixed script.
			output, err := command.CombinedOutput()
			require.Error(t, err)
			require.Contains(t, string(output), "version must be X.Y.Z or X.Y.Z-rc.N")
		})
	}
}

func TestReleaseBuildPreservesExistingOutput(t *testing.T) {
	directory := t.TempDir()
	marker := filepath.Join(directory, "keep.txt")
	require.NoError(t, os.WriteFile(marker, []byte("existing release"), 0o600))
	command := exec.CommandContext(t.Context(), "bash", "release-build.sh", "1.2.3-rc.1", filepath.ToSlash(directory)) //nolint:gosec // Fixed script and isolated output directory.
	output, err := command.CombinedOutput()
	require.Error(t, err)
	require.Contains(t, string(output), "output directory must not exist")
	content, err := os.ReadFile(marker) //nolint:gosec // Marker inside t.TempDir.
	require.NoError(t, err)
	require.Equal(t, "existing release", string(content))
}
