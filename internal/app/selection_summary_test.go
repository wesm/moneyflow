package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

func summaryTransactions(t *testing.T) []domain.Transaction {
	t.Helper()
	hidden := appTransaction(t, "txn-c", "2024-01-03", "-7.00", "Gamma", "Category", "Group")
	hidden.Hidden = true
	euro := appTransaction(t, "txn-d", "2024-01-04", "-2.00", "Delta", "Category", "Group")
	euro.Amount.Currency = "EUR"
	return []domain.Transaction{
		appTransaction(t, "txn-a", "2024-01-01", "-5.00", "Alpha", "Category", "Group"),
		appTransaction(t, "txn-b", "2024-01-02", "3.00", "Beta", "Category", "Group"),
		hidden,
		euro,
	}
}

func summaryProjection(
	t *testing.T,
	service *Service,
	state AnalyticalState,
	selection SelectionValue,
) WebProjection {
	t.Helper()
	projection, err := service.ProjectView(
		ViewState{Version: ViewStateSchemaVersion, Current: state}, selection, WindowRequest{},
	)
	require.NoError(t, err)
	return projection
}

func toggled(
	t *testing.T,
	service *Service,
	state AnalyticalState,
	kind IdentityKind,
	identities ...string,
) SelectionValue {
	t.Helper()
	selection := EmptySelection()
	var err error
	for _, identity := range identities {
		selection, err = service.ToggleSelection(state, selection, kind, identity)
		require.NoError(t, err)
	}
	return selection
}

func usdStats(t *testing.T, count int, in, out string) domain.CurrencyStats {
	t.Helper()
	inMoney, err := domain.ParseMoney(in, "USD", 2)
	require.NoError(t, err)
	outMoney, err := domain.ParseMoney(out, "USD", 2)
	require.NoError(t, err)
	net, err := inMoney.Add(outMoney)
	require.NoError(t, err)
	return domain.CurrencyStats{
		Currency: "USD", Scale: 2, Count: count, In: inMoney, Out: outMoney, Net: net,
	}
}

func TestSelectionSummaryCountsHiddenTransactionsWithoutMoney(t *testing.T) {
	t.Parallel()

	service, err := NewService(summaryTransactions(t))
	require.NoError(t, err)
	state := selectionDetailState()
	selection := toggled(t, service, state, IdentityTransaction, "txn-a", "txn-c")

	projection := summaryProjection(t, service, state, selection)

	assert.Equal(t, []domain.CurrencyStats{usdStats(t, 2, "0.00", "-5.00")},
		projection.SelectionSummary)
}

func TestSelectionSummarySumsSelectedMerchants(t *testing.T) {
	t.Parallel()

	service, err := NewService(summaryTransactions(t))
	require.NoError(t, err)
	state := DefaultViewState().Current
	identities := make(map[string]string)
	for _, row := range summaryProjection(t, service, state, EmptySelection()).AggregateRows {
		identities[row.Row.Label] = row.Identity
	}
	alpha, beta := identities["Alpha"], identities["Beta"]
	require.NotEmpty(t, alpha)
	require.NotEmpty(t, beta)
	selection := toggled(t, service, state, IdentityAggregate, alpha, beta)

	projection := summaryProjection(t, service, state, selection)

	assert.Equal(t, []domain.CurrencyStats{usdStats(t, 2, "3.00", "-5.00")},
		projection.SelectionSummary)
}

func TestSelectionSummaryOfSelectAllEqualsStatistics(t *testing.T) {
	t.Parallel()

	service, err := NewService(summaryTransactions(t))
	require.NoError(t, err)
	grouped := DefaultViewState().Current
	timeGrouped := DefaultViewState().Current
	timeGrouped.Dimension = domain.DimensionTime
	timeGrouped.TimeGranularity = domain.TimeGranularityMonth
	for name, state := range map[string]AnalyticalState{
		"detail": selectionDetailState(), "merchant": grouped, "time": timeGrouped,
	} {
		selection, err := service.ToggleAllSelection(state, EmptySelection())
		require.NoError(t, err, name)

		projection := summaryProjection(t, service, state, selection)

		assert.Equal(t, projection.Statistics, projection.SelectionSummary, name)
	}
}

func TestSelectionSummaryKeepsCurrenciesApart(t *testing.T) {
	t.Parallel()

	service, err := NewService(summaryTransactions(t))
	require.NoError(t, err)
	state := selectionDetailState()
	selection := toggled(t, service, state, IdentityTransaction, "txn-d", "txn-b")

	projection := summaryProjection(t, service, state, selection)

	require.Len(t, projection.SelectionSummary, 2)
	assert.Equal(t, domain.Currency("EUR"), projection.SelectionSummary[0].Currency)
	assert.Equal(t, int64(-200), projection.SelectionSummary[0].Out.Minor)
	assert.Equal(t, usdStats(t, 1, "3.00", "0.00"), projection.SelectionSummary[1])
}

func TestSelectionSummaryIsNilWithoutSelection(t *testing.T) {
	t.Parallel()

	service, err := NewService(summaryTransactions(t))
	require.NoError(t, err)
	for _, state := range []AnalyticalState{selectionDetailState(), DefaultViewState().Current} {
		projection := summaryProjection(t, service, state, EmptySelection())
		assert.Nil(t, projection.SelectionSummary)
	}
}

func TestSelectionSummaryMatchesAnalyticsStatisticsForDetailSelection(t *testing.T) {
	t.Parallel()

	transactions := summaryTransactions(t)
	rows := make([]domain.DetailRow, len(transactions))
	for index, transaction := range transactions {
		rows[index] = domain.DetailRow{
			Transaction: transaction, Flags: domain.RowFlags{Selected: index != 1},
		}
	}

	got, err := selectionSummary(domain.QueryResult{DetailRows: rows})

	require.NoError(t, err)
	want, err := analytics.Statistics([]domain.Transaction{
		transactions[0], transactions[2], transactions[3],
	})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestSelectionSummaryCoversSelectedTransactionsHiddenBySearch(t *testing.T) {
	t.Parallel()

	service, err := NewService(summaryTransactions(t))
	require.NoError(t, err)
	state := selectionDetailState()
	selection := toggled(t, service, state, IdentityTransaction, "txn-a", "txn-b")
	session := NewSession()
	session.ShowAllDetail()
	session.SetSearch("Alpha")

	projection := summaryProjection(t, service, session.ViewState().Current, selection)

	require.Equal(t, 2, projection.SelectionCount)
	require.Equal(t, 1, projection.TotalRows)
	assert.Equal(t, []domain.CurrencyStats{usdStats(t, 2, "3.00", "-5.00")},
		projection.SelectionSummary)
	var count int
	for _, stats := range projection.SelectionSummary {
		count += stats.Count
	}
	assert.Equal(t, projection.SelectionCount, count)
}
