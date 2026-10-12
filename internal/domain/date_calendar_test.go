package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDateCalendarBoundaries(t *testing.T) {
	t.Parallel()
	leapDay := mustTestDate(t, "2028-02-29")
	assert.Equal(t, "2028-02-01", leapDay.MonthStart().String())
	assert.Equal(t, "2028-02-29", leapDay.MonthEnd().String())
	assert.Equal(t, "2027-02-28", mustTestDate(t, "2027-02-10").MonthEnd().String())
	assert.Equal(t, "2028-01-01", leapDay.YearStart().String())
	assert.Equal(t, "2028-12-31", leapDay.YearEnd().String())
}

func TestDateAddMonthsClamped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		start string
		delta int
		want  string
	}{
		{start: "2026-01-31", delta: 1, want: "2026-02-28"},
		{start: "2028-01-31", delta: 1, want: "2028-02-29"},
		{start: "2026-10-08", delta: -12, want: "2025-10-08"},
		{start: "2026-03-31", delta: -1, want: "2026-02-28"},
		{start: "2026-01-15", delta: -1, want: "2025-12-15"},
		{start: "2026-12-15", delta: 1, want: "2027-01-15"},
	}
	for _, test := range tests {
		got, err := mustTestDate(t, test.start).AddMonthsClamped(test.delta)
		require.NoError(t, err)
		assert.Equal(t, test.want, got.String(), "%s %+d", test.start, test.delta)
	}
	_, err := mustTestDate(t, "0001-01-15").AddMonthsClamped(-1)
	require.Error(t, err)
	_, err = mustTestDate(t, "9999-12-15").AddMonthsClamped(1)
	require.Error(t, err)
}

func TestDaysBetweenCountsCalendarDays(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 30, DaysBetween(mustTestDate(t, "2026-03-10"), mustTestDate(t, "2026-04-09")))
	assert.Equal(t, 366, DaysBetween(mustTestDate(t, "2028-01-01"), mustTestDate(t, "2029-01-01")))
	assert.Equal(t, -1, DaysBetween(mustTestDate(t, "2026-01-02"), mustTestDate(t, "2026-01-01")))
	first, last := mustTestDate(t, "0001-01-01"), mustTestDate(t, "9999-12-31")
	assert.Equal(t, 3652058, DaysBetween(first, last), "spans beyond 292 years stay exact")
	assert.True(t, Date{}.IsZero())
	assert.False(t, mustTestDate(t, "2026-01-01").IsZero())
}

func mustTestDate(t *testing.T, value string) Date {
	t.Helper()
	date, err := ParseDate(value)
	require.NoError(t, err)
	return date
}
