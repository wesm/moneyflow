package monarch

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

func decodeMoney(raw json.RawMessage, currency domain.Currency, scale uint8) (domain.Money, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
	}
	decimal := string(trimmed)
	if trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &decimal); err != nil {
			return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
		}
	}
	decimal = trimExactZeroPrecision(decimal, scale)
	money, err := domain.ParseMoney(decimal, currency, scale)
	if err != nil {
		return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
	}
	return money, nil
}

func trimExactZeroPrecision(decimal string, scale uint8) string {
	point := strings.IndexByte(decimal, '.')
	if point < 0 || strings.LastIndexByte(decimal, '.') != point {
		return decimal
	}
	fraction := decimal[point+1:]
	if len(fraction) <= int(scale) || strings.Trim(fraction[scale:], "0") != "" {
		return decimal
	}
	if scale == 0 {
		return decimal[:point]
	}
	return decimal[:point+1+int(scale)]
}
