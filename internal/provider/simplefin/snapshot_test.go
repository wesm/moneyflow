package simplefin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

var testNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

const postedTransaction = `{"id":"transaction-a","posted":1789776000,"amount":"-12.34","description":"Example Merchant"}`

func accountJSON(connection, account, currency, transactions string) string {
	return fmt.Sprintf(`{"id":%q,"conn_id":%q,"name":"Example Account","currency":%q,"balance":"0.00","balance-date":1789776000,"transactions":[%s]}`, account, connection, currency, transactions)
}

func accountsJSON(accounts ...string) string {
	return `{"connections":[],"errlist":[],"accounts":[` + strings.Join(accounts, ",") + `]}`
}

func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(ClientOptions{
		AccessURL: strings.Replace(server.URL, "://", "://user:synthetic-secret@", 1),
		Import:    ImportConfig{Currency: "USD", Scale: 2}, HTTPClient: server.Client(),
	})
	require.NoError(t, err)
	return client
}

func shortFetch() provider.FetchRequest {
	return provider.FetchRequest{LastSuccess: testNow.AddDate(0, 0, -1), Now: testNow}
}

func TestFetchSnapshotPostedMoneyAndPendingExclusion(t *testing.T) {
	calls := 0
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "user", user)
		require.Equal(t, "synthetic-secret", password)
		require.Nil(t, r.URL.User)
		require.Equal(t, "/accounts", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("version"))
		_, _ = io.WriteString(w, accountsJSON(accountJSON("connection-a", "account-a", "USD",
			postedTransaction+`,{"id":"pending-a","pending":true,"amount":{}}`)))
	})
	var progress provider.Progress
	snapshot, err := client.FetchSnapshot(t.Context(), shortFetch(), func(value provider.Progress) { progress = value })
	require.NoError(t, err)
	require.NoError(t, snapshot.Snapshot.Validate())
	require.Len(t, snapshot.Snapshot.Transactions, 1)
	require.Equal(t, domain.Money{Minor: -1234, Currency: "USD", Scale: 2}, snapshot.Snapshot.Transactions[0].Amount)
	require.Equal(t, "2026-09-19", snapshot.Snapshot.Transactions[0].Date.String())
	require.Equal(t, `["connection-a","account-a","transaction-a"]`, snapshot.Snapshot.Transactions[0].ExternalID)
	require.Equal(t, 1, calls)
	require.Equal(t, 1, progress.Fetched)
	require.Zero(t, progress.Total)
}

func TestFetchSnapshotValidation(t *testing.T) {
	validAccount := accountJSON("connection-a", "account-a", "USD", postedTransaction)
	for _, test := range []struct {
		name, body string
		want       int
		invalid    bool
	}{
		{"empty", accountsJSON(), 0, false},
		{"no transactions", accountsJSON(strings.Replace(validAccount, `"transactions":[`+postedTransaction+`]`, `"extra":{}`, 1)), 0, false},
		{"missing accounts", `{"errlist":[],"connections":[]}`, 0, true},
		{"null accounts", `{"errlist":[],"connections":[],"accounts":null}`, 0, true},
		{"missing errors", `{"connections":[],"accounts":[]}`, 0, true},
		{"missing connections", `{"errlist":[],"accounts":[]}`, 0, true},
		{"partial", `{"errlist":[{"code":"act.missingdata","msg":"synthetic-secret"}],"connections":[],"accounts":[]}`, 0, true},
		{"deprecated errors", `{"errlist":[],"errors":["synthetic-secret"],"connections":[],"accounts":[]}`, 0, true},
		{"precision", accountsJSON(strings.Replace(validAccount, "-12.34", "-12.345", 1)), 0, true},
		{"no date", accountsJSON(strings.Replace(validAccount, `"posted":1789776000`, `"posted":0`, 1)), 0, true},
		{"date fallback", accountsJSON(strings.Replace(validAccount, `"posted":1789776000`, `"posted":0,"transacted_at":1789776000`, 1)), 1, false},
		{"mixed empty account", accountsJSON(validAccount, accountJSON("connection-b", "account-b", "GBP", "")), 0, true},
		{"custom currency", accountsJSON(accountJSON("connection-a", "account-a", "https://example.com/points", "")), 0, true},
		{"missing connection", accountsJSON(accountJSON("", "account-a", "USD", postedTransaction)), 0, true},
		{"missing account", accountsJSON(accountJSON("connection-a", "", "USD", postedTransaction)), 0, true},
		{"missing transaction ID", accountsJSON(strings.Replace(validAccount, `"id":"transaction-a"`, `"id":""`, 1)), 0, true},
		{"duplicate agreement", accountsJSON(validAccount, validAccount), 1, false},
		{"duplicate conflict", accountsJSON(validAccount, strings.Replace(validAccount, "-12.34", "-11.11", 1)), 0, true},
		{"connection scoped", accountsJSON(validAccount, accountJSON("connection-b", "account-a", "USD", postedTransaction)), 2, false},
		{"account scoped", accountsJSON(validAccount, accountJSON("connection-a", "account-b", "USD", postedTransaction)), 2, false},
		{"trailing JSON", accountsJSON() + ` {}`, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, test.body) })
			result, err := client.FetchSnapshot(t.Context(), shortFetch(), nil)
			if test.invalid {
				require.Error(t, err)
				code, ok := provider.CodeOf(err)
				require.True(t, ok)
				require.Equal(t, provider.CodeDataInvalid, code)
				require.NotContains(t, err.Error(), "synthetic-secret")
				require.Empty(t, result.Snapshot.Transactions)
			} else {
				require.NoError(t, err)
				require.Len(t, result.Snapshot.Transactions, test.want)
			}
		})
	}
}

func TestFetchWindows(t *testing.T) {
	for _, test := range []struct {
		name       string
		request    provider.FetchRequest
		start, end string
		count      int
	}{
		{"initial", provider.FetchRequest{Now: testNow}, "2023-09-20", "2026-09-20", 13},
		{"overlap", shortFetch(), "2026-09-04", "2026-09-20", 1},
		{"catchup capped", provider.FetchRequest{Now: testNow, LastSuccess: testNow.AddDate(-6, 0, 0)}, "2023-09-20", "2026-09-20", 13},
		{"leap and UTC", provider.FetchRequest{Now: time.Date(2024, 3, 1, 0, 30, 0, 0, time.FixedZone("example", 3600)), LastSuccess: time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)}, "2024-02-15", "2024-03-01", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			windows, err := fetchWindows(test.request)
			require.NoError(t, err)
			require.Len(t, windows, test.count)
			require.Equal(t, test.start, windows[0].Start.Format(time.DateOnly))
			require.Equal(t, test.end, windows[len(windows)-1].End.Format(time.DateOnly))
			for i, window := range windows {
				require.Positive(t, window.End.Sub(window.Start))
				require.LessOrEqual(t, window.End.Sub(window.Start), 90*24*time.Hour)
				if i > 0 {
					require.Equal(t, windows[i-1].End, window.Start)
				}
			}
		})
	}
}

func TestFetchSnapshotWindowFailureIsAtomic(t *testing.T) {
	for _, cancelSecond := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelSecond), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				start, err := strconv.ParseInt(r.URL.Query().Get("start-date"), 10, 64)
				require.NoError(t, err)
				end, err := strconv.ParseInt(r.URL.Query().Get("end-date"), 10, 64)
				require.NoError(t, err)
				require.LessOrEqual(t, end-start, int64(90*24*60*60))
				if calls == 2 {
					if cancelSecond {
						cancel()
						<-r.Context().Done()
					} else {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				_, _ = io.WriteString(w, accountsJSON(accountJSON("connection-a", "account-a", "USD", postedTransaction)))
			})
			result, err := client.FetchSnapshot(ctx, provider.FetchRequest{Now: testNow}, nil)
			require.Error(t, err)
			require.Empty(t, result.Snapshot.Transactions)
			require.Equal(t, 2, calls)
			if cancelSecond {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}
