package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/version"
)

// Dependencies are the renderer-neutral values owned by one profile-scoped MCP process.
type Dependencies struct {
	Service     *app.Service
	ProfileID   string
	ProfileName string
	ProfileRoot string
	Clock       func() time.Time
	Random      io.Reader
	Logger      *slog.Logger
}

// Options controls the registered MCP surface.
type Options struct {
	AllowWrite bool
}

// Server owns one SDK server and all process-lifetime background work.
type Server struct {
	SDK        *mcpsdk.Server
	supervisor *Supervisor
}

// Supervisor is expanded by the provider-work checkpoints. Its lifetime exists from server start.
type Supervisor struct {
	cancel context.CancelFunc
	once   sync.Once
	wait   sync.WaitGroup
}

// New validates one profile binding and constructs an empty MCP surface.
func New(dependencies Dependencies, _ Options) (*Server, error) {
	switch {
	case dependencies.Service == nil:
		return nil, errors.New("new MCP server: service is nil")
	case dependencies.ProfileID == "":
		return nil, errors.New("new MCP server: profile ID is empty")
	case dependencies.ProfileRoot == "" || !filepath.IsAbs(dependencies.ProfileRoot):
		return nil, errors.New("new MCP server: profile root is not absolute")
	case dependencies.Clock == nil:
		return nil, errors.New("new MCP server: clock is nil")
	case dependencies.Random == nil:
		return nil, errors.New("new MCP server: random source is nil")
	case dependencies.Logger == nil:
		return nil, errors.New("new MCP server: logger is nil")
	}
	// The server owns this process-lifetime cancellation and invokes it from Close.
	_, cancel := context.WithCancel(context.Background()) //nolint:gosec // stored cancellation is the owned cleanup path
	supervisor := &Supervisor{cancel: cancel}
	sdk := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "moneyflow", Version: version.Version},
		&mcpsdk.ServerOptions{Logger: dependencies.Logger},
	)
	return &Server{SDK: sdk, supervisor: supervisor}, nil
}

// Close cancels process-owned work and waits for it or the caller deadline.
func (server *Server) Close(ctx context.Context) error {
	if server == nil || server.supervisor == nil {
		return nil
	}
	server.supervisor.once.Do(server.supervisor.cancel)
	done := make(chan struct{})
	go func() {
		server.supervisor.wait.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
