package api

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/access"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestOperationsClassifyEveryRouteWithAbsolutePatterns(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, "/embedded/")
	operations := server.Operations()
	require.NotEmpty(t, operations)
	byPathThenMethod := func(left, right Operation) int {
		return cmp.Or(strings.Compare(left.Path, right.Path), strings.Compare(left.Method, right.Method))
	}
	assert.True(t, slices.IsSortedFunc(operations, byPathThenMethod),
		"Operations must be sorted by path, then method")
	mux := http.NewServeMux()
	classes := map[string]access.Class{}
	for _, operation := range operations {
		assert.True(t, operation.Class.Valid(), operation.ID)
		assert.True(t, strings.HasPrefix(operation.Path, "/embedded/"), operation.Path)
		// ServeMux panics on an invalid or conflicting pattern, which fails the test.
		mux.Handle(operation.Method+" "+operation.Path, http.NotFoundHandler())
		classes[operation.ID] = operation.Class
	}
	assert.Equal(t, access.Read, classes["listProfiles"])
	assert.Equal(t, access.Read, classes["openAPIJSON"])
	assert.Equal(t, access.Stage, classes["mutateProfile"])
	assert.Equal(t, access.Stage, classes["startProfileOnboarding"])
	assert.Equal(t, access.Commit, classes["commitProfile"])
	assert.Equal(t, access.Commit, classes["confirmProviderRefresh"])
}

// A status poll is not a pure read: depending on the flow it renews the attempt's idle lifetime,
// retires an attempt that idled too long, or takes a completed attempt's profile lease. A host
// that admits only Read must therefore refuse it.
func TestOperationsNeverClassifyAnAttemptStatusPollAsRead(t *testing.T) {
	t.Parallel()
	var polls []string
	for _, operation := range newTestServer(t, "/embedded/").Operations() {
		if operation.Method == http.MethodGet && strings.Contains(operation.Path, "{attempt_id}") {
			polls = append(polls, operation.ID)
			assert.Equal(t, access.Stage, operation.Class, operation.ID)
		}
	}
	assert.Subset(t, polls, []string{
		"profileOnboardingStatus", "readSimpleFINOnboardingStatus", "readAmazonImportStatus",
	})
}

func TestOperationsRejectAnUnclassifiedRoute(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, "/embedded/")
	huma.Register(server.api, huma.Operation{
		OperationID: "exampleUnclassified", Method: http.MethodGet, Path: "/embedded/example",
	}, func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	_, err := server.collectOperations(false)
	require.EqualError(t, err, "operation exampleUnclassified has no class")
}

func TestOperationsMuxServesEnabledLegacyAliasesWithTheirAuthority(t *testing.T) {
	for _, basePath := range []string{"/", "/embedded/"} {
		t.Run(basePath, func(t *testing.T) {
			service := newPersistentAPITestService(t, []domain.Transaction{apiTransaction(t)})
			server, err := New(Config{
				Resolver: resolverForService(testProfileID, service), LegacyProfileID: testProfileID,
				BasePath: basePath, Version: "test", HostAdmission: true,
			})
			require.NoError(t, err)
			var authority access.Class
			mux := http.NewServeMux()
			for _, operation := range server.Operations() {
				mux.HandleFunc(operation.Method+" "+operation.Path, func(w http.ResponseWriter, r *http.Request) {
					if operation.Class != authority {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					server.Handler().ServeHTTP(w, r)
				})
			}
			request := func(class access.Class, method, endpoint string, body any) *httptest.ResponseRecorder {
				authority = class
				data, err := json.Marshal(body)
				require.NoError(t, err)
				r := httptest.NewRequest(method, basePath+"api/v1/"+endpoint, bytes.NewReader(data))
				r.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, r)
				return response
			}
			health := request(access.Read, http.MethodGet, "health", nil)
			require.Equal(t, http.StatusOK, health.Code, health.Body.String())
			view := request(access.Read, http.MethodPost, "view", ViewBody{
				Query: "v=1", Window: Window{Limit: 200},
			})
			require.Equal(t, http.StatusOK, view.Code, view.Body.String())
			var initial Projection
			require.NoError(t, json.Unmarshal(view.Body.Bytes(), &initial))
			require.NotEmpty(t, initial.AggregateRows)
			mutation := MutationBody{
				Version: MutationSchemaVersion, ExpectedRevision: initial.Revision,
				Query: initial.CanonicalQuery, Selection: initial.Selection, Action: app.ActionEditMerchant,
				Target: &TransitionTarget{Kind: app.IdentityAggregate, Identity: initial.AggregateRows[0].Identity},
				Input:  MutationInput{Scope: string(app.EditScopeEntity), Label: "Example Shop"},
				Window: Window{Limit: 200},
			}
			denied := request(access.Read, http.MethodPost, "mutations", mutation)
			require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
			mutated := request(access.Stage, http.MethodPost, "mutations", mutation)
			require.Equal(t, http.StatusOK, mutated.Code, mutated.Body.String())
			var staged MutationResponse
			require.NoError(t, json.Unmarshal(mutated.Body.Bytes(), &staged))
			require.Equal(t, 1, staged.Pending.ActiveOperations)
			commit := CommitBody{
				Version: MutationSchemaVersion, ExpectedRevision: staged.Revision,
				ReviewedRevision: staged.Revision, Query: initial.CanonicalQuery,
				Selection: staged.Projection.Selection, Window: Window{Limit: 200},
			}
			denied = request(access.Stage, http.MethodPost, "commit", commit)
			require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
			committed := request(access.Commit, http.MethodPost, "commit", commit)
			require.Equal(t, http.StatusOK, committed.Code, committed.Body.String())
			var saved MutationResponse
			require.NoError(t, json.Unmarshal(committed.Body.Bytes(), &saved))
			assert.Zero(t, saved.Pending.ActiveOperations)
			assert.Contains(t, committed.Body.String(), "Example Shop")
			// A server without LegacyProfileID exposes only the canonical profile route.
			canonical, err := New(Config{
				Resolver: resolverForService(testProfileID, service), BasePath: basePath,
				Version: "test", HostAdmission: true,
			})
			require.NoError(t, err)
			mux = http.NewServeMux()
			for _, operation := range canonical.Operations() {
				mux.Handle(operation.Method+" "+operation.Path, canonical.Handler())
			}
			absent := request(access.Read, http.MethodGet, "health", nil)
			assert.Equal(t, http.StatusNotFound, absent.Code)
			present := request(access.Read, http.MethodGet, "profiles/"+testProfileID+"/health", nil)
			assert.Equal(t, http.StatusOK, present.Code, present.Body.String())
		})
	}
}
