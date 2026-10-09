package app

import (
	"errors"
	"slices"
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
	drilldown := domain.Drilldown{
		Dimension: row.Dimension,
		Currency:  row.Total.Currency,
		Scale:     row.Total.Scale,
	}
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

// JumpToTime groups the current scope by time without discarding filters.
// A scope already drilled into a time period cannot group by time again.
func (session *Session) JumpToTime(position ViewPosition) bool {
	if session.Mode == domain.ResultModeAggregate && len(session.Drilldowns) == 0 && session.SubGrouping == nil {
		session.Dimension = domain.DimensionTime
		session.SubGrouping = nil
	} else {
		for _, drilldown := range session.Drilldowns {
			if drilldown.Dimension == domain.DimensionTime {
				return false
			}
		}
		if session.SubGrouping == nil {
			session.pushHistory(historySubGrouping, position)
		}
		session.SubGrouping = new(domain.DimensionTime)
	}
	session.Sort = sortForAggregateTransition(domain.DimensionTime, session.Sort)
	session.clearSelections()
	return true
}

// SetTimeRange replaces date constraints while retaining the current grouping,
// search, and non-time drill-downs. It clears the selection, and Back retains the new
// range. A nil range selects all time.
func (session *Session) SetTimeRange(dateRange *domain.DateRange) error {
	if err := validateDateRange(dateRange); err != nil {
		return err
	}
	session.applyTimeRange(dateRange)
	return nil
}

// ShiftPeriod moves the period by delta calendar days, months, or years. A range that
// covers exactly one calendar day, month, or year shifts by that unit. Otherwise the last
// time drill-down shifts and becomes the range. It reports false when neither applies or
// the shifted period is outside years 1 through 9999.
func (session *Session) ShiftPeriod(delta int) (bool, error) {
	anchor, resolution, ok := CalendarPeriod(session.DateRange)
	if !ok {
		anchor, resolution, ok = lastTimeDrillPeriod(session.Drilldowns)
	}
	if !ok {
		return false, nil
	}
	dateRange, err := CalendarPeriodRange(ShiftCalendarPeriod(anchor, resolution, delta), resolution)
	if err != nil {
		return false, nil
	}
	return true, session.SetTimeRange(&dateRange)
}

// ClearPeriod selects all time and removes time drill-downs. It reports false when the
// view already covers all time.
func (session *Session) ClearPeriod() bool {
	if session.DateRange == nil && !slices.ContainsFunc(session.Drilldowns, isTimeDrilldown) {
		return false
	}
	session.applyTimeRange(nil)
	return true
}

func (session *Session) applyTimeRange(dateRange *domain.DateRange) {
	session.applyFilters(Filters{
		DateRange: dateRange, ShowHidden: session.ShowHidden, ShowTransfers: session.ShowTransfers,
	})
	session.Drilldowns = removeTimeDrilldowns(session.Drilldowns, session.searchAnchor)
	for index := range session.history {
		snapshot := &session.history[index].snapshot
		snapshot.drilldowns = removeTimeDrilldowns(snapshot.drilldowns, snapshot.searchAnchor)
		clear(snapshot.selectedTransactionIDs)
		clear(snapshot.selectedAggregateKeys)
	}
}

func isTimeDrilldown(drilldown domain.Drilldown) bool {
	return drilldown.Dimension == domain.DimensionTime
}

func lastTimeDrillPeriod(drilldowns []domain.Drilldown) (time.Time, domain.TimeGranularity, bool) {
	for index := len(drilldowns) - 1; index >= 0; index-- {
		period := drilldowns[index].Period
		if !isTimeDrilldown(drilldowns[index]) || period == nil {
			continue
		}
		month, day := time.Month(max(period.Month, 1)), max(period.Day, 1)
		return time.Date(period.Year, month, day, 0, 0, 0, 0, time.UTC), period.Granularity, true
	}
	return time.Time{}, "", false
}

func removeTimeDrilldowns(drilldowns []domain.Drilldown, searchAnchor *navigationMarker) []domain.Drilldown {
	for index := len(drilldowns) - 1; index >= 0; index-- {
		if drilldowns[index].Dimension != domain.DimensionTime {
			continue
		}
		if searchAnchor != nil && index < searchAnchor.drilldownSize {
			searchAnchor.drilldownSize--
		}
		drilldowns = append(drilldowns[:index], drilldowns[index+1:]...)
	}
	return drilldowns
}

// Back clears the selection first, then the search applied at this level, then the
// sub-grouping, then returns to the parent. It reports false at the top level with
// nothing to undo.
func (session *Session) Back() (ViewPosition, bool) {
	if session.hasSelection() {
		session.clearSelections()
		return ViewPosition{}, true
	}
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

var calendarResolutions = []domain.TimeGranularity{
	domain.TimeGranularityYear, domain.TimeGranularityMonth, domain.TimeGranularityDay,
}

// CalendarPeriod reports the anchor and resolution of a range that covers exactly one
// calendar day, month, or year.
func CalendarPeriod(dateRange *domain.DateRange) (time.Time, domain.TimeGranularity, bool) {
	if dateRange != nil {
		start := dateRange.Start
		anchor := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		for _, resolution := range calendarResolutions {
			fullPeriod, err := CalendarPeriodRange(anchor, resolution)
			if err == nil && fullPeriod == *dateRange {
				return anchor, resolution, true
			}
		}
	}
	return time.Time{}, "", false
}

// CalendarPeriodRange returns the calendar day, month, or year that contains anchor.
func CalendarPeriodRange(
	anchor time.Time,
	resolution domain.TimeGranularity,
) (domain.DateRange, error) {
	year, month, day := anchor.Date()
	endMonth, endDay := month, day
	switch resolution {
	case domain.TimeGranularityYear:
		month, day, endMonth, endDay = time.January, 1, time.December, 31
	case domain.TimeGranularityMonth:
		day = 1
		endDay = time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	}
	start, err := domain.NewDate(year, month, day)
	if err != nil {
		return domain.DateRange{}, err
	}
	end, err := domain.NewDate(year, endMonth, endDay)
	return domain.DateRange{Start: start, End: end}, err
}

// ShiftCalendarPeriod moves anchor by delta days, months, or years. It keeps the day when
// possible and clamps it at month end. Anchors use UTC because a period holds calendar
// dates, not instants.
func ShiftCalendarPeriod(anchor time.Time, resolution domain.TimeGranularity, delta int) time.Time {
	year, month, day := anchor.Date()
	if resolution == domain.TimeGranularityDay {
		return time.Date(year, month, day+delta, 0, 0, 0, 0, time.UTC)
	}
	if resolution == domain.TimeGranularityYear {
		year += delta
	} else {
		month += time.Month(delta)
	}
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(year, month, min(day, lastDay), 0, 0, 0, 0, time.UTC)
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
	session.clearSelections()
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

func markersEqual(left navigationMarker, right navigationMarker) bool {
	if left.mode != right.mode || left.dimension != right.dimension || left.drilldownSize != right.drilldownSize {
		return false
	}
	if left.subGrouping == nil || right.subGrouping == nil {
		return left.subGrouping == nil && right.subGrouping == nil
	}
	return *left.subGrouping == *right.subGrouping
}
