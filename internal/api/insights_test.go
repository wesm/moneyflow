package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const insightsPath = "/api/v1/view/insights"

// insightsMetricsExample builds the spec's Metrics worked example: Online Megastore in Sep 2026
// has a refund of +120.00 and purchases of −480.00, negative Net across all rows totals
// −5,844.18, and August Net was −300.00.
func insightsMetricsExample(t testing.TB) []domain.Transaction {
	t.Helper()
	return []domain.Transaction{
		insightsTransaction(t, "m1", "2026-09-03", -16000, "Online Megastore", "Shopping"),
		insightsTransaction(t, "m2", "2026-09-05", 12000, "Online Megastore", "Shopping"),
		insightsTransaction(t, "m3", "2026-09-10", -16000, "Online Megastore", "Shopping"),
		insightsTransaction(t, "m4", "2026-09-20", -16000, "Online Megastore", "Shopping"),
		insightsTransaction(t, "rent", "2026-09-01", -215000, "Maple Court Apartments", "Rent"),
		insightsTransaction(t, "food", "2026-09-12", -333418, "Corner Grocery", "Groceries"),
		insightsTransaction(t, "pay", "2026-09-15", 841072, "Example Employer", "Payroll"),
		insightsTransaction(t, "m0", "2026-08-14", -30000, "Online Megastore", "Shopping"),
	}
}

func insightsTransaction(
	t testing.TB, id, date string, minor int64, merchant, category string,
) domain.Transaction {
	t.Helper()
	return apiTransactionWith(t, id, func(transaction *domain.Transaction) {
		transaction.Date = mustDate(t, date)
		transaction.Amount.Minor = minor
		transaction.Merchant = domain.EntityRef{ID: "merchant-" + merchant, Name: merchant}
		transaction.Category = domain.CategoryRef{
			ID: "category-" + category, Name: category, GroupID: "group-" + category, Group: category,
		}
	})
}

func insightsClock(service *app.Service) {
	today := time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return today })
}

func insightsServer(t testing.TB, transactions []domain.Transaction) *Server {
	t.Helper()
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	insightsClock(service)
	return newAPITestServerForService(t, service)
}

// insightsQuery encodes a USD view of September 2026 grouped by the given dimension.
func insightsQuery(t testing.TB, dimension domain.Dimension) string {
	t.Helper()
	state := app.DefaultViewState()
	state.Current.Dimension = dimension
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	state.Current.DateRange = &domain.DateRange{
		Start: mustDate(t, "2026-09-01"), End: mustDate(t, "2026-09-30"),
	}
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)
	return query
}

func rowIdentity(t testing.TB, server *Server, query, label string) string {
	t.Helper()
	for _, row := range projectPersistentViewForQuery(t, server, query).AggregateRows {
		if row.Label == label {
			return row.Identity
		}
	}
	require.Failf(t, "missing aggregate row", "%s", label)
	return ""
}

func requestInsights(t testing.TB, server *Server, body InsightsBody) InsightsResponse {
	t.Helper()
	response := requestJSON(t, server, insightsPath, body)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var insights InsightsResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &insights))
	return insights
}

func compositionEntry(
	t testing.TB, entries []InsightsCompositionEntry, label string,
) InsightsCompositionEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Label == label {
			return entry
		}
	}
	require.Failf(t, "missing composition entry", "%s", label)
	return InsightsCompositionEntry{}
}

func TestServerInsightsServeTheMetricsWorkedExample(t *testing.T) {
	t.Parallel()
	server := insightsServer(t, insightsMetricsExample(t))
	query := insightsQuery(t, domain.DimensionMerchant)
	megastore := rowIdentity(t, server, query, "Online Megastore")

	insights := requestInsights(t, server, InsightsBody{
		Query: query, Rows: []string{megastore}, Summary: true,
	})

	assert.Equal(t, InsightsSchemaVersion, insights.Version)
	assert.Equal(t, "0", insights.Revision)
	assert.Equal(t, query, insights.Query)
	assert.Equal(t, "USD", insights.Currency)
	assert.Equal(t, uint8(2), insights.Scale)
	assert.Equal(t, "2026-10-08", insights.Today)
	require.NotNil(t, insights.Latest)
	assert.Equal(t, "2026-09-20", *insights.Latest)

	ranges := insights.Ranges
	assert.Equal(t, "month", ranges.Kind)
	assert.Equal(t, &DateRange{Start: "2026-09-01", End: "2026-09-30"}, ranges.Scope)
	assert.Equal(t, "month", ranges.ContextGranularity)
	require.Len(t, ranges.Context, 13)
	assert.Equal(t, InsightsPeriodRange{
		Period: Period{Granularity: "month", Year: 2026, Month: 9},
		Start:  "2026-09-01", End: "2026-09-30",
	}, ranges.Context[12])
	require.Len(t, ranges.Trend, 12)
	assert.Equal(t, "2025-10-01", ranges.Trend[0].Start)
	assert.True(t, ranges.Trend[0].Absent, "months before the first transaction are absent")
	assert.Equal(t, &DateRange{Start: "2026-08-01", End: "2026-08-31"}, ranges.Comparison)
	assert.Equal(t, "vs Aug", ranges.ComparisonLabel)
	assert.Equal(t, &InsightsHeatmap{Layout: "months", Start: "2026-09-01", End: "2026-09-30"},
		ranges.Heatmap)

	require.Len(t, insights.Series, 1)
	series := insights.Series[0]
	assert.Equal(t, megastore, series.Identity)
	require.Len(t, series.Trend, 12)
	assert.Equal(t, "-30000", series.Trend[10])
	assert.Equal(t, "-36000", series.Trend[11])
	require.Len(t, series.Context.In, 13)
	require.Len(t, series.Context.Out, 13)
	assert.Equal(t, "12000", series.Context.In[12])
	assert.Equal(t, "-48000", series.Context.Out[12])
	assert.Equal(t, "0", series.Context.In[0])
	require.NotNil(t, series.PriorNet)
	assert.Equal(t, "-30000", *series.PriorNet)
	assert.Equal(t, &InsightsChange{Kind: "percent", PercentTenths: 200, Grew: true}, series.VsPrior,
		"vs prior shows red ▲ 20.0%")

	summary := insights.Summary
	require.NotNil(t, summary)
	require.NotNil(t, summary.Composition)
	entry := compositionEntry(t, summary.Composition.Spending, "Online Megastore")
	assert.Equal(t, "-36000", entry.Net)
	assert.Equal(t, 62, entry.ShareTenths, "Share is 360.00 ÷ 5,844.18 = 6.2%")
	assert.Equal(t, 0, entry.Others)
	assert.Equal(t, megastore, entry.Identity, "a composition bar links to its table row")
	for _, ranked := range summary.Composition.Spending {
		assert.Equal(t, rowIdentity(t, server, query, ranked.Label), ranked.Identity)
	}
	assert.Equal(t, "Corner Grocery", summary.Composition.Spending[0].Label)
	require.Len(t, summary.Composition.Income, 1)
	assert.Equal(t, "Example Employer", summary.Composition.Income[0].Label)
	assert.Nil(t, summary.PeriodComposition)
	assert.Nil(t, summary.Calendar, "a grouped month view draws no calendar")

	kpis := summary.KPIs
	assert.Equal(t, InsightsTotals{In: "853072", Out: "-596418", Net: "256654", Count: 7}, kpis.Scope)
	assert.Equal(t, &InsightsTotals{In: "0", Out: "-30000", Net: "-30000", Count: 1}, kpis.Comparison)
	assert.Equal(t, &InsightsChange{Kind: "new", Favorable: true}, kpis.IncomeChange)
	assert.Equal(t, &InsightsChange{Kind: "percent", PercentTenths: 18881, Grew: true},
		kpis.SpendingChange)
	amount := "286654"
	assert.Equal(t, &InsightsChange{Kind: "amount", Favorable: true, Amount: &amount}, kpis.NetChange)
	require.NotNil(t, kpis.SavingsRateTenths)
	assert.Equal(t, 301, *kpis.SavingsRateTenths, "2,566.54 of 8,530.72 income is 30.1%")
	assert.Nil(t, kpis.AveragePerTransaction)

	cashflow := summary.Cashflow
	for _, values := range [][]string{cashflow.In, cashflow.Out, cashflow.Net} {
		assert.Len(t, values, 13)
	}
	require.Len(t, cashflow.Labels, 13)
	require.Len(t, cashflow.Count, 13)
	assert.Equal(t, "Sep 2026", cashflow.Labels[12])
	assert.Equal(t, "256654", cashflow.Net[12])
	assert.Equal(t, 7, cashflow.Count[12])
	assert.Equal(t, "-30000", cashflow.Net[11])
}

func TestServerInsightsAverageInsideADrillWithACalendar(t *testing.T) {
	t.Parallel()
	server := insightsServer(t, insightsMetricsExample(t))
	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	state.Current.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	state.Current.DateRange = &domain.DateRange{
		Start: mustDate(t, "2026-09-01"), End: mustDate(t, "2026-09-30"),
	}
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2,
		Key: "merchant-Online Megastore",
	}}
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc,
	}
	query, err := EncodeViewQuery(state)
	require.NoError(t, err)

	insights := requestInsights(t, server, InsightsBody{Query: query, Summary: true})

	require.NotNil(t, insights.Summary)
	require.NotNil(t, insights.Summary.KPIs.AveragePerTransaction)
	assert.Equal(t, "-9000", *insights.Summary.KPIs.AveragePerTransaction,
		"Avg per transaction over 4 non-hidden transactions is −90.00")
	assert.Nil(t, insights.Summary.KPIs.SavingsRateTenths)
	assert.Equal(t, &InsightsHeatmap{Layout: "months", Start: "2026-09-01", End: "2026-09-30"},
		insights.Ranges.Heatmap)
	calendar := insights.Summary.Calendar
	require.NotNil(t, calendar)
	require.Len(t, calendar.In, 30)
	require.Len(t, calendar.Out, 30)
	assert.Equal(t, "12000", calendar.In[4], "September 5 has the refund")
	assert.Equal(t, "-16000", calendar.Out[2], "September 3 has a purchase")
	assert.Equal(t, "0", calendar.Out[0])
	assert.Empty(t, insights.Series)
}

func TestServerInsightsTimeGroupingHasPeriodCompositionAndNoSeries(t *testing.T) {
	t.Parallel()
	server := insightsServer(t, insightsMetricsExample(t))
	query := insightsQuery(t, domain.DimensionTime)

	response := requestJSON(t, server, insightsPath, InsightsBody{Query: query, Summary: true})

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"series":[]`)
	assert.Contains(t, response.Body.String(), `"trend":[]`)
	var insights InsightsResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &insights))
	assert.Equal(t, "day", insights.Ranges.ContextGranularity)
	require.Len(t, insights.Ranges.Context, 30)
	summary := insights.Summary
	require.NotNil(t, summary)
	assert.Nil(t, summary.Composition)
	require.Len(t, summary.PeriodComposition, 30)
	first := summary.PeriodComposition[0]
	require.Len(t, first.Spending, 1)
	assert.Equal(t, InsightsCompositionEntry{
		Key: "group-Rent", Label: "Rent", Net: "-215000", ShareTenths: 1000,
	}, first.Spending[0])
	assert.Empty(t, first.Income)
	assert.Len(t, summary.Cashflow.Net, 30)
}

func TestServerInsightsOthersEntryLinksToNoRow(t *testing.T) {
	t.Parallel()
	var transactions []domain.Transaction
	for index := range 9 {
		merchant := fmt.Sprintf("Example Merchant %d", index)
		transactions = append(transactions, insightsTransaction(
			t, fmt.Sprintf("t%d", index), "2026-09-10", -int64(1000*(index+1)), merchant, "Shopping",
		))
	}
	server := insightsServer(t, transactions)

	insights := requestInsights(t, server, InsightsBody{
		Query: insightsQuery(t, domain.DimensionMerchant), Summary: true,
	})

	require.NotNil(t, insights.Summary)
	spending := insights.Summary.Composition.Spending
	require.Len(t, spending, 8)
	others := spending[7]
	assert.Equal(t, 2, others.Others)
	assert.Empty(t, others.Key)
	assert.Empty(t, others.Identity, "the others entry is not a table row")
	assert.NotEmpty(t, spending[0].Identity)
}

func TestServerInsightsCollapseDuplicateRowsAndSkipTheSummary(t *testing.T) {
	t.Parallel()
	server := insightsServer(t, insightsMetricsExample(t))
	query := insightsQuery(t, domain.DimensionMerchant)
	megastore := rowIdentity(t, server, query, "Online Megastore")
	grocery := rowIdentity(t, server, query, "Corner Grocery")

	insights := requestInsights(t, server, InsightsBody{
		Query: query, Rows: []string{grocery, megastore, grocery, megastore},
	})

	require.Len(t, insights.Series, 2)
	assert.Equal(t, grocery, insights.Series[0].Identity)
	assert.Equal(t, megastore, insights.Series[1].Identity)
	assert.Equal(t, "-333418", insights.Series[0].Context.Out[12])
	assert.Nil(t, insights.Summary)
}

func TestServerInsightsRejectInvalidRequests(t *testing.T) {
	t.Parallel()
	server := insightsServer(t, insightsMetricsExample(t))
	query := insightsQuery(t, domain.DimensionMerchant)
	megastore := rowIdentity(t, server, query, "Online Megastore")
	category := rowIdentity(t, server, insightsQuery(t, domain.DimensionCategory), "Shopping")
	euro := app.AggregateIdentity(domain.AggregateRow{
		Dimension: domain.DimensionMerchant, Key: "merchant-Online Megastore",
		Total: domain.Money{Currency: "EUR", Scale: 2},
	})
	tooMany := make([]string, app.MaxInsightsRows+1)
	for index := range tooMany {
		tooMany[index] = megastore
	}
	allCurrencies := app.DefaultViewState()
	allCurrencies.Current.DateRange = &domain.DateRange{
		Start: mustDate(t, "2026-09-01"), End: mustDate(t, "2026-09-30"),
	}
	noCurrency, err := EncodeViewQuery(allCurrencies)
	require.NoError(t, err)
	tests := []struct {
		name string
		body InsightsBody
		code string
	}{
		{name: "missing currency", body: InsightsBody{Query: noCurrency}, code: "invalid_web_request"},
		{
			name: "malformed row", body: InsightsBody{Query: query, Rows: []string{"bogus"}},
			code: "invalid_web_request",
		},
		{
			name: "row of another grouping", body: InsightsBody{Query: query, Rows: []string{category}},
			code: "invalid_web_request",
		},
		{
			name: "more than 200 rows", body: InsightsBody{Query: query, Rows: tooMany},
			code: "invalid_web_request",
		},
		{
			name: "row in another currency", body: InsightsBody{Query: query, Rows: []string{euro}},
			code: "invalid_web_request",
		},
		{name: "bad query", body: InsightsBody{Query: "v=1&bogus=1"}, code: "invalid_view_state"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestJSON(t, server, insightsPath, test.body)
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			assert.Contains(t, response.Header().Get("Content-Type"), "application/problem+json")
			var problem Problem
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &problem))
			assert.Equal(t, test.code, problem.Code)
		})
	}
}

func TestServerInsightsIgnorePendingEditsAndNeedNoMutationToken(t *testing.T) {
	t.Parallel()
	type pendingEdit struct {
		grouping domain.Dimension
		row      string
		apply    func(testing.TB, *Server)
	}
	edits := map[string]pendingEdit{
		"rename": {grouping: domain.DimensionMerchant, row: "Online Megastore",
			apply: func(t testing.TB, server *Server) {
				appendMerchantMutation(t, server, "Renamed Merchant")
			}},
		"hide": {grouping: domain.DimensionMerchant, row: "Online Megastore",
			apply: func(t testing.TB, server *Server) {
				mutateMegastorePurchase(t, server, app.ActionToggleHidden, MutationInput{})
			}},
		"recategorize": {grouping: domain.DimensionCategory, row: "Shopping",
			apply: func(t testing.TB, server *Server) {
				mutateMegastorePurchase(t, server, app.ActionEditCategory, MutationInput{
					Scope: string(app.EditScopeTransactions), DestinationID: "category-Rent",
				})
			}},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			service := newPersistentAPITestService(t, insightsMetricsExample(t))
			insightsClock(service)
			server := newAPITestServerForService(t, service)
			query := insightsQuery(t, edit.grouping)
			row := rowIdentity(t, server, query, edit.row)
			body := InsightsBody{Query: query, Rows: []string{row}, Summary: true}
			path := fmt.Sprintf("/api/v1/profiles/%s/view/insights", testProfileID)
			before := requestJSON(t, server, path, body)
			require.Equal(t, http.StatusOK, before.Code, before.Body.String())

			edit.apply(t, server)
			require.NotZero(t, projectPersistentView(t, server).Pending.ActiveOperations)

			after := requestJSON(t, server, path, body)
			require.Equal(t, http.StatusOK, after.Code, after.Body.String())
			var beforeBody, afterBody map[string]any
			require.NoError(t, json.Unmarshal(before.Body.Bytes(), &beforeBody))
			require.NoError(t, json.Unmarshal(after.Body.Bytes(), &afterBody))
			assert.Equal(t, "1", beforeBody["revision"])
			assert.Equal(t, "2", afterBody["revision"], "the pending edit advances the revision")
			delete(beforeBody, "revision")
			delete(afterBody, "revision")
			assert.Equal(t, beforeBody, afterBody, "insights use committed values")
		})
	}
}

// mutateMegastorePurchase stages one edit of a September Online Megastore purchase.
func mutateMegastorePurchase(
	t testing.TB, server *Server, action app.ActionID, input MutationInput,
) {
	t.Helper()
	view := projectPersistentViewForQuery(t, server, "mode=detail&v=1")
	var target DetailRow
	for _, row := range view.DetailRows {
		if row.Merchant == "Online Megastore" && row.Date == "2026-09-03" {
			target = row
		}
	}
	require.NotEmpty(t, target.Identity)
	response := requestProtectedJSON(t, server, "/api/v1/mutations", MutationBody{
		Version: MutationSchemaVersion, ExpectedRevision: view.Revision,
		Query: view.CanonicalQuery, Selection: view.Selection, Action: action,
		Target: &TransitionTarget{Kind: app.IdentityTransaction, Identity: target.Identity},
		Input:  input, Window: Window{Limit: 200},
	})
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
}
