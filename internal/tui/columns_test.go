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
		{150, []int{0, 78, 89, 104, 112, 148}},
		{120, []int{0, 48, 59, 74, 82, 118}},
		{80, []int{0, 26, 35, 49, 56, 78}},
	}
	for _, test := range tests {
		columns := AggregateColumns(test.width, domain.DimensionMerchant, domain.SortSpec{
			Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc,
		})
		assert.Equal(t, test.wantStarts, ColumnStarts(columns))
		assert.Equal(t, AlignRight, columns[1].Align)
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
		{150, []int{0, 13, 34, 56, 79, 94}},
		{120, []int{0, 13, 34, 56, 79, 94}},
		{80, []int{0, 13, 29, 45, 62, 77}},
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
