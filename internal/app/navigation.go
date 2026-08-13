package app

import (
	"errors"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
)

// Drill enters the selected aggregate row and records the current view position.
func (session *Session) Drill(row domain.AggregateRow, position ViewPosition) error {
	dimension := session.Dimension
	if session.SubGrouping != nil {
		dimension = *session.SubGrouping
	}
	if row.Dimension != dimension || row.Key == "" || row.Label == "" {
		return errors.New("drill: row does not match the visible dimension")
	}
	if row.Dimension == domain.DimensionTime {
		if row.Period == nil {
			return errors.New("drill: time row requires a period")
		}
		if err := row.Period.Validate(); err != nil {
			return errors.New("drill: invalid time period")
		}
	}
	session.pushHistory(historyNavigation, position)
	drilldown := domain.Drilldown{Dimension: row.Dimension}
	if row.Dimension == domain.DimensionTime {
		period := *row.Period
		drilldown.Period = &period
	} else {
		drilldown.Key = row.Key
		drilldown.Label = row.Label
	}
	session.Drilldowns = append(session.Drilldowns, drilldown)
	session.Mode = domain.ResultModeDetail
	session.SubGrouping = nil
	session.Sort = sortForDetailTransition(row.Dimension, session.Sort)
	session.clearSelections()
	return nil
}

// CycleSubGrouping cycles the remaining dimensions below the current drill path.
func (session *Session) CycleSubGrouping() {
	available := session.availableSubGroupings()
	if len(available) == 0 {
		return
	}
	if session.SubGrouping == nil {
		session.pushHistory(historySubGrouping, ViewPosition{})
		dimension := available[0]
		session.SubGrouping = &dimension
		session.Sort = sortForAggregateTransition(dimension, session.Sort)
		session.clearSelections()
		return
	}
	current := -1
	for index, dimension := range available {
		if dimension == *session.SubGrouping {
			current = index
			break
		}
	}
	if current < 0 || current == len(available)-1 {
		session.SubGrouping = nil
		session.Sort = sortForDetailTransition(session.Dimension, session.Sort)
		if len(session.history) > 0 && session.history[len(session.history)-1].kind == historySubGrouping {
			session.history = session.history[:len(session.history)-1]
		}
	} else {
		dimension := available[current+1]
		session.SubGrouping = &dimension
		session.Sort = sortForAggregateTransition(dimension, session.Sort)
	}
	session.clearSelections()
}

// Back applies search, subgroup, and navigation-history priority in that order.
func (session *Session) Back() (ViewPosition, bool) {
	if session.Search != "" && session.searchAnchor != nil && markersEqual(session.marker(), *session.searchAnchor) {
		session.Search = ""
		session.searchAnchor = nil
		return ViewPosition{}, true
	}
	if session.SubGrouping != nil {
		if len(session.history) > 0 && session.history[len(session.history)-1].kind == historySubGrouping {
			return session.popHistory()
		}
		session.SubGrouping = nil
		session.Sort = sortForDetailTransition(session.Dimension, session.Sort)
		return ViewPosition{}, true
	}
	if len(session.history) > 0 {
		return session.popHistory()
	}
	return ViewPosition{}, false
}

// NavigatePeriod shifts the active time drill-down by delta periods.
func (session *Session) NavigatePeriod(delta int) bool {
	for index := len(session.Drilldowns) - 1; index >= 0; index-- {
		drilldown := &session.Drilldowns[index]
		if drilldown.Dimension != domain.DimensionTime || drilldown.Period == nil {
			continue
		}
		period, ok := shiftPeriod(*drilldown.Period, delta)
		if !ok {
			return false
		}
		drilldown.Period = &period
		return true
	}
	return false
}

// ClearTimePeriod removes the active time drill-down without changing other filters.
func (session *Session) ClearTimePeriod() bool {
	for index := len(session.Drilldowns) - 1; index >= 0; index-- {
		if session.Drilldowns[index].Dimension != domain.DimensionTime {
			continue
		}
		session.Drilldowns = append(session.Drilldowns[:index], session.Drilldowns[index+1:]...)
		return true
	}
	return false
}

func (session *Session) availableSubGroupings() []domain.Dimension {
	used := make(map[domain.Dimension]struct{}, len(session.Drilldowns))
	for _, drilldown := range session.Drilldowns {
		used[drilldown.Dimension] = struct{}{}
	}
	available := make([]domain.Dimension, 0, len(dimensionOrder)-len(used))
	for _, dimension := range dimensionOrder {
		if _, exists := used[dimension]; !exists {
			available = append(available, dimension)
		}
	}
	return available
}

func (session *Session) popHistory() (ViewPosition, bool) {
	last := len(session.history) - 1
	entry := session.history[last]
	session.history = session.history[:last]
	session.restore(entry.snapshot)
	return entry.position, true
}

func sortForDetailTransition(dimension domain.Dimension, current domain.SortSpec) domain.SortSpec {
	if dimension == domain.DimensionTime {
		return amountDescending()
	}
	switch current.Field {
	case domain.SortFieldCount:
		return dateDescending()
	case domain.SortFieldDate, domain.SortFieldMerchant, domain.SortFieldCategory,
		domain.SortFieldAccount, domain.SortFieldAmount:
		return current
	default:
		return amountDescending()
	}
}

func shiftPeriod(period domain.Period, delta int) (domain.Period, bool) {
	switch period.Granularity {
	case domain.TimeGranularityYear:
		period.Year += delta
		return period, period.Year >= 1 && period.Year <= 9999
	case domain.TimeGranularityMonth:
		monthIndex := period.Year*12 + period.Month - 1 + delta
		if monthIndex < 12 || monthIndex >= 10000*12 {
			return domain.Period{}, false
		}
		period.Year = monthIndex / 12
		period.Month = monthIndex%12 + 1
		return period, period.Validate() == nil
	case domain.TimeGranularityDay:
		date, err := domain.NewDate(period.Year, time.Month(period.Month), period.Day)
		if err != nil {
			return domain.Period{}, false
		}
		date, err = date.AddDays(delta)
		if err != nil {
			return domain.Period{}, false
		}
		period.Year = date.Year()
		period.Month = int(date.Month())
		period.Day = date.Day()
		return period, true
	default:
		return domain.Period{}, false
	}
}

func markersEqual(left navigationMarker, right navigationMarker) bool {
	if left.mode != right.mode || left.dimension != right.dimension || left.drilldownSize != right.drilldownSize {
		return false
	}
	if left.subGrouping == nil || right.subGrouping == nil {
		return left.subGrouping == nil && right.subGrouping == nil
	}
	return *left.subGrouping == *right.subGrouping
}
