package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// Breadcrumb returns the full renderer-independent navigation path.
func (session Session) Breadcrumb(resultDateRange *domain.DateRange) string {
	if session.Mode == domain.ResultModeAggregate && len(session.Drilldowns) == 0 && session.SubGrouping == nil {
		return session.topLevelBreadcrumb(resultDateRange)
	}
	parts := make([]string, 0, len(session.Drilldowns)+1)
	for _, drilldown := range session.Drilldowns {
		parts = append(parts, drilldownBreadcrumb(drilldown))
	}
	if len(parts) == 0 {
		parts = append(parts, "Transactions")
	}
	if session.SubGrouping != nil {
		parts = append(parts, "(by "+dimensionLabel(*session.SubGrouping, false)+")")
	}
	return strings.Join(parts, " > ")
}

func (session Session) topLevelBreadcrumb(dateRange *domain.DateRange) string {
	label := dimensionLabel(session.Dimension, true)
	if session.Dimension == domain.DimensionTime {
		switch session.TimeGranularity {
		case domain.TimeGranularityYear:
			label = "Years"
		case domain.TimeGranularityMonth:
			label = "Months"
		case domain.TimeGranularityDay:
			label = "Days"
		}
	}
	if dateRange != nil {
		label += fmt.Sprintf(" (%s to %s)", dateRange.Start, dateRange.End)
	}
	return label
}

func drilldownBreadcrumb(drilldown domain.Drilldown) string {
	if drilldown.Dimension == domain.DimensionTime && drilldown.Period != nil {
		return "T: " + formatBreadcrumbPeriod(*drilldown.Period)
	}
	prefix := map[domain.Dimension]string{
		domain.DimensionMerchant: "M",
		domain.DimensionCategory: "C",
		domain.DimensionGroup:    "G",
		domain.DimensionAccount:  "A",
	}[drilldown.Dimension]
	return prefix + ": " + drilldown.Label
}

func formatBreadcrumbPeriod(period domain.Period) string {
	switch period.Granularity {
	case domain.TimeGranularityYear:
		return fmt.Sprintf("%04d", period.Year)
	case domain.TimeGranularityMonth:
		return fmt.Sprintf("%s %04d", time.Month(period.Month).String()[:3], period.Year)
	default:
		return fmt.Sprintf("%04d-%02d-%02d", period.Year, period.Month, period.Day)
	}
}

func dimensionLabel(dimension domain.Dimension, plural bool) string {
	labels := map[domain.Dimension]string{
		domain.DimensionMerchant: "Merchant",
		domain.DimensionCategory: "Category",
		domain.DimensionGroup:    "Group",
		domain.DimensionAccount:  "Account",
		domain.DimensionTime:     "Time",
	}
	label := labels[dimension]
	if !plural {
		return label
	}
	if dimension == domain.DimensionCategory {
		return "Categories"
	}
	return label + "s"
}
