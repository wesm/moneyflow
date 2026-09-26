package bankcsv

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/csvlimit"
)

// Row contains canonical source facts for one occurrence in an export.
type Row struct {
	SourceKey                 string
	AccountKey                string
	Date                      domain.Date
	Amount                    domain.Money
	Merchant, Category, Notes string
	Metadata                  map[string]string
	Occurrence                int
}

// File is a fully parsed snapshot; its keys never expose the absolute source path.
type File struct {
	Key, Digest, Account, AccountKey string
	Rows                             []Row
	Skipped                          []Diagnostic
	Bytes                            int64
	Records                          int
}

// Diagnostic names a source coordinate without echoing rejected financial values.
type Diagnostic struct {
	File           string
	Record         int
	Column, Reason string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s: record %d, column %s: %s", d.File, d.Record, d.Column, d.Reason)
}

// SourceKey includes the per-file occurrence, preserving identical purchases.
func SourceKey(mapping Mapping, row Row) string {
	return tupleHash(mapping.Name, row.AccountKey, row.Date.String(), row.Amount.Minor,
		row.Amount.Currency, row.Amount.Scale, row.Merchant, row.Occurrence)
}

func tupleHash(values ...any) string {
	data, _ := json.Marshal(values) // All callers supply primitive scalars.
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// Parse hashes the bytes it consumes and returns no partial file on a framing or I/O failure.
func Parse(ctx context.Context, input io.Reader, mapping Mapping, account, path string, limits Limits) (File, error) {
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err := mapping.Validate(); err != nil {
		return File{}, err
	}
	if err := limits.validate(); err != nil {
		return File{}, err
	}
	label, err := domain.NormalizeDisplayLabel(account)
	if err != nil {
		return File{}, errors.New("invalid account label")
	}
	key, err := domain.CollisionKey(label)
	if err != nil {
		return File{}, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return File{}, err
	}
	result := File{Key: tupleHash(absolute, mapping.Name, key), Account: label, AccountKey: key}
	fail := func(record int, column, reason string) (File, error) {
		return File{}, Diagnostic{filepath.Base(path), record, column, reason}
	}
	limit := &io.LimitedReader{R: input, N: limits.BytesPerFile + 1}
	hash := sha256.New()
	var source io.Reader = contextReader{ctx, io.TeeReader(limit, hash)}
	// Strip BOM before both CSV state machines, but include it in the file digest.
	buffer := make([]byte, 3)
	n, readErr := io.ReadFull(source, buffer)
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		return fail(0, "", "cannot read CSV header")
	}
	if string(buffer[:n]) != "\ufeff" {
		source = io.MultiReader(strings.NewReader(string(buffer[:n])), source)
	}
	reader := csv.NewReader(csvlimit.NewReader(source, limits.BytesPerRecord, limits.Columns))
	header, err := reader.Read()
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	if err != nil {
		return fail(0, "", "invalid CSV header")
	}
	indexes := make(map[string]int, len(header))
	for i, field := range header {
		if !utf8.ValidString(field) || int64(len(field)) > limits.BytesPerField {
			return fail(0, "", "invalid header field")
		}
		if _, exists := indexes[field]; exists {
			return fail(0, "", "duplicate header")
		}
		indexes[field] = i
	}
	for _, column := range []string{mapping.Date, mapping.Merchant, mapping.Amount, mapping.Credit, mapping.Debit} {
		if column != "" {
			if _, ok := indexes[column]; !ok {
				return fail(0, column, "required header missing")
			}
		}
	}
	occurrences := map[string]int{}
	for {
		if err := ctx.Err(); err != nil {
			return File{}, err
		}
		fields, err := reader.Read()
		if err := ctx.Err(); err != nil {
			return File{}, err
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(result.Records+1, "", "invalid CSV framing or record limit exceeded")
		}
		result.Records++
		if result.Records > limits.Records {
			return fail(result.Records, "", "record limit exceeded")
		}
		for _, field := range fields {
			if !utf8.ValidString(field) || int64(len(field)) > limits.BytesPerField {
				return fail(result.Records, "", "invalid UTF-8 or field limit exceeded")
			}
		}
		get := func(column string) string {
			if i, ok := indexes[column]; ok {
				return fields[i]
			}
			return ""
		}
		row, column, reason := parseRow(mapping, get)
		if reason != "" {
			result.Skipped = append(result.Skipped, Diagnostic{filepath.Base(path), result.Records, column, reason})
			continue
		}
		row.AccountKey = key
		base := SourceKey(mapping, row)
		occurrences[base]++
		row.Occurrence = occurrences[base]
		row.SourceKey = SourceKey(mapping, row)
		result.Rows = append(result.Rows, row)
	}
	result.Bytes = limits.BytesPerFile + 1 - limit.N
	if result.Bytes > limits.BytesPerFile || result.Bytes > limits.TotalBytes {
		return fail(result.Records, "", "file byte limit exceeded")
	}
	if err := ctx.Err(); err != nil {
		return File{}, err
	}
	result.Digest = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}

func parseRow(mapping Mapping, get func(string) string) (Row, string, string) {
	row := Row{Merchant: strings.TrimSpace(get(mapping.Merchant)), Category: strings.TrimSpace(get(mapping.Category)), Notes: get(mapping.Notes), Metadata: map[string]string{}}
	parsed, err := time.Parse(mapping.DateLayout, strings.TrimSpace(get(mapping.Date)))
	if err != nil {
		return Row{}, mapping.Date, "invalid date"
	}
	row.Date, err = domain.ParseDate(parsed.Format("2006-01-02"))
	if err != nil {
		return Row{}, mapping.Date, "invalid date"
	}
	if _, err = domain.NormalizeDisplayLabel(row.Merchant); err != nil {
		return Row{}, mapping.Merchant, "invalid merchant"
	}
	if row.Category == "" {
		row.Category = "Uncategorized"
	}
	if _, err = domain.NormalizeDisplayLabel(row.Category); err != nil {
		return Row{}, mapping.Category, "invalid category"
	}
	amount := func(column string) (domain.Money, error) {
		value := strings.TrimSpace(get(column))
		if value == "" && mapping.Amount == "" {
			value = "0"
		}
		return domain.ParseMoney(value, mapping.Currency, mapping.Scale)
	}
	if mapping.Amount != "" {
		row.Amount, err = amount(mapping.Amount)
	} else {
		if strings.TrimSpace(get(mapping.Credit)) == "" && strings.TrimSpace(get(mapping.Debit)) == "" {
			return Row{}, "amount", "both credit and debit are blank"
		}
		var debit domain.Money
		row.Amount, err = amount(mapping.Credit)
		if err == nil {
			debit, err = amount(mapping.Debit)
		}
		if err == nil {
			row.Amount, err = row.Amount.Sub(debit)
		}
	}
	if err != nil {
		return Row{}, "amount", "invalid exact decimal amount"
	}
	for _, column := range mapping.Metadata {
		if value := get(column); value != "" {
			row.Metadata[column] = value
		}
	}
	return row, "", ""
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
