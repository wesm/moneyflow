package analytics

import (
	"sort"

	"github.com/wesm/moneyflow/internal/domain"
)

// Currencies lists each currency and scale with its transaction count and date bounds, most
// transactions first. Hidden transactions count.
func Currencies(transactions []domain.Transaction) []domain.CurrencyCount {
	byPartition := make(map[moneyPartition]*domain.CurrencyCount)
	for _, transaction := range transactions {
		key := moneyPartition{currency: transaction.Amount.Currency, scale: transaction.Amount.Scale}
		value, exists := byPartition[key]
		if !exists {
			value = &domain.CurrencyCount{
				Currency: key.currency, Scale: key.scale,
				First: transaction.Date, Latest: transaction.Date,
			}
			byPartition[key] = value
		}
		value.Count++
		value.First = earlierDate(value.First, transaction.Date)
		value.Latest = laterDate(value.Latest, transaction.Date)
	}
	result := make([]domain.CurrencyCount, 0, len(byPartition))
	for _, value := range byPartition {
		result = append(result, *value)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Count != result[right].Count {
			return result[left].Count > result[right].Count
		}
		if result[left].Currency != result[right].Currency {
			return result[left].Currency < result[right].Currency
		}
		return result[left].Scale < result[right].Scale
	})
	return result
}

func laterDate(left, right domain.Date) domain.Date {
	if left.Compare(right) >= 0 {
		return left
	}
	return right
}

func earlierDate(left, right domain.Date) domain.Date {
	if left.Compare(right) <= 0 {
		return left
	}
	return right
}
