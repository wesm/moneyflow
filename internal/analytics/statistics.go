package analytics

import (
	"fmt"
	"sort"

	"github.com/wesm/moneyflow/internal/domain"
)

type moneyPartition struct {
	currency domain.Currency
	scale    uint8
}

// Statistics calculates exact visible-view totals partitioned by currency and scale.
func Statistics(filtered []domain.Transaction) ([]domain.CurrencyStats, error) {
	statistics := make(map[moneyPartition]domain.CurrencyStats)
	for _, transaction := range filtered {
		key := moneyPartition{currency: transaction.Amount.Currency, scale: transaction.Amount.Scale}
		value, exists := statistics[key]
		if !exists {
			zero := domain.Money{Currency: key.currency, Scale: key.scale}
			value = domain.CurrencyStats{
				Currency: key.currency,
				Scale:    key.scale,
				In:       zero,
				Out:      zero,
				Net:      zero,
			}
		}
		value.Count++
		if !transaction.Hidden {
			var err error
			switch {
			case transaction.Amount.Minor > 0:
				value.In, err = value.In.Add(transaction.Amount)
			case transaction.Amount.Minor < 0:
				value.Out, err = value.Out.Add(transaction.Amount)
			}
			if err != nil {
				return nil, fmt.Errorf("statistics: %s/%d total: %w", key.currency, key.scale, err)
			}
		}
		statistics[key] = value
	}

	keys := make([]moneyPartition, 0, len(statistics))
	for key := range statistics {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].currency != keys[right].currency {
			return keys[left].currency < keys[right].currency
		}
		return keys[left].scale < keys[right].scale
	})

	result := make([]domain.CurrencyStats, 0, len(keys))
	for _, key := range keys {
		value := statistics[key]
		net, err := value.In.Add(value.Out)
		if err != nil {
			return nil, fmt.Errorf("statistics: %s/%d net: %w", key.currency, key.scale, err)
		}
		value.Net = net
		result = append(result, value)
	}
	return result, nil
}
