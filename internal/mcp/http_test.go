package mcp

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/httpsecurity"
)

func TestHTTPHandlerRejectsBeforeReadingBody(t *testing.T) {
	handler, _, _, cleanup := newHTTPTestHandler(t, "127.0.0.1:8081")
	defer cleanup()
	body := &trackingBody{}
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8081/mcp/", body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.False(t, body.read.Load())
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Credentials"))
	assert.Empty(t, response.Header().Values("Set-Cookie"))
}

func TestHTTPHandlerEnforcesExactPathTokenAuthorityAndOptionalOrigin(t *testing.T) {
	handler, token, origin, cleanup := newHTTPTestHandler(t, "127.0.0.1:8081")
	defer cleanup()
	tests := []struct {
		name   string
		target string
		modify func(*http.Request)
		status int
	}{
		{name: "other path", target: "http://127.0.0.1:8081/other", status: http.StatusNotFound},
		{name: "query token", target: "http://127.0.0.1:8081/mcp/?access_token=x", status: http.StatusNotFound},
		{name: "missing token", target: "http://127.0.0.1:8081/mcp/", status: http.StatusUnauthorized},
		{name: "wrong token", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 43))
		}, status: http.StatusUnauthorized},
		{name: "multiple tokens", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Header["Authorization"] = []string{"Bearer " + token, "Bearer " + token}
		}, status: http.StatusUnauthorized},
		{name: "wrong host", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Host = "localhost:8081"
		}, status: http.StatusForbidden},
		{name: "wrong origin", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Origin", "https://attacker.example")
		}, status: http.StatusForbidden},
		{name: "multiple origins", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header["Origin"] = []string{origin.Origin(), origin.Origin()}
		}, status: http.StatusForbidden},
		{name: "null origin", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Origin", "null")
		}, status: http.StatusForbidden},
		{name: "preflight", target: "http://127.0.0.1:8081/mcp/", modify: func(request *http.Request) {
			request.Method = http.MethodOptions
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Origin", origin.Origin())
		}, status: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.target, strings.NewReader(`{}`))
			if test.modify != nil {
				test.modify(request)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assert.Equal(t, test.status, response.Code, response.Body.String())
		})
	}
}

func TestHTTPHandlerAcceptsExactPresentOriginAndRejectsDirectExternalAuthority(t *testing.T) {
	handler, token, origin, cleanup := newHTTPTestHandler(t, "127.0.0.1:8081")
	defer cleanup()
	request := httptest.NewRequest(
		http.MethodPost, "http://127.0.0.1:8081/mcp/", strings.NewReader(`{}`),
	)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Origin", origin.Origin())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.NotEqual(t, http.StatusUnauthorized, response.Code)
	assert.NotEqual(t, http.StatusForbidden, response.Code)

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	store, err := NewTokenStore(root, bytes.NewReader(bytes.Repeat([]byte{0x7a}, 32)))
	require.NoError(t, err)
	externalToken, err := store.Reveal()
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileRoot: root,
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("r", 512)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	defer func() { require.NoError(t, server.Close(context.Background())) }()
	externalOrigin, err := httpsecurity.ResolveOrigin(
		"127.0.0.1:8081", "/mcp/", "https://moneyflow.example/mcp/",
	)
	require.NoError(t, err)
	externalHandler, err := server.HTTPHandler(HTTPOptions{Origin: externalOrigin, TokenStore: store})
	require.NoError(t, err)
	direct := httptest.NewRequest(
		http.MethodPost, "http://127.0.0.1:8081/mcp/", strings.NewReader(`{}`),
	)
	direct.Header.Set("Authorization", "Bearer "+externalToken)
	directResponse := httptest.NewRecorder()
	externalHandler.ServeHTTP(directResponse, direct)
	assert.Equal(t, http.StatusForbidden, directResponse.Code)
}

func TestOfficialClientUsesAuthenticatedStatelessHTTP(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	store, err := NewTokenStore(root, bytes.NewReader(append(
		bytes.Repeat([]byte{0x55}, 32), bytes.Repeat([]byte{0x56}, 32)...,
	)))
	require.NoError(t, err)
	token, err := store.Reveal()
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileName: "Profile A", ProfileRoot: root,
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("r", 512)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(context.Background())) })
	httpServer := httptest.NewUnstartedServer(nil)
	origin, err := httpsecurity.ResolveOrigin(httpServer.Listener.Addr().String(), "/mcp/", "")
	require.NoError(t, err)
	handler, err := server.HTTPHandler(HTTPOptions{Origin: origin, TokenStore: store})
	require.NoError(t, err)
	httpServer.Config.Handler = handler
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "moneyflow-http-test", Version: "1"}, nil)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: httpServer.URL + "/mcp/", DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{
			base: http.DefaultTransport, token: token,
		}},
	}
	session, err := client.Connect(t.Context(), transport, nil)
	require.NoError(t, err)
	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, tools.Tools, 14)
	result, err := session.CallTool(t.Context(), &mcpsdk.CallToolParams{Name: "get_account_info"})
	require.NoError(t, err)
	assert.False(t, result.IsError)
	require.NoError(t, session.Close())
	rotated, err := store.Rotate()
	require.NoError(t, err)
	oldRequest, err := http.NewRequestWithContext(
		t.Context(), http.MethodPost, httpServer.URL+"/mcp/", strings.NewReader(`{}`),
	)
	require.NoError(t, err)
	oldRequest.Header.Set("Authorization", "Bearer "+token)
	oldResponse, err := http.DefaultClient.Do(oldRequest)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, oldResponse.StatusCode)
	require.NoError(t, oldResponse.Body.Close())
	transport.HTTPClient = &http.Client{Transport: bearerRoundTripper{
		base: http.DefaultTransport, token: rotated,
	}}
	reconnected, err := client.Connect(t.Context(), transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reconnected.Close()) })
	tools, err = reconnected.ListTools(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, tools.Tools, 14)
}

func newHTTPTestHandler(
	t *testing.T,
	authority string,
) (http.Handler, string, httpsecurity.OriginConfig, func()) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	store, err := NewTokenStore(root, bytes.NewReader(bytes.Repeat([]byte{0x66}, 32)))
	require.NoError(t, err)
	token, err := store.Reveal()
	require.NoError(t, err)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileRoot: root,
		Clock: time.Now, Random: strings.NewReader(strings.Repeat("r", 512)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	origin, err := httpsecurity.ResolveOrigin(authority, "/mcp/", "")
	require.NoError(t, err)
	handler, err := server.HTTPHandler(HTTPOptions{Origin: origin, TokenStore: store})
	require.NoError(t, err)
	return handler, token, origin, func() { require.NoError(t, server.Close(context.Background())) }
}

type trackingBody struct{ read atomic.Bool }

func (body *trackingBody) Read([]byte) (int, error) {
	body.read.Store(true)
	return 0, io.EOF
}

func (*trackingBody) Close() error { return nil }

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (transport bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(clone)
}
