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
	Key      string
	Label    string
	Start    int
	Width    int
	Align    Alignment
	HardClip bool
}

// AggregateColumns returns stable aggregate columns fitted to the available width.
func AggregateColumns(width int, dimension domain.Dimension, sortSpec domain.SortSpec) []Column {
	nameKey, nameLabel := dimensionColumn(dimension)
	columns := []Column{
		{Key: nameKey, Label: withArrow(nameLabel, sortFieldForDimension(dimension), sortSpec)},
		{Key: "count", Label: withArrow("Count", domain.SortFieldCount, sortSpec)},
		{Key: "in", Label: "In ($)", Align: AlignRight},
		{Key: "out", Label: "Out ($)", Align: AlignRight},
		{Key: "total", Label: withArrow("Net ($)", domain.SortFieldAmount, sortSpec), Align: AlignRight},
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
	widths := []int{nameWidth, 7, 11, 11, 11, 6}
	flexible := []int{0}
	// Keep all three amounts readable at 80 columns. Top category is secondary.
	if dimension == domain.DimensionMerchant && width >= 100 {
		columns = append(columns, Column{Key: "top_category", Label: "Top Category"})
		widths = append(widths, 35)
		flexible = append(flexible, 6)
	}
	columns = append(columns, Column{Key: "flags"})
	widths = fitColumnWidths(width, append(widths, 2), flexible)
	return placeColumns(width, columns, widths)
}

// DetailColumns returns detail columns fitted to the available width.
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
	if profileKind == "amazon" {
		// Product names get most of the available line; order IDs stay secondary.
		widths = []int{12, 60, 21, 20, 14}
		flexible = []int{1, 2, 3}
	}
	if amazonMatchColumn {
		columns = append(columns, Column{Key: "amazon_match", Label: "Amazon"})
		// Reserve room for the matched product indicator by narrowing the merchant column.
		widths = []int{12, 15, 21, 22, 14, 40}
		columns[1].HardClip = true
		flexible = append(flexible, 5)
	}
	columns = append(columns, Column{Key: "flags"})
	widths = append(widths, 3)
	widths = fitColumnWidths(width, widths, flexible)
	return placeColumns(width, columns, widths)
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

func fitColumnWidths(totalWidth int, widths []int, flexible []int) []int {
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
