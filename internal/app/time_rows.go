package app

import (
	"errors"
	"fmt"
	"math/big"

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
// period instead of stored state. Derivation applies to currency-scoped states. The browser
// sends them once its shell sets a currency; until then it sends no currency. The terminal
// and MCP clients send none either, so they keep their own granularity.
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
	if state.TimeGranularityChosen {
		input.TimeUnit = state.TimeGranularity
	}
	input.First, input.Latest = currencyDates(currencies, *state.Currency)
	granularity, periods, err := analytics.TimeRowPeriods(input)
	if err != nil {
		return nil, fmt.Errorf("time rows: %w", err)
	}
	state.TimeGranularity = granularity
	return periods, nil
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
// row resolves here too. The request computes the profile's currencies once and passes them
// in; only a view that derives its Time rows reads them.
func (service *Service) webQuery(
	state AnalyticalState, currencies []domain.CurrencyCount,
) (domain.QueryResult, error) {
	periods, err := service.deriveTimeRows(&state, currencies)
	if err != nil {
		return domain.QueryResult{}, err
	}
	result, err := service.Query(sessionFromAnalyticalState(state))
	if err != nil {
		return domain.QueryResult{}, err
	}
	result.AggregateRows = mergeTimeRows(state, result.AggregateRows, periods)
	return result, nil
}

// Currencies lists the profile's currencies with their transaction counts and first and latest
// transaction dates, most transactions first.
func (service *Service) Currencies() []domain.CurrencyCount {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return analytics.Currencies(service.browseTransactionsLocked())
}

// currenciesFor lists the profile's currencies when one of states derives its Time rows, and
// nil otherwise, so a request that needs no currencies skips the scan.
func (service *Service) currenciesFor(states ...*AnalyticalState) []domain.CurrencyCount {
	for _, state := range states {
		if state != nil && derivesTimeGranularity(*state) {
			return service.Currencies()
		}
	}
	return nil
}

// selectionCurrencies lists the currencies that resolving value in state needs. A selection's
// defining state derives its Time rows as the view does. A value that does not decode needs
// only the view's; resolving it then reports the decode error.
func (service *Service) selectionCurrencies(
	state AnalyticalState, value SelectionValue,
) []domain.CurrencyCount {
	document, err := decodeSelection(value)
	if err != nil {
		return service.currenciesFor(&state)
	}
	return service.currenciesFor(&state, document.State)
}

// mergeTimeRows lists every period of a Time grouping, including periods without
// transactions, in the view's sort. Data rows outside the periods stay, but the empty rows
// the aggregate fills between them do not, so a future transaction lists no empty days
// after today.
func mergeTimeRows(
	state AnalyticalState, rows []domain.AggregateRow, periods []analytics.PeriodRange,
) []domain.AggregateRow {
	query := analyticalQuerySpec(state)
	if len(periods) == 0 || query.Mode != domain.ResultModeAggregate {
		return rows
	}
	empty := make([]domain.AggregateRow, len(periods))
	inPeriods := make(map[string]struct{}, len(periods))
	for index, period := range periods {
		empty[index] = analytics.EmptyTimeRow(period.Period, *query.Currency)
		inPeriods[empty[index].Key] = struct{}{}
	}
	listed := make(map[string]struct{}, len(rows))
	merged := make([]domain.AggregateRow, 0, len(rows)+len(periods))
	for _, row := range rows {
		if _, ok := inPeriods[row.Key]; ok || row.Count > 0 {
			merged = append(merged, row)
			listed[row.Key] = struct{}{}
		}
	}
	for _, row := range empty {
		if _, ok := listed[row.Key]; !ok {
			merged = append(merged, row)
		}
	}
	return analytics.SortAggregateRows(merged, query.Sort)
}

// netRatioScale is the scale of a Time row's net ratio: 10000 is the largest |Net|.
const netRatioScale = 10000

// decorateNetRatios sets each windowed Time row's net ratio. The largest |Net| comes from the
// whole result, so a row's bar keeps its size when the window moves. Amounts of different
// currencies or scales are never compared; each partition has its own maximum. Every row of
// a result has the same dimension, and only a Time result has net ratios.
func decorateNetRatios(window []WebAggregateRow, all []domain.AggregateRow) {
	if len(all) == 0 || all[0].Dimension != domain.DimensionTime {
		return
	}
	maxima := make(map[moneyPartition]*big.Int)
	for _, row := range all {
		partition := moneyPartition{currency: row.Total.Currency, scale: row.Total.Scale}
		absolute := new(big.Int).Abs(big.NewInt(row.Total.Minor))
		if current := maxima[partition]; current == nil || absolute.Cmp(current) > 0 {
			maxima[partition] = absolute
		}
	}
	for index := range window {
		total := window[index].Row.Total
		partition := moneyPartition{currency: total.Currency, scale: total.Scale}
		ratio := netRatio(total.Minor, maxima[partition])
		window[index].NetRatio = &ratio
	}
}

// netRatio returns round_half_up(|net| * netRatioScale / maximum) with the sign of net.
func netRatio(net int64, maximum *big.Int) int {
	if net == 0 || maximum == nil || maximum.Sign() == 0 {
		return 0
	}
	numerator := new(big.Int).Abs(big.NewInt(net))
	numerator.Mul(numerator, big.NewInt(2*netRatioScale))
	numerator.Add(numerator, maximum)
	denominator := new(big.Int).Lsh(maximum, 1)
	ratio := int(numerator.Quo(numerator, denominator).Int64())
	if net < 0 {
		return -ratio
	}
	return ratio
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
