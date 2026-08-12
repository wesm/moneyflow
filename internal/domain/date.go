package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Date is a calendar date without a time or time zone.
type Date struct {
	year  int
	month time.Month
	day   int
}

// NewDate validates and constructs a Date.
func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < 1 || year > 9999 {
		return Date{}, errors.New("new date: year must be between 1 and 9999")
	}
	value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return Date{}, errors.New("new date: invalid calendar date")
	}
	return Date{year: year, month: month, day: day}, nil
}

// ParseDate parses an ISO YYYY-MM-DD date.
func ParseDate(iso string) (Date, error) {
	if len(iso) != len("2006-01-02") {
		return Date{}, errors.New("parse date: expected YYYY-MM-DD")
	}
	value, err := time.Parse("2006-01-02", iso)
	if err != nil || value.Format("2006-01-02") != iso {
		return Date{}, fmt.Errorf("parse date: invalid ISO date %q", iso)
	}
	return NewDate(value.Year(), value.Month(), value.Day())
}

// Year returns the four-digit year.
func (d Date) Year() int { return d.year }

// Month returns the calendar month.
func (d Date) Month() time.Month { return d.month }

// Day returns the day of the month.
func (d Date) Day() int { return d.day }

// Compare returns -1, 0, or 1 according to chronological order.
func (d Date) Compare(other Date) int {
	left := d.ordinal()
	right := other.ordinal()
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

// AddDays adds calendar days without leaving the supported year range.
func (d Date) AddDays(delta int) (Date, error) {
	value := time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC).AddDate(0, 0, delta)
	if value.Year() < 1 || value.Year() > 9999 {
		return Date{}, errors.New("add date: result outside supported range")
	}
	return NewDate(value.Year(), value.Month(), value.Day())
}

// String returns the ISO YYYY-MM-DD representation.
func (d Date) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}

// MarshalJSON encodes Date as an ISO string.
func (d Date) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// UnmarshalJSON decodes Date from an ISO string.
func (d *Date) UnmarshalJSON(data []byte) error {
	var iso string
	if err := json.Unmarshal(data, &iso); err != nil {
		return fmt.Errorf("decode date: %w", err)
	}
	parsed, err := ParseDate(iso)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

func (d Date) ordinal() int {
	return d.year*10000 + int(d.month)*100 + d.day
}
