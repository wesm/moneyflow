package mcp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/version"
)

// Dependencies are the renderer-neutral values one tool call uses for its profile.
type Dependencies struct {
	Service     *app.Service
	ProfileID   string
	ProfileName string
	ProfileRoot string
	Clock       func() time.Time
	Random      io.Reader
	Logger      *slog.Logger
	// Supervisor owns the profile's refresh, reconcile and write attempts. New creates one for a
	// standalone server; a Profiles implementation returns the same one on every call.
	Supervisor *Supervisor
	// Hold acquires the profile again for supervised work that outlives the tool call and
	// returns its service and the function that releases it. Each tool call sets it.
	Hold func(context.Context) (*app.Service, func(), error)
}

// Options controls the registered MCP surface.
type Options struct {
	AllowWrite bool
}

// Server registers tools on one SDK server. A standalone server also owns its profile's
// background work.
type Server struct {
	SDK        *mcpsdk.Server
	profiles   profileAcquirer
	prefix     string
	classes    map[access.Class]bool
	supervisor *Supervisor
	standalone Dependencies
}

// New validates one profile binding and constructs its configured MCP surface.
func New(dependencies Dependencies, options Options) (*Server, error) {
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
	supervisor := NewSupervisor(
		context.Background(), dependencies.Clock, dependencies.Random, nil, dependencies.Logger,
	)
	dependencies.Supervisor = supervisor
	sdk := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "moneyflow", Version: version.Version},
		&mcpsdk.ServerOptions{Logger: dependencies.Logger},
	)
	sdk.AddReceivingMiddleware(privateProfileCache)
	server := &Server{
		SDK: sdk, profiles: singleProfile{dependencies: dependencies},
		supervisor: supervisor, standalone: dependencies,
	}
	registerReadTools(server)
	registerExportTools(server)
	registerResources(server, dependencies)
	if options.AllowWrite {
		registerWriteTools(server)
	}
	return server, nil
}

// Cacheable protocol results describe this profile, never a publicly reusable
// resource. Revalidate on every call, including when the client uses stdio.
func privateProfileCache(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
	return func(ctx context.Context, method string, request mcpsdk.Request) (mcpsdk.Result, error) {
		result, err := next(ctx, method, request)
		if err != nil {
			return result, err
		}
		cache := mcpsdk.Cacheable{TTLMs: 0, CacheScope: "private"}
		switch value := result.(type) {
		case *mcpsdk.ListToolsResult:
			value.Cacheable = cache
		case *mcpsdk.ListResourcesResult:
			value.Cacheable = cache
		case *mcpsdk.ListResourceTemplatesResult:
			value.Cacheable = cache
		case *mcpsdk.ReadResourceResult:
			value.Cacheable = cache
		case *mcpsdk.ListPromptsResult:
			value.Cacheable = cache
		}
		return result, nil
	}
}

// Close cancels process-owned work and waits for it or the caller deadline.
func (server *Server) Close(ctx context.Context) error {
	if server == nil || server.supervisor == nil {
		return nil
	}
	return server.supervisor.Close(ctx)
}
