package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProfileCatalogListsLocalStatusAndCreatesWithCatalogAuthority(t *testing.T) {
	t.Parallel()
	catalog := &apiCatalogFake{entries: []profilecatalog.Entry{{
		Key: testProfileID, ID: testProfileID, DisplayName: "Example Profile",
		ProviderKind: "monarch", Status: profilecatalog.StatusReady,
	}}}
	server := newCatalogAPIServer(t, catalog, &apiEvictorFake{})

	response := requestServer(t, server, http.MethodGet, "/api/v1/profiles", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var listed ProfileCatalogResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &listed))
	require.Len(t, listed.Profiles, 1)
	assert.Equal(t, "Example Profile", listed.Profiles[0].DisplayName)
	assert.Equal(t, string(profilecatalog.StatusReady), listed.Profiles[0].Status)
	assert.NotContains(t, response.Body.String(), "profile root")

	created := requestScopedMutation(t, server, CatalogMutationScope, "/api/v1/profiles", ProfileCreateBody{
		Version: ProfileCatalogSchemaVersion, DisplayName: "New Profile", ProviderKind: "monarch",
	})
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	assert.Equal(t, 1, catalog.creates)

	wrongScope := requestScopedMutation(t, server, testProfileID, "/api/v1/profiles", ProfileCreateBody{
		Version: ProfileCatalogSchemaVersion, DisplayName: "Wrong Scope", ProviderKind: "monarch",
	})
	assert.Equal(t, http.StatusForbidden, wrongScope.Code)
}

func TestProfileCatalogActivationUsesCatalogAuthorityAndReturnsCanonicalIdentity(t *testing.T) {
	t.Parallel()
	profileID := "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa"
	catalog := &apiCatalogFake{activate: profilecatalog.Entry{
		Key: profileID, ID: profileID, DisplayName: "Moneyflow",
		ProviderKind: "monarch", Status: profilecatalog.StatusReady,
	}}
	server := newCatalogAPIServer(t, catalog, &apiEvictorFake{})

	response := requestScopedMutation(t, server, CatalogMutationScope, "/api/v1/profiles/activate", ProfileActivateBody{
		Version: ProfileCatalogSchemaVersion, Key: profilecatalog.LegacyKey, ProviderKind: "monarch",
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body ProfileResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, profileID, body.Profile.ID)
	assert.Equal(t, profilecatalog.LegacyKey, catalog.activatedKey)
	assert.Equal(t, "monarch", catalog.activatedProvider)

	wrongScope := requestScopedMutation(t, server, profileID, "/api/v1/profiles/activate", ProfileActivateBody{
		Version: ProfileCatalogSchemaVersion, Key: profilecatalog.LegacyKey,
	})
	assert.Equal(t, http.StatusForbidden, wrongScope.Code)
}

func TestProfileCatalogCancelRemovesOnlyNewProfileWithCatalogAuthority(t *testing.T) {
	t.Parallel()
	catalog := &apiCatalogFake{cancelRemoved: true}
	server := newCatalogAPIServer(t, catalog, &apiEvictorFake{})
	path, err := ProfileAPIPath("/", testProfileID, "cancel")
	require.NoError(t, err)

	response := requestScopedMutation(t, server, CatalogMutationScope, path, struct {
		Version string `json:"version"`
	}{Version: ProfileCatalogSchemaVersion})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, testProfileID, catalog.canceledID)

	wrongScope := requestScopedMutation(t, server, testProfileID, path, struct {
		Version string `json:"version"`
	}{Version: ProfileCatalogSchemaVersion})
	assert.Equal(t, http.StatusForbidden, wrongScope.Code)
}

func TestRecoveryEvictsCachedServiceBeforeRecreate(t *testing.T) {
	t.Parallel()
	calls := make([]string, 0, 2)
	plan := profilecatalog.RecoveryPlan{
		ProfileKey: testProfileID, ProfileID: testProfileID,
		BackupPath: "/synthetic/recovery", StartedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
		OriginalCode: store.CodeSchemaIncompatible,
	}
	catalog := &apiCatalogFake{plan: plan, calls: &calls}
	evictor := &apiEvictorFake{calls: &calls}
	server := newCatalogAPIServer(t, catalog, evictor)
	path, err := ProfileAPIPath("/", testProfileID, "recovery")
	require.NoError(t, err)
	wirePlan := recoveryPlanToWire(plan)

	response := requestScopedMutation(t, server, CatalogMutationScope, path, RecoveryBody{
		Version: ProfileCatalogSchemaVersion, Confirmed: true, Plan: &wirePlan,
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, []string{"evict", "recreate"}, calls)
}

func TestRecoveryPreviewDoesNotEvictOrRecreate(t *testing.T) {
	t.Parallel()
	calls := make([]string, 0, 2)
	plan := profilecatalog.RecoveryPlan{
		ProfileKey: testProfileID, ProfileID: testProfileID,
		BackupPath: "/synthetic/recovery", StartedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
		OriginalCode: store.CodeSchemaIncompatible,
	}
	catalog := &apiCatalogFake{plan: plan, calls: &calls}
	server := newCatalogAPIServer(t, catalog, &apiEvictorFake{calls: &calls})
	path, err := ProfileAPIPath("/", testProfileID, "recovery")
	require.NoError(t, err)
	response := requestScopedMutation(t, server, CatalogMutationScope, path, RecoveryBody{
		Version: ProfileCatalogSchemaVersion,
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var preview RecoveryResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &preview))
	assert.Equal(t, testProfileID, preview.Plan.ProfileID)
	assert.False(t, preview.Recreated)
	assert.Empty(t, calls)
}

func TestRecoveryRejectsEncodedProfileNamespaceBeforeDispatch(t *testing.T) {
	t.Parallel()
	calls := make([]string, 0, 2)
	server := newCatalogAPIServer(
		t,
		&apiCatalogFake{calls: &calls},
		&apiEvictorFake{calls: &calls},
	)
	response := requestServer(
		t,
		server,
		http.MethodPost,
		"/api/v1/%70rofiles/"+testProfileID+"/recovery",
		strings.NewReader(`{"version":"1","confirmed":true}`),
	)
	assert.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
	assert.Empty(t, calls)
}

func newCatalogAPIServer(
	t testing.TB,
	catalog ProfileCatalog,
	evictor ProfileEvictor,
) *Server {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service),
		Catalog:  catalog, Evictor: evictor, BasePath: "/", Version: "test",
	})
	require.NoError(t, err)
	return server
}

func requestScopedMutation(
	t testing.TB,
	server *Server,
	scope string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	issued, err := server.security.Issue(scope)
	require.NoError(t, err)
	return requestSignedJSON(t, server, path, body, issued.Value)
}

type apiCatalogFake struct {
	entries           []profilecatalog.Entry
	activate          profilecatalog.Entry
	activatedKey      string
	activatedProvider string
	plan              profilecatalog.RecoveryPlan
	calls             *[]string
	creates           int
	canceledID        string
	cancelRemoved     bool
}

func (catalog *apiCatalogFake) CancelNewProfile(_ context.Context, profileID string) (bool, error) {
	catalog.canceledID = profileID
	return catalog.cancelRemoved, nil
}

func (catalog *apiCatalogFake) ActivateForProvider(
	_ context.Context,
	key string,
	providerKind string,
) (profilecatalog.Entry, error) {
	catalog.activatedKey = key
	catalog.activatedProvider = providerKind
	return catalog.activate, nil
}

func (catalog *apiCatalogFake) List(context.Context) ([]profilecatalog.Entry, error) {
	return append([]profilecatalog.Entry(nil), catalog.entries...), nil
}

func (catalog *apiCatalogFake) Create(
	_ context.Context,
	request profilecatalog.CreateRequest,
) (profilecatalog.Entry, error) {
	catalog.creates++
	return profilecatalog.Entry{
		Key: testProfileID, ID: testProfileID, DisplayName: request.DisplayName,
		ProviderKind: request.ProviderKind, Status: profilecatalog.StatusSetupIncomplete,
	}, nil
}

func (catalog *apiCatalogFake) RecoveryPlan(context.Context, string) (profilecatalog.RecoveryPlan, error) {
	return catalog.plan, nil
}

func (catalog *apiCatalogFake) Recreate(
	_ context.Context,
	request profilecatalog.RecoveryRequest,
) (profilecatalog.RecoveryResult, error) {
	if catalog.calls != nil {
		*catalog.calls = append(*catalog.calls, "recreate")
	}
	return profilecatalog.RecoveryResult{BackupPath: request.Plan.BackupPath}, nil
}

type apiEvictorFake struct{ calls *[]string }

func (evictor *apiEvictorFake) Evict(context.Context, string) error {
	if evictor.calls != nil {
		*evictor.calls = append(*evictor.calls, "evict")
	}
	return nil
}

func TestProfileCancelAndRecoveryCancelOnboardingBeforeEviction(t *testing.T) {
	t.Parallel()
	plan := profilecatalog.RecoveryPlan{
		ProfileKey: testProfileID, ProfileID: testProfileID,
		BackupPath: "/synthetic/recovery", StartedAt: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC),
		OriginalCode: store.CodeSchemaIncompatible,
	}
	tests := []struct {
		name     string
		endpoint string
		body     any
		calls    []string
	}{
		{
			name: "cancel", endpoint: "cancel",
			body:  ProfileCancelBody{Version: ProfileCatalogSchemaVersion},
			calls: []string{"cancel_onboarding", "evict"},
		},
		{
			name: "recovery", endpoint: "recovery",
			body: RecoveryBody{
				Version: ProfileCatalogSchemaVersion, Confirmed: true,
				Plan: func() *RecoveryPlan { wire := recoveryPlanToWire(plan); return &wire }(),
			},
			calls: []string{"cancel_onboarding", "evict", "recreate"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			calls := make([]string, 0, 3)
			catalog := &apiCatalogFake{plan: plan, calls: &calls, cancelRemoved: true}
			coordinator := &apiOnboardingFake{calls: &calls}
			service, err := app.NewService(nil)
			require.NoError(t, err)
			server, err := New(Config{
				Resolver: resolverForService(testProfileID, service),
				Catalog:  catalog, Evictor: &apiEvictorFake{calls: &calls}, Onboarding: coordinator,
				BasePath: "/", Version: "test",
			})
			require.NoError(t, err)
			path, err := ProfileAPIPath("/", testProfileID, test.endpoint)
			require.NoError(t, err)

			response := requestScopedMutation(t, server, CatalogMutationScope, path, test.body)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Equal(t, test.calls, calls)
		})
	}
}
