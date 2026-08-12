// Package analytics implements pure, exact transaction queries.
package analytics

import (
	"fmt"
	"regexp"

	"github.com/wesm/moneyflow/internal/domain"
)

// Filter applies the visible-view predicates in one pass and returns defensive copies.
func Filter(transactions []domain.Transaction, spec domain.QuerySpec) ([]domain.Transaction, error) {
	var search *regexp.Regexp
	if spec.Search != "" {
		compiled, err := regexp.Compile("(?i:" + spec.Search + ")")
		if err != nil {
			return nil, fmt.Errorf("filter transactions: invalid search expression: %w", err)
		}
		search = compiled
	}

	filtered := make([]domain.Transaction, 0, len(transactions))
	for _, transaction := range transactions {
		if !matches(transaction, spec, search) {
			continue
		}
		filtered = append(filtered, transaction.Clone())
	}
	return filtered, nil
}

func matches(transaction domain.Transaction, spec domain.QuerySpec, search *regexp.Regexp) bool {
	if spec.DateRange != nil &&
		(transaction.Date.Compare(spec.DateRange.Start) < 0 || transaction.Date.Compare(spec.DateRange.End) > 0) {
		return false
	}
	if search != nil &&
		!search.MatchString(transaction.Merchant.Name) &&
		!search.MatchString(transaction.Category.Name) {
		return false
	}
	if !spec.ShowTransfers && transaction.Category.Group == "Transfers" {
		return false
	}
	if !spec.ShowHidden && spec.Mode == domain.ResultModeAggregate && transaction.Hidden {
		return false
	}
	for _, drilldown := range spec.Drilldowns {
		if !matchesDrilldown(transaction, drilldown) {
			return false
		}
	}
	return true
}

func matchesDrilldown(transaction domain.Transaction, drilldown domain.Drilldown) bool {
	switch drilldown.Dimension {
	case domain.DimensionMerchant:
		return transaction.Merchant.Name == drilldown.Label
	case domain.DimensionCategory:
		return transaction.Category.Name == drilldown.Label
	case domain.DimensionGroup:
		return transaction.Category.Group == drilldown.Label
	case domain.DimensionAccount:
		return transaction.Account.Name == drilldown.Label
	case domain.DimensionTime:
		return drilldown.Period != nil && matchesPeriod(transaction, *drilldown.Period)
	default:
		return false
	}
}

func matchesPeriod(transaction domain.Transaction, period domain.Period) bool {
	if transaction.Date.Year() != period.Year {
		return false
	}
	if period.Granularity == domain.TimeGranularityYear {
		return true
	}
	if int(transaction.Date.Month()) != period.Month {
		return false
	}
	return period.Granularity == domain.TimeGranularityMonth || transaction.Date.Day() == period.Day
}
