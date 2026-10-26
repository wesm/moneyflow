package monarch

import (
	"bytes"
	"encoding/json"

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
	decimal = domain.TrimExactZeroPrecision(decimal, scale)
	money, err := domain.ParseMoney(decimal, currency, scale)
	if err != nil {
		return domain.Money{}, provider.NewDataInvalidError(provider.DataInvalidTransactionAmount)
	}
	return money, nil
}
