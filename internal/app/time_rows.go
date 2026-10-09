package app

import (
	"errors"
	"slices"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

// applyPeriodTransition changes the period as the navigation matrix's period-change row
// says, then sets the Time-row granularity that the new period gives. The terminal keeps its
// own granularity control and does not use this.
func (service *Service) applyPeriodTransition(
	session *Session, transition TransitionRequest,
) error {
	var err error
	switch transition.Action {
	case ActionClearTime:
		if !session.ClearPeriod() {
			return noChangeWeb(errors.New("view has no active time period"))
		}
	case ActionPreviousPeriod:
		err = shiftWebPeriod(session, -1, "view has no previous time period")
	case ActionNextPeriod:
		err = shiftWebPeriod(session, 1, "view has no next time period")
	default:
		if setErr := session.SetTimeRange(transition.Period.Range); setErr != nil {
			err = invalidWebRequest(setErr)
		}
	}
	if err != nil {
		return err
	}
	ranges, err := service.timeRowRanges(
		session.DateRange, session.Currency, service.currencies(),
	)
	if err != nil {
		return invalidWebRequest(err)
	}
	session.setPeriodGranularity(ranges.ContextGranularity)
	return nil
}

// webQuery evaluates a view with the rows the browser shows, so an identity from a projected
// row resolves here too.
func (service *Service) webQuery(state AnalyticalState) (domain.QueryResult, error) {
	result, err := service.Query(sessionFromAnalyticalState(state))
	if err != nil || !listsTimeRowsFromRange(analyticalQuerySpec(state)) {
		return result, err
	}
	result.AggregateRows, err = service.withTimeRows(
		state, result.AggregateRows, service.currencies(),
	)
	return result, err
}

// currencies lists the profile's currencies with their first and latest transaction dates.
func (service *Service) currencies() []domain.CurrencyCount {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return analytics.Currencies(service.browseTransactionsLocked())
}

// withTimeRows lists every period of a Time grouping's range, including periods without
// transactions, when the view's granularity is the one its period gives. Data rows outside
// the range stay. Other views keep their rows.
func (service *Service) withTimeRows(
	state AnalyticalState, rows []domain.AggregateRow, currencies []domain.CurrencyCount,
) ([]domain.AggregateRow, error) {
	query := analyticalQuerySpec(state)
	if !listsTimeRowsFromRange(query) {
		return rows, nil
	}
	ranges, err := service.timeRowRanges(query.DateRange, query.Currency, currencies)
	if err != nil {
		return nil, err
	}
	if ranges.ContextGranularity != query.TimeGranularity {
		return rows, nil
	}
	return mergeTimeRows(rows, ranges.Context, *query.Currency, query.Sort), nil
}

// listsTimeRowsFromRange reports whether the view is a one-currency Time grouping whose
// rows cover its period. A time drill-down narrows the rows below the period.
func listsTimeRowsFromRange(query domain.QuerySpec) bool {
	return query.Mode == domain.ResultModeAggregate && query.GroupBy == domain.DimensionTime &&
		query.Currency != nil && !slices.ContainsFunc(query.Drilldowns, isTimeDrilldown)
}

func mergeTimeRows(
	rows []domain.AggregateRow, periods []analytics.PeriodRange,
	scope domain.CurrencyScope, sort domain.SortSpec,
) []domain.AggregateRow {
	listed := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		listed[row.Key] = struct{}{}
	}
	merged := slices.Clip(rows)
	for _, period := range periods {
		row := analytics.EmptyTimeRow(period.Period, scope)
		if _, exists := listed[row.Key]; !exists {
			merged = append(merged, row)
		}
	}
	if len(merged) == len(rows) {
		return rows
	}
	return analytics.SortAggregateRows(merged, sort)
}

// timeRowRanges resolves a Time grouping's ranges from the service clock and the currency's
// first and latest transaction dates.
func (service *Service) timeRowRanges(
	scope *domain.DateRange, currency *domain.CurrencyScope, currencies []domain.CurrencyCount,
) (analytics.Ranges, error) {
	today, err := service.today()
	if err != nil {
		return analytics.Ranges{}, err
	}
	input := analytics.RangeInput{Scope: scope, Today: today, TimeGrouping: true}
	for _, candidate := range currencies {
		if currency != nil && candidate.Currency == currency.Currency &&
			candidate.Scale == currency.Scale {
			input.First, input.Latest = candidate.First, candidate.Latest
		}
	}
	return analytics.ResolveRanges(input)
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
