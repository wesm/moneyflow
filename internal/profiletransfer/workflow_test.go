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
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestAmazonTransferReimportPreservesLocalEditsAndMatching(t *testing.T) {
	source := transferStore(t)
	date, err := domain.ParseDate("2026-09-01")
	require.NoError(t, err)
	now := time.Unix(1800000000, 0).UTC()
	request := app.AmazonImportRequest{
		Candidate: amazon.Candidate{Rows: []amazon.Row{{OrderID: "example-order", ASIN: "EXAMPLE", ProductName: "Example Product", OrderDate: date, Quantity: 1, AmountMinor: -1234, Currency: "USD", Scale: 2, OrderStatus: "Closed", ShipmentStatus: "Delivered", IdentityFingerprint: strings.Repeat("a", 64), FullFingerprint: strings.Repeat("b", 64)}}, ObservedOrderIDs: []string{"example-order"}, FileCount: 1, LogicalRecordCount: 1, Digest: strings.Repeat("c", 64)},
		Settings:  amazon.Settings{Currency: "USD", Scale: 2}, ImportedAt: now,
	}
	_, err = app.ImportAmazonProfile(t.Context(), source, request)
	require.NoError(t, err)
	service, err := app.NewProfileService(t.Context(), source)
	require.NoError(t, err)
	snapshot, err := source.Load(t.Context())
	require.NoError(t, err)
	target := &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(snapshot.Committed.Transactions[0].ID)}
	session := app.NewSession()
	session.ShowAllDetail()
	state := session.ViewState()
	for _, edit := range []struct {
		action app.ActionID
		input  app.EditInput
	}{
		{app.ActionToggleHidden, app.EditInput{}},
		{app.ActionEditMerchant, app.EditInput{Scope: app.EditScopeEntity, Label: "Local Product"}},
	} {
		_, err = service.Mutate(t.Context(), app.MutationRequest{Action: edit.action, ExpectedRevision: service.Revision(), State: state, Selection: app.EmptySelection(), Target: target, Input: edit.input})
		require.NoError(t, err)
	}
	_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: service.Revision(), ReviewedRevision: service.Revision()})
	require.NoError(t, err)
	before, err := source.Load(t.Context())
	require.NoError(t, err)
	imported := transferRoundTrip(t, source, "amazon")
	baseline, err := app.ImportAmazonProfile(t.Context(), source, request)
	require.NoError(t, err)
	matchBefore, err := source.LoadAmazonMatchSource(t.Context())
	require.NoError(t, err)
	service, err = app.NewProfileService(t.Context(), imported)
	require.NoError(t, err)
	result, err := service.ImportAmazon(t.Context(), request)
	require.NoError(t, err)
	after, err := imported.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, before.Committed, after.Committed)
	matchAfter, err := imported.LoadAmazonMatchSource(t.Context())
	require.NoError(t, err)
	require.Equal(t, matchBefore.Settings, matchAfter.Settings)
	require.Equal(t, matchBefore.Items, matchAfter.Items)
	require.Equal(t, baseline.NoOp, result.NoOp)
	require.Equal(t, baseline.Unchanged, result.Unchanged)
}

func TestYNABTransferPreservesSplitAndTransferRestrictions(t *testing.T) {
	document := testDocument(t)
	document.Header.ProviderKind = "ynab"
	txn := &document.State.Snapshot.Committed.Transactions[0]
	txn.CategoryID = domain.SplitCategoryID
	txn.Amount = domain.Money{Minor: -123, Currency: "USD", Scale: 2}
	document.State.Provider.Binding = &store.ProviderBinding{Kind: "ynab", Namespace: "ynab", RemoteProfileID: "example-plan", Currency: "USD", Scale: 2, BoundAt: time.Unix(100, 0).UTC()}
	document.State.YNABSplits = []store.YNABTransactionSplit{{ParentTransactionID: txn.ID, ExternalID: "split-example", AmountMinor: -123, AmountMilliunits: -1230}}
	document.State.Provider.WriteRestrictions = []store.ProviderWriteRestriction{{Kind: domain.EntityKindTransaction, EntityID: txn.ID, Reason: "transfer"}}
	source := transferStore(t)
	require.NoError(t, source.InstallProfileTransfer(t.Context(), document.State))
	imported := transferRoundTrip(t, source, "ynab")
	service, err := app.NewProfileService(t.Context(), imported)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowAllDetail()
	state := session.ViewState()
	_, err = service.Mutate(t.Context(), app.MutationRequest{Action: app.ActionEditCategory, ExpectedRevision: service.Revision(), State: state, Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(txn.ID)}, Input: app.EditInput{Scope: app.EditScopeTransactions, DestinationID: domain.UncategorizedCategoryID}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "YNAB")
	require.Contains(t, err.Error(), "split")
	loaded, err := imported.LoadProfileTransfer(t.Context())
	require.NoError(t, err)
	require.Equal(t, document.State.YNABSplits, loaded.YNABSplits)
	require.Equal(t, document.State.Provider.WriteRestrictions, loaded.Provider.WriteRestrictions)
	require.Empty(t, loaded.Snapshot.Journal)
}

func transferStore(t *testing.T) store.Profile {
	t.Helper()
	root := t.TempDir()
	profile, err := sqlite.Open(t.Context(), home.Paths{Root: root, Database: filepath.Join(root, "moneyflow.db")}, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	return profile
}

func transferRoundTrip(t *testing.T, source store.Profile, kind string) store.Profile {
	t.Helper()
	state, err := source.LoadProfileTransfer(t.Context())
	require.NoError(t, err)
	now := time.Now().UTC()
	state, _, err = app.PrepareProfileTransfer(state, now)
	require.NoError(t, err)
	var encoded bytes.Buffer
	require.NoError(t, Encode(&encoded, Document{Header: Header{Format: "moneyflow-profile", FormatVersion: 1, SourceBuild: "test", ProviderKind: kind, ExportedAt: now, SourceRevision: state.Snapshot.Revision, SourceName: "Example"}, State: state}))
	document, err := Decode(&encoded)
	require.NoError(t, err)
	target := transferStore(t)
	require.NoError(t, target.InstallProfileTransfer(t.Context(), document.State))
	return target
}
