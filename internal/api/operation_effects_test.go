package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// An embedding host uses Operations to admit requests before invoking Moneyflow.
func requestWithClass(server *Server, class access.Class, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	for _, operation := range server.Operations() {
		mux.HandleFunc(operation.Method+" "+operation.Path, func(w http.ResponseWriter, r *http.Request) {
			if operation.Class != access.Read && operation.Class != class {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			server.Handler().ServeHTTP(w, r)
		})
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestOperationAuthorityControlsProviderSessionReplacement(t *testing.T) {
	for _, kind := range []string{"monarch", "ynab"} {
		for _, route := range []string{"canonical", "legacy"} {
			t.Run(kind+"/"+route, func(t *testing.T) {
				paths, err := home.ResolveRoot(t.TempDir(), nil, "")
				require.NoError(t, err)
				profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, profile.Close()) })
				service, err := app.NewProfileService(t.Context(), profile)
				require.NoError(t, err)
				now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
				source := &apiProviderSource{
					identity: provider.ProfileIdentity{Kind: kind, RemoteID: "profile-example"},
					snapshot: apiProviderSnapshot(t, now, 1), fingerprint: "session-a",
				}
				require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
					ReadSource: source, Provider: kind, Currency: "USD", Scale: 2,
					Renderer: "web", InstanceID: "instance-test", Now: func() time.Time { return now },
				}))
				refresh := app.ProviderRefreshRequest{
					Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
				}
				_, err = service.RefreshProvider(t.Context(), refresh)
				require.NoError(t, err)
				if kind == "monarch" {
					source.mu.Lock()
					source.readerErr = provider.NewError(provider.CodeReconnectRequired)
					source.mu.Unlock()
					_, err = service.RefreshProvider(t.Context(), refresh)
					var failure *app.AppError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, app.AppProviderReconnectRequired, failure.Code)
				}
				source.mu.Lock()
				source.readerErr = nil
				source.fingerprint = "session-b"
				changedBefore := source.changedCalls
				source.mu.Unlock()
				server, err := New(Config{
					Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
					BasePath: "/embedded/", Version: "test", HostAdmission: true,
				})
				require.NoError(t, err)
				path, err := ProfileAPIPath("/embedded/", testProfileID, "provider/status")
				require.NoError(t, err)
				if route == "legacy" {
					path = "/embedded/api/v1/provider/status"
				}
				denied := requestWithClass(server, access.Read, path)
				require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
				source.mu.Lock()
				assert.Equal(t, changedBefore, source.changedCalls, "Read must not inspect session replacement")
				source.mu.Unlock()
				assert.True(t, providerRefreshCapability(service).Available)
				allowed := requestWithClass(server, access.Stage, path)
				require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
				var status ProviderStatusResponse
				require.NoError(t, json.Unmarshal(allowed.Body.Bytes(), &status))
				if kind == "ynab" {
					assert.Equal(t, provider.CodeReconnectRequired, status.Code)
					assert.False(t, providerRefreshCapability(service).Available, "changed vault requires a new unlock")
				} else {
					assert.Empty(t, status.Code, "replaced session clears the reconnect hold")
				}
			})
		}
	}
}

func TestReadOperationMayCreateCatalogLocks(t *testing.T) {
	paths, err := home.ResolveCatalogRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	catalog, err := profilecatalog.New(profilecatalog.Config{
		Paths: paths, Random: rand.Reader, Now: time.Now, Version: "test",
	})
	require.NoError(t, err)
	entry, err := catalog.Create(t.Context(), profilecatalog.CreateRequest{
		DisplayName: "Example Profile", ProviderKind: "local",
	})
	require.NoError(t, err)
	locks := []string{filepath.Join(paths.Root, "catalog.lock"), filepath.Join(entry.Root, "profile.lock")}
	for _, path := range locks {
		require.NoError(t, os.Remove(path))
	}
	server := newCatalogAPIServer(t, catalog, &apiEvictorFake{})
	allowed := requestWithClass(server, access.Read, "/api/v1/profiles")
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	assert.Contains(t, allowed.Body.String(), entry.ID)
	for _, path := range locks {
		require.FileExists(t, path)
	}
}

func TestOperationAuthorityControlsAmazonAttemptLifetimeAndCleanup(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	closes := 0
	coordinator, err := amazonimport.New(amazonimport.Config{
		InstanceID: "test", Now: func() time.Time { return now }, Random: rand.Reader,
		Limits: amazon.ProductionLimits, Discover: amazon.DiscoverDirectory, Parse: amazon.Parse,
		ResolveTarget: func(context.Context, string) (amazonimport.Target, error) {
			return amazonimport.Target{
				ProfileID: testProfileID, Root: root, Close: func() error { closes++; return nil },
			}, nil
		},
	})
	require.NoError(t, err)
	started, err := coordinator.Start(t.Context(), amazonimport.StartRequest{
		ProfileID: testProfileID, Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	_, err = coordinator.Stage(t.Context(), amazonimport.StageRequest{
		ProfileID: testProfileID, AttemptID: started.AttemptID,
		ExpectedStateVersion: started.StateVersion,
		Files: []amazonimport.Upload{{
			RelativeName: "Retail.OrderHistory.1.csv", Reader: strings.NewReader("header\n"),
		}},
	})
	require.NoError(t, err)
	stagedFile := filepath.Join(root, ".amazon-import-stages", started.AttemptID, "000000.csv")
	require.FileExists(t, stagedFile)
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), AmazonImports: coordinator,
		BasePath: "/", Version: "test", HostAdmission: true,
	})
	require.NoError(t, err)
	path, err := ProfileAPIPath("/", testProfileID, "amazon-import/"+started.AttemptID+"/status")
	require.NoError(t, err)
	// Stage authority extends the attempt beyond its original idle deadline.
	now = now.Add(20 * time.Minute)
	allowed := requestWithClass(server, access.Stage, path)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	now = now.Add(20 * time.Minute)
	allowed = requestWithClass(server, access.Stage, path)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	// Read authority neither refreshes the lifetime nor cleans up expired files.
	now = now.Add(20 * time.Minute)
	denied := requestWithClass(server, access.Read, path)
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	now = now.Add(10 * time.Minute)
	denied = requestWithClass(server, access.Read, path)
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	require.FileExists(t, stagedFile)
	assert.Zero(t, closes)
	allowed = requestWithClass(server, access.Stage, path)
	require.Equal(t, http.StatusNotFound, allowed.Code, allowed.Body.String())
	require.NoFileExists(t, stagedFile)
	assert.Equal(t, 1, closes)
	// A new attempt can take ownership after the expired one was forgotten.
	restarted, err := coordinator.Start(t.Context(), amazonimport.StartRequest{
		ProfileID: testProfileID, Settings: amazon.Settings{Currency: "USD", Scale: 2},
	})
	require.NoError(t, err)
	_, err = coordinator.Cancel(t.Context(), amazonimport.CancelRequest{
		ProfileID: testProfileID, AttemptID: restarted.AttemptID,
		ExpectedStateVersion: restarted.StateVersion,
	})
	require.NoError(t, err)
}

type completedSimpleFINCoordinator struct {
	SimpleFINOnboardingCoordinator
	takes, closes int
}

func (*completedSimpleFINCoordinator) Status(
	context.Context, simplefinonboarding.StatusRequest,
) (simplefinonboarding.Snapshot, error) {
	return simplefinonboarding.Snapshot{
		ProfileID: testProfileID, AttemptID: "attempt_example", StateVersion: 1,
		State: simplefinonboarding.StateComplete,
	}, nil
}

func (coordinator *completedSimpleFINCoordinator) TakeOpenedProfile(
	context.Context, simplefinonboarding.StatusRequest,
) (simplefinonboarding.OpenedProfile, error) {
	coordinator.takes++
	return simplefinonboarding.OpenedProfile{
		Close: func() error { coordinator.closes++; return nil },
	}, nil
}

func TestOperationAuthorityControlsCompletedProfileOwnership(t *testing.T) {
	for _, kind := range []string{"monarch", "simplefin"} {
		t.Run(kind, func(t *testing.T) {
			monarch := &apiOnboardingFake{snapshot: onboarding.Snapshot{
				ProtocolVersion: onboarding.ProtocolVersion, AttemptID: "attempt_example",
				ProfileID: testProfileID, StateVersion: 1,
				State: onboarding.StateComplete, ProviderKind: "monarch",
			}}
			simplefin := &completedSimpleFINCoordinator{}
			service, err := app.NewService(nil)
			require.NoError(t, err)
			server, err := New(Config{
				Resolver: resolverForService(testProfileID, service), Onboarding: monarch,
				SimpleFINOnboarding: simplefin, BasePath: "/", Version: "test", HostAdmission: true,
			})
			require.NoError(t, err)
			endpoint := "onboarding/attempt_example/status"
			if kind == "simplefin" {
				endpoint = "simplefin-onboarding/attempt_example"
			}
			path, err := ProfileAPIPath("/", testProfileID, endpoint)
			require.NoError(t, err)
			denied := requestWithClass(server, access.Read, path)
			require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
			assert.Zero(t, monarch.takes.Load())
			assert.Zero(t, simplefin.takes)
			for range 2 {
				allowed := requestWithClass(server, access.Stage, path)
				require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
			}
			if kind == "simplefin" {
				assert.Equal(t, 1, simplefin.takes)
				assert.Equal(t, 1, simplefin.closes)
			} else {
				assert.Equal(t, int32(1), monarch.takes.Load())
				assert.Equal(t, int32(1), monarch.closes.Load())
			}
		})
	}
}
