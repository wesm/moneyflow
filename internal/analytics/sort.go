package analytics

import (
	"math/big"
	"sort"
	"strings"

	"github.com/wesm/moneyflow/internal/domain"
)

// SortAggregateRows returns a deterministic copy ordered for display.
func SortAggregateRows(rows []domain.AggregateRow, sortSpec domain.SortSpec) []domain.AggregateRow {
	ordered := append([]domain.AggregateRow(nil), rows...)
	for index := range ordered {
		if ordered[index].Period != nil {
			period := *ordered[index].Period
			ordered[index].Period = &period
		}
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		comparison := compareAggregatePrimary(ordered[left], ordered[right], sortSpec.Field)
		if comparison != 0 {
			ascending := sortSpec.Direction == domain.SortDirectionAsc
			if sortSpec.Field == domain.SortFieldAmount {
				ascending = !ascending
			}
			if ascending {
				return comparison < 0
			}
			return comparison > 0
		}
		return compareAggregateTie(ordered[left], ordered[right]) < 0
	})
	return ordered
}

func compareAggregatePrimary(left, right domain.AggregateRow, field domain.SortField) int {
	switch field {
	case domain.SortFieldCount:
		return compareInt(left.Count, right.Count)
	case domain.SortFieldAmount:
		return compareScaledMinor(left.Total.Minor, left.Total.Scale, right.Total.Minor, right.Total.Scale)
	case domain.SortFieldTimePeriod:
		return comparePeriodPointers(left.Period, right.Period)
	case domain.SortFieldMerchant, domain.SortFieldCategory, domain.SortFieldGroup, domain.SortFieldAccount:
		return strings.Compare(left.Label, right.Label)
	default:
		return 0
	}
}

func compareAggregateTie(left, right domain.AggregateRow) int {
	if comparison := strings.Compare(string(left.Total.Currency), string(right.Total.Currency)); comparison != 0 {
		return comparison
	}
	if comparison := compareInt(int(left.Total.Scale), int(right.Total.Scale)); comparison != 0 {
		return comparison
	}
	if comparison := comparePeriodPointers(left.Period, right.Period); comparison != 0 {
		return comparison
	}
	if comparison := strings.Compare(left.Label, right.Label); comparison != 0 {
		return comparison
	}
	return strings.Compare(left.Key, right.Key)
}

func comparePeriodPointers(left, right *domain.Period) int {
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}
	return comparePeriods(*left, *right)
}

func comparePeriods(left, right domain.Period) int {
	if comparison := compareInt(left.Year, right.Year); comparison != 0 {
		return comparison
	}
	if comparison := compareInt(left.Month, right.Month); comparison != 0 {
		return comparison
	}
	return compareInt(left.Day, right.Day)
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareInt64(left, right int64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func compareScaledMinor(left int64, leftScale uint8, right int64, rightScale uint8) int {
	if leftScale == rightScale {
		return compareInt64(left, right)
	}
	leftValue := big.NewInt(left)
	rightValue := big.NewInt(right)
	if leftScale < rightScale {
		leftValue.Mul(leftValue, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(rightScale-leftScale)), nil))
	} else {
		rightValue.Mul(rightValue, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(leftScale-rightScale)), nil))
	}
	return leftValue.Cmp(rightValue)
}
