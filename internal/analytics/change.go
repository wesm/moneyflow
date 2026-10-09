package analytics

import (
	"errors"
	"math"
)

// ChangeKind names how a comparison between two periods is displayed.
type ChangeKind string

// Supported change kinds.
const (
	// ChangeNone means both values are zero and displays as "—".
	ChangeNone ChangeKind = "none"
	// ChangeNew means the prior value is zero and the current value is not.
	ChangeNew ChangeKind = "new"
	// ChangePercent is a change in magnitude as a percent of the prior magnitude.
	ChangePercent ChangeKind = "percent"
	// ChangeAmount is the signed difference shown when Net changed sign.
	ChangeAmount ChangeKind = "amount"
)

var errChangeOverflow = errors.New("change: amount overflows int64")

// Change is one exact, server-computed comparison.
type Change struct {
	Kind ChangeKind
	// PercentTenths is the change in magnitude in tenths of a percent, rounded half up.
	PercentTenths int
	// Grew reports that the magnitude grew. It is meaningful for ChangePercent only.
	Grew bool
	// Favorable selects green rather than red.
	Favorable bool
	// Amount is current minus prior in minor units. It is set for ChangeAmount only.
	Amount int64
}

// NetChange compares two Net values with the vs prior rules: spending (negative Net) that grew
// and income that shrank are unfavorable, and a sign change shows the signed difference.
func NetChange(current, prior int64) (Change, error) {
	switch {
	case current == 0 && prior == 0:
		return Change{Kind: ChangeNone, Favorable: true}, nil
	case prior == 0:
		return Change{Kind: ChangeNew, Favorable: current > 0}, nil
	case current != 0 && (current > 0) != (prior > 0):
		if (prior < 0 && current > math.MaxInt64+prior) || (prior > 0 && current < math.MinInt64+prior) {
			return Change{}, errChangeOverflow
		}
		amount := current - prior
		return Change{Kind: ChangeAmount, Amount: amount, Favorable: amount > 0}, nil
	default:
		return magnitudeChange(absMinor(current), absMinor(prior), prior > 0)
	}
}

// IncomeChange compares two In values; growth is favorable.
func IncomeChange(current, prior int64) (Change, error) {
	return totalChange(absMinor(current), absMinor(prior), true)
}

// SpendingChange compares the magnitudes of two Out values; growth is unfavorable.
func SpendingChange(current, prior int64) (Change, error) {
	return totalChange(absMinor(current), absMinor(prior), false)
}

func totalChange(current, prior uint64, growthFavorable bool) (Change, error) {
	switch {
	case current == 0 && prior == 0:
		return Change{Kind: ChangeNone, Favorable: true}, nil
	case prior == 0:
		return Change{Kind: ChangeNew, Favorable: growthFavorable}, nil
	default:
		return magnitudeChange(current, prior, growthFavorable)
	}
}

// magnitudeChange requires a nonzero prior. Equal magnitudes are favorable.
func magnitudeChange(current, prior uint64, growthFavorable bool) (Change, error) {
	difference := current - prior
	if current < prior {
		difference = prior - current
	}
	tenths, err := ratioHalfUp(difference, prior, 1000)
	if err != nil {
		return Change{}, err
	}
	grew, shrank := current > prior, current < prior
	favorable := (growthFavorable && !shrank) || (!growthFavorable && !grew)
	return Change{Kind: ChangePercent, PercentTenths: tenths, Grew: grew, Favorable: favorable}, nil
}
