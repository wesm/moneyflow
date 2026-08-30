package ynab

import (
	"math"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func milliunitsToMoney(value int64, currency domain.Currency, scale uint8) (domain.Money, error) {
	if !domain.IsValidCurrency(currency) || scale > 9 {
		return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
	}
	minor := value
	if scale < 3 {
		divisor := int64(1)
		for range 3 - scale {
			divisor *= 10
		}
		if value%divisor != 0 {
			return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
		}
		minor = value / divisor
	} else if scale > 3 {
		multiplier := int64(1)
		for range scale - 3 {
			multiplier *= 10
		}
		if value > math.MaxInt64/multiplier || value < math.MinInt64/multiplier {
			return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
		}
		minor = value * multiplier
	}
	return domain.Money{Minor: minor, Currency: currency, Scale: scale}, nil
}
