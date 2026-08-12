package analytics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestFilterCurrentSemantics(t *testing.T) {
	t.Parallel()

	transactions := []domain.Transaction{
		testTransaction(t, "txn-1", "2024-02-28", "-10.00", "Example Grocer", "Groceries", "Living"),
		testTransaction(t, "txn-2", "2024-02-29", "-20.00", "Example Cafe", "Dining", "Living"),
		testTransaction(t, "txn-3", "2024-03-01", "-30.00", "Example Transfer", "Transfer", "Transfers"),
		testTransaction(t, "txn-4", "2024-03-02", "-40.00", "Example Utility", "Utilities", "Living"),
	}
	transactions[1].Hidden = true
	transactions[3].Account = domain.EntityRef{ID: "account-checking", Name: "Primary Checking"}

	start, err := domain.ParseDate("2024-02-28")
	require.NoError(t, err)
	end, err := domain.ParseDate("2024-02-29")
	require.NoError(t, err)

	tests := map[string]struct {
		spec domain.QuerySpec
		want []string
	}{
		"inclusive dates": {
			spec: domain.QuerySpec{DateRange: &domain.DateRange{Start: start, End: end}, Mode: domain.ResultModeDetail},
			want: []string{"txn-1", "txn-2"},
		},
		"case insensitive regex over merchant": {
			spec: domain.QuerySpec{Search: `GROC(ER|ERY)`, Mode: domain.ResultModeDetail},
			want: []string{"txn-1"},
		},
		"regex over category": {
			spec: domain.QuerySpec{Search: `dining|utilit`, Mode: domain.ResultModeDetail},
			want: []string{"txn-2", "txn-4"},
		},
		"search excludes account and metadata": {
			spec: domain.QuerySpec{Search: `checking|test`, Mode: domain.ResultModeDetail},
			want: []string{},
		},
		"transfers off": {
			spec: domain.QuerySpec{ShowTransfers: false, Mode: domain.ResultModeDetail},
			want: []string{"txn-1", "txn-2", "txn-4"},
		},
		"transfers on": {
			spec: domain.QuerySpec{ShowTransfers: true, Mode: domain.ResultModeDetail},
			want: []string{"txn-1", "txn-2", "txn-3", "txn-4"},
		},
		"hidden off aggregate": {
			spec: domain.QuerySpec{ShowTransfers: true, ShowHidden: false, Mode: domain.ResultModeAggregate},
			want: []string{"txn-1", "txn-3", "txn-4"},
		},
		"hidden retained detail": {
			spec: domain.QuerySpec{ShowTransfers: true, ShowHidden: false, Mode: domain.ResultModeDetail},
			want: []string{"txn-1", "txn-2", "txn-3", "txn-4"},
		},
		"intersecting drilldowns": {
			spec: domain.QuerySpec{
				ShowTransfers: true,
				Mode:          domain.ResultModeDetail,
				Drilldowns: []domain.Drilldown{
					{Dimension: domain.DimensionMerchant, Key: "ignored", Label: "Example Utility"},
					{Dimension: domain.DimensionAccount, Key: "ignored", Label: "Primary Checking"},
					{Dimension: domain.DimensionTime, Period: &domain.Period{Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3}},
				},
			},
			want: []string{"txn-4"},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, filterErr := Filter(transactions, test.spec)
			require.NoError(t, filterErr)
			assert.Equal(t, test.want, ids(got))
			assert.NotNil(t, got)
		})
	}
}

func TestFilterRejectsInvalidRegex(t *testing.T) {
	t.Parallel()

	_, err := Filter(nil, domain.QuerySpec{Search: "[", Mode: domain.ResultModeDetail})
	assert.ErrorContains(t, err, "search")
}

func TestFilterDoesNotMutateOrAliasInput(t *testing.T) {
	t.Parallel()

	transactions := []domain.Transaction{
		testTransaction(t, "txn-1", "2024-01-01", "-1.00", "Example Grocer", "Groceries", "Living"),
	}
	filtered, err := Filter(transactions, domain.QuerySpec{ShowTransfers: true, Mode: domain.ResultModeDetail})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	filtered[0].Metadata["source"] = "changed"

	assert.Equal(t, "test", transactions[0].Metadata["source"])
}
