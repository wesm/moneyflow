package tui

import (
	"github.com/wesm/moneyflow/internal/domain"
)

// Alignment controls how a formatted value occupies its column.
type Alignment uint8

// Supported column alignments.
const (
	AlignLeft Alignment = iota
	AlignRight
)

// Column is a deterministic table column and its absolute cell origin.
type Column struct {
	Key   string
	Label string
	Start int
	Width int
	Align Alignment
}

// AggregateColumns returns stable aggregate columns fitted to the available width.
func AggregateColumns(width int, dimension domain.Dimension, sortSpec domain.SortSpec) []Column {
	nameKey, nameLabel := dimensionColumn(dimension)
	columns := []Column{
		{Key: nameKey, Label: withArrow(nameLabel, sortFieldForDimension(dimension), sortSpec)},
		{Key: "count", Label: withArrow("Count", domain.SortFieldCount, sortSpec), Align: AlignRight},
		{Key: "total", Label: withArrow("Total ($)", domain.SortFieldAmount, sortSpec), Align: AlignRight},
		{Key: "pct", Label: "%", Align: AlignRight},
	}
	var widths []int
	if dimension == domain.DimensionMerchant {
		columns = append(columns, Column{Key: "top_category", Label: "Top Category"})
		columns = append(columns, Column{Key: "flags"})
		if width >= 120 {
			widths = []int{width - 73, 10, 14, 7, 35, 2}
		} else {
			widths = []int{width - 55, 8, 13, 6, 21, 2}
		}
	} else {
		columns = append(columns, Column{Key: "flags"})
		widths = []int{width - 39, 10, 14, 6, 5}
		if width < 80 {
			widths = []int{width - 31, 8, 13, 5, 1}
		}
	}
	return placeColumns(width, columns, widths)
}

// DetailColumns returns the Python-compatible detail columns fitted to width.
func DetailColumns(width int, sortSpec domain.SortSpec) []Column {
	columns := []Column{
		{Key: "date", Label: withArrow("Date", domain.SortFieldDate, sortSpec)},
		{Key: "merchant", Label: withArrow("Merchant", domain.SortFieldMerchant, sortSpec)},
		{Key: "category", Label: withArrow("Category", domain.SortFieldCategory, sortSpec)},
		{Key: "account", Label: withArrow("Account", domain.SortFieldAccount, sortSpec)},
		{Key: "amount", Label: withArrow("Amount ($)", domain.SortFieldAmount, sortSpec), Align: AlignRight},
		{Key: "flags"},
	}
	widths := []int{12, 20, 21, 22, 14, 3}
	if width < 98 {
		flexible := width - 34
		if flexible < 0 {
			flexible = 0
		}
		merchant := flexible / 3
		category := flexible / 3
		account := flexible - merchant - category
		widths = []int{12, merchant, category, account, 14, 3}
	}
	return placeColumns(width, columns, widths)
}

// ColumnStarts projects the stable semantic positions used by parity artifacts.
func ColumnStarts(columns []Column) []int {
	starts := make([]int, len(columns))
	for index, column := range columns {
		starts[index] = column.Start
	}
	return starts
}

func placeColumns(totalWidth int, columns []Column, widths []int) []Column {
	if totalWidth < 0 {
		totalWidth = 0
	}
	start := 0
	for index := range columns {
		if start > totalWidth {
			start = totalWidth
		}
		columnWidth := widths[index]
		if columnWidth < 0 {
			columnWidth = 0
		}
		if columnWidth > totalWidth-start {
			columnWidth = totalWidth - start
		}
		columns[index].Start = start
		columns[index].Width = columnWidth
		start += columnWidth
		if index != len(columns)-1 && start < totalWidth {
			start++
		}
	}
	return columns
}

func dimensionColumn(dimension domain.Dimension) (string, string) {
	switch dimension {
	case domain.DimensionMerchant:
		return "merchant", "Merchant"
	case domain.DimensionCategory:
		return "category", "Category"
	case domain.DimensionGroup:
		return "group", "Group"
	case domain.DimensionAccount:
		return "account", "Account"
	default:
		return "time_period", "Period"
	}
}

func sortFieldForDimension(dimension domain.Dimension) domain.SortField {
	switch dimension {
	case domain.DimensionMerchant:
		return domain.SortFieldMerchant
	case domain.DimensionCategory:
		return domain.SortFieldCategory
	case domain.DimensionGroup:
		return domain.SortFieldGroup
	case domain.DimensionAccount:
		return domain.SortFieldAccount
	default:
		return domain.SortFieldTimePeriod
	}
}

func withArrow(label string, field domain.SortField, spec domain.SortSpec) string {
	if spec.Field != field {
		return label
	}
	return label + " " + SortArrow(spec.Direction)
}
