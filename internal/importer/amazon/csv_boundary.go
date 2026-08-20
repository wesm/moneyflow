package amazon

import (
	"errors"
	"io"
)

var errCSVBoundary = errors.New("amazon CSV boundary exceeded")

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
		return errCSVBoundary
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
			return errCSVBoundary
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
