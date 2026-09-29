// Package csvlimit bounds CSV records before encoding/csv allocates fields.
package csvlimit

import (
	"errors"
	"io"
)

// ErrLimit means a logical record exceeded its byte or column budget.
var ErrLimit = errors.New("CSV record or column limit exceeded")

// NewReader bounds RFC 4180 logical records before field allocation.
func NewReader(input io.Reader, bytesPerRecord int64, columns int) io.Reader {
	return &csvBoundaryReader{reader: input, maxRecordBytes: bytesPerRecord,
		maxColumns: columns, columns: 1, atFieldStart: true}
}

// csvBoundaryReader rejects pathological records before encoding/csv allocates their field slice.
// It recognizes RFC 4180 quoted delimiters and counts the delimiters in the record-byte budget.
type csvBoundaryReader struct {
	reader         io.Reader
	maxRecordBytes int64
	maxColumns     int
	recordBytes    int64
	columns        int
	inQuotes       bool
	quotePending   bool
	atFieldStart   bool
}

func (reader *csvBoundaryReader) Read(buffer []byte) (int, error) {
	count, readErr := reader.reader.Read(buffer)
	for index := 0; index < count; index++ {
		if err := reader.accept(buffer[index]); err != nil {
			return index, err
		}
	}
	return count, readErr
}

func (reader *csvBoundaryReader) accept(value byte) error {
	reader.recordBytes++
	if reader.recordBytes > reader.maxRecordBytes {
		return ErrLimit
	}
	if reader.inQuotes {
		if !reader.quotePending {
			if value == '"' {
				reader.quotePending = true
			}
			return nil
		}
		if value == '"' {
			reader.quotePending = false
			return nil
		}
		reader.inQuotes = false
		reader.quotePending = false
	}
	if value == '"' && reader.atFieldStart {
		reader.inQuotes = true
		reader.atFieldStart = false
		return nil
	}
	switch value {
	case ',':
		reader.columns++
		reader.atFieldStart = true
		if reader.columns > reader.maxColumns {
			return ErrLimit
		}
	case '\n':
		reader.recordBytes = 0
		reader.columns = 1
		reader.atFieldStart = true
	default:
		reader.atFieldStart = false
	}
	return nil
}
