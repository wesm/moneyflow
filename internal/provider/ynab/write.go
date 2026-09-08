package ynab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
)

type transactionWriter struct {
	client   *Client
	planID   string
	currency domain.Currency
	scale    uint8
	now      func() time.Time
	validate func() error
}

var _ provider.Writer = (*transactionWriter)(nil)

func (writer *transactionWriter) ProbeIdentity(ctx context.Context) (provider.ProfileIdentity, error) {
	var response struct {
		Data struct {
			Plan struct {
				ID             string `json:"id"`
				CurrencyFormat *struct {
					ISOCode       string `json:"iso_code"`
					DecimalDigits *int   `json:"decimal_digits"`
				} `json:"currency_format"`
			} `json:"plan"`
		} `json:"data"`
	}
	err := writer.requestJSON(ctx, http.MethodGet, "plans/"+url.PathEscape(writer.planID), nil, &response, writer.client.maxBodyBytes)
	if reason, ok := provider.WriteFailureReasonOf(err); ok && reason == provider.WriteTargetNotFound {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeIdentityMismatch)
	}
	if err != nil {
		return provider.ProfileIdentity{}, err
	}
	plan := response.Data.Plan
	if plan.ID != writer.planID {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeIdentityMismatch)
	}
	if plan.CurrencyFormat == nil || plan.CurrencyFormat.DecimalDigits == nil {
		return provider.ProfileIdentity{}, provider.NewDataInvalidError(provider.DataInvalidSnapshot)
	}
	if plan.CurrencyFormat.ISOCode != string(writer.currency) || *plan.CurrencyFormat.DecimalDigits != int(writer.scale) {
		return provider.ProfileIdentity{}, provider.NewError(provider.CodeMoneyMismatch)
	}
	return provider.ProfileIdentity{Kind: "ynab", RemoteID: plan.ID}, nil
}

// UpdateTransaction owns one GET/PUT attempt. Only the shared durable worker retries.
func (writer *transactionWriter) UpdateTransaction(ctx context.Context, update provider.TransactionUpdate) (provider.TransactionUpdateResult, error) {
	patch, err := ynabUpdatePatch(update)
	if err != nil {
		return provider.TransactionUpdateResult{}, err
	}
	before, err := writer.readTransaction(ctx, update.TransactionExternalID)
	if err != nil {
		return provider.TransactionUpdateResult{}, err
	}
	if *before.transaction.Deleted {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteTargetNotFound)
	}
	if before.transfer() || (len(before.children) > 0 && (update.ClearCategory || update.CategoryExternalID.Present)) {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteRejected)
	}
	if update.MerchantExternalID.Present {
		if err = writer.validateDestinationPayee(ctx, update.MerchantExternalID.Value); err != nil {
			return provider.TransactionUpdateResult{}, err
		}
	}
	patch["approved"] = *before.transaction.Approved
	var response writeTransactionResponse
	if err = writer.requestJSON(ctx, http.MethodPut, writer.transactionPath(update.TransactionExternalID), map[string]any{"transaction": patch}, &response, writer.transactionLimit()); err != nil {
		return provider.TransactionUpdateResult{}, err
	}
	after, err := writer.decodeTransaction(response.Data.Transaction)
	if err != nil {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	if after.transaction.ID != update.TransactionExternalID {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteIdentityConflict)
	}
	if *after.transaction.Deleted || !before.preservedBy(after) {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	if (update.MerchantName.Present || update.MerchantExternalID.Present) && after.transaction.PayeeID == "" {
		return provider.TransactionUpdateResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	result := provider.TransactionUpdateResult{TransactionExternalID: after.transaction.ID}
	if after.transaction.PayeeID != "" {
		result.MerchantExternalID = provider.Some(after.transaction.PayeeID)
	}
	if after.payeeName != "" {
		result.MerchantLabel = provider.Some(after.payeeName)
	}
	if after.transaction.CategoryID != "" {
		result.CategoryExternalID = provider.Some(after.transaction.CategoryID)
	} else if len(after.children) == 0 {
		result.CategoryCleared = true
	}
	return result, nil
}

func (writer *transactionWriter) validateDestinationPayee(ctx context.Context, id string) error {
	var response struct {
		Data struct {
			Payee Payee `json:"payee"`
		} `json:"data"`
	}
	if err := writer.requestJSON(ctx, http.MethodGet, "plans/"+url.PathEscape(writer.planID)+"/payees/"+url.PathEscape(id), nil, &response, writer.transactionLimit()); err != nil {
		return err
	}
	payee := response.Data.Payee
	if payee.ID != id || payee.Deleted == nil {
		return provider.NewWriteFailure(provider.WriteResponseIncomplete)
	}
	if *payee.Deleted {
		return provider.NewWriteFailure(provider.WriteTargetNotFound)
	}
	if payee.TransferAccountID != "" {
		return provider.NewWriteFailure(provider.WriteRejected)
	}
	return nil
}

func ynabUpdatePatch(update provider.TransactionUpdate) (map[string]any, error) {
	unsupported := provider.NewError(provider.CodeWriteUnsupported)
	if validateID(update.TransactionExternalID) != nil || update.Hidden.Present ||
		(update.MerchantName.Present && update.MerchantExternalID.Present) ||
		(update.ClearCategory && update.CategoryExternalID.Present) ||
		(!update.MerchantName.Present && !update.MerchantExternalID.Present && !update.CategoryExternalID.Present && !update.ClearCategory) {
		return nil, unsupported
	}
	patch := make(map[string]any)
	if update.MerchantName.Present {
		if validateLabel(update.MerchantName.Value) != nil || utf8.RuneCountInString(update.MerchantName.Value) > 200 {
			return nil, unsupported
		}
		patch["payee_id"], patch["payee_name"] = nil, update.MerchantName.Value
	}
	if update.MerchantExternalID.Present {
		if validateID(update.MerchantExternalID.Value) != nil {
			return nil, unsupported
		}
		patch["payee_id"] = update.MerchantExternalID.Value
	}
	if update.CategoryExternalID.Present {
		if validateID(update.CategoryExternalID.Value) != nil {
			return nil, unsupported
		}
		patch["category_id"] = update.CategoryExternalID.Value
	}
	if update.ClearCategory {
		patch["category_id"] = nil
	}
	return patch, nil
}

func (writer *transactionWriter) DeleteTransaction(ctx context.Context, externalID string) (provider.TransactionDeleteResult, error) {
	if validateID(externalID) != nil {
		return provider.TransactionDeleteResult{}, provider.NewError(provider.CodeWriteUnsupported)
	}
	before, err := writer.readTransaction(ctx, externalID)
	if reason, ok := provider.WriteFailureReasonOf(err); ok && reason == provider.WriteTargetNotFound {
		return writer.alreadyAbsent(ctx, externalID)
	}
	if err != nil {
		return provider.TransactionDeleteResult{}, err
	}
	if *before.transaction.Deleted {
		return writer.alreadyAbsent(ctx, externalID)
	}
	if before.transfer() {
		return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteRejected)
	}
	var response writeTransactionResponse
	err = writer.requestJSON(ctx, http.MethodDelete, writer.transactionPath(externalID), nil, &response, writer.transactionLimit())
	if reason, ok := provider.WriteFailureReasonOf(err); ok && reason == provider.WriteTargetNotFound {
		return writer.alreadyAbsent(ctx, externalID)
	}
	if err != nil {
		return provider.TransactionDeleteResult{}, err
	}
	after, err := writer.decodeTransaction(response.Data.Transaction)
	if err != nil || after.transaction.ID != externalID || !*after.transaction.Deleted {
		return provider.TransactionDeleteResult{}, provider.NewWriteFailure(provider.WriteOutcomeUnknown)
	}
	return provider.TransactionDeleteResult{TransactionExternalID: externalID}, nil
}

func (writer *transactionWriter) alreadyAbsent(ctx context.Context, externalID string) (provider.TransactionDeleteResult, error) {
	if _, err := writer.ProbeIdentity(ctx); err != nil {
		return provider.TransactionDeleteResult{}, err
	}
	return provider.TransactionDeleteResult{TransactionExternalID: externalID, AlreadyAbsent: true}, nil
}

func (writer *transactionWriter) transactionPath(id string) string {
	return "plans/" + url.PathEscape(writer.planID) + "/transactions/" + url.PathEscape(id)
}

func (writer *transactionWriter) transactionLimit() int64 {
	return min(writer.client.maxBodyBytes, 4<<20)
}

func (writer *transactionWriter) readTransaction(ctx context.Context, externalID string) (writeTransactionSnapshot, error) {
	var response writeTransactionResponse
	if err := writer.requestJSON(ctx, http.MethodGet, writer.transactionPath(externalID), nil, &response, writer.transactionLimit()); err != nil {
		return writeTransactionSnapshot{}, err
	}
	transaction, err := writer.decodeTransaction(response.Data.Transaction)
	if err != nil {
		return writeTransactionSnapshot{}, provider.NewWriteFailure(provider.WriteResponseIncomplete)
	}
	if transaction.transaction.ID != externalID {
		return writeTransactionSnapshot{}, provider.NewWriteFailure(provider.WriteIdentityConflict)
	}
	return transaction, nil
}

func (writer *transactionWriter) requestJSON(ctx context.Context, method, path string, body any, target any, limit int64) error {
	if err := writer.validate(); err != nil {
		return err
	}
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return provider.NewError(provider.CodeWriteUnsupported)
		}
	}
	// #nosec G704 -- the privately owned client base is validated; paths contain only fixed
	// route segments and PathEscape-encoded provider identities.
	request, err := http.NewRequestWithContext(ctx, method, writer.client.baseURL.String()+path, bytes.NewReader(encoded))
	if err != nil {
		return provider.NewError(provider.CodeWriteUnsupported)
	}
	request.Header.Set("Authorization", "Bearer "+writer.client.accessToken)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	// #nosec G704 -- URL construction is confined to the validated client boundary above.
	response, err := writer.client.httpClient.Do(request)
	if err != nil {
		return writeTransportFailure(method)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return provider.NewError(provider.CodeReconnectRequired)
		case http.StatusNotFound:
			return provider.NewWriteFailure(provider.WriteTargetNotFound)
		case http.StatusTooManyRequests:
			return provider.NewErrorWithRetry(provider.CodeRateLimited, ynabWriteRetryAfter(response.Header.Get("Retry-After"), writer.now()))
		default:
			if response.StatusCode >= 500 {
				return writeTransportFailure(method)
			}
			return provider.NewWriteFailure(provider.WriteRejected)
		}
	}
	contents, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(contents)) > limit || decodeJSON(contents, target) != nil {
		if method != http.MethodGet {
			return provider.NewWriteFailure(provider.WriteOutcomeUnknown)
		}
		return provider.NewWriteFailure(provider.WriteResponseIncomplete)
	}
	return nil
}

func writeTransportFailure(method string) error {
	if method == http.MethodGet {
		return provider.NewError(provider.CodeUnavailable)
	}
	return provider.NewWriteFailure(provider.WriteOutcomeUnknown)
}

func ynabWriteRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(provider.MaxRetryAfter/time.Second) {
			return provider.MaxRetryAfter
		}
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return min(date.Sub(now), provider.MaxRetryAfter)
	}
	return time.Hour
}
