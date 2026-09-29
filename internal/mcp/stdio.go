package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RunStdio serves MCP frames on the process standard streams until shutdown.
func (server *Server) RunStdio(ctx context.Context) error {
	return server.SDK.Run(ctx, &mcpsdk.StdioTransport{})
}
