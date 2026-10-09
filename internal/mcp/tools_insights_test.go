package mcp

import (
	"maps"
	"strconv"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestGetInsightsSummarizesAMonthAtTheServiceClock(t *testing.T) {
	service := insightsTestService(t)
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()

	result := callInsights(t, client, map[string]any{
		"start_date": "2026-08-01", "end_date": "2026-08-31", "group_by": "category",
		"summary": true,
	})

	require.False(t, result.IsError, "%v", result.StructuredContent)
	document := result.StructuredContent.(map[string]any)
	assert.JSONEq(t, result.Content[0].(*mcpsdk.TextContent).Text, mustJSON(t, document))
	assert.Equal(t, "1", document["version"])
	assert.Equal(t, "ok", document["status"])
	assert.Equal(t, "USD", document["currency"])
	assert.Equal(t, "2026-08-29", document["today"])
	assert.Equal(t, "2026-08-15", document["latest"])
	ranges := document["ranges"].(map[string]any)
	assert.Equal(t, "month", ranges["kind"])
	assert.Equal(t, map[string]any{"start": "2026-08-01", "end": "2026-08-31"}, ranges["scope"])
	summary := document["summary"].(map[string]any)
	kpis := summary["kpis"].(map[string]any)
	scope := kpis["scope"].(map[string]any)
	assert.Equal(t, usd("5000.00", "500000"), scope["in"])
	assert.Equal(t, usd("-148.45", "-14845"), scope["out"])
	assert.Equal(t, usd("4851.55", "485155"), scope["net"])
	assert.Equal(t, float64(3), scope["count"])
	comparison := kpis["comparison"].(map[string]any)
	assert.Equal(t, usd("-100.00", "-10000"), comparison["out"])
	assert.Equal(t, float64(1), comparison["count"])
	cashflow := summary["cashflow"].(map[string]any)
	assert.Len(t, cashflow["net"], len(ranges["context"].([]any)))
	// A month's chart context ends with that month and reaches back to cover July too.
	assert.Equal(t, "475155", sumMinor(t, cashflow["net"]))
	assert.Equal(t, usd("4851.55", "485155"), last(cashflow["net"]))
	composition := summary["composition"].(map[string]any)
	spending := composition["spending"].([]any)
	require.NotEmpty(t, spending)
	top := spending[0].(map[string]any)
	assert.Equal(t, "category_a", top["key"])
	assert.Equal(t, usd("-123.45", "-12345"), top["net"])
	assert.Empty(t, document["series"])
}

func TestGetInsightsTurnsIDFiltersIntoDrillDownsAndKeysIntoSeries(t *testing.T) {
	date, err := domain.ParseDate("2026-08-12")
	require.NoError(t, err)
	service := insightsTestService(t,
		insightsTransaction("transaction_other", date, "merchant_b", "category_a", -700))
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()

	result := callInsights(t, client, map[string]any{
		"start_date": "2026-08-01", "end_date": "2026-08-31",
		"merchant_id": "merchant_a", "group_by": "category",
		"keys": []string{"category_a", "category_b", "category_a"}, "summary": false,
	})

	require.False(t, result.IsError, "%v", result.StructuredContent)
	document := result.StructuredContent.(map[string]any)
	assert.NotContains(t, document, "summary")
	series := document["series"].([]any)
	require.Len(t, series, 2)
	groceries, dining := series[0].(map[string]any), series[1].(map[string]any)
	assert.Equal(t, "category_a", groceries["key"])
	assert.Equal(t, "category_b", dining["key"])
	assert.Equal(t, usd("-123.45", "-12345"), last(groceries["context"].(map[string]any)["out"]))
	assert.Equal(t, "-22345", sumMinor(t, groceries["context"].(map[string]any)["out"]))
	assert.Equal(t, "-2500", sumMinor(t, dining["context"].(map[string]any)["out"]))
	assert.Equal(t, usd("-100.00", "-10000"), groceries["prior_net"])
	assert.Len(t, groceries["trend"], 12)
	assert.Contains(t, groceries, "vs_prior")

	both := callInsights(t, client, map[string]any{
		"start_date": "2026-08-01", "end_date": "2026-08-31",
		"category_id": "category_a", "merchant_id": "merchant_b",
	})
	require.False(t, both.IsError, "%v", both.StructuredContent)
	summary := both.StructuredContent.(map[string]any)["summary"].(map[string]any)
	scope := summary["kpis"].(map[string]any)["scope"].(map[string]any)
	assert.Equal(t, usd("-7.00", "-700"), scope["out"])
	assert.Equal(t, float64(1), scope["count"])
}

func TestGetInsightsRequiresACurrencyInAMultiCurrencyProfile(t *testing.T) {
	date, err := domain.ParseDate("2026-08-20")
	require.NoError(t, err)
	euro := insightsTransaction("transaction_eur", date, "merchant_b", "category_c", -4200)
	euro.Amount.Currency = "EUR"
	service := insightsTestService(t, euro)
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()

	missing := callInsights(t, client, map[string]any{"summary": true})
	assert.True(t, missing.IsError)
	assert.Equal(t, "invalid_operation", missing.StructuredContent.(map[string]any)["code"])

	chosen := callInsights(t, client, map[string]any{"currency": "EUR", "scale": 2, "summary": true})
	require.False(t, chosen.IsError, "%v", chosen.StructuredContent)
	document := chosen.StructuredContent.(map[string]any)
	assert.Equal(t, "EUR", document["currency"])
	assert.Equal(t, "all", document["ranges"].(map[string]any)["kind"])
	scope := document["summary"].(map[string]any)["kpis"].(map[string]any)["scope"].(map[string]any)
	assert.Equal(t, map[string]any{
		"amount": "-42.00", "amount_minor": "-4200", "currency": "EUR", "scale": float64(2),
	}, scope["net"])
}

func TestGetInsightsIgnoresPendingEdits(t *testing.T) {
	service, closeProfile := writeTestService(t, 2)
	defer closeProfile()
	service.SetClock(func() time.Time { return time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC) })
	client, cleanup := connectWriteTestServer(t, service, true)
	defer cleanup()
	arguments := map[string]any{
		"start_date": "2026-08-01", "end_date": "2026-08-31", "group_by": "category",
		"keys": []string{"category_a", "category_b"}, "summary": true,
	}
	before := callInsights(t, client, arguments)
	require.False(t, before.IsError, "%v", before.StructuredContent)

	staged := callWriteTool(t, client, "update_transaction_category", map[string]any{
		"expected_revision": before.StructuredContent.(map[string]any)["revision"],
		"transaction_id":    "transaction_000", "category_id": "category_b",
	})
	require.False(t, staged.IsError, "%v", staged.StructuredContent)
	after := callInsights(t, client, arguments)
	require.False(t, after.IsError, "%v", after.StructuredContent)

	assert.NotEqual(t, before.StructuredContent.(map[string]any)["revision"],
		after.StructuredContent.(map[string]any)["revision"])
	assert.Equal(t, withoutRevision(before), withoutRevision(after))
}

func TestGetInsightsRejectsInvalidInput(t *testing.T) {
	service := insightsTestService(t)
	client, cleanup := connectReadTestServer(t, service)
	defer cleanup()
	tooMany := make([]string, app.MaxInsightsRows+1)
	for index := range tooMany {
		tooMany[index] = "category_" + strconv.Itoa(index)
	}
	tests := map[string]map[string]any{
		"invalid regular expression":      {"search": "(", "summary": true},
		"start date without end date":     {"start_date": "2026-08-01"},
		"end before start":                {"start_date": "2026-08-31", "end_date": "2026-08-01"},
		"currency without scale":          {"currency": "USD"},
		"unknown grouping":                {"group_by": "payee"},
		"grouping by a filtered key":      {"merchant_id": "merchant_a", "group_by": "merchant"},
		"keys in a transaction view":      {"keys": []string{"category_a"}},
		"keys in the time grouping":       {"group_by": "time", "keys": []string{"2026"}},
		"more keys than one window":       {"group_by": "category", "keys": tooMany},
		"grouping by a filtered category": {"category_id": "category_a", "group_by": "category"},
	}
	for name, arguments := range tests {
		t.Run(name, func(t *testing.T) {
			result := callInsights(t, client, arguments)
			assert.True(t, result.IsError)
			assert.Equal(t, "invalid_operation", result.StructuredContent.(map[string]any)["code"])
		})
	}
}

func insightsTestService(t *testing.T, extra ...domain.Transaction) *app.Service {
	t.Helper()
	dates := make([]domain.Date, 0, 4)
	for _, text := range []string{"2026-08-03", "2026-08-10", "2026-08-15", "2026-07-20"} {
		date, err := domain.ParseDate(text)
		require.NoError(t, err)
		dates = append(dates, date)
	}
	transactions := append([]domain.Transaction{
		insightsTransaction("transaction_1", dates[0], "merchant_a", "category_a", -12345),
		insightsTransaction("transaction_2", dates[1], "merchant_a", "category_b", -2500),
		insightsTransaction("transaction_3", dates[2], "merchant_b", "category_c", 500000),
		insightsTransaction("transaction_4", dates[3], "merchant_a", "category_a", -10000),
	}, extra...)
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	service.SetClock(func() time.Time { return time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC) })
	return service
}

func insightsTransaction(
	id string, date domain.Date, merchant, category string, minor int64,
) domain.Transaction {
	group := "group_a"
	if category == "category_c" {
		group = "group_b"
	}
	return domain.Transaction{
		ID: id, ProviderID: "provider_" + id, Provider: "fixture", Date: date,
		Account:  domain.EntityRef{ID: "account_a", Name: "Example Account"},
		Merchant: domain.EntityRef{ID: merchant, Name: "Example " + merchant},
		Category: domain.CategoryRef{
			ID: category, Name: "Example " + category, GroupID: group, Group: "Example " + group,
		},
		Amount: domain.Money{Minor: minor, Currency: "USD", Scale: 2},
	}
}

func callInsights(
	t *testing.T, client *mcpsdk.ClientSession, arguments map[string]any,
) *mcpsdk.CallToolResult {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcpsdk.CallToolParams{
		Name: "get_insights", Arguments: arguments,
	})
	require.NoError(t, err)
	return result
}

func usd(amount, minor string) map[string]any {
	return map[string]any{
		"amount": amount, "amount_minor": minor, "currency": "USD", "scale": float64(2),
	}
}

// sumMinor adds the exact minor units of a JSON list of money documents.
func sumMinor(t *testing.T, values any) string {
	t.Helper()
	var total int64
	for _, value := range values.([]any) {
		money := value.(map[string]any)
		require.Equal(t, "USD", money["currency"])
		minor, err := strconv.ParseInt(money["amount_minor"].(string), 10, 64)
		require.NoError(t, err)
		total += minor
	}
	return strconv.FormatInt(total, 10)
}

func last(values any) any {
	list := values.([]any)
	return list[len(list)-1]
}

func withoutRevision(result *mcpsdk.CallToolResult) map[string]any {
	document := maps.Clone(result.StructuredContent.(map[string]any))
	delete(document, "revision")
	return document
}
