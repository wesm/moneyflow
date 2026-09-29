package mcp

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestCSVMCPReadsAndCommitsCategoryLocally(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	mapping, err := bankcsv.Lookup("chase_credit")
	require.NoError(t, err)
	file, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount,Category\n09/01/2026,Example Shop,-12.34,Food\n"), mapping, "Card", "input.csv", bankcsv.ProductionLimits)
	require.NoError(t, err)
	_, err = app.ImportBankCSVProfile(t.Context(), profile, file, mapping.Name, false)
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	rows := callRefreshTool(t, client, "get_transactions", map[string]any{"limit": 10})
	require.Len(t, rows["transactions"], 1)
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	staged := callWriteTool(t, client, "update_transaction_category", map[string]any{"expected_revision": strconv.FormatUint(service.Revision(), 10), "transaction_id": string(snapshot.Committed.Transactions[0].ID), "category_id": string(domain.UncategorizedCategoryID)})
	require.False(t, staged.IsError)
	revision := strconv.FormatUint(service.Revision(), 10)
	committed := callWriteTool(t, client, "commit_changes", map[string]any{"expected_revision": revision, "reviewed_revision": revision})
	require.False(t, committed.IsError)
	_, err = service.ImportBankCSV(t.Context(), file, mapping.Name, true)
	require.NoError(t, err)
	snapshot, err = profile.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, domain.UncategorizedCategoryID, snapshot.Committed.Transactions[0].CategoryID)
	require.Empty(t, snapshot.Journal)
}
