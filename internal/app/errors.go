package app

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/store"
)

// AppErrorCode is a stable renderer-neutral interaction failure.
//
//nolint:revive // The App prefix distinguishes this contract from WebErrorCode and MutationErrorCode.
type AppErrorCode string

// Stable application interaction failure codes.
const (
	AppRevisionConflict                     AppErrorCode = "revision_conflict"
	AppInvalidOperation                     AppErrorCode = "invalid_operation"
	AppInvalidTarget                        AppErrorCode = "invalid_target"
	AppSelectionStale                       AppErrorCode = "selection_stale"
	AppStoreBusy                            AppErrorCode = "store_busy"
	AppStoreError                           AppErrorCode = "store_error"
	AppSchemaNewer                          AppErrorCode = "schema_newer"
	AppSchemaIncompatible                   AppErrorCode = "schema_incompatible"
	AppStoreCorrupt                         AppErrorCode = "store_corrupt"
	AppJournalFull                          AppErrorCode = "journal_full"
	AppExportInvalid                        AppErrorCode = "export_invalid"
	AppExportEmpty                          AppErrorCode = "export_empty"
	AppExportFailed                         AppErrorCode = "export_failed"
	AppProviderReconnectRequired            AppErrorCode = "provider_reconnect_required"
	AppProviderIdentityMismatch             AppErrorCode = "provider_identity_mismatch"
	AppProviderSnapshotUnstable             AppErrorCode = "provider_snapshot_unstable"
	AppProviderRefreshInProgress            AppErrorCode = "provider_refresh_in_progress"
	AppProviderDeletionConfirmationRequired AppErrorCode = "provider_deletion_confirmation_required"
	AppProviderConfirmationInvalid          AppErrorCode = "provider_confirmation_invalid"
	AppProviderRefreshStale                 AppErrorCode = "provider_refresh_stale"
	AppProviderRateLimited                  AppErrorCode = "provider_rate_limited"
	AppProviderUnavailable                  AppErrorCode = "provider_unavailable"
	AppProviderMoneyMismatch                AppErrorCode = "provider_money_mismatch"
	AppProviderDataInvalid                  AppErrorCode = "provider_data_invalid"
	AppProviderWriteInProgress              AppErrorCode = "provider_write_in_progress"
	AppProviderWriteAttentionRequired       AppErrorCode = "provider_write_attention_required"
	AppProviderWriteStale                   AppErrorCode = "provider_write_stale"
	AppProviderWritePaused                  AppErrorCode = "provider_write_paused"
	AppProviderWriteNotEligible             AppErrorCode = "provider_write_not_eligible"
	AppProviderWriteUnsupported             AppErrorCode = "provider_write_unsupported"
)

var appErrorDetails = map[AppErrorCode]string{
	AppRevisionConflict:                     "The profile changed and must be refreshed.",
	AppInvalidOperation:                     "The requested operation is invalid.",
	AppInvalidTarget:                        "The requested target is no longer available.",
	AppSelectionStale:                       "The selection changed and must be reviewed.",
	AppStoreBusy:                            "The profile is busy. Try the action again.",
	AppStoreError:                           "The profile could not be updated.",
	AppSchemaNewer:                          "The profile was created by a newer application.",
	AppSchemaIncompatible:                   "The profile format is not supported.",
	AppStoreCorrupt:                         "The profile is corrupt and cannot be opened.",
	AppJournalFull:                          "The pending edit limit is reached. Review or undo existing edits.",
	AppExportInvalid:                        "The export request is invalid.",
	AppExportEmpty:                          "No committed transactions are available to export.",
	AppExportFailed:                         "The export could not be created.",
	AppProviderReconnectRequired:            "Reconnect the provider through the command line.",
	AppProviderIdentityMismatch:             "The provider profile does not match this local profile.",
	AppProviderSnapshotUnstable:             "The provider returned inconsistent data after three attempts. No financial data changed. Retry once; if it repeats, report the progress counts.",
	AppProviderRefreshInProgress:            "Another process is refreshing this profile.",
	AppProviderDeletionConfirmationRequired: "Confirm the proposed provider removals.",
	AppProviderConfirmationInvalid:          "The refresh confirmation is no longer valid.",
	AppProviderRefreshStale:                 "A newer provider refresh already committed.",
	AppProviderRateLimited:                  "The provider rate limit prevented refresh.",
	AppProviderUnavailable:                  "The provider is temporarily unavailable.",
	AppProviderMoneyMismatch:                "The provider currency or scale does not match this profile.",
	AppProviderDataInvalid:                  "The provider returned invalid data.",
	AppProviderWriteInProgress:              "Another process is writing this profile.",
	AppProviderWriteAttentionRequired:       "The provider write requires attention.",
	AppProviderWriteStale:                   "The provider write changed and must be refreshed.",
	AppProviderWritePaused:                  "The provider write is paused.",
	AppProviderWriteNotEligible:             "The provider write is not eligible yet.",
	AppProviderWriteUnsupported:             "The pending changes cannot be written to this provider.",
}

// ProviderErrorDetail returns the readable explanation of a provider status code, or "".
func ProviderErrorDetail(code provider.ErrorCode) string {
	if code == "" {
		return ""
	}
	return appErrorDetails[AppErrorCode(code)]
}

// AppError carries allowlisted recovery state without exposing diagnostics.
//
//nolint:revive // The App prefix distinguishes this contract from WebError and MutationError.
type AppError struct {
	Code            AppErrorCode
	Detail          string
	CurrentRevision uint64
	Selection       SelectionValue
	cause           error
}

func (failure *AppError) Error() string {
	if failure == nil {
		return "<nil>"
	}
	return failure.Detail
}

func (failure *AppError) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func newAppError(code AppErrorCode, current uint64, cause error) *AppError {
	detail := appErrorDetails[code]
	if code == AppProviderUnavailable && errors.Is(cause, provider.ErrPaymentRequired) {
		detail = "Check your SimpleFIN subscription, then retry the import."
	}
	if code == AppProviderWriteUnsupported && errors.Is(cause, errYNABSplitCategory) {
		detail = "Edit split transaction categories in YNAB, then refresh Moneyflow."
	}
	if code == AppProviderDataInvalid {
		if reason, ok := provider.DataInvalidReasonOf(cause); ok {
			detail += " " + provider.DataInvalidDetail(reason)
		}
	}
	if code == AppInvalidOperation {
		if reason, ok := store.InvalidOperationReasonOf(cause); ok {
			detail += " " + store.InvalidOperationDetail(reason)
		}
	}
	return &AppError{
		Code: code, Detail: detail, CurrentRevision: current, cause: cause,
	}
}

func mapAppError(err error, reliableRevision uint64) error {
	if err == nil {
		return nil
	}
	var existing *AppError
	if errors.As(err, &existing) {
		return err
	}
	// A call stopped by its context may be a cancelled or timed-out request, whatever layer
	// reported it. Only the interface knows whether its request ended, so it gets the context
	// error and decides with RequestFailure; a storage code would blame the profile.
	if isContextError(err) {
		return err
	}
	return applicationError(err, reliableRevision)
}

// RequestFailure returns err as the interface serving a request with context ctx should report
// it. A context error means the request stopped only while ctx itself is done. While the request
// goes on, the error came from a time limit inside Moneyflow, such as the bounded audit write
// after a provider write. A storage step it ended is a storage failure and keeps its storage
// code; any other such error is returned unchanged, and the interface must not call it a stop.
func RequestFailure(ctx context.Context, err error, revision uint64) error {
	var existing *AppError
	var storage *store.Error
	if ctx.Err() != nil || !isContextError(err) || errors.As(err, &existing) ||
		!errors.As(err, &storage) {
		return err
	}
	return applicationError(err, revision)
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// applicationError gives err, which carries no application code, the code of its cause.
func applicationError(err error, reliableRevision uint64) error {
	var mutation *MutationError
	if errors.As(err, &mutation) {
		code := AppInvalidOperation
		switch mutation.Code {
		case MutationInvalidTarget:
			code = AppInvalidTarget
		case MutationSelectionStale:
			code = AppSelectionStale
		case MutationRevisionConflict:
			code = AppRevisionConflict
		case MutationInvalidOperation:
		}
		failure := newAppError(code, reliableRevision, err)
		if mutation.CurrentRevision != 0 {
			failure.CurrentRevision = mutation.CurrentRevision
		}
		failure.Selection = mutation.Selection
		return failure
	}
	if providerCode, ok := provider.CodeOf(err); ok {
		return newAppError(AppErrorCode(providerCode), reliableRevision, err)
	}
	var storage *store.Error
	if errors.As(err, &storage) {
		code := AppStoreError
		switch storage.Code {
		case store.CodeRevisionConflict:
			code = AppRevisionConflict
		case store.CodeInvalidOperation:
			code = AppInvalidOperation
		case store.CodeInvalidTarget:
			code = AppInvalidTarget
		case store.CodeStoreBusy:
			code = AppStoreBusy
		case store.CodeSchemaNewer:
			code = AppSchemaNewer
		case store.CodeSchemaIncompatible:
			code = AppSchemaIncompatible
		case store.CodeStoreCorrupt:
			code = AppStoreCorrupt
		case store.CodeJournalFull:
			code = AppJournalFull
		case store.CodeStoreError:
		}
		if storage.CurrentRevision != nil {
			reliableRevision = *storage.CurrentRevision
		}
		return newAppError(code, reliableRevision, err)
	}
	return newAppError(AppStoreError, reliableRevision, err)
}
