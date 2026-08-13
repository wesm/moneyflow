package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validAggregateQuery() QuerySpec {
	return QuerySpec{
		ShowHidden:      true,
		Mode:            ResultModeAggregate,
		GroupBy:         DimensionMerchant,
		TimeGranularity: TimeGranularityYear,
		Sort: SortSpec{
			Field:     SortFieldAmount,
			Direction: SortDirectionDesc,
		},
	}
}

func TestEnumJSONRoundTrip(t *testing.T) {
	t.Parallel()

	input := struct {
		Dimension Dimension       `json:"dimension"`
		Mode      ResultMode      `json:"mode"`
		Sort      SortDirection   `json:"sort"`
		Field     SortField       `json:"field"`
		Time      TimeGranularity `json:"time"`
	}{DimensionMerchant, ResultModeAggregate, SortDirectionDesc, SortFieldAmount, TimeGranularityYear}

	data, err := json.Marshal(input)
	require.NoError(t, err)
	assert.JSONEq(t, `{"dimension":"merchant","mode":"aggregate","sort":"desc","field":"amount","time":"year"}`, string(data))

	var output struct {
		Dimension Dimension       `json:"dimension"`
		Mode      ResultMode      `json:"mode"`
		Sort      SortDirection   `json:"sort"`
		Field     SortField       `json:"field"`
		Time      TimeGranularity `json:"time"`
	}
	require.NoError(t, json.Unmarshal(data, &output))
	assert.Equal(t, input.Dimension, output.Dimension)
	assert.Equal(t, input.Mode, output.Mode)
	assert.Equal(t, input.Sort, output.Sort)
	assert.Equal(t, input.Field, output.Field)
	assert.Equal(t, input.Time, output.Time)
}

func TestEnumJSONRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	var dimension Dimension
	assert.Error(t, json.Unmarshal([]byte(`"unknown"`), &dimension))
	var mode ResultMode
	assert.Error(t, json.Unmarshal([]byte(`"unknown"`), &mode))
	var field SortField
	assert.Error(t, json.Unmarshal([]byte(`"unknown"`), &field))
	for _, value := range []any{
		Dimension("unknown"), ResultMode("unknown"), SortDirection("unknown"),
		SortField("unknown"), TimeGranularity("unknown"),
	} {
		_, err := json.Marshal(value)
		assert.Error(t, err)
	}
}

func TestQuerySpecValidate(t *testing.T) {
	t.Parallel()

	query := validAggregateQuery()
	require.NoError(t, query.Validate())

	start, err := ParseDate("2024-01-01")
	require.NoError(t, err)
	end, err := ParseDate("2024-12-31")
	require.NoError(t, err)
	query.DateRange = &DateRange{Start: start, End: end}
	query.Drilldowns = []Drilldown{{Dimension: DimensionAccount, Key: "account-1", Label: "Everyday Card"}}
	require.NoError(t, query.Validate())
}

func TestQuerySpecRejectsInvalidCombinations(t *testing.T) {
	t.Parallel()

	start, err := ParseDate("2024-12-31")
	require.NoError(t, err)
	end, err := ParseDate("2024-01-01")
	require.NoError(t, err)

	tests := map[string]func(*QuerySpec){
		"invalid mode":   func(query *QuerySpec) { query.Mode = "bad" },
		"invalid group":  func(query *QuerySpec) { query.GroupBy = "bad" },
		"backward range": func(query *QuerySpec) { query.DateRange = &DateRange{Start: start, End: end} },
		"duplicate drill": func(query *QuerySpec) {
			query.Drilldowns = []Drilldown{{Dimension: DimensionMerchant, Key: "a", Label: "A"}, {Dimension: DimensionMerchant, Key: "b", Label: "B"}}
		},
		"period on account": func(query *QuerySpec) {
			query.Drilldowns = []Drilldown{{Dimension: DimensionAccount, Period: &Period{Granularity: TimeGranularityYear, Year: 2024}}}
		},
		"time without period": func(query *QuerySpec) {
			query.Drilldowns = []Drilldown{{Dimension: DimensionTime, Key: "2024", Label: "2024"}}
		},
		"time with string identity": func(query *QuerySpec) {
			query.Drilldowns = []Drilldown{{
				Dimension: DimensionTime,
				Key:       "2024",
				Label:     "2025",
				Period:    &Period{Granularity: TimeGranularityYear, Year: 2024},
			}}
		},
		"date aggregate sort":  func(query *QuerySpec) { query.Sort.Field = SortFieldDate },
		"wrong dimension sort": func(query *QuerySpec) { query.Sort.Field = SortFieldCategory },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			query := validAggregateQuery()
			mutate(&query)
			assert.Error(t, query.Validate())
		})
	}
}

func TestDetailQuerySortValidation(t *testing.T) {
	t.Parallel()

	for _, field := range []SortField{SortFieldDate, SortFieldMerchant, SortFieldCategory, SortFieldAccount, SortFieldAmount} {
		query := QuerySpec{Mode: ResultModeDetail, TimeGranularity: TimeGranularityYear, Sort: SortSpec{Field: field, Direction: SortDirectionDesc}}
		assert.NoError(t, query.Validate(), field)
	}
	for _, field := range []SortField{SortFieldCount, SortFieldGroup, SortFieldTimePeriod} {
		query := QuerySpec{Mode: ResultModeDetail, TimeGranularity: TimeGranularityYear, Sort: SortSpec{Field: field, Direction: SortDirectionDesc}}
		assert.Error(t, query.Validate(), field)
	}
	query := QuerySpec{
		Mode: ResultModeDetail, GroupBy: "invalid", TimeGranularity: TimeGranularityYear,
		Sort: SortSpec{Field: SortFieldDate, Direction: SortDirectionDesc},
	}
	assert.Error(t, query.Validate())
}

func TestQuerySpecCloneCopiesNestedValues(t *testing.T) {
	t.Parallel()

	query := validAggregateQuery()
	query.Drilldowns = []Drilldown{{Dimension: DimensionTime, Period: &Period{Granularity: TimeGranularityYear, Year: 2024}}}
	clone := query.Clone()
	clone.Drilldowns[0].Period.Year = 2025
	assert.Equal(t, 2024, query.Drilldowns[0].Period.Year)
}

func TestQueryResultCloneCopiesRows(t *testing.T) {
	t.Parallel()

	transaction := validTransaction(t)
	result := QueryResult{
		DetailRows: []DetailRow{{Transaction: transaction}},
		AggregateRows: []AggregateRow{{
			Key:   "merchant-1",
			Label: "Example Grocer",
			Total: transaction.Amount,
		}},
	}
	clone := result.Clone()
	clone.DetailRows[0].Transaction.Metadata["source"] = "changed"
	clone.AggregateRows[0].Label = "changed"
	assert.Equal(t, "fixture", result.DetailRows[0].Transaction.Metadata["source"])
	assert.Equal(t, "Example Grocer", result.AggregateRows[0].Label)
}
