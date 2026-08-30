package mcp

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/wesm/moneyflow/internal/httpsecurity"
)

const (
	// MaxHTTPRequestBytes bounds one streamable-HTTP JSON-RPC request before SDK decoding.
	MaxHTTPRequestBytes = 1 << 20
	// HTTPShutdownTimeout bounds graceful MCP server shutdown.
	HTTPShutdownTimeout = 10 * time.Second
)

// HTTPOptions supplies one exact authenticated streamable-HTTP endpoint.
type HTTPOptions struct {
	Origin     httpsecurity.OriginConfig
	TokenStore *TokenStore
}

// HTTPHandler returns the stateless JSON-response SDK handler behind Moneyflow's exact transport
// security contract.
func (server *Server) HTTPHandler(options HTTPOptions) (http.Handler, error) {
	if server == nil || server.SDK == nil {
		return nil, errors.New("create MCP HTTP handler: server is nil") //nolint:revive // product name
	}
	if options.Origin.Canonical == nil || options.Origin.BasePath == "" {
		return nil, errors.New("create MCP HTTP handler: canonical origin is missing") //nolint:revive // product name
	}
	if options.TokenStore == nil {
		return nil, errors.New("create MCP HTTP handler: token store is nil") //nolint:revive // product name
	}
	sdkHandler := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server.SDK },
		&mcpsdk.StreamableHTTPOptions{
			Stateless: true, JSONResponse: true,
			DisableLocalhostProtection: true,
		},
	)
	var handler http.Handler = sdkHandler
	handler = limitMCPRequestBody(handler)
	handler = requireMCPOrigin(handler, options.Origin)
	handler = requireMCPAuthority(handler, options.Origin)
	handler = requireMCPToken(handler, options.TokenStore)
	handler = exactMCPPath(handler, options.Origin.BasePath)
	return mcpResponseHeaders(handler), nil
}

// NewHTTPServer creates the bounded production server without binding a listener.
func NewHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2*time.Minute + 10*time.Second,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
}

func exactMCPPath(next http.Handler, path string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != path || request.URL.Path != path || request.URL.RawQuery != "" {
			http.NotFound(response, request)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func requireMCPToken(next http.Handler, store *TokenStore) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		values := request.Header.Values("Authorization")
		if len(values) != 1 || len(values[0]) > maxAuthorizationBytes ||
			store.VerifyAuthorization(values[0]) != nil {
			response.Header().Set("WWW-Authenticate", `Bearer realm="moneyflow-mcp"`)
			http.Error(response, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func requireMCPAuthority(next http.Handler, origin httpsecurity.OriginConfig) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if httpsecurity.ValidateCanonicalHost(request, origin) != nil {
			http.Error(response, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func requireMCPOrigin(next http.Handler, origin httpsecurity.OriginConfig) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if httpsecurity.ValidateOptionalOrigin(request, origin) != nil {
			http.Error(response, "Forbidden", http.StatusForbidden)
			return
		}
		if request.Method == http.MethodOptions {
			response.Header().Set("Allow", "POST")
			http.Error(response, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func limitMCPRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Body != nil {
			request.Body = http.MaxBytesReader(response, request.Body, MaxHTTPRequestBytes)
		}
		next.ServeHTTP(response, request)
	})
}

func mcpResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Del("Access-Control-Allow-Origin")
		response.Header().Del("Access-Control-Allow-Credentials")
		next.ServeHTTP(response, request)
	})
}
