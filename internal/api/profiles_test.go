package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/store"
)

func TestProfileAPIRoutesResolveIndependentServices(t *testing.T) {
	t.Parallel()
	first := apiTransaction(t)
	second := apiTransaction(t)
	second.ID = "txn-2"
	second.ProviderID = "provider-txn-2"
	second.Merchant = domain.EntityRef{ID: "merchant-other", Name: "Other Merchant"}
	firstService, err := app.NewService([]domain.Transaction{first})
	require.NoError(t, err)
	secondService, err := app.NewService([]domain.Transaction{second})
	require.NoError(t, err)
	resolver := &testProfileResolver{services: map[string]*app.Service{
		testProfileID:  firstService,
		otherProfileID: secondService,
	}}
	server, err := New(Config{Resolver: resolver, BasePath: "/", Version: "test"})
	require.NoError(t, err)

	firstProjection := requestProfileView(t, server, testProfileID)
	secondProjection := requestProfileView(t, server, otherProfileID)
	require.Len(t, firstProjection.AggregateRows, 1)
	require.Len(t, secondProjection.AggregateRows, 1)
	assert.NotEqual(t, firstProjection.AggregateRows[0].Identity, secondProjection.AggregateRows[0].Identity)
	assert.Equal(t, map[string]int{testProfileID: 1, otherProfileID: 1}, resolver.releases)
}

func TestProfileAPIRouteReportsWhyAProfileCouldNotBeOpened(t *testing.T) {
	t.Parallel()
	_, catalogNotFound := profilecatalog.ResolveEntries(nil, testProfileID)
	require.Error(t, catalogNotFound)
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		detail string
	}{
		{
			name: "unexpected failure", err: errors.New("resolver-private-detail"),
			status: http.StatusInternalServerError, code: "internal_error",
			detail: "The profile could not be opened.",
		},
		{
			name: "missing profile", err: ErrProfileNotFound,
			status: http.StatusNotFound, code: "not_found",
			detail: "The requested profile was not found.",
		},
		{
			name: "profile not in the catalog", err: catalogNotFound,
			status: http.StatusNotFound, code: "not_found",
			detail: "The requested profile was not found.",
		},
		{
			name: "profile closing", err: fmt.Errorf("evicting: %w", ErrProfileUnavailable),
			status: http.StatusServiceUnavailable, code: "profile_busy", detail: "Wait a moment",
		},
		{
			name: "profile locked", err: fmt.Errorf("open profile: %w", home.ErrLockBusy),
			status: http.StatusServiceUnavailable, code: "profile_busy", detail: "Wait a moment",
		},
		{
			name: "preview schema",
			err: fmt.Errorf("open profile: %w", store.NewError(
				store.CodeSchemaIncompatible, errors.New("resolver-private-detail"),
			)),
			status: http.StatusConflict, code: "schema_incompatible", detail: "choose Add profile",
		},
		{
			name: "newer schema",
			err: fmt.Errorf("open profile: %w", store.NewError(
				store.CodeSchemaNewer, errors.New("resolver-private-detail"),
			)),
			status: http.StatusConflict, code: "schema_newer", detail: "newer Moneyflow",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server, err := New(Config{
				Resolver: &testProfileResolver{failure: test.err},
				BasePath: "/", Version: "test",
			})
			require.NoError(t, err)
			path, err := ProfileAPIPath("/", testProfileID, "health")
			require.NoError(t, err)
			response := requestServer(t, server, http.MethodGet, path, nil)
			assert.Equal(t, test.status, response.Code)
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, test.code, problem.Code)
			assert.Contains(t, problem.Detail, test.detail)
			assert.NotContains(t, response.Body.String(), "resolver-private-detail")
		})
	}
}

func requestProfileView(t testing.TB, server *Server, profileID string) Projection {
	t.Helper()
	path, err := ProfileAPIPath("/", profileID, "view")
	require.NoError(t, err)
	response := requestJSON(t, server, path, ViewBody{Query: "v=1", Window: Window{Limit: 200}})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var projection Projection
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &projection))
	return projection
}

type testProfileResolver struct {
	mutex     sync.Mutex
	services  map[string]*app.Service
	roots     map[string]string
	temporary map[string]bool
	releases  map[string]int
	failure   error
}

func (resolver *testProfileResolver) Acquire(_ context.Context, profileID string) (ProfileLease, error) {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	service := resolver.services[profileID]
	if resolver.failure != nil {
		return nil, resolver.failure
	}
	if service == nil {
		return nil, errors.New("resolver-private-detail")
	}
	if resolver.releases == nil {
		resolver.releases = make(map[string]int)
	}
	return &testProfileLease{
		service: service, root: resolver.roots[profileID], temporary: resolver.temporary[profileID],
		release: func() {
			resolver.mutex.Lock()
			defer resolver.mutex.Unlock()
			resolver.releases[profileID]++
		}}, nil
}

type testProfileLease struct {
	service   *app.Service
	root      string
	temporary bool
	release   func()
	once      sync.Once
}

func (lease *testProfileLease) Service() *app.Service { return lease.service }
func (lease *testProfileLease) ProfileRoot() string   { return lease.root }
func (lease *testProfileLease) Temporary() bool       { return lease.temporary }

func (lease *testProfileLease) Release() error {
	lease.once.Do(lease.release)
	return nil
}
