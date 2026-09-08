package mcp

import (
	"bufio"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestMCPSubprocessInteroperability(t *testing.T) {
	if testing.Short() {
		t.Skip("subprocess interoperability is not part of short tests")
	}
	binary, homeRoot, entry, transactionID, categoryID := buildMCPSubprocessFixture(t)
	t.Run("stdio", func(t *testing.T) {
		testMCPStdioSubprocess(t, binary, homeRoot, entry.ID, transactionID, categoryID)
	})
	t.Run("http token rotation", func(t *testing.T) {
		testMCPHTTPSubprocess(t, binary, homeRoot, entry)
	})
}

func buildMCPSubprocessFixture(
	t *testing.T,
) (string, string, profilecatalog.Entry, string, string) {
	t.Helper()
	repositoryRoot, err := filepath.Abs("../..")
	require.NoError(t, err)
	binaryName := "moneyflow"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(t.TempDir(), binaryName)
	build := exec.Command("go", "build", "-o", binary, "./cmd/moneyflow") // #nosec G204 -- fixed test-owned build command and path.
	build.Dir = repositoryRoot
	build.Env = os.Environ()
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	homeRoot := t.TempDir()
	paths, err := home.ResolveCatalogRoot(homeRoot, nil, "")
	require.NoError(t, err)
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: cryptorand.Reader, Now: time.Now, Version: "test",
	})
	require.NoError(t, err)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Example Profile", ProviderKind: "local",
	})
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), entry.ProfilePaths(), sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions := fixture.Generate(20260830, 2)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(t.Context(), committed)
	require.NoError(t, err)
	require.NoError(t, profile.Close())
	destination := ""
	for _, category := range committed.Categories {
		if string(category.ID) != transactions[0].Category.ID && !category.Retired {
			destination = string(category.ID)
			break
		}
	}
	require.NotEmpty(t, destination)
	return binary, homeRoot, entry, string(transactions[0].ID), destination
}

func testMCPStdioSubprocess(
	t *testing.T,
	binary, homeRoot, profileID, transactionID, categoryID string,
) {
	t.Helper()
	command := exec.Command(binary, "mcp", "--profile", profileID, "--allow-write") // #nosec G204 -- test-built binary and synthetic profile.
	command.Env = mcpSubprocessEnvironment(homeRoot)
	stdout, err := command.StdoutPipe()
	require.NoError(t, err)
	stdin, err := command.StdinPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	require.NoError(t, command.Start())

	var protocol bytes.Buffer
	transport := &mcpsdk.IOTransport{
		Reader: &teeReadCloser{ReadCloser: stdout, writer: &protocol}, Writer: stdin,
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "subprocess-test", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), transport, nil)
	require.NoError(t, err)
	resources, err := session.ListResources(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, resources.Resources, 5)
	account, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "get_account_info"})
	require.NoError(t, err)
	assert.False(t, account.IsError)
	dryRun, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "update_transaction_category",
		Arguments: map[string]any{
			"expected_revision": "1", "transaction_id": transactionID,
			"category_id": categoryID, "dry_run": true,
		},
	})
	require.NoError(t, err)
	assert.False(t, dryRun.IsError)
	require.NoError(t, session.Close())
	require.NoError(t, waitForMCPProcess(command, 5*time.Second))
	assertProtocolFrames(t, protocol.Bytes())
	assert.Contains(t, protocol.String(), `"resultType":"complete"`)
	assertMCPStderrAllowlist(t, stderr.String(), false)
}

func testMCPHTTPSubprocess(
	t *testing.T,
	binary, homeRoot string,
	entry profilecatalog.Entry,
) {
	t.Helper()
	tokenStore, err := NewTokenStore(entry.Root, cryptorand.Reader)
	require.NoError(t, err)
	token, err := tokenStore.Reveal()
	require.NoError(t, err)
	command, serverURL, stdout, stderrDone := startMCPHTTPSubprocess(t, binary, homeRoot, entry)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "http-subprocess-test", Version: "1"}, nil)
	session, err := connectMCPHTTPClient(t.Context(), client, serverURL, token)
	require.NoError(t, err)
	resources, err := session.ListResources(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, resources.Resources, 5)
	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "get_account_info"})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	require.NoError(t, session.Close())

	rotate := exec.Command(binary, "mcp", "token", "rotate", "--profile", entry.ID) // #nosec G204 -- test-built binary and synthetic profile.
	rotate.Env = mcpSubprocessEnvironment(homeRoot)
	var rotateError bytes.Buffer
	rotate.Stderr = &rotateError
	rotatedBytes, err := rotate.Output()
	require.NoError(t, err, rotateError.String())
	rotated := strings.TrimSpace(string(rotatedBytes))
	require.Len(t, rotated, 43)
	assert.NotEqual(t, token, rotated)
	request, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, serverURL, strings.NewReader(`{}`),
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request) // #nosec G704 -- test-owned loopback endpoint.
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.NoError(t, response.Body.Close())
	reconnected, err := connectMCPHTTPClient(t.Context(), client, serverURL, rotated)
	require.NoError(t, err)
	tools, err := reconnected.ListTools(t.Context(), nil)
	require.NoError(t, err)
	assert.Contains(t, toolNames(tools.Tools), "rename_merchant")
	require.NoError(t, reconnected.Close())

	stopMCPProcess(t, command)
	capturedStderr := <-stderrDone
	assert.Empty(t, stdout.String())
	assert.NotContains(t, capturedStderr, token)
	assert.NotContains(t, capturedStderr, rotated)
	assertMCPStderrAllowlist(t, capturedStderr, true)
}

type mcpHTTPSubprocess struct {
	command    *exec.Cmd
	stdout     *bytes.Buffer
	endpoint   chan string
	stderrDone chan string
}

// startMCPHTTPSubprocess starts the HTTP MCP child on a reserved ephemeral port. The port is
// reserved by binding and closing, so another process can claim it first; when the child then
// exits with a bind failure before announcing its endpoint, a fresh port is tried. The bind
// failure is recognized by the child's own "listen on <address>" wrapping rather than the
// OS-specific error text, which differs between Unix and Windows.
func startMCPHTTPSubprocess(
	t *testing.T,
	binary, homeRoot string,
	entry profilecatalog.Entry,
) (*exec.Cmd, string, *bytes.Buffer, chan string) {
	t.Helper()
	const attempts = 5
	for attempt := 1; attempt <= attempts; attempt++ {
		listenAddress := reserveLoopbackAddress(t)
		child := launchMCPHTTPSubprocess(t, binary, homeRoot, entry, listenAddress)
		select {
		case serverURL := <-child.endpoint:
			return child.command, serverURL, child.stdout, child.stderrDone
		case captured := <-child.stderrDone:
			err := waitForMCPProcess(child.command, 5*time.Second)
			if strings.Contains(captured, "listen on "+listenAddress+": ") {
				t.Logf("attempt %d lost the ephemeral port race: %v", attempt, err)
				continue
			}
			require.FailNowf(t, "MCP HTTP subprocess exited before announcing", "%v\n%s", err, captured)
		case <-time.After(5 * time.Second):
			_ = child.command.Process.Kill()
			require.FailNow(t, "MCP HTTP subprocess did not announce its endpoint")
		}
	}
	require.FailNowf(t, "MCP HTTP subprocess never bound", "%d attempts lost the port race", attempts)
	return nil, "", nil, nil
}

func reserveLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

func launchMCPHTTPSubprocess(
	t *testing.T,
	binary, homeRoot string,
	entry profilecatalog.Entry,
	listenAddress string,
) mcpHTTPSubprocess {
	t.Helper()
	command := exec.Command(
		binary, "mcp", "--profile", entry.ID, "--allow-write",
		"--transport", "streamable-http", "--listen", listenAddress,
	) // #nosec G204 -- test-built binary, synthetic profile, and loopback listener.
	command.Env = mcpSubprocessEnvironment(homeRoot)
	stderr, err := command.StderrPipe()
	require.NoError(t, err)
	child := mcpHTTPSubprocess{
		command:    command,
		stdout:     &bytes.Buffer{},
		endpoint:   make(chan string, 1),
		stderrDone: make(chan string, 1),
	}
	command.Stdout = child.stdout
	require.NoError(t, command.Start())
	go func() {
		var captured strings.Builder
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			captured.WriteString(line)
			captured.WriteByte('\n')
			if value, found := strings.CutPrefix(line, "Moneyflow MCP: "); found {
				select {
				case child.endpoint <- value:
				default:
				}
			}
		}
		child.stderrDone <- captured.String()
	}()
	return child
}

func connectMCPHTTPClient(
	ctx context.Context,
	client *mcpsdk.Client,
	endpoint, token string,
) (*mcpsdk.ClientSession, error) {
	return client.Connect(ctx, &mcpsdk.StreamableClientTransport{
		Endpoint: endpoint, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: subprocessBearerRoundTripper{
			base: http.DefaultTransport, token: token,
		}},
	}, nil)
}

func mcpSubprocessEnvironment(homeRoot string) []string {
	return append(os.Environ(), "MONEYFLOW_HOME="+homeRoot)
}

func waitForMCPProcess(command *exec.Cmd, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = command.Process.Kill()
		<-done
		return context.DeadlineExceeded
	}
}

func stopMCPProcess(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if err := command.Process.Signal(os.Interrupt); err != nil {
		require.NoError(t, command.Process.Kill())
	}
	// Let the application's bounded graceful shutdown finish before the test
	// kills the process. The old five-second wait was shorter than that contract.
	err := waitForMCPProcess(command, HTTPShutdownTimeout+5*time.Second)
	if err != nil {
		var exitError *exec.ExitError
		assert.ErrorAs(t, err, &exitError)
	}
}

func assertProtocolFrames(t *testing.T, content []byte) {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(content), []byte{'\n'})
	require.NotEmpty(t, lines)
	for _, line := range lines {
		var frame map[string]any
		require.NoError(t, json.Unmarshal(line, &frame), string(line))
		assert.Equal(t, "2.0", frame["jsonrpc"])
	}
}

func assertMCPStderrAllowlist(t *testing.T, content string, allowStartup bool) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "level=") && strings.HasSuffix(line, " code=mcp_sdk_event") {
			continue
		}
		if allowStartup && (strings.HasPrefix(line, "Moneyflow MCP: http://127.0.0.1:") ||
			strings.HasPrefix(line, "Bearer token file: ")) {
			continue
		}
		assert.Fail(t, "stderr line is outside the MCP allowlist", "%q", line)
	}
}

type teeReadCloser struct {
	io.ReadCloser
	writer io.Writer
}

func (reader *teeReadCloser) Read(destination []byte) (int, error) {
	count, err := reader.ReadCloser.Read(destination)
	if count > 0 {
		_, _ = reader.writer.Write(destination[:count])
	}
	return count, err
}

type subprocessBearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (transport subprocessBearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(clone)
}
