package monarch

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

type transactionReadbackRow struct {
	ID       string `json:"id"`
	Date     string `json:"date"`
	Merchant *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"merchant"`
	Category        json.RawMessage `json:"category"`
	HideFromReports *bool           `json:"hideFromReports"`
}

type transactionReadbackData struct {
	AllTransactions struct {
		TotalCount int                      `json:"totalCount"`
		Results    []transactionReadbackRow `json:"results"`
	} `json:"allTransactions"`
}

// ReadTransaction checks one exact transaction on its known date. It reads at
// most one bounded page per visibility state and never imports a snapshot.
func (client *Client) ReadTransaction(
	ctx context.Context,
	externalID string,
	date domain.Date,
) (provider.TransactionUpdateResult, error) {
	unknown := provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	if !validProviderText(externalID) {
		return provider.TransactionUpdateResult{}, unknown
	}
	if _, err := domain.ParseDate(date.String()); err != nil {
		return provider.TransactionUpdateResult{}, unknown
	}
	for _, hidden := range []bool{false, true} {
		data, err := graphQLCall[transactionReadbackData](ctx, client,
			"GetTransactionsList", getTransactionsQuery, map[string]any{
				"offset": 0, "limit": 1000, "orderBy": "date",
				"filters": map[string]any{
					"startDate": date.String(), "endDate": date.String(), "hideFromReports": hidden,
				},
			})
		if err != nil {
			return provider.TransactionUpdateResult{}, err
		}
		page := data.AllTransactions
		if page.TotalCount > 1000 || page.TotalCount < len(page.Results) || len(page.Results) > 1000 {
			return provider.TransactionUpdateResult{}, unknown
		}
		for _, row := range page.Results {
			if row.ID != externalID {
				continue
			}
			if row.Date != date.String() || row.Merchant == nil || row.HideFromReports == nil || len(row.Category) == 0 {
				return provider.TransactionUpdateResult{}, unknown
			}
			result, err := normalizeTransactionUpdateResult(&updatedTransaction{
				ID: row.ID, Merchant: row.Merchant, HideFromReports: row.HideFromReports,
			})
			if err != nil {
				return provider.TransactionUpdateResult{}, err
			}
			if bytes.Equal(bytes.TrimSpace(row.Category), []byte("null")) {
				result.CategoryCleared = true
			} else {
				var category struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(row.Category, &category); err != nil || !validProviderText(category.ID) {
					return provider.TransactionUpdateResult{}, unknown
				}
				result.CategoryExternalID = provider.Some(category.ID)
			}
			return result, nil
		}
	}
	return provider.TransactionUpdateResult{}, unknown
}
