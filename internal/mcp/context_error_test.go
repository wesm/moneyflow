package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestRequestThatStopsOnItsContextSaysWhy(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		code   string
		detail string
	}{
		{
			name: "deadline", err: fmt.Errorf("load window: %w", context.DeadlineExceeded),
			code: "mcp_request_timeout", detail: "ran out of time",
		},
		{
			name: "canceled", err: fmt.Errorf("load window: %w", context.Canceled),
			code: "mcp_request_canceled", detail: "was canceled",
		},
		{
			name: "unexpected", err: errors.New("unexpected failure"),
			code: "mcp_internal_error", detail: "could not be completed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := connectFailingServer(t, test.err)

			result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "failing_tool"})
			require.NoError(t, err)
			require.True(t, result.IsError)
			tool := result.StructuredContent.(map[string]any)
			assert.Equal(t, test.code, tool["code"])
			assert.Contains(t, tool["detail"], test.detail)

			read, err := client.ReadResource(t.Context(), &mcpsdk.ReadResourceParams{
				URI: "moneyflow://test/failing",
			})
			require.NoError(t, err)
			var resource map[string]any
			require.NoError(t, json.Unmarshal([]byte(read.Contents[0].Text), &resource))
			assert.Equal(t, test.code, resource["code"])
		})
	}
}

func connectFailingServer(t *testing.T, failure error) *mcpsdk.ClientSession {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A", ProfileRoot: t.TempDir(),
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("q", 256)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	registerTool(server, "failing_tool", "Fails with a fixed error.", true,
		func(context.Context, struct{}) (any, error) { return nil, failure })
	addResource(server, "moneyflow://test/failing", "failing", "Fails with a fixed error.",
		func(context.Context) (any, error) { return nil, failure })
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, clientSession.Close())
		require.NoError(t, serverSession.Close())
		require.NoError(t, server.Close(context.Background()))
	})
	return clientSession
}
