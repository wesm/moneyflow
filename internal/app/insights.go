package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// MaxInsightsRows bounds requested series rows to one loaded browser window.
const MaxInsightsRows = analytics.MaxInsightsKeys

// The insights result types are named here too, so interface adapters that may not import
// analytics can convert them.

// InsightsRanges holds the scope, chart context, trend, and comparison ranges.
type InsightsRanges = analytics.Ranges

// InsightsPeriodRange is one chart period.
type InsightsPeriodRange = analytics.PeriodRange

// InsightsSummary holds the view-wide chart data.
type InsightsSummary = analytics.InsightsSummary

// InsightsKPIs holds the scope totals and their comparison.
type InsightsKPIs = analytics.InsightsKPIs

// InsightsTotals holds exact totals for one range.
type InsightsTotals = analytics.InsightsTotals

// InsightsChange compares a value with its prior range.
type InsightsChange = analytics.Change

// InsightsPeriod is one cash-flow period.
type InsightsPeriod = analytics.InsightsPeriod

// InsightsComposition ranks rows by their share of spending and of income.
type InsightsComposition = analytics.Composition

// InsightsCompositionEntry is one ranked row, or the entry folding the rest.
type InsightsCompositionEntry = analytics.CompositionEntry

// InsightsSeries is one requested row's chart data.
type InsightsSeries = analytics.InsightsSeries

// InsightsChangeAmount marks a change reported as an amount rather than a percentage.
const InsightsChangeAmount = analytics.ChangeAmount

var errMalformedIdentity = errors.New("malformed aggregate row identity")

// InsightsRequest asks for the chart data of one view.
type InsightsRequest struct {
	State AnalyticalState
	// Rows are aggregate row identities from AggregateIdentity, at most MaxInsightsRows.
	Rows    []string
	Summary bool
}

// InsightsResult ties chart data to the profile revision it was computed from.
type InsightsResult struct {
	Revision uint64
	State    AnalyticalState
	Insights analytics.Insights
}

// SetClock replaces the clock that defines today for insights. Harnesses pass a fixed clock;
// the default is the system clock.
func (service *Service) SetClock(now func() time.Time) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.now = now
}

// clock returns the service clock; nil means the system clock.
func (service *Service) clock() func() time.Time {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.now
}

// today returns the service clock's date.
func (service *Service) today() (domain.Date, error) {
	now := service.clock()
	if now == nil {
		now = time.Now
	}
	current := now()
	return domain.NewDate(current.Year(), current.Month(), current.Day())
}

// Insights returns chart data computed from committed values; pending edits never change it.
func (service *Service) Insights(
	ctx context.Context, request InsightsRequest,
) (InsightsResult, error) {
	spec, err := insightsSpec(request)
	if err != nil {
		return InsightsResult{}, invalidWebRequest(err)
	}
	service.interactions.Lock()
	defer service.interactions.Unlock()
	transactions, err := service.insightsInputs(ctx, &spec)
	if err != nil {
		return InsightsResult{}, err
	}
	insights, err := analytics.ComputeInsights(transactions, spec)
	if err != nil {
		return InsightsResult{}, invalidWebRequest(err)
	}
	var revision uint64
	if snapshot, snapshotErr := service.effectiveSnapshotReadOnly(); snapshotErr == nil {
		revision = snapshot.Revision
	}
	return InsightsResult{Revision: revision, State: request.State.Clone(), Insights: insights}, nil
}

// insightsSpec validates a request before it waits for the interaction lock.
func insightsSpec(request InsightsRequest) (analytics.InsightsSpec, error) {
	if err := request.State.validate(); err != nil {
		return analytics.InsightsSpec{}, err
	}
	if request.State.Currency == nil {
		return analytics.InsightsSpec{}, errors.New("insights require a currency")
	}
	keys, err := insightsKeys(request.State, request.Rows)
	if err != nil {
		return analytics.InsightsSpec{}, err
	}
	query := analyticalQuerySpec(request.State)
	if _, err := analytics.Filter(nil, query); err != nil {
		return analytics.InsightsSpec{}, err
	}
	return analytics.InsightsSpec{Query: query, Keys: keys, Summary: request.Summary}, nil
}

// insightsInputs returns committed transactions and sets today's date. For Amazon profiles a
// search also matches product names, as it does in the table.
func (service *Service) insightsInputs(
	ctx context.Context, spec *analytics.InsightsSpec,
) ([]domain.Transaction, error) {
	service.mu.RLock()
	transactions := append([]domain.Transaction(nil), service.browseTransactionsLocked()...)
	matcher := service.amazonMatcher
	rawLabels := service.rawProviderMerchantLabelsLocked()
	service.mu.RUnlock()
	today, err := service.today()
	if err != nil {
		return nil, fmt.Errorf("insights: today: %w", err)
	}
	spec.Today = today
	if matcher == nil || spec.Query.Search == "" {
		return transactions, nil
	}
	spec.SearchMatches, err = amazonSearchMatches(ctx, transactions, spec.Query, matcher, rawLabels)
	if err != nil {
		return nil, fmt.Errorf("insights: Amazon product search: %w", err)
	}
	return transactions, nil
}

// amazonSearchMatches lists every transaction the search matches, by text or by Amazon
// product name, in any range. Date bounds and time drill-downs are dropped, because the
// comparison and trend ranges reach outside the scope.
func amazonSearchMatches(
	ctx context.Context, transactions []domain.Transaction, query domain.QuerySpec,
	matcher *AmazonMatchingService, rawLabels map[string]string,
) (map[string]struct{}, error) {
	unbounded := query.Clone()
	unbounded.DateRange = nil
	drilldowns := unbounded.Drilldowns[:0]
	for _, drilldown := range unbounded.Drilldowns {
		if drilldown.Dimension != domain.DimensionTime {
			drilldowns = append(drilldowns, drilldown)
		}
	}
	unbounded.Drilldowns = drilldowns
	matched, err := amazonProductSearch(ctx, transactions, unbounded, matcher, rawLabels)
	if err != nil {
		return nil, err
	}
	matches := make(map[string]struct{}, len(matched))
	for _, transaction := range matched {
		matches[transaction.ID] = struct{}{}
	}
	return matches, nil
}

// insightsKeys converts row identities into aggregate keys of the view's grouping.
func insightsKeys(state AnalyticalState, rows []string) ([]string, error) {
	if len(rows) > MaxInsightsRows {
		return nil, fmt.Errorf("at most %d insights rows", MaxInsightsRows)
	}
	grouping := state.Dimension
	if state.SubGrouping != nil {
		grouping = *state.SubGrouping
	} else if state.Mode != domain.ResultModeAggregate {
		grouping = ""
	}
	keys := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		dimension, key, scope, err := parseAggregateIdentity(row)
		if err != nil {
			return nil, fmt.Errorf("insights row %d: %w", index, err)
		}
		if dimension != grouping || scope != *state.Currency {
			return nil, fmt.Errorf("insights row %d does not belong to this view", index)
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys, nil
}

// parseAggregateIdentity reverses AggregateIdentity.
func parseAggregateIdentity(
	identity string,
) (domain.Dimension, string, domain.CurrencyScope, error) {
	dimension, rest, found := strings.Cut(identity, ":")
	lengthText, rest, foundLength := strings.Cut(rest, ":")
	length, err := strconv.Atoi(lengthText)
	if !found || !foundLength || err != nil || length < 0 || length >= len(rest) {
		return "", "", domain.CurrencyScope{}, errMalformedIdentity
	}
	key := rest[:length]
	scaleText, currency, found := strings.Cut(strings.TrimPrefix(rest[length:], ":"), ":")
	scale, err := strconv.ParseUint(scaleText, 10, 8)
	if !found || err != nil {
		return "", "", domain.CurrencyScope{}, errMalformedIdentity
	}
	scope := domain.CurrencyScope{Currency: domain.Currency(currency), Scale: uint8(scale)}
	row := domain.AggregateRow{
		Dimension: domain.Dimension(dimension), Key: key,
		Total: domain.Money{Currency: scope.Currency, Scale: scope.Scale},
	}
	if AggregateIdentity(row) != identity {
		return "", "", domain.CurrencyScope{}, errMalformedIdentity
	}
	return row.Dimension, key, scope, nil
}
