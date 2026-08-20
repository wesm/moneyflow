// Package amazon discovers and parses bounded Amazon order-history CSV exports.
package amazon

import (
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

type Code string

const (
	CodeEmpty    Code = "amazon_import_empty"
	CodeTooLarge Code = "amazon_import_too_large"
	CodeInvalid  Code = "amazon_import_invalid"
)

var (
	ErrEmpty    = errors.New("Amazon import contains no eligible data")
	ErrTooLarge = errors.New("Amazon import exceeds a fixed limit")
	ErrInvalid  = errors.New("Amazon import is invalid")
)

type Error struct {
	Code       Code
	Coordinate Coordinate
	cause      error
}

func (err *Error) Error() string {
	if err == nil {
		return "Amazon import failed"
	}
	return string(err.Code)
}

func (err *Error) Unwrap() error { return err.cause }

func newError(code Code, cause error) error {
	return &Error{Code: code, cause: cause}
}

func coordinateError(file string, record int, column, reason string, cause error) error {
	return &Error{
		Code: CodeInvalid, Coordinate: Coordinate{
			RelativeFilename: file, Record: record, Column: column, Reason: reason,
		}, cause: cause,
	}
}

type Settings struct {
	Currency domain.Currency
	Scale    uint8
}

type Limits struct {
	Files          int
	Records        int
	Columns        int
	BytesPerFile   int64
	TotalBytes     int64
	BytesPerRecord int64
	BytesPerField  int64
}

var ProductionLimits = Limits{
	Files: 256, Records: 1_000_000, Columns: 128,
	BytesPerFile: 64 << 20, TotalBytes: 512 << 20,
	BytesPerRecord: 1 << 20, BytesPerField: 16 << 10,
}

func (limits Limits) validate() error {
	if limits.Files < 1 || limits.Records < 1 || limits.Columns < 1 ||
		limits.BytesPerFile < 1 || limits.TotalBytes < 1 ||
		limits.BytesPerRecord < 1 || limits.BytesPerField < 1 {
		return fmt.Errorf("validate Amazon limits: %w", ErrInvalid)
	}
	return nil
}

type Coordinate struct {
	RelativeFilename string
	Record           int
	Column           string
	Reason           string
}

type SourceFile struct {
	RelativeName string
	Path         string
}

type Row struct {
	OrderID             string
	ProductName         string
	ASIN                string
	ASINLessKey         string
	OrderDate           domain.Date
	Quantity            int64
	AmountMinor         int64
	UnitPriceMinor      *int64
	Currency            domain.Currency
	Scale               uint8
	OrderStatus         string
	ShipmentStatus      string
	IdentityFingerprint string
	FullFingerprint     string
	RelativeFilename    string
	Record              int
}

type Candidate struct {
	Rows                 []Row
	ObservedOrderIDs     []string
	FileCount            int
	LogicalRecordCount   int
	BlankRecordCount     int
	CancelledRecordCount int
	Digest               string
}

type Progress struct {
	Phase     string
	Completed int
	Total     int
}

type ObserveFunc func(Progress)
