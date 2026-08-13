package analytics

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

type aggregateKey struct {
	partition moneyPartition
	bucket    string
}

type accumulator struct {
	row                domain.AggregateRow
	categoryActivities map[string]uint64
}

// Aggregate materializes exact rows for one supported dimension.
func Aggregate(
	filtered []domain.Transaction,
	dimension domain.Dimension,
	granularity domain.TimeGranularity,
) ([]domain.AggregateRow, error) {
	if !dimension.Valid() {
		return nil, errors.New("aggregate: invalid dimension")
	}
	if !granularity.Valid() {
		return nil, errors.New("aggregate: invalid time granularity")
	}
	if dimension == domain.DimensionTime {
		return aggregateTime(filtered, granularity)
	}

	accumulators := make(map[aggregateKey]*accumulator)
	for _, transaction := range filtered {
		key, label := dimensionValue(transaction, dimension)
		partition := moneyPartition{
			currency: transaction.Amount.Currency,
			scale:    transaction.Amount.Scale,
		}
		mapKey := aggregateKey{partition: partition, bucket: key}
		value, exists := accumulators[mapKey]
		if !exists {
			value = &accumulator{
				row: domain.AggregateRow{
					Dimension: dimension,
					Key:       key,
					Label:     label,
					Total:     domain.Money{Currency: partition.currency, Scale: partition.scale},
				},
				categoryActivities: make(map[string]uint64),
			}
			accumulators[mapKey] = value
		}
		value.row.Count++
		if transaction.Hidden {
			continue
		}
		total, err := value.row.Total.Add(transaction.Amount)
		if err != nil {
			return nil, fmt.Errorf("aggregate: %s %q total: %w", dimension, label, err)
		}
		value.row.Total = total
		if dimension == domain.DimensionMerchant {
			activity := absMinor(transaction.Amount.Minor)
			current := value.categoryActivities[transaction.Category.Name]
			if math.MaxUint64-current < activity {
				return nil, fmt.Errorf("aggregate: merchant %q category activity overflow", label)
			}
			value.categoryActivities[transaction.Category.Name] = current + activity
		}
	}

	rows := make([]domain.AggregateRow, 0, len(accumulators))
	for _, value := range accumulators {
		if dimension == domain.DimensionMerchant {
			if err := setTopCategory(value); err != nil {
				return nil, err
			}
		}
		rows = append(rows, value.row)
	}
	if err := applyShares(rows); err != nil {
		return nil, err
	}
	return sortBaseRows(rows), nil
}

func dimensionValue(transaction domain.Transaction, dimension domain.Dimension) (string, string) {
	switch dimension {
	case domain.DimensionMerchant:
		return transaction.Merchant.ID, transaction.Merchant.Name
	case domain.DimensionCategory:
		return transaction.Category.ID, transaction.Category.Name
	case domain.DimensionGroup:
		return transaction.Category.Group, transaction.Category.Group
	case domain.DimensionAccount:
		return transaction.Account.ID, transaction.Account.Name
	default:
		return "", ""
	}
}

func setTopCategory(value *accumulator) error {
	var totalActivity uint64
	for _, activity := range value.categoryActivities {
		if math.MaxUint64-totalActivity < activity {
			return fmt.Errorf("aggregate: merchant %q total activity overflow", value.row.Label)
		}
		totalActivity += activity
	}
	if totalActivity == 0 {
		return nil
	}
	categories := make([]string, 0, len(value.categoryActivities))
	for category := range value.categoryActivities {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		activity := value.categoryActivities[category]
		if value.row.TopCategory == "" || activity > value.categoryActivities[value.row.TopCategory] {
			value.row.TopCategory = category
		}
	}
	percentage, err := ratioHalfUp(
		value.categoryActivities[value.row.TopCategory], totalActivity, 100,
	)
	if err != nil {
		return fmt.Errorf("aggregate: merchant %q top category: %w", value.row.Label, err)
	}
	value.row.TopCategoryPercent = percentage
	return nil
}

func aggregateTime(
	filtered []domain.Transaction, granularity domain.TimeGranularity,
) ([]domain.AggregateRow, error) {
	if len(filtered) == 0 {
		return []domain.AggregateRow{}, nil
	}
	minimum := filtered[0].Date
	maximum := filtered[0].Date
	partitions := make(map[moneyPartition]struct{})
	actual := make(map[aggregateKey]*accumulator)
	for _, transaction := range filtered {
		if transaction.Date.Compare(minimum) < 0 {
			minimum = transaction.Date
		}
		if transaction.Date.Compare(maximum) > 0 {
			maximum = transaction.Date
		}
		partition := moneyPartition{
			currency: transaction.Amount.Currency,
			scale:    transaction.Amount.Scale,
		}
		partitions[partition] = struct{}{}
		period := periodForDate(transaction.Date, granularity)
		mapKey := aggregateKey{partition: partition, bucket: periodKey(period)}
		value, exists := actual[mapKey]
		if !exists {
			periodCopy := period
			value = &accumulator{row: domain.AggregateRow{
				Dimension: domain.DimensionTime,
				Key:       mapKey.bucket,
				Label:     periodLabel(period),
				Period:    &periodCopy,
				Total:     domain.Money{Currency: partition.currency, Scale: partition.scale},
			}}
			actual[mapKey] = value
		}
		value.row.Count++
		if !transaction.Hidden {
			total, err := value.row.Total.Add(transaction.Amount)
			if err != nil {
				return nil, fmt.Errorf("aggregate: time %s total: %w", mapKey.bucket, err)
			}
			value.row.Total = total
		}
	}

	periods, err := periodsBetween(minimum, maximum, granularity)
	if err != nil {
		return nil, fmt.Errorf("aggregate: time periods: %w", err)
	}
	partitionKeys := sortedPartitions(partitions)
	rows := make([]domain.AggregateRow, 0, len(periods)*len(partitionKeys))
	for _, period := range periods {
		for _, partition := range partitionKeys {
			key := aggregateKey{partition: partition, bucket: periodKey(period)}
			if value, exists := actual[key]; exists {
				rows = append(rows, value.row)
				continue
			}
			periodCopy := period
			rows = append(rows, domain.AggregateRow{
				Dimension: domain.DimensionTime,
				Key:       key.bucket,
				Label:     periodLabel(period),
				Period:    &periodCopy,
				Total:     domain.Money{Currency: partition.currency, Scale: partition.scale},
			})
		}
	}
	if err := applyShares(rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func periodForDate(date domain.Date, granularity domain.TimeGranularity) domain.Period {
	period := domain.Period{Granularity: granularity, Year: date.Year()}
	if granularity == domain.TimeGranularityMonth || granularity == domain.TimeGranularityDay {
		period.Month = int(date.Month())
	}
	if granularity == domain.TimeGranularityDay {
		period.Day = date.Day()
	}
	return period
}

func periodsBetween(
	minimum domain.Date, maximum domain.Date, granularity domain.TimeGranularity,
) ([]domain.Period, error) {
	start := periodForDate(minimum, granularity)
	end := periodForDate(maximum, granularity)
	periods := make([]domain.Period, 0)
	for current := start; comparePeriods(current, end) <= 0; {
		periods = append(periods, current)
		if comparePeriods(current, end) == 0 {
			break
		}
		var err error
		current, err = nextPeriod(current)
		if err != nil {
			return nil, err
		}
	}
	return periods, nil
}

func nextPeriod(period domain.Period) (domain.Period, error) {
	switch period.Granularity {
	case domain.TimeGranularityYear:
		if period.Year == 9999 {
			return domain.Period{}, errors.New("year exceeds supported range")
		}
		period.Year++
		return period, nil
	case domain.TimeGranularityMonth:
		date, err := domain.NewDate(period.Year, time.Month(period.Month), 1)
		if err != nil {
			return domain.Period{}, err
		}
		year, month := date.Year(), date.Month()+1
		if month > time.December {
			year++
			month = time.January
		}
		if year > 9999 {
			return domain.Period{}, errors.New("month exceeds supported range")
		}
		return domain.Period{Granularity: period.Granularity, Year: year, Month: int(month)}, nil
	case domain.TimeGranularityDay:
		date, err := domain.NewDate(period.Year, time.Month(period.Month), period.Day)
		if err != nil {
			return domain.Period{}, err
		}
		next, err := date.AddDays(1)
		if err != nil {
			return domain.Period{}, err
		}
		return periodForDate(next, period.Granularity), nil
	default:
		return domain.Period{}, errors.New("invalid time granularity")
	}
}

func periodKey(period domain.Period) string {
	switch period.Granularity {
	case domain.TimeGranularityYear:
		return fmt.Sprintf("%04d", period.Year)
	case domain.TimeGranularityMonth:
		return fmt.Sprintf("%04d-%02d", period.Year, period.Month)
	default:
		return fmt.Sprintf("%04d-%02d-%02d", period.Year, period.Month, period.Day)
	}
}

func periodLabel(period domain.Period) string {
	if period.Granularity == domain.TimeGranularityMonth {
		return fmt.Sprintf("%s %04d", time.Month(period.Month).String()[:3], period.Year)
	}
	return periodKey(period)
}

func sortedPartitions(input map[moneyPartition]struct{}) []moneyPartition {
	partitions := make([]moneyPartition, 0, len(input))
	for partition := range input {
		partitions = append(partitions, partition)
	}
	sort.Slice(partitions, func(left, right int) bool {
		if partitions[left].currency != partitions[right].currency {
			return partitions[left].currency < partitions[right].currency
		}
		return partitions[left].scale < partitions[right].scale
	})
	return partitions
}

func applyShares(rows []domain.AggregateRow) error {
	type denominators struct {
		income   uint64
		expenses uint64
	}
	byPartition := make(map[moneyPartition]denominators)
	for _, row := range rows {
		key := moneyPartition{currency: row.Total.Currency, scale: row.Total.Scale}
		value := byPartition[key]
		if row.Total.Minor > 0 {
			amount := uint64(row.Total.Minor)
			if math.MaxUint64-value.income < amount {
				return fmt.Errorf("aggregate: %s/%d income denominator overflow", key.currency, key.scale)
			}
			value.income += amount
		} else if row.Total.Minor < 0 {
			amount := absMinor(row.Total.Minor)
			if math.MaxUint64-value.expenses < amount {
				return fmt.Errorf("aggregate: %s/%d expense denominator overflow", key.currency, key.scale)
			}
			value.expenses += amount
		}
		byPartition[key] = value
	}
	for index := range rows {
		key := moneyPartition{currency: rows[index].Total.Currency, scale: rows[index].Total.Scale}
		value := byPartition[key]
		denominator := value.expenses
		if rows[index].Total.Minor > 0 {
			denominator = value.income
		}
		if rows[index].Total.Minor == 0 || denominator == 0 {
			continue
		}
		share, err := ratioHalfUp(absMinor(rows[index].Total.Minor), denominator, 1000)
		if err != nil {
			return fmt.Errorf("aggregate: row %q share: %w", rows[index].Label, err)
		}
		rows[index].ShareTenths = share
	}
	return nil
}

func ratioHalfUp(numerator, denominator, multiplier uint64) (int, error) {
	if denominator == 0 {
		return 0, errors.New("ratio denominator is zero")
	}
	high, low := bits.Mul64(numerator, multiplier)
	if high >= denominator {
		return 0, errors.New("ratio result overflows uint64")
	}
	quotient, remainder := bits.Div64(high, low, denominator)
	halfUpThreshold := denominator/2 + denominator%2
	if remainder >= halfUpThreshold {
		quotient++
	}
	if quotient > uint64(math.MaxInt) {
		return 0, errors.New("ratio result overflows int")
	}
	return int(quotient), nil
}

func absMinor(value int64) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	// Adding before negation keeps MinInt64 representable; the result is non-negative.
	return uint64(-(value + 1)) + 1 //nolint:gosec // conversion is guarded by the negative branch.
}

func sortBaseRows(rows []domain.AggregateRow) []domain.AggregateRow {
	return SortAggregateRows(rows, domain.SortSpec{
		Field:     domain.SortFieldMerchant,
		Direction: domain.SortDirectionAsc,
	})
}
