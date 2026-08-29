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
	assert.Len(t, tools.Tools, 14)
	assert.Equal(t, []string{
		"confirm_refresh_deletions", "get_account_info", "get_amazon_order_details",
		"get_categories", "get_commit_status", "get_merchants", "get_refresh_status",
		"get_spending_summary", "get_transaction_details", "get_transactions",
		"get_uncategorized_transactions", "refresh_data", "review_changes",
		"search_transactions",
	}, toolNames(tools.Tools))
	resources, err := clientSession.ListResources(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, resources.Resources, 5)
	require.NoError(t, clientSession.Close())
	require.NoError(t, serverSession.Wait())
}

func toolNames(tools []*mcpsdk.Tool) []string {
	result := make([]string, len(tools))
	for index := range tools {
		result[index] = tools[index].Name
	}
	return result
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
