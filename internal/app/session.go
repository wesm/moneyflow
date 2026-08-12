package app

import (
	"errors"

	"github.com/wesm/moneyflow/internal/domain"
)

var dimensionOrder = []domain.Dimension{
	domain.DimensionMerchant,
	domain.DimensionCategory,
	domain.DimensionGroup,
	domain.DimensionAccount,
	domain.DimensionTime,
}

// Filters contains user-controlled predicates that are independent of navigation.
type Filters struct {
	DateRange     *domain.DateRange
	ShowHidden    bool
	ShowTransfers bool
}

// ViewPosition identifies the row and viewport restored when navigating back.
type ViewPosition struct {
	Cursor int
	Scroll int
}

type historyKind uint8

const (
	historyNavigation historyKind = iota
	historySubGrouping
)

type historyEntry struct {
	snapshot sessionSnapshot
	position ViewPosition
	kind     historyKind
}

type navigationMarker struct {
	mode          domain.ResultMode
	dimension     domain.Dimension
	subGrouping   *domain.Dimension
	drilldownSize int
}

type sessionSnapshot struct {
	mode                   domain.ResultMode
	dimension              domain.Dimension
	subGrouping            *domain.Dimension
	timeGranularity        domain.TimeGranularity
	drilldowns             []domain.Drilldown
	dateRange              *domain.DateRange
	search                 string
	showHidden             bool
	showTransfers          bool
	sort                   domain.SortSpec
	selectedTransactionIDs map[string]struct{}
	selectedAggregateKeys  map[string]struct{}
}

// Session is renderer-neutral UI state. All mutable nested values are privately copied.
type Session struct {
	Mode                   domain.ResultMode
	Dimension              domain.Dimension
	SubGrouping            *domain.Dimension
	TimeGranularity        domain.TimeGranularity
	Drilldowns             []domain.Drilldown
	DateRange              *domain.DateRange
	Search                 string
	ShowHidden             bool
	ShowTransfers          bool
	Sort                   domain.SortSpec
	SelectedTransactionIDs map[string]struct{}
	SelectedAggregateKeys  map[string]struct{}
	history                []historyEntry
	searchAnchor           *navigationMarker
}

// NewSession returns the Python application's default visible state.
func NewSession() Session {
	return Session{
		Mode:                   domain.ResultModeAggregate,
		Dimension:              domain.DimensionMerchant,
		TimeGranularity:        domain.TimeGranularityYear,
		ShowHidden:             true,
		Sort:                   amountDescending(),
		SelectedTransactionIDs: make(map[string]struct{}),
		SelectedAggregateKeys:  make(map[string]struct{}),
	}
}

// QuerySpec returns a defensive renderer-neutral query for the current session.
func (session Session) QuerySpec() domain.QuerySpec {
	mode := session.Mode
	groupBy := session.Dimension
	if session.SubGrouping != nil {
		mode = domain.ResultModeAggregate
		groupBy = *session.SubGrouping
	}
	return domain.QuerySpec{
		DateRange:       cloneDateRange(session.DateRange),
		Search:          session.Search,
		ShowHidden:      session.ShowHidden,
		ShowTransfers:   session.ShowTransfers,
		Drilldowns:      cloneDrilldowns(session.Drilldowns),
		Mode:            mode,
		GroupBy:         groupBy,
		TimeGranularity: session.TimeGranularity,
		Sort:            session.Sort,
	}
}

// CycleGrouping moves through the five top-level aggregate views.
func (session *Session) CycleGrouping() {
	if len(session.Drilldowns) > 0 {
		session.CycleSubGrouping()
		return
	}
	if session.Mode == domain.ResultModeDetail {
		session.Mode = domain.ResultModeAggregate
	}
	current := dimensionIndex(session.Dimension)
	session.Dimension = dimensionOrder[(current+1)%len(dimensionOrder)]
	session.SubGrouping = nil
	session.clearSelections()
	session.Sort = sortForAggregateTransition(session.Dimension, session.Sort)
}

// ShowAllDetail switches from a top-level aggregate to all visible transactions.
func (session *Session) ShowAllDetail() {
	if session.Mode == domain.ResultModeDetail && session.SubGrouping == nil {
		return
	}
	session.pushHistory(historyNavigation, ViewPosition{})
	session.Mode = domain.ResultModeDetail
	session.SubGrouping = nil
	session.Drilldowns = nil
	session.Sort = dateDescending()
	session.clearSelections()
}

// SwitchAccounts jumps directly to the top-level account aggregate.
func (session *Session) SwitchAccounts() {
	session.Mode = domain.ResultModeAggregate
	session.Dimension = domain.DimensionAccount
	session.SubGrouping = nil
	session.Drilldowns = nil
	session.history = nil
	session.Sort = sortForAggregateTransition(domain.DimensionAccount, session.Sort)
	session.clearSelections()
}

// ToggleTimeGranularity cycles year, month, and day aggregation.
func (session *Session) ToggleTimeGranularity() {
	switch session.TimeGranularity {
	case domain.TimeGranularityYear:
		session.TimeGranularity = domain.TimeGranularityMonth
	case domain.TimeGranularityMonth:
		session.TimeGranularity = domain.TimeGranularityDay
	default:
		session.TimeGranularity = domain.TimeGranularityYear
	}
}

// SetSearch replaces the case-insensitive regular expression search predicate.
func (session *Session) SetSearch(search string) {
	session.Search = search
	if search == "" {
		session.searchAnchor = nil
		return
	}
	marker := session.marker()
	session.searchAnchor = &marker
}

// SetFilters validates and defensively copies all independent filters.
func (session *Session) SetFilters(filters Filters) error {
	if filters.DateRange != nil {
		if filters.DateRange.Start.Year() == 0 || filters.DateRange.End.Year() == 0 {
			return errors.New("set filters: invalid date range")
		}
		if filters.DateRange.Start.Compare(filters.DateRange.End) > 0 {
			return errors.New("set filters: date range starts after it ends")
		}
	}
	session.DateRange = cloneDateRange(filters.DateRange)
	session.ShowHidden = filters.ShowHidden
	session.ShowTransfers = filters.ShowTransfers
	return nil
}

func (session *Session) snapshot() sessionSnapshot {
	return sessionSnapshot{
		mode:                   session.Mode,
		dimension:              session.Dimension,
		subGrouping:            cloneDimension(session.SubGrouping),
		timeGranularity:        session.TimeGranularity,
		drilldowns:             cloneDrilldowns(session.Drilldowns),
		dateRange:              cloneDateRange(session.DateRange),
		search:                 session.Search,
		showHidden:             session.ShowHidden,
		showTransfers:          session.ShowTransfers,
		sort:                   session.Sort,
		selectedTransactionIDs: cloneSet(session.SelectedTransactionIDs),
		selectedAggregateKeys:  cloneSet(session.SelectedAggregateKeys),
	}
}

func (session *Session) restore(snapshot sessionSnapshot) {
	session.Mode = snapshot.mode
	session.Dimension = snapshot.dimension
	session.SubGrouping = cloneDimension(snapshot.subGrouping)
	session.TimeGranularity = snapshot.timeGranularity
	session.Drilldowns = cloneDrilldowns(snapshot.drilldowns)
	session.DateRange = cloneDateRange(snapshot.dateRange)
	session.Search = snapshot.search
	session.ShowHidden = snapshot.showHidden
	session.ShowTransfers = snapshot.showTransfers
	session.Sort = snapshot.sort
	session.SelectedTransactionIDs = cloneSet(snapshot.selectedTransactionIDs)
	session.SelectedAggregateKeys = cloneSet(snapshot.selectedAggregateKeys)
}

func (session *Session) pushHistory(kind historyKind, position ViewPosition) {
	session.history = append(session.history, historyEntry{
		snapshot: session.snapshot(),
		position: position,
		kind:     kind,
	})
}

func (session *Session) marker() navigationMarker {
	return navigationMarker{
		mode:          session.Mode,
		dimension:     session.Dimension,
		subGrouping:   cloneDimension(session.SubGrouping),
		drilldownSize: len(session.Drilldowns),
	}
}

func (session *Session) clearSelections() {
	session.SelectedTransactionIDs = make(map[string]struct{})
	session.SelectedAggregateKeys = make(map[string]struct{})
}

func cloneDateRange(dateRange *domain.DateRange) *domain.DateRange {
	if dateRange == nil {
		return nil
	}
	cloned := *dateRange
	return &cloned
}

func cloneDimension(dimension *domain.Dimension) *domain.Dimension {
	if dimension == nil {
		return nil
	}
	cloned := *dimension
	return &cloned
}

func cloneDrilldowns(drilldowns []domain.Drilldown) []domain.Drilldown {
	cloned := append([]domain.Drilldown(nil), drilldowns...)
	for index := range cloned {
		if cloned[index].Period != nil {
			period := *cloned[index].Period
			cloned[index].Period = &period
		}
	}
	return cloned
}

func cloneSet(values map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(values))
	for value := range values {
		cloned[value] = struct{}{}
	}
	return cloned
}

func dimensionIndex(dimension domain.Dimension) int {
	for index, candidate := range dimensionOrder {
		if candidate == dimension {
			return index
		}
	}
	return 0
}

func amountDescending() domain.SortSpec {
	return domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}
}

func dateDescending() domain.SortSpec {
	return domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
}

func sortForAggregateTransition(dimension domain.Dimension, current domain.SortSpec) domain.SortSpec {
	if dimension == domain.DimensionTime {
		return domain.SortSpec{Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc}
	}
	if current.Field == domain.SortFieldCount || current.Field == domain.SortFieldAmount {
		return current
	}
	return amountDescending()
}
