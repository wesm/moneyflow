// Package domain defines moneyflow's normalized financial values.
package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	errMoneyMismatch = errors.New("money currency or scale mismatch")
	errMoneyOverflow = errors.New("money minor-unit overflow")
)

// Currency is an ISO currency identifier at a data boundary.
type Currency string

// Money is an exact signed minor-unit value.
type Money struct {
	Minor    int64
	Currency Currency
	Scale    uint8
}

// ParseMoney parses a decimal without using floating-point arithmetic.
func ParseMoney(decimal string, currency Currency, scale uint8) (Money, error) {
	if currency == "" {
		return Money{}, errors.New("parse money: currency is empty")
	}
	if decimal == "" || strings.TrimSpace(decimal) != decimal {
		return Money{}, errors.New("parse money: invalid decimal")
	}

	negative := false
	digits := decimal
	if digits[0] == '+' || digits[0] == '-' {
		negative = digits[0] == '-'
		digits = digits[1:]
	}
	if digits == "" {
		return Money{}, errors.New("parse money: missing digits")
	}

	parts := strings.Split(digits, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return Money{}, errors.New("parse money: invalid decimal point")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > int(scale) {
		return Money{}, fmt.Errorf("parse money: precision exceeds scale %d", scale)
	}
	allDigits := parts[0] + fraction + strings.Repeat("0", int(scale)-len(fraction))
	limit := uint64(math.MaxInt64)
	if negative {
		limit++
	}

	var magnitude uint64
	for _, digit := range allDigits {
		if digit < '0' || digit > '9' {
			return Money{}, errors.New("parse money: non-decimal character")
		}
		value := uint64(digit - '0')
		if magnitude > (limit-value)/10 {
			return Money{}, errMoneyOverflow
		}
		magnitude = magnitude*10 + value
	}

	minor := int64(magnitude)
	if negative {
		if magnitude == uint64(math.MaxInt64)+1 {
			minor = math.MinInt64
		} else {
			minor = -minor
		}
	}
	return Money{Minor: minor, Currency: currency, Scale: scale}, nil
}

// Add adds compatible money values with overflow checking.
func (m Money) Add(other Money) (Money, error) {
	if err := m.compatible(other); err != nil {
		return Money{}, err
	}
	if (other.Minor > 0 && m.Minor > math.MaxInt64-other.Minor) ||
		(other.Minor < 0 && m.Minor < math.MinInt64-other.Minor) {
		return Money{}, errMoneyOverflow
	}
	m.Minor += other.Minor
	return m, nil
}

// Sub subtracts compatible money values with overflow checking.
func (m Money) Sub(other Money) (Money, error) {
	if err := m.compatible(other); err != nil {
		return Money{}, err
	}
	if (other.Minor > 0 && m.Minor < math.MinInt64+other.Minor) ||
		(other.Minor < 0 && m.Minor > math.MaxInt64+other.Minor) {
		return Money{}, errMoneyOverflow
	}
	m.Minor -= other.Minor
	return m, nil
}

// Abs returns the absolute value with overflow checking.
func (m Money) Abs() (Money, error) {
	if m.Minor == math.MinInt64 {
		return Money{}, errMoneyOverflow
	}
	if m.Minor < 0 {
		m.Minor = -m.Minor
	}
	return m, nil
}

// Cmp compares compatible money values.
func (m Money) Cmp(other Money) (int, error) {
	if err := m.compatible(other); err != nil {
		return 0, err
	}
	switch {
	case m.Minor < other.Minor:
		return -1, nil
	case m.Minor > other.Minor:
		return 1, nil
	default:
		return 0, nil
	}
}

// DecimalString returns the exact canonical decimal representation.
func (m Money) DecimalString() string {
	negative := m.Minor < 0
	digits := strings.TrimPrefix(strconv.FormatInt(m.Minor, 10), "-")
	if m.Scale > 0 {
		minimum := int(m.Scale) + 1
		if len(digits) < minimum {
			digits = strings.Repeat("0", minimum-len(digits)) + digits
		}
		point := len(digits) - int(m.Scale)
		digits = digits[:point] + "." + digits[point:]
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func (m Money) compatible(other Money) error {
	if m.Currency != other.Currency || m.Scale != other.Scale {
		return errMoneyMismatch
	}
	return nil
}
