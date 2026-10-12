package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
			state.SearchAnchor = &app.NavigationScope{Mode: domain.ResultModeAggregate}
		}), want: "q=caf%C3%A9+%E6%9D%B1%E4%BA%AC&search-at=aggregate%3A0&v=1"},
		{name: "search anchor at depth", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{{
				Dimension: domain.DimensionMerchant, Key: "merchant-grocer", Currency: "USD", Scale: 2,
			}}
			state.Search = "milk"
			state.SearchAnchor = &app.NavigationScope{Mode: domain.ResultModeDetail, DrilldownSize: 1}
		}), want: "drill=merchant%3AUSD%3A2%3Amerchant-grocer&mode=detail&q=milk" +
			"&search-at=detail%3A1&v=1"},
		{name: "drill key with colon", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{{
				Dimension: domain.DimensionMerchant, Key: "merchant:grocer", Currency: "USD", Scale: 2,
			}}
		}), want: "drill=merchant%3AUSD%3A2%3Amerchant%3Agrocer&mode=detail&v=1"},
		{name: "ordered drill path", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{
				{Dimension: domain.DimensionMerchant, Key: "merchant-grocer", Currency: "USD", Scale: 2},
				{Dimension: domain.DimensionCategory, Key: "category-grocery", Currency: "USD", Scale: 2},
			}
		}), want: "drill=merchant%3AUSD%3A2%3Amerchant-grocer&drill=category%3AUSD%3A2%3Acategory-grocery&mode=detail&v=1"},
		{name: "time drill", state: withState(func(state *app.AnalyticalState) {
			state.Mode = domain.ResultModeDetail
			state.Dimension = domain.DimensionTime
			state.Sort = domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
			state.Drilldowns = []domain.Drilldown{{
				Dimension: domain.DimensionTime,
				Currency:  "USD",
				Scale:     2,
				Period: &domain.Period{
					Granularity: domain.TimeGranularityMonth, Year: 2024, Month: 2,
				},
			}}
		}), want: "drill=time%3AUSD%3A2%3Amonth%3A2024-02&group=time&mode=detail&v=1"},
		{name: "currency scope", state: withState(func(state *app.AnalyticalState) {
			state.Currency = &domain.CurrencyScope{Currency: "EUR", Scale: 2}
		}), want: "cur=EUR%3A2&v=1"},
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
		Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant-grocer",
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
		Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant-grocer",
	}}
	parent := app.DefaultViewState().Current
	parent.Search = "milk"
	parent.SearchAnchor = &app.NavigationScope{Mode: domain.ResultModeAggregate}
	state.Returns = []app.ReturnFrame{{Kind: app.ReturnNavigation, State: parent}}

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

	tests := []struct {
		name  string
		query string
		cause string
	}{
		{"leading question", "?v=1", "leading question mark"},
		{"unknown", "v=1&missing=1", `unknown field "missing"`},
		{"version", "v=2", "unsupported schema version"},
		{"duplicate scalar", "v=1&v=1", `field "v" must appear once`},
		{"invalid percent", "q=%zz", "parse query"},
		{"invalid mode", "v=1&mode=missing", "invalid result mode"},
		{"invalid boolean", "v=1&hidden=true", "parse hidden flag"},
		{"one date bound", "v=1&from=2024-01-01", "date range requires both bounds"},
		{"invalid date", "v=1&from=2024-01-01&to=2024-02-31", "parse end date"},
		{"invalid sort", "v=1&sort=date%3Aup", "invalid sort direction"},
		{"incompatible sort", "v=1&sort=date%3Adesc", "sort is incompatible with aggregate"},
		{
			"duplicate drill",
			"v=1&mode=detail&sort=date%3Adesc" +
				"&drill=merchant%3AUSD%3A2%3Aa&drill=merchant%3AUSD%3A2%3Ab",
			"duplicate drill-down dimension",
		},
		{
			"invalid period",
			"v=1&mode=detail&sort=date%3Adesc&drill=time%3AUSD%3A2%3Amonth%3A2024-13",
			"time period is invalid",
		},
		{"search no anchor", "q=grocer&v=1", "search and search anchor must be present"},
		{"anchor no search", "search-at=aggregate%3A0&v=1", "search and search anchor must be present"},
		{
			"anchor four parts",
			"q=grocer&search-at=aggregate%3Amerchant%3A_%3A0&v=1",
			"depth is malformed",
		},
		{"anchor bad mode", "q=grocer&search-at=missing%3A0&v=1", "invalid search anchor: invalid mode"},
		{"anchor bad depth", "q=grocer&search-at=aggregate%3Ax&v=1", "depth is malformed"},
		{"anchor deep", "q=grocer&search-at=aggregate%3A9&v=1", "invalid drill-down size"},
		{"anchor non canonical", "q=grocer&search-at=aggregate%3A01&v=1", "anchor is not canonical"},
		{
			"recursive return",
			returnQuery(t, "navigation:return=navigation%253Av%253D1") + "&v=1",
			"nested return frames",
		},
		{"invalid return kind", returnQuery(t, "missing:group=category") + "&v=1", "invalid kind"},
		{"label field", "v=1&drill=merchant%3A", "drill-down is malformed"},
		{"currency no scale", "cur=USD&v=1", "must be CODE:SCALE"},
		{"currency lowercase", "cur=usd%3A2&v=1", "currency scope code is invalid"},
		{"currency bad scale", "cur=USD%3A02&v=1", "must be CODE:SCALE"},
		{
			"currency vs drill",
			"cur=EUR%3A2&drill=merchant%3AUSD%3A2%3Am&mode=detail&sort=date%3Adesc&v=1",
			"drill-down currency differs from the currency scope",
		},
		{"time with currency", "cur=USD%3A2&time=month&v=1", "time granularity is derived"},
		{
			"return time with currency",
			returnQuery(t, "navigation:cur=USD%3A2&time=day") + "&cur=USD%3A2&v=1",
			"time granularity is derived",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := DecodeViewQuery(test.query)
			require.Error(t, err)
			assertSafeCode(t, err, CodeInvalidViewState)
			var safe *SafeError
			require.ErrorAs(t, err, &safe)
			assert.ErrorContains(t, safe.Unwrap(), test.cause)
		})
	}
}

func TestViewQueryRequiresVersionOnNonemptyInput(t *testing.T) {
	t.Parallel()

	_, _, err := DecodeViewQuery("group=category")
	require.Error(t, err)
	assertSafeCode(t, err, CodeInvalidViewState)
	var safe *SafeError
	require.ErrorAs(t, err, &safe)
	assert.ErrorContains(t, safe.Unwrap(), "schema version is required")

	state, canonical, err := DecodeViewQuery("")
	require.NoError(t, err)
	assert.Equal(t, app.DefaultViewState(), state)
	assert.Equal(t, "v=1", canonical)
}

func TestViewQueryBounds(t *testing.T) {
	t.Parallel()

	state := withState(func(current *app.AnalyticalState) {
		current.Search = strings.Repeat("a", MaxSearchBytes)
		current.SearchAnchor = &app.NavigationScope{Mode: domain.ResultModeAggregate}
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
			Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2,
			Key: strings.Repeat("k", MaxEntityKeyBytes),
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

func TestViewQueryOmitsTimeWithACurrency(t *testing.T) {
	t.Parallel()

	state := withState(func(state *app.AnalyticalState) {
		state.Dimension = domain.DimensionTime
		state.Sort = domain.SortSpec{
			Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
		}
		state.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
		state.TimeGranularity = domain.TimeGranularityDay
	})
	parent := state.Current
	parent.TimeGranularity = domain.TimeGranularityMonth
	state.Returns = []app.ReturnFrame{{Kind: app.ReturnNavigation, State: parent}}
	encoded, err := EncodeViewQuery(state)
	require.NoError(t, err)
	assert.NotContains(t, encoded, "time=", "the service derives the granularity")
	decoded, canonical, err := DecodeViewQuery(encoded)
	require.NoError(t, err)
	assert.Equal(t, encoded, canonical)
	assert.Equal(t, domain.TimeGranularityYear, decoded.Current.TimeGranularity)
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
	f.Add("q=grocer&search-at=aggregate%3A0")
	f.Add("drill=time%3Aday%3A2024-02-29&group=time&mode=detail")
	f.Add("cur=USD%3A2&v=1")
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

func TestProjectionReportsDrillDepthAndRoot(t *testing.T) {
	t.Parallel()

	server := directNavigationServer(t)
	merchants := ViewRoot{Mode: "aggregate", Grouping: "merchant", TimeGranularity: "year"}
	transactions := ViewRoot{Mode: "detail", Grouping: "merchant", TimeGranularity: "year"}

	merchantDrill := drillFirstAggregate(t, server, "v=1")
	detailView := rootTransition(t, server, "v=1", TransitionBody{Action: app.ActionShowDetail})
	subGroupedDetail := subGroupedByTime(t, detailView.CanonicalQuery)
	monthDrill := drillFirstAggregate(t, server, timeQuery(t, func(state *app.AnalyticalState) {
		state.TimeGranularity = domain.TimeGranularityMonth
	}))
	currencyDrill := drillFirstAggregate(t, server, timeQuery(t, func(state *app.AnalyticalState) {
		state.Currency = &domain.CurrencyScope{Currency: "USD", Scale: 2}
	}))
	category := "category"
	nested := rootTransition(t, server, merchantDrill.CanonicalQuery, TransitionBody{
		Action: app.ActionSetSubGrouping, Dimension: &category,
	})

	for _, test := range []struct {
		name  string
		query string
		depth int
		root  ViewRoot
	}{
		{"the top level", "v=1", 0, merchants},
		{"a merchant drill from Merchants", merchantDrill.CanonicalQuery, 1, merchants},
		{"a drill from the d view",
			drillFirstAggregate(t, server, subGroupedDetail).CanonicalQuery, 1, transactions},
		{"a drill opened from a link", withoutReturns(t, merchantDrill.CanonicalQuery), 1, merchants},
		{"a sub-grouped d view", subGroupedDetail, 0, transactions},
		{"a month drill from Time by month", monthDrill.CanonicalQuery, 1,
			ViewRoot{Mode: "aggregate", Grouping: "time", TimeGranularity: "month"}},
		{"a drill whose return frame is currency-scoped", currencyDrill.CanonicalQuery, 1,
			ViewRoot{Mode: "aggregate", Grouping: "time", TimeGranularity: "month"}},
		{"two drills deep",
			drillFirstAggregate(t, server, nested.CanonicalQuery).CanonicalQuery, 2, merchants},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := requestJSON(t, server, "/api/v1/view", ViewBody{
				Query: test.query, Window: Window{Limit: 200},
			})
			projection := decodeRootProjection(t, response)
			assert.Equal(t, test.depth, projection.View.DrillDepth)
			assert.Equal(t, test.root, projection.View.Root)
		})
	}
}

// decodeRootProjection decodes a projection whose view names its drill depth and root.
func decodeRootProjection(t testing.TB, response *httptest.ResponseRecorder) Projection {
	t.Helper()
	var raw struct {
		View map[string]json.RawMessage `json:"view"`
	}
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &raw))
	assert.Contains(t, raw.View, "drill_depth")
	assert.Contains(t, raw.View, "root")
	return decodeProjection(t, response)
}

func rootTransition(t testing.TB, server *Server, query string, body TransitionBody) Projection {
	t.Helper()
	return decodeRootProjection(t, transitionFrom(t, server, query, body))
}

func drillFirstAggregate(t testing.TB, server *Server, query string) Projection {
	t.Helper()
	response := requestJSON(t, server, "/api/v1/view", ViewBody{
		Query: query, Window: Window{Limit: 200},
	})
	projection := decodeRootProjection(t, response)
	require.NotEmpty(t, projection.AggregateRows)
	return rootTransition(t, server, query, TransitionBody{
		Action: app.ActionDrill,
		Target: &TransitionTarget{
			Kind: app.IdentityAggregate, Identity: projection.AggregateRows[0].Identity,
		},
	})
}

// subGroupedByTime breaks a view down by Time, as the terminal's Time key does; the browser
// has no transition for it, so a link is its only source there.
func subGroupedByTime(t testing.TB, query string) string {
	t.Helper()
	state, _, err := DecodeViewQuery(query)
	require.NoError(t, err)
	state.Returns = append(state.Returns, app.ReturnFrame{
		Kind: app.ReturnSubgroup, State: state.Current.Clone(),
	})
	timeDimension := domain.DimensionTime
	state.Current.SubGrouping = &timeDimension
	state.Current.Sort = domain.SortSpec{
		Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
	}
	encoded, err := EncodeViewQuery(state)
	require.NoError(t, err)
	return encoded
}

// timeQuery encodes the top-level Time view after mutate.
func timeQuery(t testing.TB, mutate func(*app.AnalyticalState)) string {
	t.Helper()
	encoded, err := EncodeViewQuery(withState(func(state *app.AnalyticalState) {
		state.Dimension = domain.DimensionTime
		state.Sort = domain.SortSpec{
			Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc,
		}
		mutate(state)
	}))
	require.NoError(t, err)
	return encoded
}

func withoutReturns(t testing.TB, query string) string {
	t.Helper()
	state, _, err := DecodeViewQuery(query)
	require.NoError(t, err)
	state.Returns = nil
	encoded, err := EncodeViewQuery(state)
	require.NoError(t, err)
	return encoded
}
