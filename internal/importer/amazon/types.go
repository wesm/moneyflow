// Package amazon discovers and parses bounded Amazon order-history CSV exports.
package amazon

import (
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// Code is a stable renderer-neutral Amazon import failure code.
type Code string

// Stable Amazon import failure codes.
const (
	CodeEmpty    Code = "amazon_import_empty"
	CodeTooLarge Code = "amazon_import_too_large"
	CodeInvalid  Code = "amazon_import_invalid"
)

var (
	// ErrEmpty reports a source with no eligible order rows.
	ErrEmpty = errors.New("amazon import contains no eligible data")
	// ErrTooLarge reports a source that exceeds a fixed parser bound.
	ErrTooLarge = errors.New("amazon import exceeds a fixed limit")
	// ErrInvalid reports malformed or unsupported source data.
	ErrInvalid = errors.New("amazon import is invalid")
)

// Error retains one optional in-session source coordinate without exposing it in Error.
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

// Settings defines the exact-money binding used to parse one candidate.
type Settings struct {
	Currency domain.Currency
	Scale    uint8
}

// Limits bounds discovery and parsing resource use.
type Limits struct {
	Files          int
	Records        int
	Columns        int
	BytesPerFile   int64
	TotalBytes     int64
	BytesPerRecord int64
	BytesPerField  int64
}

// ProductionLimits defines the fixed import bounds used by application surfaces.
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

// Coordinate identifies one actionable source record for the initiating UI only.
type Coordinate struct {
	RelativeFilename string
	Record           int
	Column           string
	Reason           string
}

// SourceFile is one rooted regular CSV input in canonical relative-name order.
type SourceFile struct {
	RelativeName string
	Path         string
}

// Row is one normalized non-cancelled Amazon order item.
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

// Candidate is one fully parsed, bounded, deterministic import observation.
type Candidate struct {
	Rows                 []Row
	ObservedOrderIDs     []string
	FileCount            int
	LogicalRecordCount   int
	BlankRecordCount     int
	CancelledRecordCount int
	Digest               string
}

// Progress contains counts-only parser progress safe for every renderer.
type Progress struct {
	Phase     string
	Completed int
	Total     int
}

// ObserveFunc receives counts-only parser progress.
type ObserveFunc func(Progress)
