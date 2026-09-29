package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
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

func TestProfileAPIRouteRejectsUnknownProfileWithoutLeakingResolverError(t *testing.T) {
	t.Parallel()
	server, err := New(Config{
		Resolver: &testProfileResolver{services: map[string]*app.Service{}},
		BasePath: "/", Version: "test",
	})
	require.NoError(t, err)
	path, err := ProfileAPIPath("/", testProfileID, "health")
	require.NoError(t, err)
	response := requestServer(t, server, http.MethodGet, path, nil)
	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.NotContains(t, response.Body.String(), "resolver-private-detail")
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
}

func (resolver *testProfileResolver) Acquire(_ context.Context, profileID string) (ProfileLease, error) {
	resolver.mutex.Lock()
	defer resolver.mutex.Unlock()
	service := resolver.services[profileID]
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
