package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	mcpserver "github.com/wesm/moneyflow/internal/mcp"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

func TestMCPCommandDefaultsToStdioAndPassesWritePolicy(t *testing.T) {
	var built ProfileOptions
	var allowWrite bool
	var received MCPOptions
	streams := IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	streams.BuildMCP = func(
		_ context.Context,
		options ProfileOptions,
		write bool,
		_ IOStreams,
	) (MCPDependencies, error) {
		built = options
		allowWrite = write
		return MCPDependencies{}, nil
	}
	streams.RunMCP = func(_ context.Context, _ MCPDependencies, options MCPOptions, _ IOStreams) error {
		received = options
		return nil
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"mcp", "--profile", "profile-a", "--allow-write"})
	require.NoError(t, command.Execute())
	assert.Equal(t, "profile-a", built.Profile)
	assert.True(t, allowWrite)
	assert.Equal(t, MCPTransportStdio, received.Transport)
	assert.Empty(t, streams.Out.(*bytes.Buffer).String())
}

func TestMCPCommandHTTPDefaultsAndStdioFlagRejection(t *testing.T) {
	var received MCPOptions
	streams := IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	streams.BuildMCP = func(context.Context, ProfileOptions, bool, IOStreams) (MCPDependencies, error) {
		return MCPDependencies{}, nil
	}
	streams.RunMCP = func(_ context.Context, _ MCPDependencies, options MCPOptions, _ IOStreams) error {
		received = options
		return nil
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"mcp", "--profile", "profile-a", "--transport", "streamable-http"})
	require.NoError(t, command.Execute())
	assert.Equal(t, MCPTransportStreamableHTTP, received.Transport)
	assert.Equal(t, "127.0.0.1:8081", received.Listen)
	assert.Equal(t, "/mcp/", received.BasePath)

	command = newRootCommand(streams)
	command.SetArgs([]string{"mcp", "--profile", "profile-a", "--listen", "127.0.0.1:9090"})
	err := command.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "require --transport streamable-http")
}

func TestMCPTokenRevealAndRotateDoNotStartServer(t *testing.T) {
	homeRoot := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", homeRoot)
	catalog, err := openProfileCatalog("")
	require.NoError(t, err)
	entry, err := catalog.Create(context.Background(), profilecatalog.CreateRequest{
		DisplayName: "Example Profile", ProviderKind: "local",
	})
	require.NoError(t, err)
	started := false
	var stdout bytes.Buffer
	streams := IOStreams{
		In: strings.NewReader(""), Out: &stdout, Err: &bytes.Buffer{},
		BuildMCP: func(context.Context, ProfileOptions, bool, IOStreams) (MCPDependencies, error) {
			started = true
			return MCPDependencies{}, nil
		},
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"mcp", "token", "reveal", "--profile", entry.ID})
	require.NoError(t, command.Execute())
	first := strings.TrimSpace(stdout.String())
	assert.Len(t, first, 43)
	assert.False(t, started)

	stdout.Reset()
	command = newRootCommand(streams)
	command.SetArgs([]string{"mcp", "token", "rotate", "--profile", "Example Profile"})
	require.NoError(t, command.Execute())
	second := strings.TrimSpace(stdout.String())
	assert.Len(t, second, 43)
	assert.NotEqual(t, first, second)
	assert.False(t, started)
}

func TestMCPProfileResolutionDoesNotCreateAnImplicitProfile(t *testing.T) {
	homeRoot := t.TempDir()
	t.Setenv("MONEYFLOW_HOME", homeRoot)
	_, _, err := resolveMCPProfile(context.Background(), "", "")
	assert.Error(t, err)
	catalog, catalogErr := openProfileCatalog("")
	require.NoError(t, catalogErr)
	entries, catalogErr := catalog.List(context.Background())
	require.NoError(t, catalogErr)
	assert.Empty(t, entries)
}

func TestRunMCPHTTPServesOnlyTheDefaultEndpointWithoutRevealingToken(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := mcpserver.New(mcpserver.Dependencies{
		Service: service, ProfileID: "profile-a", ProfileRoot: root, Clock: time.Now,
		Random: strings.NewReader(strings.Repeat("r", 512)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, mcpserver.Options{})
	require.NoError(t, err)
	store, err := mcpserver.NewTokenStore(root, bytes.NewReader(bytes.Repeat([]byte{0x77}, 32)))
	require.NoError(t, err)
	token, err := store.Reveal()
	require.NoError(t, err)
	dependencies := MCPDependencies{Server: server, TokenStore: store, ProfileID: "profile-a"}
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })

	ctx, cancel := context.WithCancel(context.Background())
	address := make(chan string, 1)
	var stderr bytes.Buffer
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr,
		Listen: func(ctx context.Context, network, _ string) (net.Listener, error) {
			listener, listenErr := (&net.ListenConfig{}).Listen(ctx, network, "127.0.0.1:0")
			if listenErr == nil {
				address <- listener.Addr().String()
			}
			return listener, listenErr
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}
	result := make(chan error, 1)
	go func() {
		result <- runMCP(ctx, dependencies, MCPOptions{
			Transport: MCPTransportStreamableHTTP, Listen: "127.0.0.1:8081", BasePath: "/mcp/",
		}, streams)
	}()
	baseURL := "http://" + <-address
	assert.Equal(t, http.StatusUnauthorized, eventuallyMCPStatus(t, baseURL+"/mcp/"))
	assert.Equal(t, http.StatusNotFound, eventuallyMCPStatus(t, baseURL+"/"))
	cancel()
	require.NoError(t, <-result)
	assert.Contains(t, stderr.String(), baseURL+"/mcp/")
	assert.Contains(t, stderr.String(), store.Path())
	assert.NotContains(t, stderr.String(), token)
}

func eventuallyMCPStatus(t *testing.T, endpoint string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, http.NoBody)
		var response *http.Response
		if err == nil {
			response, err = http.DefaultClient.Do(request) // #nosec G704 -- test-owned loopback endpoint.
		}
		if err == nil {
			_ = response.Body.Close()
			return response.StatusCode
		}
		if time.Now().After(deadline) {
			require.NoError(t, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
