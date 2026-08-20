package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/amazonimport"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

func TestAmazonImportStartRequiresProfileScopedMutationSecurity(t *testing.T) {
	t.Parallel()
	coordinator := &apiAmazonImportFake{}
	server := newAmazonImportAPIServer(t, coordinator)
	path, err := ProfileAPIPath("/", testProfileID, "amazon-import/start")
	require.NoError(t, err)
	body := AmazonImportStartBody{Version: AmazonImportWireVersion, Currency: "USD", Scale: 2}

	unprotected := requestJSON(t, server, path, body)
	assert.Equal(t, http.StatusForbidden, unprotected.Code)
	assert.Zero(t, coordinator.starts)

	wrongScope := requestScopedMutation(t, server, otherProfileID, path, body)
	assert.Equal(t, http.StatusForbidden, wrongScope.Code)
	assert.Zero(t, coordinator.starts)

	protected := requestScopedMutation(t, server, testProfileID, path, body)
	require.Equal(t, http.StatusOK, protected.Code, protected.Body.String())
	assert.Equal(t, 1, coordinator.starts)
	assert.Equal(t, amazon.Settings{Currency: "USD", Scale: 2}, coordinator.start.Settings)
}

func TestAmazonImportStatusIsCoordinateBlind(t *testing.T) {
	t.Parallel()
	coordinator := &apiAmazonImportFake{snapshot: amazonimport.Snapshot{
		AttemptID: "attempt_example", ProfileID: testProfileID, State: amazonimport.StateFailed,
		StateVersion: 4, Failure: amazonimport.Failure{Code: amazonimport.CodeImportInvalid},
	}}
	server := newAmazonImportAPIServer(t, coordinator)
	path, err := ProfileAPIPath("/", testProfileID, "amazon-import/attempt_example/status")
	require.NoError(t, err)
	response := requestServer(t, server, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.NotContains(t, response.Body.String(), "relative_filename")
	assert.NotContains(t, response.Body.String(), "record")
	assert.Contains(t, response.Body.String(), string(amazonimport.CodeImportInvalid))
}

func TestAmazonImportOpenAPIIncludesAttemptLifecycle(t *testing.T) {
	t.Parallel()
	server := newAmazonImportAPIServer(t, &apiAmazonImportFake{})
	paths := server.api.OpenAPI().Paths
	base := "/api/v1/profiles/{profile_id}/amazon-import"
	require.NotNil(t, paths[base+"/start"].Post)
	require.NotNil(t, paths[base+"/{attempt_id}/files"].Post)
	require.NotNil(t, paths[base+"/{attempt_id}/execute"].Post)
	require.NotNil(t, paths[base+"/{attempt_id}/status"].Get)
	require.NotNil(t, paths[base+"/{attempt_id}/cancel"].Post)
}

func TestAmazonImportUploadStreamsMultipartFilesThroughProtectedBoundary(t *testing.T) {
	t.Parallel()
	coordinator := &apiAmazonImportFake{}
	server := newAmazonImportAPIServer(t, coordinator)
	path, err := ProfileAPIPath("/", testProfileID, "amazon-import/attempt_example/files")
	require.NoError(t, err)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("version", AmazonImportWireVersion))
	require.NoError(t, writer.WriteField("expected_state_version", "1"))
	file, err := writer.CreateFormFile("files", "Retail.OrderHistory.csv")
	require.NoError(t, err)
	payload := bytes.Repeat([]byte("x"), MaxViewBodyBytes+1)
	_, err = file.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	issued, err := server.security.Issue(testProfileID)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(MutationTokenHeader, issued.Value)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "Retail.OrderHistory.csv", coordinator.stagedName)
	assert.Len(t, coordinator.stagedBody, len(payload))
}

func newAmazonImportAPIServer(t testing.TB, coordinator AmazonImportCoordinator) *Server {
	t.Helper()
	service, err := app.NewService(nil)
	require.NoError(t, err)
	server, err := New(Config{
		Resolver: resolverForService(testProfileID, service), AmazonImports: coordinator,
		BasePath: "/", Version: "test",
	})
	require.NoError(t, err)
	return server
}

type apiAmazonImportFake struct {
	starts     int
	start      amazonimport.StartRequest
	snapshot   amazonimport.Snapshot
	stagedName string
	stagedBody string
}

func (fake *apiAmazonImportFake) Start(_ context.Context, request amazonimport.StartRequest) (amazonimport.Snapshot, error) {
	fake.starts++
	fake.start = request
	if fake.snapshot.AttemptID != "" {
		return fake.snapshot, nil
	}
	return amazonimport.Snapshot{
		AttemptID: "attempt_example", ProfileID: request.ProfileID,
		State: amazonimport.StateSourceRequired, StateVersion: 1,
	}, nil
}

func (fake *apiAmazonImportFake) Stage(_ context.Context, request amazonimport.StageRequest) (amazonimport.Snapshot, error) {
	if len(request.Files) > 0 {
		fake.stagedName = request.Files[0].RelativeName
		data, _ := io.ReadAll(request.Files[0].Reader)
		fake.stagedBody = string(data)
	}
	return amazonimport.Snapshot{
		AttemptID: request.AttemptID, ProfileID: request.ProfileID,
		State: amazonimport.StateSourceRequired, StateVersion: request.ExpectedStateVersion + 1,
	}, nil
}

func (fake *apiAmazonImportFake) Execute(context.Context, amazonimport.ExecuteRequest) (amazonimport.Snapshot, error) {
	return fake.snapshot, nil
}

func (fake *apiAmazonImportFake) Status(context.Context, amazonimport.StatusRequest) (amazonimport.Snapshot, error) {
	return fake.snapshot, nil
}

func (fake *apiAmazonImportFake) Cancel(context.Context, amazonimport.CancelRequest) (amazonimport.Snapshot, error) {
	return fake.snapshot, nil
}
