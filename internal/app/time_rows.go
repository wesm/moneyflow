package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// applyPeriodTransition changes the period as the navigation matrix's period-change row
// says. A currency-scoped Time grouping gets the granularity of its new period when the
// transition derives the next state.
func applyPeriodTransition(session *Session, transition TransitionRequest) error {
	switch transition.Action {
	case ActionClearTime:
		if !session.ClearPeriod() {
			return noChangeWeb(errors.New("view has no active time period"))
		}
		return nil
	case ActionPreviousPeriod:
		return shiftWebPeriod(session, -1, "view has no previous time period")
	case ActionNextPeriod:
		return shiftWebPeriod(session, 1, "view has no next time period")
	case ActionSetPeriod:
		if err := session.SetTimeRange(transition.Period.Range); err != nil {
			return invalidWebRequest(err)
		}
		return nil
	default:
		return invalidWebRequest(fmt.Errorf("%q is not a period action", transition.Action))
	}
}

// derivesTimeGranularity reports whether a view's Time-row granularity follows from its
// period instead of stored state. The browser always scopes a view to one currency; the
// terminal and MCP clients do not, so they keep their own granularity.
func derivesTimeGranularity(state AnalyticalState) bool {
	grouping := state.Dimension
	if state.SubGrouping != nil {
		grouping = *state.SubGrouping
	}
	return state.Currency != nil && grouping == domain.DimensionTime
}

// deriveTimeRows sets a currency-scoped Time grouping's granularity from its period, which
// is the date range narrowed by any time drill-down, and returns the periods its rows cover.
// Other views are unchanged and get no periods. Deriving twice gives the same state.
func (service *Service) deriveTimeRows(
	state *AnalyticalState, currencies []domain.CurrencyCount,
) ([]analytics.PeriodRange, error) {
	if !derivesTimeGranularity(*state) {
		return nil, nil
	}
	scope, err := analytics.TimeScope(analyticalQuerySpec(*state))
	if err != nil {
		return nil, fmt.Errorf("time rows: %w", err)
	}
	today, err := service.today()
	if err != nil {
		return nil, fmt.Errorf("time rows: today: %w", err)
	}
	input := analytics.RangeInput{Scope: scope, Today: today, TimeGrouping: true}
	input.First, input.Latest = currencyDates(currencies, *state.Currency)
	ranges, err := analytics.ResolveRanges(input)
	if err != nil {
		return nil, fmt.Errorf("time rows: %w", err)
	}
	state.TimeGranularity = ranges.ContextGranularity
	return ranges.Context, nil
}

// deriveViewState derives the current view's granularity. Back history keeps its stored
// granularity; a view restored by Back is derived when it becomes current.
func (service *Service) deriveViewState(
	state ViewState, currencies []domain.CurrencyCount,
) (ViewState, error) {
	derived := state.Clone()
	if _, err := service.deriveTimeRows(&derived.Current, currencies); err != nil {
		return ViewState{}, invalidWebRequest(err)
	}
	return derived, nil
}

func currencyDates(
	currencies []domain.CurrencyCount, scope domain.CurrencyScope,
) (domain.Date, domain.Date) {
	for _, candidate := range currencies {
		if candidate.Currency == scope.Currency && candidate.Scale == scope.Scale {
			return candidate.First, candidate.Latest
		}
	}
	return domain.Date{}, domain.Date{}
}

// webQuery evaluates a view with the rows the browser shows, so an identity from a projected
// row resolves here too.
func (service *Service) webQuery(state AnalyticalState) (domain.QueryResult, error) {
	var periods []analytics.PeriodRange
	if derivesTimeGranularity(state) {
		var err error
		if periods, err = service.deriveTimeRows(&state, service.currencies()); err != nil {
			return domain.QueryResult{}, err
		}
	}
	result, err := service.Query(sessionFromAnalyticalState(state))
	if err != nil {
		return domain.QueryResult{}, err
	}
	result.AggregateRows = mergeTimeRows(state, result.AggregateRows, periods)
	return result, nil
}

// currencies lists the profile's currencies with their first and latest transaction dates.
func (service *Service) currencies() []domain.CurrencyCount {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return analytics.Currencies(service.browseTransactionsLocked())
}

// mergeTimeRows lists every period of a Time grouping, including periods without
// transactions, in the view's sort. Data rows outside the periods stay.
func mergeTimeRows(
	state AnalyticalState, rows []domain.AggregateRow, periods []analytics.PeriodRange,
) []domain.AggregateRow {
	query := analyticalQuerySpec(state)
	if len(periods) == 0 || query.Mode != domain.ResultModeAggregate {
		return rows
	}
	listed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		listed[row.Key] = struct{}{}
	}
	merged := slices.Clip(rows)
	for _, period := range periods {
		row := analytics.EmptyTimeRow(period.Period, *query.Currency)
		if _, exists := listed[row.Key]; !exists {
			merged = append(merged, row)
		}
	}
	if len(merged) == len(rows) {
		return rows
	}
	return analytics.SortAggregateRows(merged, query.Sort)
}

// decorateSavingsRates sets each Time row's savings rate.
func decorateSavingsRates(rows []WebAggregateRow) error {
	for index := range rows {
		row := rows[index].Row
		if row.Dimension != domain.DimensionTime {
			continue
		}
		rate, err := analytics.SavingsRateTenths(row.Total.Minor, row.In.Minor)
		if err != nil {
			return err
		}
		rows[index].SavingsRateTenths = rate
	}
	return nil
}
