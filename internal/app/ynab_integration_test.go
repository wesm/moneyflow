package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

// Keep the real reader, vault, service, journal, and SQLite boundary in this journey.
// Only the remote server is synthetic, so a normalized fake cannot hide wire/fold gaps.
func TestYNABHTTPRefreshPreservesJournalThroughFailureAndOfflineRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodGet, request.Method)
		assert.Empty(t, request.URL.RawQuery, "refresh must not request a delta or filtered feed")
		writer.Header().Set("Content-Type", "application/json")
		parent := `{"id":"transaction-example","date":"2026-08-30","amount":-12340,"memo":"Original memo","cleared":"uncleared","approved":true,"account_id":"account-example","payee_id":"payee-example","category_id":"category-historical","deleted":false}`
		if phase.Load() == 3 {
			parent = strings.ReplaceAll(parent, "Original memo", "Updated memo")
			parent = strings.ReplaceAll(parent, "uncleared", "cleared")
		}
		if phase.Load() == 4 {
			parent = strings.ReplaceAll(parent, `,"amount":-12340`, "")
		}
		if request.URL.Path == "/v1/plans/plan-example/transactions" {
			knowledge := 1
			if phase.Load() == 1 {
				knowledge = 2
			}
			detail := strings.TrimSuffix(parent, "}") + `,"category_name":"Historical Category","subtransactions":[]}`
			_, _ = fmt.Fprintf(writer, `{"data":{"transactions":[%s],"server_knowledge":%d}}`, detail, knowledge)
			return
		}
		assert.Equal(t, "/v1/plans/plan-example", request.URL.Path)
		currency, transactions := "USD", "["+parent+"]"
		if phase.Load() == 2 {
			currency, transactions = "EUR", "[]"
		}
		_, _ = fmt.Fprintf(writer, `{"data":{"server_knowledge":1,"plan":{
			"id":"plan-example","name":"Example Budget","currency_format":{"iso_code":%q,"decimal_digits":2},
			"accounts":[{"id":"account-example","name":"Account Name","type":"checking","on_budget":true,"closed":true,"deleted":false}],
			"payees":[{"id":"payee-example","name":"Example Payee","deleted":false}],
			"category_groups":[],"categories":[],"transactions":%s,"subtransactions":[]}}}`, currency, transactions)
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL + "/v1/")
	require.NoError(t, err)
	paths, err := home.ResolveRoot(t.TempDir()+"/profile", nil, "")
	require.NoError(t, err)
	handle, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handle.Close()) })
	vault, err := ynab.NewCredentialVault(paths)
	require.NoError(t, err)
	credentials := ynab.StoredCredentials{AccessToken: "synthetic-token", PlanID: "plan-example", Currency: "USD", Scale: 2} //nolint:gosec // synthetic credential.
	require.NoError(t, vault.Save(credentials, []byte("account-password")))
	source, err := ynab.NewSource(ynab.SourceOptions{
		Client: ynab.ClientOptions{BaseURL: base}, Vault: vault, Credentials: credentials,
	})
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, handle)
	require.NoError(t, err)
	require.NoError(t, service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: source, Provider: "ynab", Currency: "USD", Scale: 2,
		Renderer: "tui", InstanceID: "ynab-http-integration", Now: time.Now,
	}))
	request := app.ProviderRefreshRequest{Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection()}
	_, err = service.RefreshProvider(ctx, request)
	require.NoError(t, err)
	loaded, err := handle.Load(ctx)
	require.NoError(t, err)
	require.Len(t, loaded.Committed.Transactions, 1)
	target := loaded.Committed.Transactions[0].ID
	info, err := service.TransactionInfo(ctx, app.TransactionInfoRequest{TransactionID: string(target)})
	require.NoError(t, err)
	assert.Equal(t, int64(-1234), info.Transaction.Amount.Minor)
	assert.True(t, info.Transaction.Pending)
	assert.Equal(t, "Historical Category", info.Transaction.Category.Name)
	assert.Equal(t, string(domain.UncategorizedGroupID), info.Transaction.Category.GroupID)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	_, err = service.Mutate(ctx, app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: service.Revision(),
		State: state, Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: string(target)},
	})
	require.NoError(t, err)
	before, err := handle.Load(ctx)
	require.NoError(t, err)

	for _, failure := range []struct {
		phase int32
		code  provider.ErrorCode
	}{
		{1, provider.CodeSnapshotUnstable},
		{2, provider.CodeMoneyMismatch},
		{4, provider.CodeDataInvalid},
	} {
		phase.Store(failure.phase)
		_, err = service.RefreshProvider(ctx, request)
		code, ok := provider.CodeOf(err)
		require.True(t, ok)
		assert.Equal(t, failure.code, code)
		after, err := handle.Load(ctx)
		require.NoError(t, err)
		assert.Equal(t, before, after, "rejected snapshots must preserve committed rows, identities, and the journal")
		metadata, err := handle.ProviderState(ctx)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), metadata.Refresh.Generation)
		assert.Nil(t, metadata.Lease)
	}

	phase.Store(3)
	_, err = service.RefreshProvider(ctx, request)
	require.NoError(t, err)
	assert.Equal(t, 1, service.Pending().ActiveOperations)
	require.NoError(t, handle.Close())
	server.Close()
	reopened, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reopened.Close()) })
	offline, err := app.NewProfileService(ctx, reopened)
	require.NoError(t, err)
	info, err = offline.TransactionInfo(ctx, app.TransactionInfoRequest{TransactionID: string(target)})
	require.NoError(t, err)
	assert.Equal(t, "ynab", offline.ProfileKind())
	assert.Equal(t, int64(-1234), info.Transaction.Amount.Minor)
	assert.Equal(t, "Updated memo", info.Transaction.Notes)
	assert.Equal(t, "Historical Category", info.Transaction.Category.Name)
	assert.False(t, info.Transaction.Pending, "posting is a provider fact, separate from local edit markers")
	assert.True(t, info.Transaction.Hidden, "staged hide must survive refresh and process restart")
	assert.Equal(t, 1, offline.Pending().ActiveOperations)
}
