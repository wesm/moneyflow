package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/exporter"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestExportPreviewReturnsCurrentCommittedCountsAndProfileMetadata(t *testing.T) {
	server, root, otherRoot, service := exportTestServer(t, true)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	_, err := service.Mutate(context.Background(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: 1, State: state,
		Selection: app.EmptySelection(),
		Target:    &app.RowTarget{Kind: app.IdentityTransaction, Identity: "txn-1"},
		Window:    app.WindowRequest{Limit: 20},
	})
	require.NoError(t, err)
	path, err := ProfileAPIPath("/", testProfileID, "export/preview")
	require.NoError(t, err)
	response := requestJSON(t, server, path, ExportPreviewBody{Version: ExportWireVersion, Query: "hidden=1&v=1"})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body ExportPreviewResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, ExportWireVersion, body.Version)
	assert.Equal(t, "v=1", body.CanonicalQuery)
	assert.Equal(t, 1, body.FullCount)
	assert.Equal(t, 1, body.FilteredCount)
	assert.Equal(t, 1, body.ActiveOperations)
	assert.Zero(t, body.InactiveOperations)
	assert.True(t, body.CommitAvailable)
	assert.True(t, body.TemporaryProfile)
	assert.NoDirExists(t, filepath.Join(root, "exports"))
	assert.NoDirExists(t, filepath.Join(otherRoot, "exports"))
}

func TestExportPreviewReturnsZeroCountsForAnEmptyProfile(t *testing.T) {
	server, _, _, _ := exportTestServerWithTransactions(t, false, nil)
	path, err := ProfileAPIPath("/", testProfileID, "export/preview")
	require.NoError(t, err)
	response := requestJSON(t, server, path, ExportPreviewBody{
		Version: ExportWireVersion, Query: "v=1",
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var body ExportPreviewResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
	assert.Zero(t, body.FullCount)
	assert.Zero(t, body.FilteredCount)
}

func TestExportPreviewRejectsPrivateInvalidInputWithoutEcho(t *testing.T) {
	server, _, _, _ := exportTestServer(t, false)
	path, err := ProfileAPIPath("/", testProfileID, "export/preview")
	require.NoError(t, err)
	privateQuery := "v=99&search=private-merchant-name"
	response := requestJSON(t, server, path, ExportPreviewBody{Version: "wrong", Query: privateQuery})
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.NotContains(t, response.Body.String(), privateQuery)
	assert.NotContains(t, response.Body.String(), "private-merchant-name")
	assert.Contains(t, response.Body.String(), string(CodeExportInvalid))
}

func TestExportDownloadRequiresMutationSecurityAndStreamsCompleteFormats(t *testing.T) {
	formats := []struct {
		format      exporter.Format
		contentType string
		extension   string
	}{
		{exporter.FormatCSV, "text/csv", ".csv"},
		{exporter.FormatSQLite, "application/vnd.sqlite3", ".db"},
		{exporter.FormatParquet, "application/vnd.apache.parquet", ".parquet"},
	}
	for _, test := range formats {
		t.Run(string(test.format), func(t *testing.T) {
			server, root, otherRoot, _ := exportTestServer(t, false)
			path, err := ProfileAPIPath("/", testProfileID, "export")
			require.NoError(t, err)
			body := ExportBody{Version: ExportWireVersion, Format: test.format, Scope: app.ExportScopeFull, Query: "v=1"}

			unauthorized := unprotectedSameOriginExportRequest(t, server, path, body)
			assert.Equal(t, http.StatusForbidden, unauthorized.Code)
			assert.NoDirExists(t, filepath.Join(root, "exports"))

			response := protectedExportRequest(t, server, path, body)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			assert.Contains(t, response.Header().Get("Content-Type"), test.contentType)
			assert.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, int64(response.Body.Len()), response.Result().ContentLength)
			disposition := response.Header().Get("Content-Disposition")
			assert.Contains(t, disposition, `attachment; filename="`)
			assert.Contains(t, disposition, "filename*=UTF-8''")
			assert.Contains(t, disposition, test.extension)
			assert.NotEmpty(t, response.Body.Bytes())
			if test.format == exporter.FormatCSV {
				reader := csv.NewReader(strings.NewReader(response.Body.String()))
				reader.Comment = '#'
				records, readErr := reader.ReadAll()
				require.NoError(t, readErr)
				assert.Len(t, records, 2)
			}
			assert.NoDirExists(t, filepath.Join(otherRoot, "exports"))
			entries, readErr := os.ReadDir(filepath.Join(root, "exports", ".tmp"))
			require.NoError(t, readErr)
			assert.Empty(t, entries)
		})
	}
}

func TestExportDownloadRejectsCrossOriginBeforeCreatingStage(t *testing.T) {
	server, root, _, _ := exportTestServer(t, false)
	path, err := ProfileAPIPath("/", testProfileID, "export")
	require.NoError(t, err)
	body, err := json.Marshal(ExportBody{
		Version: ExportWireVersion, Format: exporter.FormatCSV, Scope: app.ExportScopeFull, Query: "v=1",
	})
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.NoDirExists(t, filepath.Join(root, "exports"))
}

func TestExportDownloadCleansStageWhenTheClientDisconnects(t *testing.T) {
	server, root, _, _ := exportTestServer(t, false)
	path, err := ProfileAPIPath("/", testProfileID, "export")
	require.NoError(t, err)
	body, err := json.Marshal(ExportBody{
		Version: ExportWireVersion, Format: exporter.FormatCSV,
		Scope: app.ExportScopeFull, Query: "v=1",
	})
	require.NoError(t, err)
	issued, err := server.security.Issue(testProfileID)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(MutationTokenHeader, issued.Value)
	response := &disconnectingResponseWriter{header: make(http.Header)}

	server.Handler().ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.status)
	entries, readErr := os.ReadDir(filepath.Join(root, "exports", ".tmp"))
	require.NoError(t, readErr)
	assert.Empty(t, entries)
}

type disconnectingResponseWriter struct {
	header http.Header
	status int
}

func (response *disconnectingResponseWriter) Header() http.Header { return response.header }
func (response *disconnectingResponseWriter) WriteHeader(status int) {
	response.status = status
}
func (*disconnectingResponseWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func protectedExportRequest(
	t testing.TB,
	server *Server,
	path string,
	body ExportBody,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	issued, err := server.security.Issue(testProfileID)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set(MutationTokenHeader, issued.Value)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func unprotectedSameOriginExportRequest(
	t testing.TB,
	server *Server,
	path string,
	body ExportBody,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", server.security.origin.Origin())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func exportTestServer(t testing.TB, temporary bool) (*Server, string, string, *app.Service) {
	t.Helper()
	return exportTestServerWithTransactions(t, temporary, []domain.Transaction{apiTransaction(t)})
}

func exportTestServerWithTransactions(
	t testing.TB,
	temporary bool,
	transactions []domain.Transaction,
) (*Server, string, string, *app.Service) {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	root := paths.Root
	otherRoot := t.TempDir()
	resolver := &testProfileResolver{
		services:  map[string]*app.Service{testProfileID: service},
		roots:     map[string]string{testProfileID: root},
		temporary: map[string]bool{testProfileID: temporary},
	}
	server, err := New(Config{Resolver: resolver, BasePath: "/", Version: "test"})
	require.NoError(t, err)
	return server, root, otherRoot, service
}
