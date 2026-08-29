package mcp

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
)

func TestServerLifecycle(t *testing.T) {
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A",
		ProfileRoot: t.TempDir(), Clock: time.Now,
		Random: strings.NewReader(strings.Repeat("r", 128)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })

	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "moneyflow-test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)

	tools, err := clientSession.ListTools(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, tools.Tools)
	require.NoError(t, clientSession.Close())
	require.NoError(t, serverSession.Wait())
}

func TestNewRejectsIncompleteDependencies(t *testing.T) {
	service, err := app.NewService(nil)
	require.NoError(t, err)
	valid := Dependencies{
		Service: service, ProfileID: "profile-a", ProfileRoot: t.TempDir(),
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("r", 128)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	tests := map[string]func(*Dependencies){
		"service":      func(value *Dependencies) { value.Service = nil },
		"profile ID":   func(value *Dependencies) { value.ProfileID = "" },
		"profile root": func(value *Dependencies) { value.ProfileRoot = "relative" },
		"clock":        func(value *Dependencies) { value.Clock = nil },
		"random":       func(value *Dependencies) { value.Random = nil },
		"logger":       func(value *Dependencies) { value.Logger = nil },
	}
	for name, invalidate := range tests {
		t.Run(name, func(t *testing.T) {
			dependencies := valid
			invalidate(&dependencies)
			_, err := New(dependencies, Options{})
			require.Error(t, err)
		})
	}
}
