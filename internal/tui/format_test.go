package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestFormatAmount(t *testing.T) {
	t.Parallel()

	tests := map[int64]string{
		-123456: "-1,234.56",
		500000:  "+5,000.00",
		0:       "+0.00",
		1:       "+0.01",
	}
	for minor, want := range tests {
		money := domain.Money{Minor: minor, Currency: "USD", Scale: 2}
		assert.Equal(t, want, FormatAmount(money))
	}
}

func TestFormatPeriodPercentageFlagsAndSortArrow(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2024", FormatPeriod(domain.Period{Granularity: domain.TimeGranularityYear, Year: 2024}))
	assert.Equal(t, "Mar 2024", FormatPeriod(domain.Period{
		Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 3,
	}))
	assert.Equal(t, "2024-03-15", FormatPeriod(domain.Period{
		Granularity: domain.TimeGranularityDay, Year: 2024, Month: 3, Day: 15,
	}))
	assert.Equal(t, "12.5%", FormatPercent(125))
	assert.Equal(t, "10.0%", FormatPercent(100))
	assert.Equal(t, "✓H", FormatFlags(domain.RowFlags{Selected: true, Hidden: true, Pending: true}))
	assert.Equal(t, "", FormatFlags(domain.RowFlags{Pending: true}))
	assert.Equal(t, "↑", SortArrow(domain.SortDirectionAsc))
	assert.Equal(t, "↓", SortArrow(domain.SortDirectionDesc))
}

func TestFormatStatisticsAndEmptyState(t *testing.T) {
	t.Parallel()

	stats := []domain.CurrencyStats{{
		Currency: "USD", Scale: 2, Count: 3,
		In:  domain.Money{Minor: 500000, Currency: "USD", Scale: 2},
		Out: domain.Money{Minor: -123456, Currency: "USD", Scale: 2},
		Net: domain.Money{Minor: 376544, Currency: "USD", Scale: 2},
	}}
	assert.Equal(t, "3 txns | In: +5,000.00 | Out: -1,234.56 | Net: +3,765.44", FormatStatistics(stats))
	assert.Equal(t, "0 txns | No data in view", FormatStatistics(nil))
	assert.Equal(t, "No transactions in view", EmptyStateText(domain.ResultModeDetail))
	assert.Equal(t, "No groups in view", EmptyStateText(domain.ResultModeAggregate))
}

func TestTruncateUsesDisplayWidth(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Example…", Truncate("Example Grocer", 8))
	assert.Equal(t, "界…", Truncate("界界界", 3))
	assert.Equal(t, "", Truncate("anything", 0))
}
