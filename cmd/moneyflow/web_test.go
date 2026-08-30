package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/api"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	webserver "github.com/wesm/moneyflow/internal/web"
)

func TestBuildWebDependenciesOrdinaryStartupDoesNotOpenProfile(t *testing.T) {
	t.Parallel()
	opened := false
	dependencies, err := buildWebDependencies(context.Background(), ProfileOptions{
		ExplicitHome: t.TempDir(),
	}, IOStreams{OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
		opened = true
		return OpenedProfile{}, errors.New("profile must remain lazy")
	}})
	require.NoError(t, err)
	assert.False(t, opened)
	assert.NotNil(t, dependencies.Catalog)
	_, protectedRecovery := dependencies.Catalog.(amazonMatchingProfileLifecycle)
	assert.True(t, protectedRecovery)
	assert.NotNil(t, dependencies.Registry)
	assert.NotNil(t, dependencies.Onboarding)
	assert.Empty(t, dependencies.PreselectedProfileID)
	require.NoError(t, dependencies.Close(context.Background()))
}

func TestBrowserRecoveryRunsInsideAmazonMatchingInvalidation(t *testing.T) {
	profileID := "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	plan := profilecatalog.RecoveryPlan{
		ProfileKey: profileID, ProfileID: profileID,
		BackupPath: "/synthetic/recovery", StartedAt: time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		OriginalCode: "schema_incompatible",
	}
	catalog := &webRecoveryCatalog{plan: plan}
	invalidator := &blockingRecoveryInvalidator{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	dependencies := testWebDependencies(t)
	t.Cleanup(func() { require.NoError(t, dependencies.Registry.Close(context.Background())) })
	origin, err := api.ResolveOrigin("127.0.0.1:8080", "/", "")
	require.NoError(t, err)
	security, err := api.NewMutationSecurity(origin, nil, nil)
	require.NoError(t, err)
	application, err := webserver.NewServer(webserver.ServerConfig{
		Resolver: dependencies.Registry, BasePath: "/", Version: "test",
		Origin: origin, Security: security,
		Catalog: amazonMatchingProfileLifecycle{
			ProfileCatalog: catalog, matcher: invalidator,
		},
		Evictor: webRecoveryEvictor{},
	})
	require.NoError(t, err)
	bootstrapRequest := httptest.NewRequest(http.MethodGet, "/api/v1/bootstrap", http.NoBody)
	bootstrapResponse := httptest.NewRecorder()
	application.Handler().ServeHTTP(bootstrapResponse, bootstrapRequest)
	require.Equal(t, http.StatusOK, bootstrapResponse.Code, bootstrapResponse.Body.String())
	var bootstrap api.Bootstrap
	require.NoError(t, json.Unmarshal(bootstrapResponse.Body.Bytes(), &bootstrap))

	body, err := json.Marshal(api.RecoveryBody{
		Version: api.ProfileCatalogSchemaVersion, Confirmed: true,
		Plan: &api.RecoveryPlan{
			ProfileKey: plan.ProfileKey, ProfileID: plan.ProfileID, BackupPath: plan.BackupPath,
			StartedAt: plan.StartedAt.Format(time.RFC3339), OriginalCode: string(plan.OriginalCode),
		},
	})
	require.NoError(t, err)
	recoveryDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(
			http.MethodPost, "/api/v1/profiles/"+profileID+"/recovery", bytes.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", origin.Origin())
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set("X-Moneyflow-Mutation-Token", bootstrap.MutationToken)
		response := httptest.NewRecorder()
		application.Handler().ServeHTTP(response, request)
		recoveryDone <- response
	}()
	select {
	case <-invalidator.started:
	case response := <-recoveryDone:
		t.Fatalf("browser recovery did not reach matching invalidation: %d %s", response.Code, response.Body.String())
	case <-time.After(time.Second):
		t.Fatal("browser recovery did not reach matching invalidation")
	}
	select {
	case <-recoveryDone:
		t.Fatal("browser recovery escaped matching invalidation")
	case <-time.After(50 * time.Millisecond):
	}
	close(invalidator.release)
	response := <-recoveryDone
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, 1, catalog.recreates)
}

func TestBuildWebDependenciesResolvesPreselectedNameWithoutOpeningService(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	catalog, err := openProfileCatalog(root)
	require.NoError(t, err)
	entry, err := catalog.Create(context.Background(), profilecatalog.CreateRequest{
		DisplayName: "Household", ProviderKind: "local",
	})
	require.NoError(t, err)
	opened := false
	dependencies, err := buildWebDependencies(context.Background(), ProfileOptions{
		ExplicitHome: root, Profile: "HOUSEHOLD",
	}, IOStreams{OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
		opened = true
		return OpenedProfile{}, errors.New("service must remain lazy")
	}})
	require.NoError(t, err)
	assert.False(t, opened)
	assert.Equal(t, entry.ID, dependencies.PreselectedProfileID)
	require.NoError(t, dependencies.Close(context.Background()))
}

func TestBuildWebDependenciesGivesDemoAProcessLocalCanonicalRoute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := testWebService(t)
	closes := 0
	dependencies, err := buildWebDependencies(context.Background(), ProfileOptions{
		Demo: true,
	}, IOStreams{OpenProfile: func(context.Context, ProfileOptions) (OpenedProfile, error) {
		return OpenedProfile{
			ID: "profile_demo", Service: service,
			Paths: home.Paths{Root: root, Database: root + "/moneyflow.db"},
			Close: func() error { closes++; return nil }, Demo: true,
		}, nil
	}})
	require.NoError(t, err)
	assert.True(t, profilecatalog.ValidProfileID(dependencies.PreselectedProfileID))
	assert.Nil(t, dependencies.Catalog)
	require.NoError(t, dependencies.Close(context.Background()))
	assert.Equal(t, 1, closes)
}

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
	var receivedDependencies WebDependencies
	streams := IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	streams.BuildWeb = testWebDependencyBuilder(t)
	streams.RunWeb = func(_ context.Context, candidate WebDependencies, options WebOptions, _ IOStreams) error {
		receivedDependencies = candidate
		received = options
		return nil
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{"web", "--listen", "localhost:9090", "--base-path", "/finance", "--open=false"})
	require.NoError(t, command.Execute())
	assert.NotNil(t, receivedDependencies.Registry)
	assert.Equal(t, WebOptions{Listen: "localhost:9090", BasePath: "/finance/", Open: false}, received)
}

func TestExternalURLValidationAndCommandPropagation(t *testing.T) {
	t.Parallel()
	var received WebOptions
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		BuildWeb: testWebDependencyBuilder(t),
		RunWeb: func(_ context.Context, _ WebDependencies, options WebOptions, _ IOStreams) error {
			received = options
			return nil
		},
	}
	command := newRootCommand(streams)
	command.SetArgs([]string{
		"web", "--listen", "127.0.0.1:8080", "--base-path", "/moneyflow",
		"--external-url", "https://moneyflow.example/moneyflow", "--open=false",
	})
	require.NoError(t, command.Execute())
	assert.Equal(t, "https://moneyflow.example/moneyflow", received.ExternalURL)

	for _, value := range []string{
		"https://moneyflow.example/other", "ftp://moneyflow.example/moneyflow",
		"https://user@moneyflow.example/moneyflow",
	} {
		command = newRootCommand(streams)
		command.SetArgs([]string{"web", "--base-path", "/moneyflow", "--external-url", value})
		assert.Error(t, command.Execute(), value)
	}
}

func TestWebCommandWarnsBeforeNonLoopbackRunner(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	var warningSeen bool
	streams := IOStreams{In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &stderr}
	streams.BuildWeb = testWebDependencyBuilder(t)
	streams.RunWeb = func(_ context.Context, _ WebDependencies, _ WebOptions, _ IOStreams) error {
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
	dependencies := testWebDependencies(t)
	dependencies.PreselectedProfileID = "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, runWeb(ctx, dependencies, WebOptions{Listen: "127.0.0.1:8080", BasePath: "/", Open: true}, streams))
	assert.Contains(t, stderr.String(), "browser unavailable")
	assert.NotContains(t, opened, "?")
	assert.Equal(
		t,
		"http://"+listener.Addr().String()+"/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/",
		opened,
	)
}

func TestRunWebStartsServingBeforeOpeningBrowser(t *testing.T) {
	t.Parallel()

	dependencies := testWebDependencies(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var openedHealthy bool
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		Listen: func(ctx context.Context, network, _ string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, network, "127.0.0.1:0")
		},
		OpenBrowser: func(url string) error {
			defer cancel()
			response, openErr := http.Get( // #nosec G107 -- injected loopback URL.
				url + "api/v1/profiles/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/health",
			)
			if openErr != nil {
				return openErr
			}
			openedHealthy = response.StatusCode == http.StatusOK
			return response.Body.Close()
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}
	require.NoError(t, runWeb(ctx, dependencies, WebOptions{
		Listen: "127.0.0.1:8080", BasePath: "/", Open: true,
	}, streams))
	assert.True(t, openedHealthy)
}

func TestRunWebSweepsIdleProfilesUntilShutdown(t *testing.T) {
	t.Parallel()
	dependencies := testWebDependencies(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	swept := make(chan struct{}, 1)
	dependencies.CloseIdle = func(context.Context) error {
		select {
		case swept <- struct{}{}:
		default:
		}
		cancel()
		return nil
	}
	dependencies.IdleSweepInterval = time.Millisecond
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		Listen: func(ctx context.Context, network, _ string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, network, "127.0.0.1:0")
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}

	require.NoError(t, runWeb(ctx, dependencies, WebOptions{
		Listen: "127.0.0.1:8080", BasePath: "/", Open: false,
	}, streams))
	select {
	case <-swept:
	default:
		t.Fatal("idle profile sweep did not run")
	}
}

func TestRunWebServesRootAndNestedPathsThenShutsDown(t *testing.T) {
	for _, basePath := range []string{"/", "/moneyflow"} {
		t.Run(basePath, func(t *testing.T) {
			dependencies := testWebDependencies(t)
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
				result <- runWeb(ctx, dependencies, WebOptions{
					Listen: "127.0.0.1:8080", BasePath: basePath, Open: false,
				}, streams)
			}()
			actualAddress := <-address
			normalized, err := api.NormalizeBasePath(basePath)
			require.NoError(t, err)
			healthURL := "http://" + actualAddress + normalized +
				"api/v1/profiles/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/health"
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

func TestRunWebHonorsExternalBasePathProfileContract(t *testing.T) {
	root := t.TempDir()
	seedCatalog, err := openProfileCatalog(root)
	require.NoError(t, err)
	created, err := seedCatalog.Create(context.Background(), profilecatalog.CreateRequest{
		DisplayName: "Household", ProviderKind: "local",
	})
	require.NoError(t, err)
	require.True(t, profilecatalog.ValidProfileID(created.ID))

	dependencies, err := buildWebDependencies(
		context.Background(), ProfileOptions{ExplicitHome: root}, IOStreams{},
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, dependencies.Close(context.Background())) })
	require.NotNil(t, dependencies.Catalog)
	require.NotNil(t, dependencies.Onboarding)
	ctx, cancel := context.WithCancel(context.Background())
	address := make(chan string, 1)
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
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
	var stopOnce sync.Once
	stopServer := func() {
		stopOnce.Do(func() {
			cancel()
			select {
			case err := <-result:
				assert.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Error("web server did not shut down after cancellation")
			}
		})
	}
	t.Cleanup(stopServer)
	go func() {
		result <- runWeb(ctx, dependencies, WebOptions{
			Listen: "127.0.0.1:8080", BasePath: "/moneyflow/",
			ExternalURL: "https://host-a.example/moneyflow/", Open: false,
		}, streams)
	}()
	directOrigin := "http://" + <-address
	baseURL := directOrigin + "/moneyflow/"

	selector := eventuallyGET(t, baseURL)
	require.Equal(t, http.StatusOK, selector.StatusCode)
	selectorBody, err := io.ReadAll(selector.Body)
	require.NoError(t, err)
	require.NoError(t, selector.Body.Close())
	assert.Contains(t, string(selectorBody), `<base href="/moneyflow/"`)
	canonicalLink := regexp.MustCompile(`<link rel="canonical" href="([^"]*)"\s*/?>`).
		FindSubmatch(selectorBody)
	require.Len(t, canonicalLink, 2, "selector document must carry one canonical link element")
	assert.Equal(t, "https://host-a.example/moneyflow/", string(canonicalLink[1]))
	assetMatch := regexp.MustCompile(`(?:src|href)="\./(assets/[^"]+)"`).FindSubmatch(selectorBody)
	require.Len(t, assetMatch, 2)
	asset := eventuallyGET(t, baseURL+string(assetMatch[1]))
	assert.Equal(t, http.StatusOK, asset.StatusCode)
	require.NoError(t, asset.Body.Close())

	listed := eventuallyGET(t, baseURL+"api/v1/profiles")
	require.Equal(t, http.StatusOK, listed.StatusCode)
	var catalogResponse api.ProfileCatalogResponse
	require.NoError(t, json.NewDecoder(listed.Body).Decode(&catalogResponse))
	require.NoError(t, listed.Body.Close())
	assert.Equal(t, api.ProfileCatalogSchemaVersion, catalogResponse.Version)
	require.Len(t, catalogResponse.Profiles, 1)
	summary := catalogResponse.Profiles[0]
	assert.Equal(t, created.ID, summary.ID)
	assert.Equal(t, "Household", summary.DisplayName)
	assert.Equal(t, "local", summary.ProviderKind)
	assert.Equal(t, string(profilecatalog.StatusSetupIncomplete), summary.Status)

	profileID := summary.ID
	selected := eventuallyGETAccept(t, baseURL+"p/"+profileID+"/", "text/html")
	assert.Equal(t, http.StatusOK, selected.StatusCode)
	require.NoError(t, selected.Body.Close())
	health := eventuallyGET(t, baseURL+"api/v1/profiles/"+profileID+"/health")
	assert.Equal(t, http.StatusOK, health.StatusCode)
	require.NoError(t, health.Body.Close())
	bootstrap := eventuallyGET(t, baseURL+"api/v1/profiles/"+profileID+"/bootstrap")
	require.Equal(t, http.StatusOK, bootstrap.StatusCode)
	var configuration api.Bootstrap
	require.NoError(t, json.NewDecoder(bootstrap.Body).Decode(&configuration))
	require.NoError(t, bootstrap.Body.Close())
	assert.Equal(t, "/moneyflow/", configuration.BasePath)
	assert.Equal(t, "https://host-a.example/moneyflow/", configuration.CanonicalURL)

	mutation, err := http.NewRequestWithContext(
		context.Background(), http.MethodPost,
		baseURL+"api/v1/profiles/"+profileID+"/mutations", strings.NewReader("{}"),
	)
	require.NoError(t, err)
	mutation.Header.Set("Content-Type", "application/json")
	mutation.Header.Set("Origin", directOrigin)
	mutation.Header.Set("Sec-Fetch-Site", "same-origin")
	mutation.Header.Set("X-Moneyflow-Mutation-Token", configuration.MutationToken)
	mutation.Close = true
	rejected, err := http.DefaultClient.Do(mutation)
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, rejected.StatusCode)
	var problem struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.NewDecoder(rejected.Body).Decode(&problem))
	require.NoError(t, rejected.Body.Close())
	assert.Equal(t, string(api.CodeInvalidOrigin), problem.Code)

	stopServer()
	require.NoError(t, dependencies.Close(context.Background()))
}

func TestRunWebPropagatesServeFailure(t *testing.T) {
	t.Parallel()
	dependencies := testWebDependencies(t)
	streams := IOStreams{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		Listen: func(context.Context, string, string) (net.Listener, error) {
			return failingListener{}, nil
		},
		SignalContext: func(parent context.Context) (context.Context, context.CancelFunc) {
			return context.WithCancel(parent)
		},
	}
	err := runWeb(context.Background(), dependencies, WebOptions{
		Listen: "127.0.0.1:8080", BasePath: "/", Open: false,
	}, streams)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accept failed")
}

func eventuallyGET(t testing.TB, url string) *http.Response {
	return eventuallyGETAccept(t, url, "")
}

func eventuallyGETAccept(t testing.TB, url, accept string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
		if err == nil && accept != "" {
			request.Header.Set("Accept", accept)
		}
		if err == nil {
			request.Close = true
		}
		var response *http.Response
		if err == nil {
			response, err = http.DefaultClient.Do(request) // #nosec G704 -- injected loopback URL.
		}
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

func testWebService(t *testing.T) *app.Service {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	return service
}

func testWebDependencies(t testing.TB) WebDependencies {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	root := t.TempDir()
	registry, err := webserver.NewProfileRegistry(webserver.ProfileRegistryConfig{
		Open: func(context.Context, string) (webserver.RegistryProfile, error) {
			return webserver.RegistryProfile{
				ID:      "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
				Paths:   home.Paths{Root: root, Database: root + "/moneyflow.db"},
				Service: service, Close: func() error { return nil },
			}, nil
		},
	})
	require.NoError(t, err)
	return WebDependencies{Registry: registry}
}

func testWebDependencyBuilder(t testing.TB) WebDependencyBuilder {
	t.Helper()
	return func(context.Context, ProfileOptions, IOStreams) (WebDependencies, error) {
		return testWebDependencies(t), nil
	}
}

type webRecoveryCatalog struct {
	plan      profilecatalog.RecoveryPlan
	recreates int
}

func (catalog *webRecoveryCatalog) List(context.Context) ([]profilecatalog.Entry, error) {
	return nil, nil
}

func (catalog *webRecoveryCatalog) Create(
	context.Context,
	profilecatalog.CreateRequest,
) (profilecatalog.Entry, error) {
	return profilecatalog.Entry{}, errors.New("unexpected create")
}

func (catalog *webRecoveryCatalog) CancelNewProfile(context.Context, string) (bool, error) {
	return false, errors.New("unexpected cancel")
}

func (catalog *webRecoveryCatalog) ActivateForProvider(
	context.Context,
	string,
	string,
) (profilecatalog.Entry, error) {
	return profilecatalog.Entry{}, errors.New("unexpected activation")
}

func (catalog *webRecoveryCatalog) RecoveryPlan(
	context.Context,
	string,
) (profilecatalog.RecoveryPlan, error) {
	return catalog.plan, nil
}

func (catalog *webRecoveryCatalog) Recreate(
	_ context.Context,
	request profilecatalog.RecoveryRequest,
) (profilecatalog.RecoveryResult, error) {
	catalog.recreates++
	return profilecatalog.RecoveryResult{BackupPath: request.Plan.BackupPath}, nil
}

type blockingRecoveryInvalidator struct {
	started chan struct{}
	release chan struct{}
}

func (invalidator *blockingRecoveryInvalidator) InvalidateDuring(
	_ string,
	recoverProfile func() error,
) error {
	close(invalidator.started)
	<-invalidator.release
	return recoverProfile()
}

type webRecoveryEvictor struct{}

func (webRecoveryEvictor) Evict(context.Context, string) error { return nil }
