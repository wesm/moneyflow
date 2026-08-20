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
		{Key: "count", Label: withArrow("Count", domain.SortFieldCount, sortSpec)},
		{Key: "total", Label: withArrow("Total ($)", domain.SortFieldAmount, sortSpec), Align: AlignRight},
		{Key: "pct", Label: "%"},
	}
	nameWidth := 40
	switch dimension {
	case domain.DimensionMerchant:
		nameWidth = 20
	case domain.DimensionAccount:
		nameWidth = 22
	case domain.DimensionTime:
		nameWidth = 15
	}
	totalWidth := 11
	if dimension == domain.DimensionTime && sortSpec.Field != domain.SortFieldAmount {
		totalWidth = 9
	}
	var widths []int
	if dimension == domain.DimensionMerchant {
		columns = append(columns, Column{Key: "top_category", Label: "Top Category"})
		columns = append(columns, Column{Key: "flags"})
		widths = fitPythonWidths(width, []int{nameWidth, 10, totalWidth, 6, 35, 2}, []int{0, 4})
	} else {
		columns = append(columns, Column{Key: "flags"})
		widths = fitPythonWidths(width, []int{nameWidth, 10, totalWidth, 6, 2}, []int{0})
	}
	return placeColumns(width, columns, widths)
}

// DetailColumns returns the Python-compatible detail columns fitted to width.
func DetailColumns(width int, sortSpec domain.SortSpec) []Column {
	return ProfileDetailColumns(width, sortSpec, "local", false)
}

// ProfileDetailColumns adapts semantic labels and the bounded Amazon match column by profile.
func ProfileDetailColumns(
	width int,
	sortSpec domain.SortSpec,
	profileKind string,
	amazonMatchColumn bool,
) []Column {
	merchantLabel, accountLabel := "Merchant", "Account"
	if profileKind == "amazon" {
		merchantLabel, accountLabel = "Product", "Order"
	}
	columns := []Column{
		{Key: "date", Label: withArrow("Date", domain.SortFieldDate, sortSpec)},
		{Key: "merchant", Label: withArrow(merchantLabel, domain.SortFieldMerchant, sortSpec)},
		{Key: "category", Label: withArrow("Category", domain.SortFieldCategory, sortSpec)},
		{Key: "account", Label: withArrow(accountLabel, domain.SortFieldAccount, sortSpec)},
		{Key: "amount", Label: withArrow("Amount ($)", domain.SortFieldAmount, sortSpec), Align: AlignRight},
	}
	widths := []int{12, 20, 21, 22, 14}
	flexible := []int{1, 2, 3}
	if amazonMatchColumn {
		columns = append(columns, Column{Key: "amazon_match", Label: "Amazon match"})
		widths = append(widths, 22)
		flexible = append(flexible, 5)
	}
	columns = append(columns, Column{Key: "flags"})
	widths = append(widths, 3)
	widths = fitPythonWidths(width, widths, flexible)
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
	start := 1
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
			start += min(2, totalWidth-start)
		}
	}
	return columns
}

func fitPythonWidths(totalWidth int, widths []int, flexible []int) []int {
	result := append([]int(nil), widths...)
	required := 1 + 2*(len(result)-1)
	for _, width := range result {
		required += width
	}
	for required > totalWidth {
		changed := false
		for _, index := range flexible {
			if result[index] > 1 && required > totalWidth {
				result[index]--
				required--
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return result
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
