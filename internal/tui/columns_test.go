package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/domain"
)

func TestColumnsAggregateLayouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		width      int
		wantStarts []int
	}{
		{150, []int{1, 23, 35, 48, 56, 93}},
		{120, []int{1, 23, 35, 48, 56, 93}},
		{80, []int{1, 15, 27, 40, 48, 78}},
	}
	for _, test := range tests {
		columns := AggregateColumns(test.width, domain.DimensionMerchant, domain.SortSpec{
			Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc,
		})
		assert.Equal(t, test.wantStarts, ColumnStarts(columns))
		assert.Equal(t, AlignLeft, columns[1].Align)
		assert.Equal(t, AlignRight, columns[2].Align)
		assert.Equal(t, "Total ($) ↓", columns[2].Label)
		assert.LessOrEqual(t, columns[len(columns)-1].Start+columns[len(columns)-1].Width, test.width)
	}
}

func TestColumnsDetailLayouts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		width      int
		wantStarts []int
	}{
		{150, []int{1, 15, 37, 60, 84, 100}},
		{120, []int{1, 15, 37, 60, 84, 100}},
		{80, []int{1, 15, 29, 44, 61, 77}},
	}
	for _, test := range tests {
		columns := DetailColumns(test.width, domain.SortSpec{
			Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc,
		})
		assert.Equal(t, test.wantStarts, ColumnStarts(columns))
		assert.Equal(t, "Date ↓", columns[0].Label)
		assert.Equal(t, AlignRight, columns[4].Align)
	}
}

func TestProfileDetailColumnsUseAmazonLabelsAndBoundedMatchColumn(t *testing.T) {
	t.Parallel()
	columns := ProfileDetailColumns(150, domain.SortSpec{}, "amazon", false)
	assert.Equal(t, "Product", columns[1].Label)
	assert.Equal(t, "Order", columns[3].Label)

	matched := ProfileDetailColumns(150, domain.SortSpec{}, "monarch", true)
	labels := make([]string, len(matched))
	for index, column := range matched {
		labels[index] = column.Label
	}
	assert.Contains(t, labels, "Amazon match")
}

func TestColumnsNeverEscapeNarrowPositiveWidth(t *testing.T) {
	t.Parallel()

	for width := 1; width <= 79; width++ {
		for _, columns := range [][]Column{
			AggregateColumns(width, domain.DimensionCategory, domain.SortSpec{
				Field: domain.SortFieldCategory, Direction: domain.SortDirectionAsc,
			}),
			DetailColumns(width, domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionAsc}),
		} {
			for _, column := range columns {
				assert.GreaterOrEqual(t, column.Start, 0)
				assert.GreaterOrEqual(t, column.Width, 0)
				assert.LessOrEqual(t, column.Start+column.Width, width)
			}
		}
	}
}
