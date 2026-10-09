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

// Insights returns chart data computed from committed values; pending edits never change it.
func (service *Service) Insights(
	ctx context.Context, request InsightsRequest,
) (InsightsResult, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	if err := request.State.validate(); err != nil {
		return InsightsResult{}, invalidWebRequest(err)
	}
	if request.State.Currency == nil {
		return InsightsResult{}, invalidWebRequest(errors.New("insights require a currency"))
	}
	keys, err := insightsKeys(request.State, request.Rows)
	if err != nil {
		return InsightsResult{}, invalidWebRequest(err)
	}
	query := sessionFromAnalyticalState(request.State).QuerySpec()
	transactions, today, err := service.insightsInputs(ctx, &query)
	if err != nil {
		return InsightsResult{}, err
	}
	insights, err := analytics.ComputeInsights(transactions, analytics.InsightsSpec{
		Query: query, Today: today, Keys: keys, Summary: request.Summary,
	})
	if err != nil {
		return InsightsResult{}, invalidWebRequest(err)
	}
	var revision uint64
	if snapshot, snapshotErr := service.effectiveSnapshotReadOnly(); snapshotErr == nil {
		revision = snapshot.Revision
	}
	return InsightsResult{Revision: revision, State: request.State.Clone(), Insights: insights}, nil
}

// insightsInputs returns committed transactions and today's date. For Amazon profiles a search
// also matches product names, as it does in the table, so the expanded set replaces the
// regular-expression search.
func (service *Service) insightsInputs(
	ctx context.Context, query *domain.QuerySpec,
) ([]domain.Transaction, domain.Date, error) {
	service.mu.RLock()
	transactions := append([]domain.Transaction(nil), service.browseTransactionsLocked()...)
	matcher := service.amazonMatcher
	rawLabels := service.rawProviderMerchantLabelsLocked()
	now := service.now
	service.mu.RUnlock()
	if now == nil {
		now = time.Now
	}
	current := now()
	today, err := domain.NewDate(current.Year(), current.Month(), current.Day())
	if err != nil {
		return nil, domain.Date{}, fmt.Errorf("insights: today: %w", err)
	}
	if matcher == nil || query.Search == "" {
		return transactions, today, nil
	}
	unbounded := query.Clone()
	unbounded.DateRange = nil
	drilldowns := unbounded.Drilldowns[:0]
	for _, drilldown := range unbounded.Drilldowns {
		if drilldown.Dimension != domain.DimensionTime {
			drilldowns = append(drilldowns, drilldown)
		}
	}
	unbounded.Drilldowns = drilldowns
	transactions, err = amazonProductSearch(ctx, transactions, unbounded, matcher, rawLabels)
	if err != nil {
		return nil, domain.Date{}, fmt.Errorf("insights: Amazon product search: %w", err)
	}
	query.Search = ""
	return transactions, today, nil
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
	for _, row := range rows {
		dimension, key, scope, err := parseAggregateIdentity(row)
		if err != nil {
			return nil, err
		}
		if dimension != grouping || scope != *state.Currency {
			return nil, errors.New("insights row does not belong to this view")
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
