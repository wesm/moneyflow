package analytics

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetChangeFollowsVsPriorRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		current, prior int64
		want           Change
	}{
		{name: "both zero", want: Change{Kind: ChangeNone, Favorable: true}},
		{name: "new spending", current: -5000,
			want: Change{Kind: ChangeNew, Favorable: false}},
		{name: "new income", current: 5000,
			want: Change{Kind: ChangeNew, Favorable: true}},
		// The spec's worked example: −360.00 against −300.00 is red ▲ 20.0%.
		{name: "spending grew", current: -36000, prior: -30000,
			want: Change{Kind: ChangePercent, PercentTenths: 200, Grew: true, Favorable: false}},
		{name: "spending shrank", current: -30000, prior: -36000,
			want: Change{Kind: ChangePercent, PercentTenths: 167, Favorable: true}},
		{name: "income grew", current: 12000, prior: 10000,
			want: Change{Kind: ChangePercent, PercentTenths: 200, Grew: true, Favorable: true}},
		{name: "income shrank", current: 10000, prior: 12000,
			want: Change{Kind: ChangePercent, PercentTenths: 167, Favorable: false}},
		{name: "unchanged spending", current: -100, prior: -100,
			want: Change{Kind: ChangePercent, Favorable: true}},
		{name: "spending fell to zero", current: 0, prior: -2500,
			want: Change{Kind: ChangePercent, PercentTenths: 1000, Favorable: true}},
		{name: "income fell to zero", current: 0, prior: 2500,
			want: Change{Kind: ChangePercent, PercentTenths: 1000, Favorable: false}},
		{name: "spending became income", current: 16000, prior: -30000,
			want: Change{Kind: ChangeAmount, Amount: 46000, Favorable: true}},
		{name: "income became spending", current: -16000, prior: 30000,
			want: Change{Kind: ChangeAmount, Amount: -46000, Favorable: false}},
		{name: "half rounds up", current: -1001, prior: -2000,
			want: Change{Kind: ChangePercent, PercentTenths: 500, Favorable: true}},
	}
	for _, test := range tests {
		got, err := NetChange(test.current, test.prior)
		require.NoError(t, err, test.name)
		assert.Equal(t, test.want, got, test.name)
	}
}

func TestNetChangeRejectsOverflow(t *testing.T) {
	t.Parallel()
	_, err := NetChange(math.MaxInt64, -1)
	require.Error(t, err)
	_, err = NetChange(math.MinInt64, 1)
	require.Error(t, err)
}

func TestIncomeAndSpendingChanges(t *testing.T) {
	t.Parallel()
	income, err := IncomeChange(150000, 100000)
	require.NoError(t, err)
	want := Change{Kind: ChangePercent, PercentTenths: 500, Grew: true, Favorable: true}
	assert.Equal(t, want, income)

	spending, err := SpendingChange(-150000, -100000)
	require.NoError(t, err)
	want.Favorable = false
	assert.Equal(t, want, spending)

	newSpending, err := SpendingChange(-1, 0)
	require.NoError(t, err)
	assert.Equal(t, Change{Kind: ChangeNew, Favorable: false}, newSpending)

	none, err := IncomeChange(0, 0)
	require.NoError(t, err)
	assert.Equal(t, ChangeNone, none.Kind)
}
