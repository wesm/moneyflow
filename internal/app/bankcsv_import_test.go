package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func csvTestProfile(t *testing.T) (store.Profile, *Service) {
	t.Helper()
	root := t.TempDir()
	profile, err := sqlite.Open(t.Context(), home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	service, err := NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	return profile, service
}

func csvTestFile(t *testing.T, name, rows string) bankcsv.File {
	t.Helper()
	mapping, err := bankcsv.Lookup("chase_credit")
	require.NoError(t, err)
	file, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount,Category,Memo\n"+rows), mapping, "Example Card", name, bankcsv.ProductionLimits)
	require.NoError(t, err)
	return file
}

func TestCSVImportOverlapCorrectionAndRestoration(t *testing.T) {
	profile, service := csvTestProfile(t)
	input := "09/01/2026,Example Shop,-12.34,Food,one\n09/01/2026,Example Shop,-12.34,Food,two\n"
	file := csvTestFile(t, "first.csv", input)
	first, err := service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	assert.Equal(t, 2, first.Inserted)
	assert.Equal(t, "csv", service.ProfileKind())
	original, err := profile.Load(t.Context())
	require.NoError(t, err)
	repeat, err := service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	assert.True(t, repeat.Unchanged)
	assert.Equal(t, first.Revision, repeat.Revision)
	forced, err := service.ImportBankCSV(t.Context(), file, "chase_credit", true)
	require.NoError(t, err)
	assert.Zero(t, forced.Inserted)
	assert.Equal(t, 2, forced.Duplicates)
	overlap := csvTestFile(t, "second.csv", "09/01/2026,Example Shop,-12.34,Travel,other owner\n")
	_, err = service.ImportBankCSV(t.Context(), overlap, "chase_credit", false)
	require.NoError(t, err)
	_, err = service.ImportBankCSV(t.Context(), csvTestFile(t, "first.csv", ""), "chase_credit", false)
	require.NoError(t, err)
	current, err := profile.Load(t.Context())
	require.NoError(t, err)
	require.Len(t, current.Committed.Transactions, 1)
	assert.Equal(t, "one", current.Committed.Transactions[0].Notes)
	restored, err := service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	assert.Equal(t, 1, restored.Restored)
	current, err = profile.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, original.Committed.Transactions, current.Committed.Transactions)
}

func TestCSVSkippedReplacementRetainsClaimsAndRetries(t *testing.T) {
	profile, service := csvTestProfile(t)
	_, err := service.ImportBankCSV(t.Context(), csvTestFile(t, "one.csv", "09/01/2026,Example Shop,-1,Food,\n"), "chase_credit", false)
	require.NoError(t, err)
	bad := csvTestFile(t, "one.csv", "bad date,Example Shop,-1,Food,\n09/02/2026,Example Shop,-2,Food,\n")
	_, err = service.ImportBankCSV(t.Context(), bad, "chase_credit", false)
	require.NoError(t, err)
	again, err := service.ImportBankCSV(t.Context(), bad, "chase_credit", false)
	require.NoError(t, err)
	assert.False(t, again.Unchanged)
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	assert.Len(t, snapshot.Committed.Transactions, 2)
	result, err := service.ImportBankCSV(t.Context(), csvTestFile(t, "one.csv", ""), "chase_credit", false)
	require.NoError(t, err)
	assert.Equal(t, 2, result.Superseded)
}

func csvTransactionIDs(snapshot domain.ProfileSnapshot) []domain.EntityID {
	ids := make([]domain.EntityID, len(snapshot.Committed.Transactions))
	for i, transaction := range snapshot.Committed.Transactions {
		ids[i] = transaction.ID
	}
	return ids
}

func csvCommitOperation(t *testing.T, profile store.Profile, service *Service, operation domain.Operation) {
	t.Helper()
	operation.ID = "operation-csv-test"
	operation.PayloadVersion = 1
	operation.CreatedAt = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	operation.CreatedRevision = service.Revision()
	_, err := profile.Append(t.Context(), service.Revision(), operation)
	require.NoError(t, err)
	_, err = service.Refresh(t.Context())
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision(), State: DefaultViewState(), Selection: EmptySelection(), Window: WindowRequest{Limit: 20}})
	require.NoError(t, err)
}

func TestCSVLocalEditsAndDeletionSurviveCorrections(t *testing.T) {
	profile, service := csvTestProfile(t)
	file := csvTestFile(t, "one.csv", "09/01/2026,Example Shop,-12.34,Food,\n")
	_, err := service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	ids := csvTransactionIDs(snapshot)
	csvCommitOperation(t, profile, service, domain.Operation{Type: domain.OperationCategoryAssign, Targets: ids, Reassign: &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}})
	corrected := csvTestFile(t, "one.csv", "09/01/2026,Example Shop,-12.34,Travel,changed\n")
	_, err = service.ImportBankCSV(t.Context(), corrected, "chase_credit", false)
	require.NoError(t, err)
	snapshot, err = profile.Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, domain.UncategorizedCategoryID, snapshot.Committed.Transactions[0].CategoryID)
	assert.Equal(t, "changed", snapshot.Committed.Transactions[0].Notes)
	result, err := service.ImportBankCSV(t.Context(), csvTestFile(t, "one.csv", ""), "chase_credit", false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Retained)
	csvCommitOperation(t, profile, service, domain.Operation{Type: domain.OperationTransactionDelete, Targets: ids, TransactionDelete: &domain.TransactionDeletePayload{}})
	_, err = service.ImportBankCSV(t.Context(), file, "chase_credit", true)
	require.NoError(t, err)
	snapshot, err = profile.Load(t.Context())
	require.NoError(t, err)
	assert.Empty(t, snapshot.Committed.Transactions)
}

func TestCSVPendingEditProtectsMissingRowsAndUndoDoesNotSetOverride(t *testing.T) {
	profile, service := csvTestProfile(t)
	file := csvTestFile(t, "one.csv", "09/01/2026,Example Shop,-1,Food,\n")
	_, err := service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	operation := domain.Operation{ID: "operation-csv-pending", PayloadVersion: 1, CreatedRevision: service.Revision(), CreatedAt: time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), Type: domain.OperationCategoryAssign, Targets: csvTransactionIDs(snapshot), Reassign: &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}}
	_, err = profile.Append(t.Context(), service.Revision(), operation)
	require.NoError(t, err)
	result, err := service.ImportBankCSV(t.Context(), csvTestFile(t, "one.csv", ""), "chase_credit", false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Retained)
	assert.Equal(t, 1, service.Pending().ActiveOperations)
	_, err = service.Undo(t.Context(), service.Revision())
	require.NoError(t, err)
	_, err = service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	assert.Zero(t, service.Pending().InactiveOperations)
	result, err = service.ImportBankCSV(t.Context(), csvTestFile(t, "one.csv", ""), "chase_credit", false)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Superseded)
	assert.Zero(t, result.Retained)
}

func TestCSVCategoryRenameAppliesToNewRowsAndSourceDeletionDoesNotResurrect(t *testing.T) {
	profile, service := csvTestProfile(t)
	file := csvTestFile(t, "one.csv", "09/01/2026,Example Shop,-1,Food,\n")
	_, err := service.ImportBankCSV(t.Context(), file, "chase_credit", false)
	require.NoError(t, err)
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	id := snapshot.Committed.Transactions[0].CategoryID
	csvCommitOperation(t, profile, service, domain.Operation{Type: domain.OperationCategoryLabel, Targets: []domain.EntityID{id}, Label: &domain.LabelPayload{EntityID: id, Label: "Groceries", CollisionKey: "groceries"}})
	_, err = service.ImportBankCSV(t.Context(), csvTestFile(t, "two.csv", "09/02/2026,Example Shop,-2,Food,\n"), "chase_credit", false)
	require.NoError(t, err)
	snapshot, err = profile.Load(t.Context())
	require.NoError(t, err)
	for _, row := range snapshot.Committed.Transactions {
		assert.Equal(t, id, row.CategoryID)
	}
	csvCommitOperation(t, profile, service, domain.Operation{Type: domain.OperationCategoryDelete, Targets: []domain.EntityID{id}, Delete: &domain.DeletePayload{SourceID: id, ReplacementID: domain.UncategorizedCategoryID}})
	_, err = service.ImportBankCSV(t.Context(), csvTestFile(t, "three.csv", "09/03/2026,Example Shop,-3,Food,\n"), "chase_credit", false)
	require.NoError(t, err)
	snapshot, err = profile.Load(t.Context())
	require.NoError(t, err)
	for _, row := range snapshot.Committed.Transactions {
		assert.Equal(t, domain.UncategorizedCategoryID, row.CategoryID)
	}
}
