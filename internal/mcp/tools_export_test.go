package mcp

import (
	"context"
	"encoding/csv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
)

func TestMCPExportCapturesCommittedRowsAtExecutionRevision(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	client, root := connectExportTestServer(t, service)
	preview := callWriteTool(t, client, "preview_export", nil)
	require.False(t, preview.IsError, "%v", preview.StructuredContent)
	assert.Equal(t, "1", preview.StructuredContent.(map[string]any)["revision"])
	assert.Equal(t, float64(3), preview.StructuredContent.(map[string]any)["transaction_count"])
	assert.NoDirExists(t, filepath.Join(root, "exports"))

	selection, err := app.NewExplicitTransactionSelection([]domain.EntityID{"transaction_000"}, 1)
	require.NoError(t, err)
	_, err = service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: 1, State: mutationDetailState(),
		Selection: selection, OmitProjection: true,
	})
	require.NoError(t, err)
	result := callWriteTool(t, client, "export_transactions", map[string]any{"format": "csv"})
	require.False(t, result.IsError, "%v", result.StructuredContent)
	document := result.StructuredContent.(map[string]any)
	assert.Equal(t, "2", document["revision"])
	assert.Equal(t, "full", document["scope"])
	assert.Equal(t, "server", document["location"])
	assert.Equal(t, float64(3), document["transaction_count"])
	assert.Equal(t, float64(1), document["excluded_pending_operations"])
	assert.NotContains(t, document, "transactions")
	path := document["path"].(string)
	require.Equal(t, filepath.Join(root, "exports"), filepath.Dir(path))
	contents, err := os.ReadFile(path) // #nosec G304 -- verified export path inside this test's temporary directory.
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(len(contents)), document["size_bytes"])
	assert.Contains(t, string(contents), "# source_revision: 2\n")
	assert.Contains(t, string(contents), "# excluded_pending_operation_count: 1\n")
	reader := csv.NewReader(strings.NewReader(string(contents)))
	reader.Comment = '#'
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4)
	assert.Equal(t, "transaction_000", records[1][slices.Index(records[0], "transaction_id")])
	assert.Equal(t, "-1.00", records[1][slices.Index(records[0], "amount")])
	assert.Equal(t, "-100", records[1][slices.Index(records[0], "amount_minor")])
	assert.Equal(t, uint64(2), service.Revision())
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 3})
	require.NoError(t, err)
	assert.Equal(t, 2, rows.Total)

	second := callWriteTool(t, client, "export_transactions", map[string]any{"format": "csv"})
	require.False(t, second.IsError)
	assert.NotEqual(t, path, second.StructuredContent.(map[string]any)["path"])
	assert.FileExists(t, path)
}

func TestMCPExportFormatsAndReadOnlyRegistration(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	client, root := connectExportTestServer(t, service)
	for _, test := range []struct{ format, wantFormat, prefix string }{
		{"", "parquet", "PAR1"}, {"sqlite", "sqlite", "SQLite format 3"},
	} {
		t.Run(test.wantFormat, func(t *testing.T) {
			result := callWriteTool(t, client, "export_transactions", map[string]any{"format": test.format})
			require.False(t, result.IsError, "%v", result.StructuredContent)
			document := result.StructuredContent.(map[string]any)
			assert.Equal(t, test.wantFormat, document["format"])
			path := document["path"].(string)
			require.Equal(t, filepath.Join(root, "exports"), filepath.Dir(path))
			contents, err := os.ReadFile(path) // #nosec G304 -- verified export path inside this test's temporary directory.
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(string(contents), test.prefix))
		})
	}
	assert.Equal(t, uint64(1), service.Revision())
}

func TestMCPFilteredExportUsesCommittedPredicatesAndCompleteResult(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	selection, err := app.NewExplicitTransactionSelection([]domain.EntityID{"transaction_000"}, service.Revision())
	require.NoError(t, err)
	_, err = service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(), State: mutationDetailState(),
		Selection: selection, OmitProjection: true,
	})
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision(), State: mutationDetailState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	selection, err = app.NewExplicitTransactionSelection([]domain.EntityID{"transaction_000"}, service.Revision())
	require.NoError(t, err)
	_, err = service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionDeleteTransaction, ExpectedRevision: service.Revision(), State: mutationDetailState(),
		Selection: selection, OmitProjection: true,
	})
	require.NoError(t, err)
	client, root := connectExportTestServer(t, service)
	for _, test := range []struct {
		name   string
		filter map[string]any
		ids    []string
	}{
		{"all", map[string]any{}, []string{"transaction_000", "transaction_001", "transaction_002"}},
		{"inclusive dates", map[string]any{"start_date": "2026-08-29", "end_date": "2026-08-29"}, []string{"transaction_000", "transaction_001", "transaction_002"}},
		{"after dates", map[string]any{"start_date": "2026-08-30"}, nil},
		{"before dates", map[string]any{"end_date": "2026-08-28"}, nil},
		{"category id", map[string]any{"category_id": "category_a"}, []string{"transaction_000", "transaction_001"}},
		{"category label", map[string]any{"category_label": "CATEGORY A"}, []string{"transaction_000", "transaction_001"}},
		{"merchant literal", map[string]any{"merchant": "EXAMPLE MER"}, []string{"transaction_000", "transaction_001", "transaction_002"}},
		{"merchant not regex", map[string]any{"merchant": "Example.*"}, nil},
		{"inclusive amounts", map[string]any{"min_amount": "-1.00", "max_amount": "-1.00", "currency": "USD", "scale": 2}, []string{"transaction_000", "transaction_001", "transaction_002"}},
		{"below amounts", map[string]any{"max_amount": "-1.01", "currency": "USD", "scale": 2}, nil},
		{"above amounts", map[string]any{"min_amount": "-0.99", "currency": "USD", "scale": 2}, nil},
		{"money partition", map[string]any{"min_amount": "-1.00", "currency": "EUR", "scale": 2}, nil},
		{"visible category", map[string]any{"include_hidden": false, "category_id": "category_a"}, []string{"transaction_001"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := map[string]any{"scope": "filtered", "filter": test.filter}
			preview := callWriteTool(t, client, "preview_export", args)
			require.False(t, preview.IsError, "%v", preview.StructuredContent)
			assert.Equal(t, "filtered", preview.StructuredContent.(map[string]any)["scope"])
			assert.Equal(t, float64(len(test.ids)), preview.StructuredContent.(map[string]any)["transaction_count"])
			args["format"] = "csv"
			result := callWriteTool(t, client, "export_transactions", args)
			if len(test.ids) == 0 {
				require.True(t, result.IsError)
				assert.Equal(t, "export_empty", result.StructuredContent.(map[string]any)["code"])
				return
			}
			require.False(t, result.IsError, "%v", result.StructuredContent)
			document := result.StructuredContent.(map[string]any)
			assert.Equal(t, float64(len(test.ids)), document["transaction_count"])
			assert.Equal(t, float64(1), document["excluded_pending_operations"])
			path := document["path"].(string)
			require.Equal(t, filepath.Join(root, "exports"), filepath.Dir(path))
			contents, readErr := os.ReadFile(path) // #nosec G304 -- path checked against this test's temporary export directory.
			require.NoError(t, readErr)
			assert.Contains(t, string(contents), "# scope: filtered\n")
			assert.Contains(t, string(contents), "mcp_transactions_v1")
			if test.name == "category id" {
				assert.Contains(t, string(contents), `# canonical_query: {"kind":"mcp_transactions_v1","filter":{"category_id":"category_a","include_hidden":true}}`)
			}
			reader := csv.NewReader(strings.NewReader(string(contents)))
			reader.Comment = '#'
			records, readErr := reader.ReadAll()
			require.NoError(t, readErr)
			var ids []string
			for _, record := range records[1:] {
				ids = append(ids, record[slices.Index(records[0], "transaction_id")])
				assert.Equal(t, "-1.00", record[slices.Index(records[0], "amount")])
			}
			assert.Equal(t, test.ids, ids)
		})
	}
	assert.Equal(t, uint64(4), service.Revision())
}

func TestMCPFilteredExportRejectsInvalidSelectionBeforeWriting(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	client, root := connectExportTestServer(t, service)
	for _, args := range []map[string]any{
		{"scope": "unknown"},
		{"filter": map[string]any{"merchant": "Example"}},
		{"scope": "full", "filter": map[string]any{}},
		{"scope": "filtered"},
		{"scope": "filtered", "filter": map[string]any{"start_date": "invalid"}},
		{"scope": "filtered", "filter": map[string]any{"start_date": "2026-08-30", "end_date": "2026-08-29"}},
		{"scope": "filtered", "filter": map[string]any{"category_id": "category_a", "category_label": "Category A"}},
		{"scope": "filtered", "filter": map[string]any{"category_id": "missing"}},
		{"scope": "filtered", "filter": map[string]any{"min_amount": "-1.00"}},
		{"scope": "filtered", "filter": map[string]any{"min_amount": "1.00", "max_amount": "-1.00", "currency": "USD", "scale": 2}},
	} {
		for _, tool := range []string{"preview_export", "export_transactions"} {
			result := callWriteTool(t, client, tool, args)
			require.True(t, result.IsError, "%s must reject %v", tool, args)
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "exports", "*-export.*"))
	require.NoError(t, err)
	assert.Empty(t, files)
	assert.Equal(t, uint64(1), service.Revision())
}

func TestMCPFilteredExportResolvesCommittedTaxonomy(t *testing.T) {
	service, closeProfile := writeTestService(t, 3)
	defer closeProfile()
	_, err := service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionManageCategories, ExpectedRevision: service.Revision(), State: mutationDetailState(), Selection: app.EmptySelection(),
		Input: app.EditInput{Taxonomy: app.TaxonomyRename, EntityID: "category_a", Label: "Pending Category"}, OmitProjection: true,
	})
	require.NoError(t, err)
	client, _ := connectExportTestServer(t, service)
	for _, tool := range []string{"preview_export", "export_transactions"} {
		committed := callWriteTool(t, client, tool, map[string]any{"scope": "filtered", "filter": map[string]any{"category_label": "Category A"}})
		require.False(t, committed.IsError, "%v", committed.StructuredContent)
		assert.Equal(t, float64(2), committed.StructuredContent.(map[string]any)["transaction_count"])
		pending := callWriteTool(t, client, tool, map[string]any{"scope": "filtered", "filter": map[string]any{"category_label": "Pending Category"}})
		require.True(t, pending.IsError)
	}
	read := callWriteTool(t, client, "get_transactions", map[string]any{"category_label": "Pending Category", "limit": 1})
	require.False(t, read.IsError)
	assert.Equal(t, float64(2), read.StructuredContent.(map[string]any)["total"])
}

func TestMCPFilteredExportIsNotCappedByTransactionWindow(t *testing.T) {
	service, closeProfile := writeTestService(t, 1002)
	defer closeProfile()
	client, root := connectExportTestServer(t, service)
	result := callWriteTool(t, client, "export_transactions", map[string]any{"scope": "filtered", "filter": map[string]any{"category_id": "category_a"}, "format": "csv"})
	require.False(t, result.IsError, "%v", result.StructuredContent)
	document := result.StructuredContent.(map[string]any)
	assert.Equal(t, float64(1001), document["transaction_count"])
	path := document["path"].(string)
	require.Equal(t, filepath.Join(root, "exports"), filepath.Dir(path))
	contents, err := os.ReadFile(path) // #nosec G304 -- path checked against this test's temporary export directory.
	require.NoError(t, err)
	reader := csv.NewReader(strings.NewReader(string(contents)))
	reader.Comment = '#'
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 1002)
	assert.Equal(t, "transaction_999", records[len(records)-1][slices.Index(records[0], "transaction_id")])
}

func TestMCPExportBusyPreviewAndSafeFailure(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	client, root := connectExportTestServer(t, service)
	lock, err := home.TryLock(root, home.LockExport, home.LockExclusive)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Release()) })
	preview := callWriteTool(t, client, "preview_export", nil)
	require.False(t, preview.IsError, "%v", preview.StructuredContent)
	busy := callWriteTool(t, client, "export_transactions", nil)
	require.True(t, busy.IsError)
	assert.Equal(t, "export_busy", busy.StructuredContent.(map[string]any)["code"])
	assert.NoDirExists(t, filepath.Join(root, "exports"))
	require.NoError(t, lock.Release())

	invalid := callWriteTool(t, client, "export_transactions", map[string]any{"format": "unsupported"})
	require.True(t, invalid.IsError)
	assert.Equal(t, "export_invalid", invalid.StructuredContent.(map[string]any)["code"])
	assert.NoDirExists(t, filepath.Join(root, "exports"))
	require.NoError(t, os.WriteFile(filepath.Join(root, "exports"), []byte("existing file"), 0o600))
	failed := callWriteTool(t, client, "export_transactions", nil)
	require.True(t, failed.IsError)
	assert.Equal(t, "export_failed", failed.StructuredContent.(map[string]any)["code"])
	assert.NotContains(t, failed.StructuredContent.(map[string]any)["detail"], root)
	assert.Equal(t, uint64(1), service.Revision())
}

func TestMCPExportEmptyProfile(t *testing.T) {
	service, closeProfile := writeTestService(t, 0)
	defer closeProfile()
	client, root := connectExportTestServer(t, service)
	preview := callWriteTool(t, client, "preview_export", nil)
	require.False(t, preview.IsError, "%v", preview.StructuredContent)
	assert.Equal(t, float64(0), preview.StructuredContent.(map[string]any)["transaction_count"])
	result := callWriteTool(t, client, "export_transactions", nil)
	require.True(t, result.IsError)
	assert.Equal(t, "export_empty", result.StructuredContent.(map[string]any)["code"])
	files, err := filepath.Glob(filepath.Join(root, "exports", "*-export.*"))
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestMCPExportDuringProviderBatchDoesNotRunProviderWork(t *testing.T) {
	service, source, closeProfile := providerTestService(t, 1, "ynab")
	defer closeProfile()
	rows, err := service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 1})
	require.NoError(t, err)
	selection, err := app.NewExplicitTransactionSelection([]domain.EntityID{domain.EntityID(rows.Rows[0].ID)}, rows.Revision)
	require.NoError(t, err)
	_, err = service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionEditCategory, ExpectedRevision: rows.Revision, State: mutationDetailState(), Selection: selection,
		Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: domain.UncategorizedCategoryID}, OmitProjection: true,
	})
	require.NoError(t, err)
	revision := service.Revision()
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: revision, ReviewedRevision: revision, State: mutationDetailState(), Selection: app.EmptySelection()})
	require.NoError(t, err)
	before, err := service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, store.WritePhaseWriting, before.Phase)
	revision = service.Revision()
	source.mu.Lock()
	fetches := source.fetches
	source.mu.Unlock()
	client, _ := connectExportTestServer(t, service)
	preview := callWriteTool(t, client, "preview_export", nil)
	require.False(t, preview.IsError)
	assert.Equal(t, float64(1), preview.StructuredContent.(map[string]any)["excluded_pending_operations"])
	result := callWriteTool(t, client, "export_transactions", nil)
	require.False(t, result.IsError, "%v", result.StructuredContent)
	assert.Equal(t, float64(1), result.StructuredContent.(map[string]any)["excluded_pending_operations"])
	assert.Equal(t, strconv.FormatUint(revision, 10), result.StructuredContent.(map[string]any)["revision"])
	assert.Equal(t, revision, service.Revision())
	after, err := service.ProviderWriteStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before, after)
	source.mu.Lock()
	defer source.mu.Unlock()
	assert.Equal(t, fetches, source.fetches)
	assert.Empty(t, source.updates)
}

func TestMCPExportCancellationLeavesNoPublishedFile(t *testing.T) {
	service, closeProfile := writeTestService(t, 1)
	defer closeProfile()
	root := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dependencies := Dependencies{Service: service, ProfileRoot: root, Clock: time.Now}
	result, err := exportTransactionsDocument(ctx, dependencies, ExportInput{})
	require.NoError(t, err)
	assert.Equal(t, "export_cancelled", result.(ErrorDocument).Code)
	assert.NoDirExists(t, filepath.Join(root, "exports"))
	result, err = exportTransactionsDocument(t.Context(), dependencies, ExportInput{})
	require.NoError(t, err)
	assert.FileExists(t, result.(ExportResultDocument).Path)
}

func connectExportTestServer(t *testing.T, service *app.Service) (*mcpsdk.ClientSession, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	server, err := New(Dependencies{
		Service: service, ProfileID: "profile-a", ProfileRoot: root,
		Clock:  func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) },
		Random: strings.NewReader(strings.Repeat("r", 128)),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, Options{})
	require.NoError(t, err)
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	serverSession, err := server.SDK.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "export-test", Version: "1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, clientSession.Close())
		require.NoError(t, serverSession.Close())
		require.NoError(t, server.Close(context.Background()))
	})
	return clientSession, root
}
