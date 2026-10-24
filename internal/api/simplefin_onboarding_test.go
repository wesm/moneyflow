package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestSimpleFINOnboardingAPILifecycle(t *testing.T) {
	var claims atomic.Int32
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "user" || password != "synthetic-secret" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"errlist":[],"errors":[],"connections":[],"accounts":[]}`))
	}))
	t.Cleanup(endpoint.Close)
	access := strings.Replace(endpoint.URL, "https://", "https://user:synthetic-secret@", 1) + "/access"
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	sessions, err := simplefin.NewSessionStore(paths)
	require.NoError(t, err)
	coordinator, err := simplefinonboarding.NewCoordinator(simplefinonboarding.Config{
		InstanceID: "api-test", OpenProfile: func(_ context.Context, id string) (simplefinonboarding.OpenedProfile, error) {
			return simplefinonboarding.OpenedProfile{ID: id, Paths: paths, Service: service, Close: func() error { return nil }}, nil
		}, Runtime: func(home.Paths) (simplefinonboarding.Runtime, error) {
			return simplefinonboarding.Runtime{InstanceID: "api-test", Sessions: sessions,
				Claim: func(context.Context, string) (string, error) { claims.Add(1); return access, nil },
				NewSource: func(_ app.ProviderConnectionState, s simplefin.Session) (provider.ReaderSource, error) {
					return simplefin.NewSource(simplefin.SourceOptions{Currency: s.Import.Currency, Scale: s.Import.Scale, HTTPClient: endpoint.Client()}, sessions)
				},
			}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, coordinator.Shutdown(context.Background())) })
	server, err := New(Config{Resolver: resolverForService(testProfileID, service), SimpleFINOnboarding: coordinator, BasePath: "/", Version: "test"})
	require.NoError(t, err)
	path, err := ProfileAPIPath("/", testProfileID, "simplefin-onboarding/start")
	require.NoError(t, err)
	started := requestScopedMutation(t, server, testProfileID, path, SimpleFINOnboardingStartBody{ProtocolVersion: 1})
	require.Equal(t, 200, started.Code, started.Body.String())
	var snapshot SimpleFINOnboardingStatusResponse
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &snapshot))
	statusPath, err := ProfileAPIPath("/", testProfileID, "simplefin-onboarding/"+snapshot.AttemptID)
	require.NoError(t, err)
	// Only a setup job in one of these phases changes the state without a request from the test,
	// so poll until the state arrives or the attempt leaves them.
	working := map[simplefinonboarding.State]bool{
		simplefinonboarding.StateInspect: true, simplefinonboarding.StateClaiming: true,
		simplefinonboarding.StateSavingSession: true, simplefinonboarding.StateImporting: true,
	}
	wait := func(state simplefinonboarding.State) {
		for {
			response := requestServer(t, server, http.MethodGet, statusPath, nil)
			require.Equal(t, 200, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), access)
			require.NotContains(t, response.Body.String(), "synthetic-secret")
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &snapshot))
			if snapshot.State == state || !working[snapshot.State] {
				break
			}
			time.Sleep(time.Millisecond)
		}
		require.Equal(t, state, snapshot.State, "last status: %+v", &snapshot)
	}
	wait(simplefinonboarding.StateCredentialsRequired)
	body := SimpleFINOnboardingSubmitBody{ProtocolVersion: 1, ExpectedStateVersion: snapshot.StateVersion, Action: simplefinonboarding.ActionConnect, Input: "synthetic-token", Settings: SimpleFINOnboardingSettingsResponse{Currency: "USD", Scale: 2}}
	wrongOrigin := requestScopedMutation(t, server, otherProfileID, statusPath+"/submit", body)
	require.Equal(t, 403, wrongOrigin.Code)
	issued, err := server.security.Issue(testProfileID)
	require.NoError(t, err)
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, statusPath+"/submit", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(MutationTokenHeader, issued.Value)
	missingMetadata := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingMetadata, request)
	require.Equal(t, 403, missingMetadata.Code)
	body.Input = strings.Repeat("x", 8193)
	oversized := requestScopedMutation(t, server, testProfileID, statusPath+"/submit", body)
	require.Equal(t, 422, oversized.Code)
	require.Zero(t, claims.Load())
	body.Input = "synthetic-token"
	body.ExpectedStateVersion--
	stale := requestScopedMutation(t, server, testProfileID, statusPath+"/submit", body)
	require.Equal(t, 409, stale.Code)
	body.ExpectedStateVersion++
	submitted := requestScopedMutation(t, server, testProfileID, statusPath+"/submit", body)
	require.Equal(t, 200, submitted.Code, submitted.Body.String())
	wait(simplefinonboarding.StateComplete)
	require.Equal(t, int32(1), claims.Load())
	otherPath, err := ProfileAPIPath("/", otherProfileID, "simplefin-onboarding/"+snapshot.AttemptID)
	require.NoError(t, err)
	other := requestServer(t, server, http.MethodGet, otherPath, nil)
	require.Equal(t, 404, other.Code)
	// Completion releases the handle once; subsequent status reads remain available.
	wait(simplefinonboarding.StateComplete)
	// A new browser attempt uses the saved session, observes the manual floor,
	// and can be canceled without deleting the connection.
	started = requestScopedMutation(t, server, testProfileID, path, SimpleFINOnboardingStartBody{ProtocolVersion: 1})
	require.Equal(t, 200, started.Code, started.Body.String())
	require.NoError(t, json.Unmarshal(started.Body.Bytes(), &snapshot))
	statusPath, err = ProfileAPIPath("/", testProfileID, "simplefin-onboarding/"+snapshot.AttemptID)
	require.NoError(t, err)
	wait(simplefinonboarding.StateFailed)
	require.NotEmpty(t, snapshot.NextEligible)
	require.Equal(t, int32(1), claims.Load())
	canceled := requestScopedMutation(t, server, testProfileID, statusPath+"/cancel", SimpleFINOnboardingCancelBody{ProtocolVersion: 1, ExpectedStateVersion: snapshot.StateVersion})
	require.Equal(t, 200, canceled.Code, canceled.Body.String())
	wait(simplefinonboarding.StateCanceled)
	_, _, err = sessions.Load()
	require.NoError(t, err)
}

// A claim can consume its SimpleFIN token even when its answer is lost, so cancelling a new
// profile keeps one whose setup began a claim, though setup saved nothing for it.
func TestProfileCancelKeepsAProfileWhoseSimpleFINClaimBegan(t *testing.T) {
	for _, test := range []struct {
		name    string
		claim   bool
		removed bool
	}{
		{name: "claim began", claim: true, removed: false},
		{name: "no claim", claim: false, removed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, catalog := newLostClaimServer(t)
			startPath, err := ProfileAPIPath("/", testProfileID, "simplefin-onboarding/start")
			require.NoError(t, err)
			started := requestScopedMutation(t, server, testProfileID, startPath, SimpleFINOnboardingStartBody{ProtocolVersion: 1})
			require.Equal(t, http.StatusOK, started.Code, started.Body.String())
			var snapshot SimpleFINOnboardingStatusResponse
			require.NoError(t, json.Unmarshal(started.Body.Bytes(), &snapshot))
			statusPath, err := ProfileAPIPath("/", testProfileID, "simplefin-onboarding/"+snapshot.AttemptID)
			require.NoError(t, err)
			snapshot = waitSimpleFINState(t, server, statusPath, simplefinonboarding.StateCredentialsRequired)
			if test.claim {
				submitted := requestScopedMutation(t, server, testProfileID, statusPath+"/submit", SimpleFINOnboardingSubmitBody{
					ProtocolVersion: 1, ExpectedStateVersion: snapshot.StateVersion,
					Action: simplefinonboarding.ActionConnect, Input: "synthetic-token",
					Settings: SimpleFINOnboardingSettingsResponse{Currency: "USD", Scale: 2},
				})
				require.Equal(t, http.StatusOK, submitted.Code, submitted.Body.String())
				waitSimpleFINState(t, server, statusPath, simplefinonboarding.StateFailed)
			}

			cancelPath, err := ProfileAPIPath("/", testProfileID, "cancel")
			require.NoError(t, err)
			response := requestScopedMutation(t, server, CatalogMutationScope, cancelPath, ProfileCancelBody{Version: ProfileCatalogSchemaVersion})
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var body ProfileCancelResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, test.removed, body.Removed)
			require.Equal(t, test.removed, catalog.canceledID == testProfileID)
		})
	}
}

// newLostClaimServer serves the API with a SimpleFIN coordinator whose every claim fails, as when
// the claim's answer is lost, and a catalog that records the profile it is asked to remove.
func newLostClaimServer(t *testing.T) (*Server, *apiCatalogFake) {
	t.Helper()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	sessions, err := simplefin.NewSessionStore(paths)
	require.NoError(t, err)
	coordinator, err := simplefinonboarding.NewCoordinator(simplefinonboarding.Config{
		InstanceID: "api-test", OpenProfile: func(_ context.Context, id string) (simplefinonboarding.OpenedProfile, error) {
			return simplefinonboarding.OpenedProfile{ID: id, Paths: paths, Service: service, Close: func() error { return nil }}, nil
		}, Runtime: func(home.Paths) (simplefinonboarding.Runtime, error) {
			return simplefinonboarding.Runtime{InstanceID: "api-test", Sessions: sessions,
				Claim: func(context.Context, string) (string, error) { return "", errors.New("connection reset") },
				NewSource: func(app.ProviderConnectionState, simplefin.Session) (provider.ReaderSource, error) {
					return nil, errors.New("no source")
				},
			}, nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, coordinator.Shutdown(context.Background())) })
	catalog := &apiCatalogFake{cancelRemoved: true}
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), Catalog: catalog, Evictor: &apiEvictorFake{},
		SimpleFINOnboarding: coordinator, BasePath: "/", Version: "test",
	})
	require.NoError(t, err)
	return server, catalog
}

// waitSimpleFINState polls the attempt at statusPath until it reaches state. Only a setup job
// changes the state without a request, so a state outside the job's phases is final.
func waitSimpleFINState(t *testing.T, server *Server, statusPath string, state simplefinonboarding.State) SimpleFINOnboardingStatusResponse {
	t.Helper()
	working := map[simplefinonboarding.State]bool{
		simplefinonboarding.StateInspect: true, simplefinonboarding.StateClaiming: true,
		simplefinonboarding.StateSavingSession: true, simplefinonboarding.StateImporting: true,
	}
	for {
		response := requestServer(t, server, http.MethodGet, statusPath, nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var snapshot SimpleFINOnboardingStatusResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &snapshot))
		if snapshot.State == state || !working[snapshot.State] {
			require.Equal(t, state, snapshot.State, "last status: %+v", &snapshot)
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
}
