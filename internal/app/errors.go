package app

import (
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
	AppProviderReconnectRequired            AppErrorCode = "provider_reconnect_required"
	AppProviderIdentityMismatch             AppErrorCode = "provider_identity_mismatch"
	AppProviderSnapshotUnstable             AppErrorCode = "provider_snapshot_unstable"
	AppProviderRefreshInProgress            AppErrorCode = "provider_refresh_in_progress"
	AppProviderDeletionConfirmationRequired AppErrorCode = "provider_deletion_confirmation_required"
	AppProviderConfirmationInvalid          AppErrorCode = "provider_confirmation_invalid"
	AppProviderRefreshStale                 AppErrorCode = "provider_refresh_stale"
	AppProviderRateLimited                  AppErrorCode = "provider_rate_limited"
	AppProviderUnavailable                  AppErrorCode = "provider_unavailable"
	AppProviderDataInvalid                  AppErrorCode = "provider_data_invalid"
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
	AppProviderReconnectRequired:            "Reconnect the provider through the command line.",
	AppProviderIdentityMismatch:             "The provider profile does not match this local profile.",
	AppProviderSnapshotUnstable:             "The provider returned inconsistent data after three complete attempts. No financial data changed. Retry once; if it repeats, report the progress counts.",
	AppProviderRefreshInProgress:            "Another process is refreshing this profile.",
	AppProviderDeletionConfirmationRequired: "Confirm the proposed provider removals.",
	AppProviderConfirmationInvalid:          "The refresh confirmation is no longer valid.",
	AppProviderRefreshStale:                 "A newer provider refresh already committed.",
	AppProviderRateLimited:                  "The provider rate limit prevented refresh.",
	AppProviderUnavailable:                  "The provider is temporarily unavailable.",
	AppProviderDataInvalid:                  "The provider returned invalid data.",
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
	return &AppError{
		Code: code, Detail: appErrorDetails[code], CurrentRevision: current, cause: cause,
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
