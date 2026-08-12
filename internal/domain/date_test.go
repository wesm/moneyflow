package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDateConstructionAndJSON(t *testing.T) {
	t.Parallel()

	date, err := NewDate(2024, time.February, 29)
	require.NoError(t, err)
	assert.Equal(t, 2024, date.Year())
	assert.Equal(t, time.February, date.Month())
	assert.Equal(t, 29, date.Day())
	assert.Equal(t, "2024-02-29", date.String())

	encoded, err := json.Marshal(date)
	require.NoError(t, err)
	assert.JSONEq(t, `"2024-02-29"`, string(encoded))

	var decoded Date
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, date, decoded)
}

func TestParseDateRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"", "2024-2-01", "2024-02-30", "2023-02-29", "0000-01-01", "10000-01-01",
	} {
		_, err := ParseDate(value)
		assert.Error(t, err, value)
	}
}

func TestDateComparison(t *testing.T) {
	t.Parallel()

	first, err := ParseDate("2024-01-31")
	require.NoError(t, err)
	same, err := ParseDate("2024-01-31")
	require.NoError(t, err)
	later, err := ParseDate("2024-02-01")
	require.NoError(t, err)

	assert.Equal(t, 0, first.Compare(same))
	assert.Equal(t, -1, first.Compare(later))
	assert.Equal(t, 1, later.Compare(first))
}

func TestDateAddDaysAcrossBoundaries(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"2024-02-28": "2024-02-29",
		"2024-02-29": "2024-03-01",
		"2024-12-31": "2025-01-01",
	}
	for input, expected := range tests {
		date, err := ParseDate(input)
		require.NoError(t, err)
		got, err := date.AddDays(1)
		require.NoError(t, err)
		assert.Equal(t, expected, got.String())
	}
}

func TestDateAddDaysRejectsSupportedRangeOverflow(t *testing.T) {
	t.Parallel()

	minimum, err := ParseDate("0001-01-01")
	require.NoError(t, err)
	_, err = minimum.AddDays(-1)
	assert.Error(t, err)

	maximum, err := ParseDate("9999-12-31")
	require.NoError(t, err)
	_, err = maximum.AddDays(1)
	assert.Error(t, err)
}
