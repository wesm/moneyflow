// Package tui formats renderer-neutral application results for terminal cells.
package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/wesm/moneyflow/internal/domain"
)

// FormatAmount renders exact money with an explicit sign and grouped integer digits.
func FormatAmount(money domain.Money) string {
	decimal := money.DecimalString()
	sign := "+"
	if strings.HasPrefix(decimal, "-") {
		sign = "-"
		decimal = strings.TrimPrefix(decimal, "-")
	}
	parts := strings.SplitN(decimal, ".", 2)
	integer := parts[0]
	for index := len(integer) - 3; index > 0; index -= 3 {
		integer = integer[:index] + "," + integer[index:]
	}
	if len(parts) == 1 {
		return sign + integer
	}
	return sign + integer + "." + parts[1]
}

// FormatPeriod renders the typed granularity used in time rows and breadcrumbs.
func FormatPeriod(period domain.Period) string {
	switch period.Granularity {
	case domain.TimeGranularityYear:
		return fmt.Sprintf("%04d", period.Year)
	case domain.TimeGranularityMonth:
		if period.Month < 1 || period.Month > 12 {
			return ""
		}
		return fmt.Sprintf("%s %04d", time.Month(period.Month).String()[:3], period.Year)
	case domain.TimeGranularityDay:
		return fmt.Sprintf("%04d-%02d-%02d", period.Year, period.Month, period.Day)
	default:
		return ""
	}
}

// FormatPercent renders signed-share tenths as one decimal percentage.
func FormatPercent(tenths int) string {
	sign := ""
	if tenths < 0 {
		sign = "-"
		tenths = -tenths
	}
	return fmt.Sprintf("%s%d.%d%%", sign, tenths/10, tenths%10)
}

// FormatFlags renders only selection and hidden-state markers in the read-only slice.
func FormatFlags(flags domain.RowFlags) string {
	var result strings.Builder
	if flags.Selected {
		result.WriteString("✓")
	}
	if flags.Hidden {
		result.WriteString("H")
	}
	return result.String()
}

// SortArrow renders the active sort direction.
func SortArrow(direction domain.SortDirection) string {
	if direction == domain.SortDirectionAsc {
		return "↑"
	}
	return "↓"
}

// FormatStatistics renders every currency partition in deterministic service order.
func FormatStatistics(statistics []domain.CurrencyStats) string {
	if len(statistics) == 0 {
		return "0 txns | No data in view"
	}
	parts := make([]string, len(statistics))
	for index, stats := range statistics {
		parts[index] = fmt.Sprintf(
			"%d txns | In: %s | Out: %s | Net: %s",
			stats.Count,
			FormatAmount(stats.In),
			FormatAmount(stats.Out),
			FormatAmount(stats.Net),
		)
		if len(statistics) > 1 {
			parts[index] = string(stats.Currency) + ": " + parts[index]
		}
	}
	return strings.Join(parts, " | ")
}

// EmptyStateText describes the expected row shape without renderer-specific markup.
func EmptyStateText(mode domain.ResultMode) string {
	if mode == domain.ResultModeDetail {
		return "No transactions in view"
	}
	return "No groups in view"
}

// Truncate clips text to terminal cells and reserves one cell for an ellipsis.
func Truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var result strings.Builder
	used := 0
	for _, cluster := range graphemeClusters(value) {
		if used+cluster.width > width-1 {
			break
		}
		result.WriteString(cluster.value)
		used += cluster.width
	}
	return result.String() + "…"
}
