package app

import (
	"fmt"
	"sort"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// selectionSummary totals the selected rows of a decorated query result per money partition.
// It returns nil when nothing is selected.
func selectionSummary(result domain.QueryResult) ([]domain.CurrencyStats, error) {
	if result.DetailRows != nil {
		return detailSelectionSummary(result.DetailRows)
	}
	return aggregateSelectionSummary(result.AggregateRows)
}

func detailSelectionSummary(rows []domain.DetailRow) ([]domain.CurrencyStats, error) {
	var selected []domain.Transaction
	for _, row := range rows {
		if row.Flags.Selected {
			selected = append(selected, row.Transaction)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	return analytics.Statistics(selected)
}

func aggregateSelectionSummary(rows []domain.AggregateRow) ([]domain.CurrencyStats, error) {
	totals := make(map[moneyPartition]domain.CurrencyStats)
	for _, row := range rows {
		if !row.Flags.Selected {
			continue
		}
		partition := moneyPartition{currency: row.Total.Currency, scale: row.Total.Scale}
		total, exists := totals[partition]
		if !exists {
			total = emptyStats(partition)
		}
		total, err := addAggregateRow(total, row)
		if err != nil {
			return nil, fmt.Errorf(
				"selection summary: %s/%d: %w", partition.currency, partition.scale, err,
			)
		}
		totals[partition] = total
	}
	if len(totals) == 0 {
		return nil, nil
	}
	summary := make([]domain.CurrencyStats, 0, len(totals))
	for _, total := range totals {
		summary = append(summary, total)
	}
	sort.Slice(summary, func(left, right int) bool {
		if summary[left].Currency != summary[right].Currency {
			return summary[left].Currency < summary[right].Currency
		}
		return summary[left].Scale < summary[right].Scale
	})
	return summary, nil
}

func emptyStats(partition moneyPartition) domain.CurrencyStats {
	zero := domain.Money{Currency: partition.currency, Scale: partition.scale}
	return domain.CurrencyStats{
		Currency: partition.currency, Scale: partition.scale, In: zero, Out: zero, Net: zero,
	}
}

func addAggregateRow(
	total domain.CurrencyStats,
	row domain.AggregateRow,
) (domain.CurrencyStats, error) {
	var err error
	total.Count += row.Count
	if total.In, err = total.In.Add(row.In); err != nil {
		return total, err
	}
	if total.Out, err = total.Out.Add(row.Out); err != nil {
		return total, err
	}
	total.Net, err = total.Net.Add(row.Total)
	return total, err
}
