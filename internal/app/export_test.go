package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

func TestPreviewExportUsesCommittedRowsAndReportsJournalState(t *testing.T) {
	t.Parallel()

	profile := exportMemoryProfile(t)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := detailViewState()
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionCategory, Key: "category_a",
		Currency: "USD", Scale: 2,
	}}

	preview, err := service.PreviewExport(context.Background(), state)
	require.NoError(t, err)
	assert.Equal(t, uint64(7), preview.Revision)
	assert.Equal(t, 2, preview.FullCount)
	assert.Equal(t, 1, preview.FilteredCount)
	assert.Equal(t, 1, preview.ActiveOperations)
	assert.Equal(t, 1, preview.InactiveOperations)
	assert.True(t, preview.CommitAvailable)

	profile.mu.Lock()
	assert.GreaterOrEqual(t, profile.revisionCalls, 1)
	profile.mu.Unlock()
}

func TestCaptureExportReturnsDetachedCommittedRowsAndNamedMetadata(t *testing.T) {
	t.Parallel()

	profile := exportMemoryProfile(t)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	exportedAt := time.Date(2026, time.August, 19, 17, 4, 5, 123_000_000, time.UTC)

	document, err := service.CaptureExport(context.Background(), app.ExportRequest{
		Scope: app.ExportScopeFull, State: detailViewState(), ExportedAt: exportedAt,
		AppVersion: "v2-test",
	})
	require.NoError(t, err)
	require.Len(t, document.Rows, 2)
	assert.Equal(t, "transaction_b", document.Rows[0].TransactionID)
	assert.Equal(t, "transaction_a", document.Rows[1].TransactionID)
	assert.Equal(t, "Merchant A", document.Rows[1].Merchant)
	assert.Equal(t, int64(-1234), document.Rows[1].AmountMinor)
	assert.Equal(t, "-12.34", document.Rows[1].Amount)
	assert.Equal(t, `{"source":"fixture","tag":"alpha"}`, document.Rows[1].TransactionMetadataJSON)
	assert.Equal(t, app.ExportDocumentSchemaVersion, document.Metadata.SchemaVersion)
	assert.Equal(t, "v2-test", document.Metadata.AppVersion)
	assert.Equal(t, exportedAt, document.Metadata.ExportedAt)
	assert.Equal(t, uint64(7), document.Metadata.ProfileRevision)
	assert.Equal(t, 1, document.Metadata.JournalCursor)
	assert.Equal(t, 1, document.Metadata.ExcludedActiveOperations)
	assert.Equal(t, 1, document.Metadata.InactiveRedoOperations)
	assert.Equal(t, app.ExportScopeFull, document.Metadata.Scope)
	assert.Empty(t, document.Metadata.CanonicalQuery)
	assert.Equal(t, 2, document.Metadata.TransactionCount)
	require.NotNil(t, document.Metadata.EarliestDate)
	require.NotNil(t, document.Metadata.LatestDate)
	assert.Equal(t, "2026-08-14", document.Metadata.EarliestDate.String())
	assert.Equal(t, "2026-08-15", document.Metadata.LatestDate.String())
	assert.Equal(t, []string{"fixture", "secondary"}, document.Metadata.ProviderKinds)

	document.Rows[1].Merchant = "Changed"
	document.Metadata.ProviderKinds[0] = "changed"
	again, err := service.CaptureExport(context.Background(), app.ExportRequest{
		Scope: app.ExportScopeFull, State: detailViewState(), ExportedAt: exportedAt,
		AppVersion: "v2-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "Merchant A", again.Rows[1].Merchant)
	assert.Equal(t, []string{"fixture", "secondary"}, again.Metadata.ProviderKinds)
}

func TestCaptureExportFiltersCommittedStateAndRejectsEmptyScope(t *testing.T) {
	t.Parallel()

	profile := exportMemoryProfile(t)
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	state := detailViewState()
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionCategory, Key: "category_a", Currency: "USD", Scale: 2,
	}}
	exportedAt := time.Date(2026, time.August, 19, 17, 4, 5, 0, time.UTC)

	document, err := service.CaptureExport(context.Background(), app.ExportRequest{
		Scope: app.ExportScopeFiltered, State: state, CanonicalQuery: "v=1&drill=category_a",
		ExportedAt: exportedAt, AppVersion: "v2-test",
	})
	require.NoError(t, err)
	require.Len(t, document.Rows, 1)
	assert.Equal(t, "transaction_a", document.Rows[0].TransactionID)
	assert.Equal(t, "Merchant A", document.Rows[0].Merchant)
	assert.Equal(t, "v=1&drill=category_a", document.Metadata.CanonicalQuery)

	start, err := domain.ParseDate("2025-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2025-01-31")
	require.NoError(t, err)
	state.Current.Drilldowns = nil
	state.Current.DateRange = &domain.DateRange{Start: start, End: end}
	_, err = service.CaptureExport(context.Background(), app.ExportRequest{
		Scope: app.ExportScopeFiltered, State: state, CanonicalQuery: "v=1&from=2025-01-01&to=2025-01-31",
		ExportedAt: exportedAt, AppVersion: "v2-test",
	})
	assertAppCode(t, err, app.AppExportEmpty)
}

func TestCaptureExportValidatesRequest(t *testing.T) {
	t.Parallel()

	service, err := app.NewProfileService(context.Background(), exportMemoryProfile(t))
	require.NoError(t, err)
	valid := app.ExportRequest{
		Scope: app.ExportScopeFull, State: detailViewState(),
		ExportedAt: time.Date(2026, time.August, 19, 17, 4, 5, 0, time.UTC),
		AppVersion: "v2-test",
	}

	tests := []struct {
		name   string
		mutate func(*app.ExportRequest)
	}{
		{name: "scope", mutate: func(request *app.ExportRequest) { request.Scope = "unknown" }},
		{name: "version", mutate: func(request *app.ExportRequest) { request.AppVersion = "" }},
		{name: "time zone", mutate: func(request *app.ExportRequest) {
			request.ExportedAt = request.ExportedAt.In(time.FixedZone("offset", 3600))
		}},
		{name: "time precision", mutate: func(request *app.ExportRequest) {
			request.ExportedAt = request.ExportedAt.Add(time.Microsecond)
		}},
		{name: "filtered query", mutate: func(request *app.ExportRequest) {
			request.Scope = app.ExportScopeFiltered
		}},
		{name: "full query", mutate: func(request *app.ExportRequest) {
			request.CanonicalQuery = "v=1"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.mutate(&request)
			_, captureErr := service.CaptureExport(context.Background(), request)
			assertAppCode(t, captureErr, app.AppExportInvalid)
		})
	}
}

func TestCaptureExportRejectsProfileWithNoCommittedTransactions(t *testing.T) {
	t.Parallel()

	profile := exportMemoryProfile(t)
	profile.snapshot.Committed.Transactions = nil
	profile.snapshot.Journal = nil
	profile.snapshot.Cursor = 0
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)

	_, err = service.CaptureExport(context.Background(), app.ExportRequest{
		Scope: app.ExportScopeFull, State: detailViewState(),
		ExportedAt: time.Date(2026, time.August, 19, 17, 4, 5, 0, time.UTC),
		AppVersion: "v2-test",
	})
	assertAppCode(t, err, app.AppExportEmpty)
}

func exportMemoryProfile(t *testing.T) *memoryProfile {
	t.Helper()
	profile := newMemoryProfile(t, 7)
	committed := replayProfile(t)
	date, err := domain.ParseDate("2026-08-15")
	require.NoError(t, err)
	committed.Transactions[0].Amount.Minor = -1234
	committed.Transactions[0].Metadata = map[string]string{"tag": "alpha", "source": "fixture"}
	committed.Transactions[1].Date = date
	committed.Transactions[1].Provider = "secondary"
	committed.Transactions[1].Hidden = false
	require.NoError(t, committed.Validate())
	profile.snapshot.Committed = committed
	profile.snapshot.Journal = []domain.Operation{
		reassignOperation(1, domain.OperationCategoryAssign, "category_b", "transaction_a"),
		hideOperation(2, "transaction_b"),
	}
	profile.snapshot.Cursor = 1
	return profile
}

type exportProviderProfile struct {
	*memoryProfile
	providerState store.ProviderState
}

func (profile *exportProviderProfile) ProviderState(context.Context) (store.ProviderState, error) {
	return profile.providerState.Clone(), nil
}
