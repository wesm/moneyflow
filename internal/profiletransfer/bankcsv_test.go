package profiletransfer

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/importer/bankcsv"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestCSVTransferPreservesReimportIdentity(t *testing.T) {
	root := t.TempDir()
	profile, err := sqlite.Open(t.Context(), home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	mapping, err := bankcsv.Lookup("chase_credit")
	require.NoError(t, err)
	file, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount,Category\n09/01/2026,Example Shop,-12.34,Food\n"), mapping, "Card", "one.csv", bankcsv.ProductionLimits)
	require.NoError(t, err)
	_, err = app.ImportBankCSVProfile(t.Context(), profile, file, mapping.Name, false)
	require.NoError(t, err)
	initial, err := profile.Load(t.Context())
	require.NoError(t, err)
	_, err = profile.Append(t.Context(), initial.Revision, domain.Operation{ID: "operation-csv-category", Type: domain.OperationCategoryAssign, PayloadVersion: 1, CreatedRevision: initial.Revision, CreatedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), Targets: []domain.EntityID{initial.Committed.Transactions[0].ID}, Reassign: &domain.ReassignPayload{DestinationID: domain.UncategorizedCategoryID}})
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision(), State: app.DefaultViewState(), Selection: app.EmptySelection(), Window: app.WindowRequest{Limit: 20}})
	require.NoError(t, err)
	state, err := profile.LoadProfileTransfer(t.Context())
	require.NoError(t, err)
	document := Document{Header: Header{Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: "test", ProviderKind: "csv", ExportedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), SourceRevision: state.Snapshot.Revision, SourceName: "Example CSV"}, State: state}
	var data bytes.Buffer
	require.NoError(t, Encode(&data, document))
	require.Contains(t, data.String(), `"type":"csv_row"`)
	require.Contains(t, data.String(), `"amount":"-12.34"`)
	decoded, err := Decode(bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	require.Equal(t, state.CSV, decoded.State.CSV)
	root2 := t.TempDir()
	restored, err := sqlite.Open(t.Context(), home.Paths{Root: root2, Database: filepath.Join(root2, "moneyflow.db")}, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restored.Close()) })
	require.NoError(t, restored.InstallProfileTransfer(t.Context(), decoded.State))
	result, err := app.ImportBankCSVProfile(t.Context(), restored, file, mapping.Name, true)
	require.NoError(t, err)
	require.Zero(t, result.Inserted)
	require.Equal(t, 1, result.Duplicates)
	loaded, err := restored.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, state.Snapshot.Committed.Transactions, loaded.Committed.Transactions)
	require.Equal(t, domain.UncategorizedCategoryID, loaded.Committed.Transactions[0].CategoryID)
	_, err = Decode(strings.NewReader(strings.Replace(data.String(), `"amount_minor":-1234`, `"amount_minor":-1235`, 1)))
	require.Error(t, err)
	for name, mutate := range map[string]func(*store.ProfileTransfer){
		"source identity":           func(s *store.ProfileTransfer) { s.CSV.Rows[0].SourceKey = strings.Repeat("0", 64) },
		"claimed supersession":      func(s *store.ProfileTransfer) { s.CSV.Rows[0].Disposition = "superseded" },
		"account mismatch":          func(s *store.ProfileTransfer) { s.CSV.Files[0].AccountKey = "other card" },
		"missing settings":          func(s *store.ProfileTransfer) { s.CSV.Settings = nil },
		"missing membership target": func(s *store.ProfileTransfer) { s.CSV.Membership[0].SourceKey = strings.Repeat("0", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			fresh, err := Decode(bytes.NewReader(data.Bytes()))
			require.NoError(t, err)
			mutate(&fresh.State)
			require.Error(t, Encode(&bytes.Buffer{}, fresh))
		})
	}
	lost := document
	lost.State.CSV = store.CSVState{}
	lost.Header.ProviderKind = "local"
	require.Error(t, Encode(&bytes.Buffer{}, lost), "CSV transaction identities cannot silently lose their source ledger")
}

func TestCSVExtensionAcceptsOriginalFooter(t *testing.T) {
	input := `{"type":"header","data":{"format":"moneyflow-profile","format_version":1,"source_build":"old-go","provider_kind":"local","exported_at":"2026-09-21T00:00:00Z","source_revision":0,"source_name":"Empty"}}
{"type":"end","data":{"counts":{"account":0,"merchant":0,"group":0,"category":0,"transaction":0,"external_identity":0,"provider_binding":0,"label_allocation":0,"provider_lineage":0,"write_restriction":0,"ynab_split":0,"amazon_settings":0,"amazon_item":0,"known_drill":0}}}
`
	_, err := Decode(strings.NewReader(input))
	require.NoError(t, err)
	_, err = Decode(strings.NewReader(strings.Replace(input, `"account":0`, `"account":0,"csv_row":0`, 1)))
	require.Error(t, err)
}

func TestCSVTransferRequiresUnclaimedDeletionRecord(t *testing.T) {
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	mapping, err := bankcsv.Lookup("chase_credit")
	require.NoError(t, err)
	file, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount\n09/01/2026,Example Shop,-12.34\n"), mapping, "Card", "one.csv", bankcsv.ProductionLimits)
	require.NoError(t, err)
	_, err = app.ImportBankCSVProfile(t.Context(), profile, file, mapping.Name, false)
	require.NoError(t, err)
	snapshot, err := profile.Load(t.Context())
	require.NoError(t, err)
	_, err = profile.Append(t.Context(), snapshot.Revision, domain.Operation{ID: "operation-csv-delete", Type: domain.OperationTransactionDelete, PayloadVersion: 1, CreatedRevision: snapshot.Revision, CreatedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), Targets: []domain.EntityID{snapshot.Committed.Transactions[0].ID}, TransactionDelete: &domain.TransactionDeletePayload{}})
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), profile)
	require.NoError(t, err)
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision(), State: app.DefaultViewState(), Selection: app.EmptySelection(), Window: app.WindowRequest{Limit: 20}})
	require.NoError(t, err)
	empty, err := bankcsv.Parse(t.Context(), strings.NewReader("Transaction Date,Description,Amount\n"), mapping, "Card", "one.csv", bankcsv.ProductionLimits)
	require.NoError(t, err)
	_, err = service.ImportBankCSV(t.Context(), empty, mapping.Name, false)
	require.NoError(t, err)
	state, err := profile.LoadProfileTransfer(t.Context())
	require.NoError(t, err)
	require.Empty(t, state.CSV.Membership)
	require.Len(t, state.CSV.Rows, 1)
	require.Equal(t, "deleted", state.CSV.Rows[0].Disposition)
	document := Document{Header: Header{Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: "test", ProviderKind: "csv", ExportedAt: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), SourceRevision: state.Snapshot.Revision, SourceName: "Example CSV"}, State: state}
	var data bytes.Buffer
	require.NoError(t, Encode(&data, document))
	decoded, err := Decode(bytes.NewReader(data.Bytes()))
	require.NoError(t, err)
	paths, err = home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	restored, err := sqlite.Open(t.Context(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restored.Close()) })
	require.NoError(t, restored.InstallProfileTransfer(t.Context(), decoded.State))
	_, err = app.ImportBankCSVProfile(t.Context(), restored, file, mapping.Name, false)
	require.NoError(t, err)
	snapshot, err = restored.Load(t.Context())
	require.NoError(t, err)
	require.Empty(t, snapshot.Committed.Transactions, "a transferred deletion must survive reimport")

	var incomplete strings.Builder
	for line := range strings.SplitSeq(data.String(), "\n") {
		if line != "" && !strings.HasPrefix(line, `{"type":"csv_row",`) {
			incomplete.WriteString(strings.Replace(line, `"csv_row":1,`, "", 1) + "\n")
		}
	}
	_, err = Decode(strings.NewReader(incomplete.String()))
	require.Error(t, err, "a CSV tombstone must not lose its source deletion record")
}
