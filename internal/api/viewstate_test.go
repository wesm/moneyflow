package api

import (
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestViewQueryCanonicalExamples(t *testing.T) {
	t.Parallel()

	start, err := domain.ParseDate("2024-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2024-12-31")
	require.NoError(t, err)

	tests := []struct {
		name  string
		state app.ViewState
		want  string
	}{
		{name: "default", state: app.DefaultViewState(), want: "v=1"},
		{name: "group", state: withState(func(state *app.AnalyticalState) {
			state.Dimension = domain.DimensionCategory
		}), want: "group=category&v=1"},
		{name: "flags and date", state: withState(func(state *app.AnalyticalState) {
			state.DateRange = &domain.DateRange{Start: start, End: end}
			state.ShowHidden = false
			state.ShowTransfers = true
		}), want: "from=2024-01-01&hidden=0&to=2024-12-31&transfers=1&v=1"},
		{name: "search anchor", state: withState(func(state *app.AnalyticalState) {
			state.Search = "café 東京"
			state.SearchAnchor = &app.NavigationScope{
				Mode: domain.ResultModeAggregate, Dimension: domain.DimensionMerchant,
			}
		}), want: "q=caf%C3%A9+%E6%9D%B1%E4%BA%AC&search-at=aggregate%3Amerchant%3A_%3A0&v=1"},
		{name: "drill key with colon", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{{
				Dimension: domain.DimensionMerchant, Key: "merchant:grocer",
			}}
		}), want: "drill=merchant%3Amerchant%3Agrocer&mode=detail&v=1"},
		{name: "ordered drill path", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{
				{Dimension: domain.DimensionMerchant, Key: "merchant-grocer"},
				{Dimension: domain.DimensionCategory, Key: "category-grocery"},
			}
		}), want: "drill=merchant%3Amerchant-grocer&drill=category%3Acategory-grocery&mode=detail&v=1"},
		{name: "time drill", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Dimension = domain.DimensionTime
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{{
				Dimension: domain.DimensionTime,
				Period: &domain.Period{
					Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 2,
				},
			}}
		}), want: "drill=time%3Amonth%3A2024-02&group=time&mode=detail&v=1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, encodeErr := EncodeViewQuery(test.state)
			require.NoError(t, encodeErr)
			assert.Equal(t, test.want, encoded)
			decoded, canonical, decodeErr := DecodeViewQuery(encoded)
			require.NoError(t, decodeErr)
			assert.Equal(t, test.state, decoded)
			assert.Equal(t, test.want, canonical)
		})
	}
}

func TestViewQueryNormalizesInputAndReturnFrames(t *testing.T) {
	t.Parallel()

	state := app.DefaultViewState()
	parent := state.Current
	parent.Dimension = domain.DimensionCategory
	state.Current.Mode = domain.ResultModeDetail
	state.Current.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant, Key: "merchant-grocer",
	}}
	state.Returns = []app.ReturnFrame{{Kind: app.ReturnNavigation, State: parent}}

	encoded, err := EncodeViewQuery(state)
	require.NoError(t, err)
	decoded, canonical, err := DecodeViewQuery(encoded)
	require.NoError(t, err)
	assert.Equal(t, state, decoded)
	assert.Equal(t, encoded, canonical)

	decoded, canonical, err = DecodeViewQuery("hidden=1&v=1&group=merchant")
	require.NoError(t, err)
	assert.Equal(t, app.DefaultViewState(), decoded)
	assert.Equal(t, "v=1", canonical)
}

func TestViewQueryRoundTripsDefaultReturnFrame(t *testing.T) {
	t.Parallel()

	state := app.DefaultViewState()
	state.Current.Mode = domain.ResultModeDetail
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc,
	}
	state.Current.Drilldowns = []domain.Drilldown{{
		Dimension: domain.DimensionMerchant, Key: "merchant-grocer",
	}}
	state.Returns = []app.ReturnFrame{{
		Kind: app.ReturnNavigation, State: app.DefaultViewState().Current,
	}}

	encoded, err := EncodeViewQuery(state)
	require.NoError(t, err)
	assert.Contains(t, encoded, "return=navigation%3A")

	decoded, canonical, err := DecodeViewQuery(encoded)
	require.NoError(t, err)
	assert.Equal(t, state, decoded)
	assert.Equal(t, encoded, canonical)
}

func TestViewQueryRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"leading question":    "?v=1",
		"unknown":             "missing=1",
		"version":             "v=2",
		"duplicate scalar":    "v=1&v=1",
		"invalid percent":     "q=%zz",
		"invalid mode":        "mode=missing",
		"invalid boolean":     "hidden=true",
		"one date bound":      "from=2024-01-01",
		"invalid date":        "from=2024-01-01&to=2024-02-31",
		"invalid sort":        "sort=date%3Aup",
		"incompatible sort":   "sort=date%3Adesc",
		"duplicate drill":     "drill=merchant%3Aa&drill=merchant%3Ab",
		"invalid period":      "drill=time%3Amonth%3A2024-13",
		"search no anchor":    "q=grocer",
		"anchor no search":    "search-at=aggregate%3Amerchant%3A_%3A0",
		"recursive return":    returnQuery(t, "navigation:return=navigation%253Av%253D1"),
		"invalid return kind": returnQuery(t, "missing:group=category"),
		"label field":         "drill=merchant%3A",
	}

	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := DecodeViewQuery(query)
			require.Error(t, err)
			assertSafeCode(t, err, CodeInvalidViewState)
		})
	}
}

func TestViewQueryBounds(t *testing.T) {
	t.Parallel()

	state := withState(func(current *app.AnalyticalState) {
		current.Search = strings.Repeat("a", MaxSearchBytes)
		current.SearchAnchor = &app.NavigationScope{
			Mode: domain.ResultModeAggregate, Dimension: domain.DimensionMerchant,
		}
	})
	_, err := EncodeViewQuery(state)
	require.NoError(t, err)
	state.Current.Search += "a"
	_, err = EncodeViewQuery(state)
	require.Error(t, err)
	assertSafeCode(t, err, CodeViewStateTooLarge)

	state = withState(func(current *app.AnalyticalState) {
		current.Mode = domain.ResultModeDetail
		current.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
		current.Drilldowns = []domain.Drilldown{{
			Dimension: domain.DimensionMerchant, Key: strings.Repeat("k", MaxEntityKeyBytes),
		}}
	})
	_, err = EncodeViewQuery(state)
	require.NoError(t, err)
	state.Current.Drilldowns[0].Key += "k"
	_, err = EncodeViewQuery(state)
	require.Error(t, err)
	assertSafeCode(t, err, CodeViewStateTooLarge)

	_, _, err = DecodeViewQuery(strings.Repeat("x", MaxEncodedViewQuery+1))
	require.Error(t, err)
	assertSafeCode(t, err, CodeInvalidViewState)

	state = app.DefaultViewState()
	state.Returns = make([]app.ReturnFrame, app.MaxReturnFrames+1)
	for index := range state.Returns {
		state.Returns[index] = app.ReturnFrame{Kind: app.ReturnNavigation, State: state.Current}
	}
	_, err = EncodeViewQuery(state)
	require.Error(t, err)
	assertSafeCode(t, err, CodeViewStateTooLarge)
}

func TestViewQueryRoundTripProperty(t *testing.T) {
	t.Parallel()

	property := func(groupIndex uint8, detail, hidden, transfers bool, granularityIndex uint8) bool {
		dimensions := []domain.Dimension{
			domain.DimensionMerchant, domain.DimensionCategory, domain.DimensionGroup,
			domain.DimensionAccount, domain.DimensionTime,
		}
		granularities := []domain.TimeGranularity{
			domain.TimeGranularityYear, domain.TimeGranularityMonth, domain.TimeGranularityDay,
		}
		state := app.DefaultViewState()
		state.Current.Dimension = dimensions[int(groupIndex)%len(dimensions)]
		state.Current.TimeGranularity = granularities[int(granularityIndex)%len(granularities)]
		state.Current.ShowHidden = hidden
		state.Current.ShowTransfers = transfers
		if detail {
			state.Current.Mode = domain.ResultModeDetail
			state.Current.Sort = domain.SortSpec{
				Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc,
			}
		} else if state.Current.Dimension == domain.DimensionTime {
			state.Current.Sort = domain.SortSpec{
				Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
			}
		}
		encoded, err := EncodeViewQuery(state)
		if err != nil {
			return false
		}
		decoded, canonical, err := DecodeViewQuery(encoded)
		return err == nil && canonical == encoded && reflect.DeepEqual(decoded, state)
	}
	require.NoError(t, quick.Check(property, nil))
}

func FuzzViewQuery(f *testing.F) {
	f.Add("")
	f.Add("v=1")
	f.Add("q=grocer&search-at=aggregate%3Amerchant%3A_%3A0")
	f.Add("drill=time%3Aday%3A2024-02-29&group=time&mode=detail")
	f.Fuzz(func(t *testing.T, query string) {
		state, canonical, err := DecodeViewQuery(query)
		if err != nil {
			return
		}
		reencoded, err := EncodeViewQuery(state)
		require.NoError(t, err)
		assert.Equal(t, canonical, reencoded)
	})
}

func withState(mutate func(*app.AnalyticalState)) app.ViewState {
	state := app.DefaultViewState()
	mutate(&state.Current)
	return state
}

func returnQuery(t testing.TB, value string) string {
	t.Helper()
	values := url.Values{}
	values.Add("return", value)
	return values.Encode()
}

func assertSafeCode(t testing.TB, err error, code ErrorCode) {
	t.Helper()
	var safe *SafeError
	require.True(t, errors.As(err, &safe), err)
	assert.Equal(t, code, safe.Code)
}
