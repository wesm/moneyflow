package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

func TestProviderConnectSimpleFINAndCachedStartup(t *testing.T) {
	t.Setenv("MONEYFLOW_HOME", t.TempDir())
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "GET", r.Method)
		_, _ = w.Write([]byte(`{"connections":[],"errlist":[],"accounts":[{"id":"a","conn_id":"c","name":"Example Account","currency":"USD","transactions":[{"id":"t","posted":1789776000,"amount":"-12.34","description":"Example Merchant"}]}]}`))
	}))
	defer server.Close()
	access, err := url.Parse(server.URL)
	require.NoError(t, err)
	access.User = url.UserPassword("user", "synthetic-secret")
	claims := 0
	factory := func(paths home.Paths) (simplefinonboarding.Runtime, error) {
		sessions, err := simplefin.NewSessionStore(paths)
		if err != nil {
			return simplefinonboarding.Runtime{}, err
		}
		return simplefinonboarding.Runtime{Sessions: sessions, InstanceID: "test", Now: func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }, Claim: func(context.Context, string) (string, error) { claims++; return access.String(), nil },
			NewSource: func(connection app.ProviderConnectionState, session simplefin.Session) (provider.ReaderSource, error) {
				currency, scale := session.Import.Currency, session.Import.Scale
				if connection.Bound {
					currency, scale = connection.Currency, connection.Scale
				}
				return simplefin.NewSource(simplefin.SourceOptions{ExpectedRemoteID: connection.RemoteProfileID, Currency: currency, Scale: scale, HTTPClient: server.Client()}, sessions)
			}}, nil
	}
	var stdout, stderr bytes.Buffer
	streams := IOStreams{In: strings.NewReader("synthetic-token\n"), Out: &stdout, Err: &stderr, OpenSimpleFIN: factory}
	command := newRootCommand(streams)
	command.SetArgs([]string{"provider", "connect", "simplefin", "--currency", "USD", "--scale", "2"})
	require.NoError(t, command.Execute())
	require.Equal(t, 1, claims)
	require.Equal(t, 13, requests)
	require.Contains(t, stdout.String(), "Imported 1 posted transaction")
	require.Contains(t, stderr.String(), "experimental")
	require.NotContains(t, stdout.String()+stderr.String(), "synthetic-")
	opened, err := openProfile(t.Context(), ProfileOptions{})
	require.NoError(t, err)
	defer func() { require.NoError(t, opened.Close()) }()
	require.NoError(t, configureOpenedProvider(t.Context(), opened, streams, "mcp"))
	require.Equal(t, 13, requests)
	status, err := opened.Service.ProviderStatus(t.Context())
	require.NoError(t, err)
	require.Equal(t, "simplefin", status.ProviderKind)
	require.False(t, status.NextEligible.IsZero())
	// The production opener does not load credentials or contact the network for cached reads.
	require.NoError(t, configureOpenedProvider(t.Context(), opened, IOStreams{}, "tui"))
	require.Equal(t, 13, requests)
	require.NoError(t, os.Remove(filepath.Join(opened.Paths.Root, "providers", "simplefin", "session.json")))
	require.NoError(t, configureOpenedProvider(t.Context(), opened, IOStreams{}, "mcp"))
	rows, err := opened.Service.TransactionWindow(t.Context(), app.TransactionWindowRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows.Rows, 1)
	require.Equal(t, int64(-1234), rows.Rows[0].Amount.Minor)
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	_, err = opened.Service.Mutate(t.Context(), app.MutationRequest{Action: app.ActionToggleHidden, ExpectedRevision: opened.Service.Revision(), State: state, Selection: app.EmptySelection(), Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: rows.Rows[0].ID}})
	require.NoError(t, err)
	result, err := opened.Service.Commit(t.Context(), app.CommitRequest{ExpectedRevision: opened.Service.Revision(), ReviewedRevision: opened.Service.Revision()})
	require.NoError(t, err)
	require.Nil(t, result.ProviderWrite)
	require.Equal(t, 13, requests)
}

func TestProviderConnectSimpleFINRequiresExplicitNoninteractiveSettings(t *testing.T) {
	for _, args := range [][]string{{"provider", "connect", "simplefin"}, {"provider", "connect", "simplefin", "--token", "synthetic-secret"}} {
		var output bytes.Buffer
		command := newRootCommand(IOStreams{In: strings.NewReader("synthetic-secret"), Out: &output, Err: &output})
		command.SetArgs(args)
		err := command.Execute()
		require.Error(t, err)
		require.NotContains(t, output.String()+err.Error(), "synthetic-secret")
	}
}
