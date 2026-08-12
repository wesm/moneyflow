package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Dimension is an aggregate or drill-down dimension.
type Dimension string

// Supported dimensions.
const (
	DimensionMerchant Dimension = "merchant"
	DimensionCategory Dimension = "category"
	DimensionGroup    Dimension = "group"
	DimensionAccount  Dimension = "account"
	DimensionTime     Dimension = "time"
)

// ResultMode selects detail or aggregate results.
type ResultMode string

// Supported result modes.
const (
	ResultModeDetail    ResultMode = "detail"
	ResultModeAggregate ResultMode = "aggregate"
)

// TimeGranularity selects the period size for time aggregation.
type TimeGranularity string

// Supported time granularities.
const (
	TimeGranularityYear  TimeGranularity = "year"
	TimeGranularityMonth TimeGranularity = "month"
	TimeGranularityDay   TimeGranularity = "day"
)

// SortField selects a result column for sorting.
type SortField string

// Supported sort fields.
const (
	SortFieldCount      SortField = "count"
	SortFieldAmount     SortField = "amount"
	SortFieldDate       SortField = "date"
	SortFieldMerchant   SortField = "merchant"
	SortFieldCategory   SortField = "category"
	SortFieldGroup      SortField = "group"
	SortFieldAccount    SortField = "account"
	SortFieldTimePeriod SortField = "time_period"
)

// SortDirection selects ascending or descending display order.
type SortDirection string

// Supported sort directions.
const (
	SortDirectionDesc SortDirection = "desc"
	SortDirectionAsc  SortDirection = "asc"
)

// DateRange is an inclusive posting-date range.
type DateRange struct {
	Start Date `json:"start"`
	End   Date `json:"end"`
}

// Period is a typed year, month, or day key.
type Period struct {
	Granularity TimeGranularity `json:"granularity"`
	Year        int             `json:"year"`
	Month       int             `json:"month,omitempty"`
	Day         int             `json:"day,omitempty"`
}

// Drilldown filters one normalized dimension.
type Drilldown struct {
	Dimension Dimension `json:"dimension"`
	Key       string    `json:"key,omitempty"`
	Label     string    `json:"label,omitempty"`
	Period    *Period   `json:"period,omitempty"`
}

// SortSpec selects a field and direction.
type SortSpec struct {
	Field     SortField     `json:"field"`
	Direction SortDirection `json:"direction"`
}

// QuerySpec fully describes the renderer-neutral visible result.
type QuerySpec struct {
	DateRange       *DateRange      `json:"date_range,omitempty"`
	Search          string          `json:"search,omitempty"`
	ShowHidden      bool            `json:"show_hidden"`
	ShowTransfers   bool            `json:"show_transfers"`
	Drilldowns      []Drilldown     `json:"drilldowns,omitempty"`
	Mode            ResultMode      `json:"mode"`
	GroupBy         Dimension       `json:"group_by,omitempty"`
	TimeGranularity TimeGranularity `json:"time_granularity"`
	Sort            SortSpec        `json:"sort"`
}

// RowFlags contains renderer-neutral row state.
type RowFlags struct {
	Selected bool `json:"selected"`
	Hidden   bool `json:"hidden"`
	Pending  bool `json:"pending"`
}

// DetailRow contains one normalized transaction and its row flags.
type DetailRow struct {
	Transaction Transaction `json:"transaction"`
	Flags       RowFlags    `json:"flags"`
}

// AggregateRow contains one typed aggregate value.
type AggregateRow struct {
	Dimension          Dimension `json:"dimension"`
	Key                string    `json:"key"`
	Label              string    `json:"label"`
	Count              int       `json:"count"`
	Total              Money     `json:"total"`
	Period             *Period   `json:"period,omitempty"`
	TopCategory        string    `json:"top_category,omitempty"`
	TopCategoryPercent int       `json:"top_category_percent,omitempty"`
	ShareTenths        int       `json:"share_tenths"`
	Flags              RowFlags  `json:"flags"`
}

// CurrencyStats contains exact statistics for one currency and scale.
type CurrencyStats struct {
	Currency Currency `json:"currency"`
	Scale    uint8    `json:"scale"`
	Count    int      `json:"count"`
	In       Money    `json:"in"`
	Out      Money    `json:"out"`
	Net      Money    `json:"net"`
}

// QueryResult is the immutable output of one analytics query.
type QueryResult struct {
	DetailRows    []DetailRow     `json:"detail_rows,omitempty"`
	AggregateRows []AggregateRow  `json:"aggregate_rows,omitempty"`
	Statistics    []CurrencyStats `json:"statistics"`
	FilteredCount int             `json:"filtered_count"`
	DateRange     *DateRange      `json:"date_range,omitempty"`
}

// Validate checks that a query can produce exactly one valid result shape.
func (query QuerySpec) Validate() error {
	if !query.Mode.Valid() {
		return errors.New("validate query: invalid result mode")
	}
	if !query.TimeGranularity.Valid() {
		return errors.New("validate query: invalid time granularity")
	}
	if !query.Sort.Direction.Valid() {
		return errors.New("validate query: invalid sort direction")
	}
	if query.DateRange != nil {
		if query.DateRange.Start.Year() == 0 || query.DateRange.End.Year() == 0 {
			return errors.New("validate query: invalid date range")
		}
		if query.DateRange.Start.Compare(query.DateRange.End) > 0 {
			return errors.New("validate query: date range starts after it ends")
		}
	}
	if err := validateDrilldowns(query.Drilldowns); err != nil {
		return err
	}
	if query.Mode == ResultModeAggregate {
		if !query.GroupBy.Valid() {
			return errors.New("validate query: invalid aggregate dimension")
		}
		if !validAggregateSort(query.GroupBy, query.Sort.Field) {
			return errors.New("validate query: sort is incompatible with aggregate dimension")
		}
		return nil
	}
	if !validDetailSort(query.Sort.Field) {
		return errors.New("validate query: sort is incompatible with detail results")
	}
	return nil
}

// Clone returns a query whose nested values are independent.
func (query QuerySpec) Clone() QuerySpec {
	if query.DateRange != nil {
		dateRange := *query.DateRange
		query.DateRange = &dateRange
	}
	query.Drilldowns = append([]Drilldown(nil), query.Drilldowns...)
	for index := range query.Drilldowns {
		if query.Drilldowns[index].Period != nil {
			period := *query.Drilldowns[index].Period
			query.Drilldowns[index].Period = &period
		}
	}
	return query
}

// Clone returns a result whose slices and transaction metadata are independent.
func (result QueryResult) Clone() QueryResult {
	result.DetailRows = append([]DetailRow(nil), result.DetailRows...)
	for index := range result.DetailRows {
		result.DetailRows[index].Transaction = result.DetailRows[index].Transaction.Clone()
	}
	result.AggregateRows = append([]AggregateRow(nil), result.AggregateRows...)
	for index := range result.AggregateRows {
		if result.AggregateRows[index].Period != nil {
			period := *result.AggregateRows[index].Period
			result.AggregateRows[index].Period = &period
		}
	}
	result.Statistics = append([]CurrencyStats(nil), result.Statistics...)
	if result.DateRange != nil {
		dateRange := *result.DateRange
		result.DateRange = &dateRange
	}
	return result
}

// Valid reports whether the dimension is known.
func (dimension Dimension) Valid() bool {
	switch dimension {
	case DimensionMerchant, DimensionCategory, DimensionGroup, DimensionAccount, DimensionTime:
		return true
	default:
		return false
	}
}

// Valid reports whether the result mode is known.
func (mode ResultMode) Valid() bool {
	return mode == ResultModeDetail || mode == ResultModeAggregate
}

// Valid reports whether the granularity is known.
func (granularity TimeGranularity) Valid() bool {
	return granularity == TimeGranularityYear || granularity == TimeGranularityMonth || granularity == TimeGranularityDay
}

// Valid reports whether the sort field is known.
func (field SortField) Valid() bool {
	switch field {
	case SortFieldCount, SortFieldAmount, SortFieldDate, SortFieldMerchant, SortFieldCategory,
		SortFieldGroup, SortFieldAccount, SortFieldTimePeriod:
		return true
	default:
		return false
	}
}

// Valid reports whether the sort direction is known.
func (direction SortDirection) Valid() bool {
	return direction == SortDirectionDesc || direction == SortDirectionAsc
}

func validateDrilldowns(drilldowns []Drilldown) error {
	seen := make(map[Dimension]struct{}, len(drilldowns))
	for _, drilldown := range drilldowns {
		if !drilldown.Dimension.Valid() {
			return errors.New("validate query: invalid drill-down dimension")
		}
		if _, exists := seen[drilldown.Dimension]; exists {
			return errors.New("validate query: duplicate drill-down dimension")
		}
		seen[drilldown.Dimension] = struct{}{}
		if drilldown.Dimension == DimensionTime {
			if drilldown.Period == nil {
				return errors.New("validate query: time drill-down requires a period")
			}
			if err := drilldown.Period.Validate(); err != nil {
				return fmt.Errorf("validate query: %w", err)
			}
			continue
		}
		if drilldown.Period != nil || drilldown.Key == "" || drilldown.Label == "" {
			return errors.New("validate query: invalid non-time drill-down")
		}
	}
	return nil
}

// Validate checks that a period matches its declared granularity.
func (period Period) Validate() error {
	if !period.Granularity.Valid() || period.Year < 1 || period.Year > 9999 {
		return errors.New("invalid time period")
	}
	switch period.Granularity {
	case TimeGranularityYear:
		if period.Month != 0 || period.Day != 0 {
			return errors.New("year period contains month or day")
		}
	case TimeGranularityMonth:
		if period.Month < 1 || period.Month > 12 || period.Day != 0 {
			return errors.New("invalid month period")
		}
	case TimeGranularityDay:
		if _, err := NewDate(period.Year, time.Month(period.Month), period.Day); err != nil {
			return errors.New("invalid day period")
		}
	}
	return nil
}

func validAggregateSort(dimension Dimension, field SortField) bool {
	if field == SortFieldCount || field == SortFieldAmount {
		return true
	}
	want := map[Dimension]SortField{
		DimensionMerchant: SortFieldMerchant,
		DimensionCategory: SortFieldCategory,
		DimensionGroup:    SortFieldGroup,
		DimensionAccount:  SortFieldAccount,
		DimensionTime:     SortFieldTimePeriod,
	}
	return field == want[dimension]
}

func validDetailSort(field SortField) bool {
	switch field {
	case SortFieldDate, SortFieldMerchant, SortFieldCategory, SortFieldAccount, SortFieldAmount:
		return true
	default:
		return false
	}
}

func marshalEnum(value string) ([]byte, error) { return json.Marshal(value) }

func unmarshalEnum(data []byte, destination *string, valid func(string) bool) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if !valid(value) {
		return fmt.Errorf("unknown enum value %q", value)
	}
	*destination = value
	return nil
}

// MarshalJSON encodes Dimension as a string.
func (dimension Dimension) MarshalJSON() ([]byte, error) { return marshalEnum(string(dimension)) }

// UnmarshalJSON decodes and validates a Dimension.
func (dimension *Dimension) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, (*string)(dimension), func(value string) bool { return Dimension(value).Valid() })
}

// MarshalJSON encodes ResultMode as a string.
func (mode ResultMode) MarshalJSON() ([]byte, error) { return marshalEnum(string(mode)) }

// UnmarshalJSON decodes and validates a ResultMode.
func (mode *ResultMode) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, (*string)(mode), func(value string) bool { return ResultMode(value).Valid() })
}

// MarshalJSON encodes TimeGranularity as a string.
func (granularity TimeGranularity) MarshalJSON() ([]byte, error) {
	return marshalEnum(string(granularity))
}

// UnmarshalJSON decodes and validates a TimeGranularity.
func (granularity *TimeGranularity) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, (*string)(granularity), func(value string) bool { return TimeGranularity(value).Valid() })
}

// MarshalJSON encodes SortField as a string.
func (field SortField) MarshalJSON() ([]byte, error) { return marshalEnum(string(field)) }

// UnmarshalJSON decodes and validates a SortField.
func (field *SortField) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, (*string)(field), func(value string) bool { return SortField(value).Valid() })
}

// MarshalJSON encodes SortDirection as a string.
func (direction SortDirection) MarshalJSON() ([]byte, error) { return marshalEnum(string(direction)) }

// UnmarshalJSON decodes and validates a SortDirection.
func (direction *SortDirection) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, (*string)(direction), func(value string) bool { return SortDirection(value).Valid() })
}
