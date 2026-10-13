package amazonimport

import (
	"context"
	"errors"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/importer/amazon"
)

// Code is a stable renderer-neutral coordinator failure code.
type Code string

// Stable coordinator failure codes.
const (
	CodeImportBusy            Code = "amazon_import_busy"
	CodeImportEmpty           Code = "amazon_import_empty"
	CodeImportTooLarge        Code = "amazon_import_too_large"
	CodeImportInvalid         Code = "amazon_import_invalid"
	CodeImportStorage         Code = "amazon_import_storage"
	CodeProfileChanged        Code = "amazon_profile_changed"
	CodeCurrencyMismatch      Code = "amazon_currency_mismatch"
	CodeProfileInvalid        Code = "amazon_profile_invalid"
	CodeTaxonomySourceInvalid Code = "amazon_taxonomy_source_invalid"
	CodeImportCanceled        Code = "import_cancelled"
	CodeAttemptInvalid        Code = "amazon_attempt_invalid"
	CodeAttemptStale          Code = "amazon_attempt_stale"
)

var details = map[Code]string{
	CodeImportBusy:            "Another process is importing this profile.",
	CodeImportEmpty:           "No eligible Amazon order-history records were found.",
	CodeImportTooLarge:        "The Amazon import exceeds a fixed safety limit.",
	CodeImportInvalid:         "The Amazon order-history export is invalid.",
	CodeImportStorage:         "Moneyflow could not save the Amazon import on this computer.",
	CodeProfileChanged:        "The profile changed while the Amazon import was being saved.",
	CodeCurrencyMismatch:      "The Amazon export currency does not match this profile.",
	CodeProfileInvalid:        "The target is not a usable Amazon profile.",
	CodeTaxonomySourceInvalid: "The taxonomy source is not usable.",
	CodeImportCanceled:        "The Amazon import was canceled.",
	CodeAttemptInvalid:        "The Amazon import attempt is no longer available.",
	CodeAttemptStale:          "The Amazon import attempt changed and must be refreshed.",
}

// Error carries an initiating-session-only source coordinate.
type Error struct {
	Code       Code
	Coordinate amazon.Coordinate
	cause      error
}

func (failure *Error) Error() string {
	if failure == nil {
		return "Amazon import failed"
	}
	return details[failure.Code]
}

func (failure *Error) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.cause
}

func newError(code Code, cause error) error { return &Error{Code: code, cause: cause} }

func mapError(err error) error {
	if err == nil {
		return nil
	}
	var existing *Error
	if errors.As(err, &existing) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return newError(CodeImportCanceled, err)
	}
	var parser *amazon.Error
	if errors.As(err, &parser) {
		code := CodeImportInvalid
		switch parser.Code {
		case amazon.CodeEmpty:
			code = CodeImportEmpty
		case amazon.CodeTooLarge:
			code = CodeImportTooLarge
		case amazon.CodeInvalid:
		}
		return &Error{Code: code, Coordinate: parser.Coordinate, cause: err}
	}
	return mapInstallError(err)
}

// mapInstallError names the profile-side reasons an install can fail; others stay unmapped.
func mapInstallError(err error) error {
	if errors.Is(err, app.ErrAmazonSettingsMismatch) {
		return newError(CodeCurrencyMismatch, err)
	}
	var failure *app.AppError
	if !errors.As(err, &failure) {
		return err
	}
	switch failure.Code {
	case app.AppStoreBusy, app.AppRevisionConflict:
		return newError(CodeProfileChanged, err)
	case app.AppStoreError:
		return newError(CodeImportStorage, err)
	default:
		return err
	}
}

// CodeOf returns a stable code or an empty value.
func CodeOf(err error) Code {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

// CoordinateOf returns an initiating-session-only coordinate.
func CoordinateOf(err error) amazon.Coordinate {
	var failure *Error
	if errors.As(err, &failure) {
		return failure.Coordinate
	}
	return amazon.Coordinate{}
}
