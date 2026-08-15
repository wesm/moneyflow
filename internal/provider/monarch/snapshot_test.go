package monarch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func TestSnapshotFetchesBothPartitionsAndExcludesPendingAfterIntegrity(t *testing.T) {
	t.Parallel()

	server := newSnapshotServer(t, snapshotScenario{})
	client := newSnapshotClient(t, server)
	var progress []provider.Progress
	snapshot, err := client.FetchSnapshot(context.Background(), func(update provider.Progress) {
		progress = append(progress, update)
	})
	require.NoError(t, err)
	require.NoError(t, snapshot.Validate())
	assert.Len(t, snapshot.Transactions, 2)
	for _, transaction := range snapshot.Transactions {
		assert.False(t, transaction.Pending)
	}
	assert.Equal(t, int64(-1234), snapshot.Transactions[0].Amount.Minor)
	assert.Equal(t, domain.Currency("USD"), snapshot.Transactions[0].Amount.Currency)
	assert.Equal(t, uint8(2), snapshot.Transactions[0].Amount.Scale)
	assert.Contains(t, progress, provider.Progress{
		Partition: "visible", Fetched: 2, Total: 2, Attempt: 1,
	})
	assert.Contains(t, progress, provider.Progress{
		Partition: "hidden", Fetched: 1, Total: 1, Attempt: 1,
	})
	assert.Equal(t, 2, server.CompleteScans())
}

func TestSnapshotProbeReturnsSubscriptionIdentity(t *testing.T) {
	t.Parallel()

	server := newSnapshotServer(t, snapshotScenario{})
	client := newSnapshotClient(t, server)
	identity, err := client.ProbeIdentity(context.Background())
	require.NoError(t, err)
	assert.Equal(t, provider.ProfileIdentity{Kind: providerKind, RemoteID: "subscription-a"}, identity)
}

func TestSnapshotRejectsInvalidMoneyWithoutRetry(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{}
	scenario.Visible = []Transaction{snapshotTransaction("transaction-invalid", false, false)}
	scenario.Visible[0].Amount = json.RawMessage(`"1.234"`)
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeDataInvalid)
	assert.Equal(t, 2, server.CompleteScans())
}

func TestSnapshotRejectsDuplicateEntityIDsWithoutRetry(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{Merchants: []Merchant{
		{ID: "merchant-a", Name: "Example Merchant"},
		{ID: "merchant-a", Name: "Example Merchant Duplicate"},
	}}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeDataInvalid)
	assert.Equal(t, 2, server.CompleteScans())
}

func TestSnapshotRetriesMissingCategoryGroup(t *testing.T) {
	t.Parallel()

	category := Category{ID: "category-a", Name: "Example Category"}
	category.Group.ID = "group-not-in-snapshot"
	server := newSnapshotServer(t, snapshotScenario{Categories: []Category{category}})
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeSnapshotUnstable)
	assert.Equal(t, 6, server.CompleteScans())
}

func TestSnapshotRetriesRelationshipRace(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{BeforePage: func(attempt int, _ bool, _ int, _ int, total int, rows []Transaction) (int, []Transaction) {
		if attempt == 1 && len(rows) > 0 {
			rows[0].Account.ID = "account-not-in-snapshot"
		}
		return total, rows
	}}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 4, server.CompleteScans())
}

func TestSnapshotPersistentRelationshipRaceExhaustsThreeAttempts(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{BeforePage: func(_ int, _ bool, _ int, _ int, total int, rows []Transaction) (int, []Transaction) {
		if len(rows) > 0 {
			rows[0].Account.ID = "account-not-in-snapshot"
		}
		return total, rows
	}}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeSnapshotUnstable)
	assert.Equal(t, 6, server.CompleteScans())
}

func TestPendingRowsParticipateInPartitionIntegrity(t *testing.T) {
	t.Parallel()

	pending := snapshotTransaction("transaction-pending", false, true)
	scenario := snapshotScenario{
		Visible: []Transaction{pending},
		Hidden:  []Transaction{pending},
	}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)

	_, err := client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeSnapshotUnstable)
	assert.Equal(t, 3, server.CompleteScans())
}

func TestSnapshotCancellationStopsRetryBackoff(t *testing.T) {
	t.Parallel()

	scenario := snapshotScenario{BeforePage: func(_ int, _ bool, _ int, _ int, total int, rows []Transaction) (int, []Transaction) {
		return total + 1, rows
	}}
	server := newSnapshotServer(t, scenario)
	client := newSnapshotClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	client.options.Sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	_, err := client.FetchSnapshot(ctx, nil)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, server.CompleteScans())
}

func TestSnapshotRequiresExplicitMoneyFormat(t *testing.T) {
	t.Parallel()

	server := newSnapshotServer(t, snapshotScenario{})
	options := testClientOptions(t, server.URL())
	client, err := NewClient(options, "session-token", "device-a")
	require.NoError(t, err)

	_, err = client.FetchSnapshot(context.Background(), nil)
	assertProviderCode(t, err, provider.CodeDataInvalid)
}

type snapshotScenario struct {
	Accounts       []Account
	Merchants      []Merchant
	Groups         []CategoryGroup
	Categories     []Category
	Visible        []Transaction
	Hidden         []Transaction
	BeforeAccounts func(scan int, rows []Account) []Account
	BeforePage     func(
		attempt int,
		hidden bool,
		offset int,
		limit int,
		total int,
		rows []Transaction,
	) (int, []Transaction)
}

type scriptedSnapshotServer struct {
	t        *testing.T
	server   *httptest.Server
	scenario snapshotScenario

	mu       sync.Mutex
	attempts int
}

func newSnapshotServer(t *testing.T, scenario snapshotScenario) *scriptedSnapshotServer {
	t.Helper()
	if scenario.Accounts == nil {
		scenario.Accounts = []Account{{ID: "account-a", DisplayName: "Example Account"}}
	}
	if scenario.Merchants == nil {
		scenario.Merchants = []Merchant{{ID: "merchant-a", Name: "Example Merchant"}}
	}
	if scenario.Groups == nil {
		scenario.Groups = []CategoryGroup{{ID: "group-a", Name: "Example Group"}}
	}
	if scenario.Categories == nil {
		category := Category{ID: "category-a", Name: "Example Category"}
		category.Group.ID = "group-a"
		scenario.Categories = []Category{category}
	}
	if scenario.Visible == nil {
		scenario.Visible = []Transaction{
			snapshotTransaction("transaction-visible", false, false),
			snapshotTransaction("transaction-pending", false, true),
		}
	}
	if scenario.Hidden == nil {
		scenario.Hidden = []Transaction{snapshotTransaction("transaction-hidden", true, false)}
	}
	fixture := &scriptedSnapshotServer{t: t, scenario: scenario}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.serveHTTP))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (server *scriptedSnapshotServer) URL() string { return server.server.URL }

func (server *scriptedSnapshotServer) CompleteScans() int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.attempts
}

func (server *scriptedSnapshotServer) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	server.t.Helper()
	var envelope graphQLRequest
	require.NoError(server.t, json.NewDecoder(request.Body).Decode(&envelope))
	switch envelope.OperationName {
	case "GetSubscriptionDetails":
		writeJSON(server.t, writer, map[string]any{"data": map[string]any{
			"subscription": map[string]any{"id": "subscription-a"},
		}})
	case "GetAccounts":
		server.mu.Lock()
		server.attempts++
		scan := server.attempts
		server.mu.Unlock()
		accounts := append([]Account(nil), server.scenario.Accounts...)
		if server.scenario.BeforeAccounts != nil {
			accounts = server.scenario.BeforeAccounts(scan, accounts)
		}
		writeJSON(server.t, writer, map[string]any{"data": map[string]any{
			"accounts": accounts,
		}})
	case "GetAllMerchants":
		aggregates := make([]map[string]any, 0, len(server.scenario.Merchants))
		for _, merchant := range server.scenario.Merchants {
			aggregates = append(aggregates, map[string]any{
				"groupBy": map[string]any{"merchant": merchant},
			})
		}
		writeJSON(server.t, writer, map[string]any{"data": map[string]any{
			"byMerchant": aggregates,
		}})
	case "ManageGetCategoryGroups":
		writeJSON(server.t, writer, map[string]any{"data": map[string]any{
			"categoryGroups": server.scenario.Groups,
		}})
	case "GetCategories":
		writeJSON(server.t, writer, map[string]any{"data": map[string]any{
			"categories": server.scenario.Categories,
		}})
	case "GetTransactionsList":
		server.writeTransactionPage(writer, envelope.Variables)
	default:
		server.t.Fatalf("unexpected operation %q", envelope.OperationName)
	}
}

func (server *scriptedSnapshotServer) writeTransactionPage(
	writer http.ResponseWriter,
	variables map[string]any,
) {
	server.t.Helper()
	require.Len(server.t, variables, 4)
	assert.Equal(server.t, "date", variables["orderBy"])
	filters, ok := variables["filters"].(map[string]any)
	require.True(server.t, ok)
	require.Len(server.t, filters, 1)
	hidden := filters["hideFromReports"].(bool)
	offset := int(variables["offset"].(float64))
	limit := int(variables["limit"].(float64))
	server.mu.Lock()
	attempt := server.attempts
	server.mu.Unlock()
	allRows := cloneTransactions(server.scenario.Visible)
	if hidden {
		allRows = cloneTransactions(server.scenario.Hidden)
	}
	rows := pageTransactions(allRows, offset, limit)
	total := len(allRows)
	if server.scenario.BeforePage != nil {
		total, rows = server.scenario.BeforePage(attempt, hidden, offset, limit, total, rows)
	}
	writeJSON(server.t, writer, map[string]any{"data": map[string]any{
		"allTransactions": TransactionPage{TotalCount: total, Results: rows},
	}})
}

func newSnapshotClient(t *testing.T, server *scriptedSnapshotServer) *Client {
	t.Helper()
	options := testClientOptions(t, server.URL())
	options.ImportCurrency = "USD"
	options.ImportScale = 2
	options.PageSize = 2
	options.Now = func() time.Time {
		return time.Date(2026, time.August, 15, 17, 0, 0, 0, time.UTC)
	}
	options.Sleep = func(context.Context, time.Duration) error { return nil }
	client, err := NewClient(options, "session-token", "device-a")
	require.NoError(t, err)
	return client
}

func snapshotTransaction(id string, hidden bool, pending bool) Transaction {
	transaction := Transaction{
		ID: id, Amount: json.RawMessage(`"-12.34"`), Pending: pending,
		Date: "2026-08-15", HideFromReports: hidden,
	}
	transaction.Account.ID = "account-a"
	transaction.Account.DisplayName = "Example Account"
	transaction.Merchant.ID = "merchant-a"
	transaction.Merchant.Name = "Example Merchant"
	transaction.Category.ID = "category-a"
	return transaction
}

func pageTransactions(rows []Transaction, offset int, limit int) []Transaction {
	if limit == 0 || offset >= len(rows) {
		return nil
	}
	end := min(offset+limit, len(rows))
	return cloneTransactions(rows[offset:end])
}

func cloneTransactions(rows []Transaction) []Transaction {
	return append([]Transaction(nil), rows...)
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	require.NoError(t, json.NewEncoder(writer).Encode(value))
}

func assertProviderCode(t *testing.T, err error, expected provider.ErrorCode) {
	t.Helper()
	code, ok := provider.CodeOf(err)
	require.True(t, ok, "%v", err)
	assert.Equal(t, expected, code)
}
