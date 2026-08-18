package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

var _ provider.Writer = (*Client)(nil)

type updateTransactionData struct {
	UpdateTransaction *updateTransactionPayload `json:"updateTransaction"`
}

type updateTransactionPayload struct {
	Transaction *updatedTransaction `json:"transaction"`
	Errors      []payloadError      `json:"errors"`
}

type updatedTransaction struct {
	ID       string `json:"id"`
	Merchant *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"merchant"`
	Category *struct {
		ID string `json:"id"`
	} `json:"category"`
	HideFromReports *bool `json:"hideFromReports"`
}

type payloadError struct {
	Field       string       `json:"field"`
	Messages    []string     `json:"messages"`
	FieldErrors []fieldError `json:"fieldErrors"`
	Message     string       `json:"message"`
	Code        string       `json:"code"`
}

type fieldError struct {
	Field    string   `json:"field"`
	Messages []string `json:"messages"`
}

// UpdateTransaction performs exactly one absolute Monarch transaction mutation.
func (client *Client) UpdateTransaction(
	ctx context.Context,
	update provider.TransactionUpdate,
) (provider.TransactionUpdateResult, error) {
	input, err := transactionUpdateInput(update)
	if err != nil {
		return provider.TransactionUpdateResult{}, provider.NewError(provider.CodeWriteUnsupported)
	}
	payload, err := client.updateTransactionOnce(ctx, input)
	if err != nil {
		return provider.TransactionUpdateResult{}, err
	}
	if len(payload.Errors) > 0 {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
	}
	if payload.Transaction == nil {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	transaction := payload.Transaction
	if transaction.ID != update.TransactionExternalID {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
			provider.WriteIdentityConflict,
		)
	}
	result, err := normalizeTransactionUpdateResult(transaction)
	if err != nil {
		return provider.TransactionUpdateResult{}, err
	}
	if (update.MerchantName.Present && !result.MerchantExternalID.Present) ||
		(update.CategoryExternalID.Present && !result.CategoryExternalID.Present) ||
		(update.Hidden.Present && !result.Hidden.Present) {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
			provider.WriteOutcomeUnknown,
		)
	}
	return result, nil
}

func transactionUpdateInput(update provider.TransactionUpdate) (map[string]any, error) {
	if !validProviderText(update.TransactionExternalID) ||
		(!update.MerchantName.Present && !update.CategoryExternalID.Present &&
			!update.Hidden.Present) {
		return nil, errors.New("transaction update is empty")
	}
	input := map[string]any{"id": update.TransactionExternalID}
	if update.MerchantName.Present {
		if _, err := domain.NormalizeDisplayLabel(update.MerchantName.Value); err != nil {
			return nil, errors.New("transaction merchant name is invalid")
		}
		input["merchantName"] = update.MerchantName.Value
	}
	if update.CategoryExternalID.Present {
		if !validProviderText(update.CategoryExternalID.Value) {
			return nil, errors.New("transaction category identity is invalid")
		}
		input["category"] = update.CategoryExternalID.Value
	}
	if update.Hidden.Present {
		input["hideFromReports"] = update.Hidden.Value
	}
	return input, nil
}

func (client *Client) updateTransactionOnce(
	ctx context.Context,
	input map[string]any,
) (updateTransactionPayload, error) {
	requestBody, err := json.Marshal(graphQLRequest{
		OperationName: "Web_TransactionDrawerUpdateTransaction",
		Query:         updateTransactionQuery,
		Variables:     map[string]any{"input": input},
	})
	if err != nil {
		return updateTransactionPayload{}, provider.NewError(provider.CodeWriteUnsupported)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.options.GraphQLURL.String(), bytes.NewReader(requestBody),
	)
	if err != nil {
		return updateTransactionPayload{}, provider.NewError(provider.CodeWriteUnsupported)
	}
	setReadHeaders(request.Header, client.authorization, client.deviceUUID)
	response, err := client.options.HTTPClient.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return updateTransactionPayload{}, ctxErr
		}
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return updateTransactionPayload{}, writeErrorForResponse(response, client.options.Now())
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, client.options.MaxBodyBytes+1))
	if err != nil {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	if int64(len(body)) > client.options.MaxBodyBytes {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	var envelope graphQLResponse
	if err = json.Unmarshal(body, &envelope); err != nil {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	if len(envelope.Errors) > 0 {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteRejected)
	}
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	var decoded updateTransactionData
	if err = json.Unmarshal(data, &decoded); err != nil || decoded.UpdateTransaction == nil {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	return *decoded.UpdateTransaction, nil
}

func writeErrorForResponse(response *http.Response, now time.Time) error {
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return provider.NewError(provider.CodeReconnectRequired)
	case http.StatusNotFound:
		return provider.NewWriteFailure(provider.WriteTargetNotFound)
	case http.StatusTooManyRequests:
		return providerErrorForResponse(response, now)
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			return provider.NewWriteFailure(provider.WriteOutcomeUnknown)
		}
		return provider.NewWriteFailure(provider.WriteRejected)
	}
}

func normalizeTransactionUpdateResult(
	transaction *updatedTransaction,
) (provider.TransactionUpdateResult, error) {
	result := provider.TransactionUpdateResult{TransactionExternalID: transaction.ID}
	if transaction.Merchant != nil {
		if !validProviderText(transaction.Merchant.ID) {
			return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
				provider.WriteResponseIncomplete,
			)
		}
		label, err := normalizeProviderLabel(transaction.Merchant.Name)
		if err != nil {
			return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
				provider.WriteResponseIncomplete,
			)
		}
		result.MerchantExternalID = provider.Some(transaction.Merchant.ID)
		result.MerchantLabel = provider.Some(label)
	}
	if transaction.Category != nil {
		if !validProviderText(transaction.Category.ID) {
			return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
				provider.WriteResponseIncomplete,
			)
		}
		result.CategoryExternalID = provider.Some(transaction.Category.ID)
	}
	if transaction.HideFromReports != nil {
		result.Hidden = provider.Some(*transaction.HideFromReports)
	}
	return result, nil
}
