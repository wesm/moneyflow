package analytics

import (
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// Query validates and evaluates one complete renderer-neutral view.
func Query(transactions []domain.Transaction, spec domain.QuerySpec) (domain.QueryResult, error) {
	if err := spec.Validate(); err != nil {
		return domain.QueryResult{}, err
	}
	filtered, err := Filter(transactions, spec)
	if err != nil {
		return domain.QueryResult{}, err
	}
	statistics, err := Statistics(filtered)
	if err != nil {
		return domain.QueryResult{}, err
	}
	result := domain.QueryResult{
		Statistics:    statistics,
		FilteredCount: len(filtered),
		DateRange:     filteredDateRange(filtered),
	}
	if spec.Mode == domain.ResultModeDetail {
		result.DetailRows = DetailRows(filtered, spec.Sort)
		return result, nil
	}
	rows, err := Aggregate(filtered, spec.GroupBy, spec.TimeGranularity)
	if err != nil {
		return domain.QueryResult{}, fmt.Errorf("query: %w", err)
	}
	result.AggregateRows = SortAggregateRows(rows, spec.Sort)
	return result, nil
}

func filteredDateRange(filtered []domain.Transaction) *domain.DateRange {
	if len(filtered) == 0 {
		return nil
	}
	minimum := filtered[0].Date
	maximum := filtered[0].Date
	for _, transaction := range filtered[1:] {
		if transaction.Date.Compare(minimum) < 0 {
			minimum = transaction.Date
		}
		if transaction.Date.Compare(maximum) > 0 {
			maximum = transaction.Date
		}
	}
	return &domain.DateRange{Start: minimum, End: maximum}
}
