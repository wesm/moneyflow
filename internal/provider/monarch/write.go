package monarch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

var _ provider.Writer = (*Client)(nil)

type updateTransactionData struct {
	UpdateTransaction *updateTransactionPayload `json:"updateTransaction"`
}

type deleteTransactionData struct {
	DeleteTransaction *deleteTransactionPayload `json:"deleteTransaction"`
}

type deleteTransactionPayload struct {
	Deleted bool           `json:"deleted"`
	Errors  []payloadError `json:"errors"`
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

// DeleteTransaction performs exactly one absolute Monarch transaction deletion.
func (client *Client) DeleteTransaction(
	ctx context.Context,
	externalID string,
) (provider.TransactionDeleteResult, error) {
	if !validProviderText(externalID) {
		return provider.TransactionDeleteResult{}, provider.NewError(provider.CodeWriteUnsupported)
	}
	data, err := client.writeGraphQLOnce(
		ctx,
		"Common_DeleteTransactionMutation",
		deleteTransactionQuery,
		map[string]any{"input": map[string]any{"transactionId": externalID}},
	)
	if err != nil {
		return provider.TransactionDeleteResult{}, err
	}
	result := provider.TransactionDeleteResult{TransactionExternalID: externalID}
	var decoded deleteTransactionData
	if err = json.Unmarshal(data, &decoded); err != nil || decoded.DeleteTransaction == nil {
		return provider.TransactionDeleteResult{}, provider.NewWriteFailure(
			provider.WriteOutcomeUnknown,
		)
	}
	payload := decoded.DeleteTransaction
	if payload.Deleted {
		return result, nil
	}
	if payloadErrorsProveNotFound(payload.Errors) {
		result.AlreadyAbsent = true
		return result, nil
	}
	if len(payload.Errors) > 0 {
		return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteRejected)
	}
	return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
}

func (client *Client) updateTransactionOnce(
	ctx context.Context,
	input map[string]any,
) (updateTransactionPayload, error) {
	data, err := client.writeGraphQLOnce(
		ctx,
		"Web_TransactionDrawerUpdateTransaction",
		updateTransactionQuery,
		map[string]any{"input": input},
	)
	if err != nil {
		return updateTransactionPayload{}, err
	}
	var decoded updateTransactionData
	if err = json.Unmarshal(data, &decoded); err != nil || decoded.UpdateTransaction == nil {
		return updateTransactionPayload{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	return *decoded.UpdateTransaction, nil
}

func (client *Client) writeGraphQLOnce(
	ctx context.Context,
	operationName string,
	query string,
	variables map[string]any,
) (json.RawMessage, error) {
	requestBody, err := json.Marshal(graphQLRequest{
		OperationName: operationName, Query: query, Variables: variables,
	})
	if err != nil {
		return nil, provider.NewError(provider.CodeWriteUnsupported)
	}
	request, err := http.NewRequestWithContext(
		ctx, http.MethodPost, client.options.GraphQLURL.String(), bytes.NewReader(requestBody),
	)
	if err != nil {
		return nil, provider.NewError(provider.CodeWriteUnsupported)
	}
	setReadHeaders(request.Header, client.authorization, client.deviceUUID)
	response, err := client.options.HTTPClient.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, writeErrorForResponse(response, client.options.Now())
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, client.options.MaxBodyBytes+1))
	if err != nil || int64(len(body)) > client.options.MaxBodyBytes {
		return nil, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	var envelope graphQLResponse
	if err = json.Unmarshal(body, &envelope); err != nil {
		return nil, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	if len(envelope.Errors) > 0 {
		return nil, provider.NewWriteFailure(provider.WriteRejected)
	}
	data := bytes.TrimSpace(envelope.Data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	return append(json.RawMessage(nil), data...), nil
}

func payloadErrorsProveNotFound(payloadErrors []payloadError) bool {
	if len(payloadErrors) == 0 {
		return false
	}
	for _, payloadFailure := range payloadErrors {
		switch strings.ToUpper(strings.TrimSpace(payloadFailure.Code)) {
		case "NOT_FOUND", "TRANSACTION_NOT_FOUND":
		default:
			return false
		}
	}
	return true
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
				provider.WriteOutcomeUnknown,
			)
		}
		label, err := normalizeProviderLabel(transaction.Merchant.Name)
		if err != nil {
			return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
				provider.WriteOutcomeUnknown,
			)
		}
		result.MerchantExternalID = provider.Some(transaction.Merchant.ID)
		result.MerchantLabel = provider.Some(label)
	}
	if transaction.Category != nil {
		if !validProviderText(transaction.Category.ID) {
			return provider.TransactionUpdateResult{}, provider.NewWriteFailure(
				provider.WriteOutcomeUnknown,
			)
		}
		result.CategoryExternalID = provider.Some(transaction.Category.ID)
	}
	if transaction.HideFromReports != nil {
		result.Hidden = provider.Some(*transaction.HideFromReports)
	}
	return result, nil
}
