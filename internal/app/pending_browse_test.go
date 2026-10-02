package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

func TestPendingEditsLeaveBrowseRowsAndInfoCommitted(t *testing.T) {
	for _, test := range []struct {
		name   string
		action app.ActionID
		input  app.EditInput
	}{
		{"hide", app.ActionToggleHidden, app.EditInput{}},
		{"delete", app.ActionDeleteTransaction, app.EditInput{}},
		{"merchant", app.ActionEditMerchant, app.EditInput{Scope: app.EditScopeEntity, Label: "Renamed Merchant"}},
		{"category", app.ActionEditCategory, app.EditInput{Scope: app.EditScopeTransactions, DestinationID: "category_b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := app.NewProfileService(t.Context(), newMemoryProfile(t, 5))
			require.NoError(t, err)
			session := app.NewSession()
			session.ShowAllDetail()
			before, err := service.Query(session)
			require.NoError(t, err)
			mutated, err := service.Mutate(t.Context(), app.MutationRequest{
				Action: test.action, ExpectedRevision: service.Revision(), State: session.ViewState(),
				Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "transaction_a"},
				Input: test.input,
			})
			require.NoError(t, err)
			after, err := service.Query(session)
			require.NoError(t, err)
			require.Len(t, after.DetailRows, len(before.DetailRows))
			assert.Equal(t, before.Statistics, after.Statistics)
			for index, row := range after.DetailRows {
				assert.Equal(t, before.DetailRows[index].Transaction, row.Transaction)
				assert.Equal(t, row.Transaction.ID == "transaction_a", row.Flags.Pending)
			}
			info, err := service.TransactionInfo(t.Context(), app.TransactionInfoRequest{TransactionID: "transaction_a"})
			require.NoError(t, err)
			for _, row := range before.DetailRows {
				if row.Transaction.ID == "transaction_a" {
					assert.Equal(t, row.Transaction, info.Transaction)
				}
			}
			_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: mutated.Revision, ReviewedRevision: mutated.Revision})
			require.NoError(t, err)
			committed, err := service.Query(session)
			require.NoError(t, err)
			assert.NotEqual(t, before.DetailRows, committed.DetailRows)
			for _, row := range committed.DetailRows {
				assert.False(t, row.Flags.Pending)
			}
		})
	}
}

func TestMixedHideTotalsWaitForProviderWrite(t *testing.T) {
	for _, outcome := range []string{"success", "rejected"} {
		t.Run(outcome, func(t *testing.T) {
			service, _ := newProviderRefreshService(t)
			now := providerWriteTime()
			snapshot := providerSnapshot(t, now, 5)
			for index := range 4 {
				snapshot.Transactions[index].Hidden = true
			}
			reader := &fakeProviderSource{
				identity: provider.ProfileIdentity{Kind: "monarch", RemoteID: "subscription-example"},
				snapshot: snapshot, fingerprint: "session-a",
			}
			writer := &scriptedProviderWriter{identity: reader.identity}
			if outcome == "rejected" {
				writer.update = func(update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
					assert.Equal(t, provider.Some(true), update.Hidden)
					return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
				}
			}
			source := &writeProviderSource{fakeProviderSource: reader, writer: writer}
			configureProviderRefreshService(t, service, source, now, "pending-browse")
			_, err := service.RefreshProvider(t.Context(), app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()})
			require.NoError(t, err)
			session := app.NewSession()
			session.ShowHidden = false
			before, err := service.Query(session)
			require.NoError(t, err)
			require.Len(t, before.AggregateRows, 1)
			assert.Equal(t, int64(-104), before.AggregateRows[0].Total.Minor)
			mutated, err := service.Mutate(t.Context(), app.MutationRequest{
				Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(), State: session.ViewState(),
				Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityAggregate, Identity: app.AggregateIdentity(before.AggregateRows[0])},
			})
			require.NoError(t, err)
			assert.Equal(t, 1, mutated.Pending.AffectedTransactions)
			for _, stage := range []string{"staged", "write prepared"} {
				if stage == "write prepared" {
					_, err = service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: mutated.Revision, ReviewedRevision: mutated.Revision, State: session.ViewState(), Selection: app.EmptySelection()})
					require.NoError(t, err)
				}
				rows, queryErr := service.Query(session)
				require.NoError(t, queryErr)
				require.Len(t, rows.AggregateRows, 1, stage)
				assert.Equal(t, int64(-104), rows.AggregateRows[0].Total.Minor, stage)
				assert.True(t, rows.AggregateRows[0].Flags.Pending, stage)
			}
			status, err := service.RunProviderWrite(t.Context())
			if outcome == "rejected" {
				assertProviderAppCode(t, err, provider.CodeWriteAttentionRequired)
				assert.Equal(t, store.WritePhaseAttentionRequired, status.Phase)
				rows, queryErr := service.Query(session)
				require.NoError(t, queryErr)
				require.Len(t, rows.AggregateRows, 1)
				assert.Equal(t, int64(-104), rows.AggregateRows[0].Total.Minor)
				assert.True(t, rows.AggregateRows[0].Flags.Pending)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 1, writer.callCount())
			after, err := service.Query(session)
			require.NoError(t, err)
			assert.Empty(t, after.AggregateRows)
			session.ShowHidden = true
			after, err = service.Query(session)
			require.NoError(t, err)
			require.Len(t, after.AggregateRows, 1)
			assert.Equal(t, domain.Money{Minor: 0, Currency: "USD", Scale: 2}, after.AggregateRows[0].Total)
			assert.False(t, after.AggregateRows[0].Flags.Pending)
		})
	}
}
