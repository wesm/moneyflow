package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
)

func TestWebListenValidation(t *testing.T) {
	t.Parallel()
	for _, address := range []string{
		"127.0.0.1:8080", "[::1]:8080", "100.64.1.2:8080", "192.168.1.2:443",
		"localhost:8080", "moneyflow.internal:8443",
	} {
		t.Run("accept "+address, func(t *testing.T) {
			host, err := validateWebListen(address)
			require.NoError(t, err)
			assert.NotEmpty(t, host)
		})
	}
	for _, address := range []string{
		"127.0.0.1", "127.0.0.1:0", "127.0.0.1:65536", ":8080", "0.0.0.0:8080",
		"[::]:8080", "::1:8080", "http://localhost:8080", "localhost:8080/path",
		"bad..name:8080", "-bad.internal:8080", "bad-.internal:8080",
	} {
		t.Run("reject "+address, func(t *testing.T) {
			_, err := validateWebListen(address)
			assert.Error(t, err)
		})
	}
}

func TestBasePathValidation(t *testing.T) {
	t.Parallel()
	for input, expected := range map[string]string{
		"/": "/", "/moneyflow": "/moneyflow/", "/finance/tools/": "/finance/tools/",
	} {
		t.Run("accept "+input, func(t *testing.T) {
			actual, err := api.NormalizeBasePath(input)
			require.NoError(t, err)
			assert.Equal(t, expected, actual)
		})
	}
	for _, input := range []string{"/../admin", "/a/./b", "/a%2fb", "/a?b", "/a#b", `/a\b`} {
		t.Run("reject "+input, func(t *testing.T) {
			_, err := api.NormalizeBasePath(input)
			assert.Error(t, err)
		})
	}
}

func TestWebCommandPassesValidatedOptionsAndEmbeddedFixture(t *testing.T) {
	t.Parallel()
	var received WebOptions
	var service *app.Service
	streams := IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	streams.RunWeb = func(_ context.Context, candidate *app.Service, options WebOptions, _ IOStreams) error {
		service = candidate
		received = options
		return nil
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"web", "--listen", "localhost:9090", "--base-path", "/finance", "--open=false"})
	require.NoError(t, command.Execute())
	assert.NotNil(t, service)
	assert.Equal(t, WebOptions{Listen: "localhost:9090", BasePath: "/finance/", Open: false}, received)
}

func TestWebCommandWarnsBeforeNonLoopbackRunner(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	var warningSeen bool
	streams := IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr}
	streams.RunWeb = func(_ context.Context, _ *app.Service, _ WebOptions, _ IOStreams) error {
		warningSeen = strings.Contains(stderr.String(), "unauthenticated")
		return nil
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"web", "--listen", "100.64.1.2:8080", "--open=false"})
	require.NoError(t, command.Execute())
	assert.True(t, warningSeen)
}

func TestRunWebBrowserFailureIsWarning(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var stderr bytes.Buffer
	var opened string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr,
		Listen: func(context.Context, string, string) (net.Listener, error) { return listener, nil },
		OpenBrowser: func(url string) error {
			opened = url
			return errors.New("browser unavailable")
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}
	service, err := newEmbeddedService()
	require.NoError(t, err)
	require.NoError(t, runWeb(ctx, service, WebOptions{Listen: "127.0.0.1:8080", BasePath: "/", Open: true}, streams))
	assert.Contains(t, stderr.String(), "browser unavailable")
	assert.NotContains(t, opened, "?")
	assert.Equal(t, "http://"+listener.Addr().String()+"/", opened)
}

func TestRunWebServesRootAndNestedPathsThenShutsDown(t *testing.T) {
	for _, basePath := range []string{"/", "/moneyflow"} {
		t.Run(basePath, func(t *testing.T) {
			service, err := newEmbeddedService()
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			address := make(chan string, 1)
			var stdout bytes.Buffer
			streams := IOStreams{
				In: strings.NewReader(""), Out: &stdout, Err: &bytes.Buffer{},
				Listen: func(ctx context.Context, network, _ string) (net.Listener, error) {
					listener, err := (&net.ListenConfig{}).Listen(ctx, network, "127.0.0.1:0")
					if err == nil {
						address <- listener.Addr().String()
					}
					return listener, err
				},
				SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
					return context.WithCancel(parent)
				},
			}
			result := make(chan error, 1)
			go func() {
				result <- runWeb(ctx, service, WebOptions{
					Listen: "127.0.0.1:8080", BasePath: basePath, Open: false,
				}, streams)
			}()
			actualAddress := <-address
			normalized, err := api.NormalizeBasePath(basePath)
			require.NoError(t, err)
			healthURL := "http://" + actualAddress + normalized + "api/v1/health"
			response := eventuallyGET(t, healthURL)
			assert.Equal(t, http.StatusOK, response.StatusCode)
			require.NoError(t, response.Body.Close())
			assert.Contains(t, stdout.String(), "http://"+actualAddress+normalized)

			cancel()
			select {
			case err := <-result:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("web server did not shut down after cancellation")
			}
		})
	}
}

func TestRunWebPropagatesServeFailure(t *testing.T) {
	t.Parallel()
	service, err := newEmbeddedService()
	require.NoError(t, err)
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		Listen: func(context.Context, string, string) (net.Listener, error) {
			return failingListener{}, nil
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}
	err = runWeb(context.Background(), service, WebOptions{
		Listen: "127.0.0.1:8080", BasePath: "/", Open: false,
	}, streams)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accept failed")
}

func eventuallyGET(t testing.TB, url string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		response, err := http.Get(url) // #nosec G107 -- tests use an injected loopback listener.
		if err == nil {
			return response
		}
		if time.Now().After(deadline) {
			require.NoError(t, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type failingListener struct{}

func (failingListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }
func (failingListener) Close() error              { return nil }
func (failingListener) Addr() net.Addr            { return testAddress("127.0.0.1:1") }

type testAddress string

func (address testAddress) Network() string { return "tcp" }
func (address testAddress) String() string  { return string(address) }

var _ io.Closer = failingListener{}
