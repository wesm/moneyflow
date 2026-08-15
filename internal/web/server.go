package web

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
)

// ServerConfig supplies the immutable dependencies shared by the API and browser application.
type ServerConfig struct {
	Service          *app.Service
	BasePath         string
	Version          string
	Origin           api.OriginConfig
	Security         *api.MutationSecurity
	WarnNonCanonical bool
}

// Server is the composed profile API and embedded browser application.
type Server struct {
	basePath string
	handler  http.Handler
}

// NewServer composes reserved API routes ahead of the single-page application fallback.
func NewServer(config ServerConfig) (*Server, error) {
	if config.Service == nil {
		return nil, errors.New("new web server: service is required")
	}
	basePath, err := api.NormalizeBasePath(config.BasePath)
	if err != nil {
		return nil, fmt.Errorf("new web server: %w", err)
	}
	if config.Origin.Canonical == nil {
		config.Origin, err = api.ResolveOrigin("127.0.0.1:8080", basePath, "")
		if err != nil {
			return nil, fmt.Errorf("new web server origin: %w", err)
		}
	}
	if config.Origin.BasePath != basePath {
		return nil, errors.New("new web server: origin base path differs from server base path")
	}
	if config.Security == nil {
		config.Security, err = api.NewMutationSecurity(config.Origin, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("new web server security: %w", err)
		}
	}
	apiServer, err := api.New(api.Config{
		Service: config.Service, BasePath: basePath, Version: config.Version,
		Origin: config.Origin, Security: config.Security,
	})
	if err != nil {
		return nil, fmt.Errorf("new web server API: %w", err)
	}
	staticHandler, err := newHandler(
		basePath, embeddedDistribution, config.Origin, config.Security, config.WarnNonCanonical,
	)
	if err != nil {
		return nil, fmt.Errorf("new web server application: %w", err)
	}
	server := &Server{basePath: basePath}
	server.handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if server.isReservedAPIPath(request.URL.Path) {
			apiServer.Handler().ServeHTTP(response, request)
			return
		}
		staticHandler.ServeHTTP(response, request)
	})
	return server, nil
}

// Handler returns the composed transport-independent HTTP handler.
func (server *Server) Handler() http.Handler {
	return server.handler
}

// HTTPServer creates the bounded production transport without binding its listener.
func (server *Server) HTTPServer(address string, errorOutput io.Writer) *http.Server {
	if errorOutput == nil {
		errorOutput = io.Discard
	}
	return &http.Server{
		Addr: address, Handler: server.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          log.New(errorOutput, "moneyflow web: ", 0),
	}
}

func (server *Server) isReservedAPIPath(requestPath string) bool {
	if !strings.HasPrefix(requestPath, server.basePath) {
		return false
	}
	relative := strings.TrimPrefix(requestPath, server.basePath)
	return strings.HasPrefix(relative, "api/") || relative == "openapi.json" || relative == "openapi.yaml"
}
