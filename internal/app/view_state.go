package app

import (
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/domain"
)

// ErrTooManyReturnFrames reports durable history beyond the stateless contract.
var ErrTooManyReturnFrames = errors.New("too many return frames")

const (
	// ViewStateSchemaVersion is the durable browser-state schema understood by this build.
	ViewStateSchemaVersion uint8 = 1
	// MaxReturnFrames bounds stateless analytical back history.
	MaxReturnFrames = 6
)

// ReturnKind identifies why one analytical parent was saved.
type ReturnKind string

const (
	// ReturnNavigation restores a parent aggregate or detail view.
	ReturnNavigation ReturnKind = "navigation"
	// ReturnSubgroup restores the view that preceded a subgroup cycle.
	ReturnSubgroup ReturnKind = "subgroup"
)

// NavigationScope identifies the scope where a committed search was opened.
type NavigationScope struct {
	Mode          domain.ResultMode `json:"mode"`
	Dimension     domain.Dimension  `json:"dimension"`
	SubGrouping   *domain.Dimension `json:"sub_grouping,omitempty"`
	DrilldownSize int               `json:"drilldown_size"`
}

// AnalyticalState is the durable renderer-neutral portion of a Session.
type AnalyticalState struct {
	Mode            domain.ResultMode      `json:"mode"`
	Dimension       domain.Dimension       `json:"dimension"`
	SubGrouping     *domain.Dimension      `json:"sub_grouping,omitempty"`
	TimeGranularity domain.TimeGranularity `json:"time_granularity"`
	Drilldowns      []domain.Drilldown     `json:"drilldowns,omitempty"`
	DateRange       *domain.DateRange      `json:"date_range,omitempty"`
	Search          string                 `json:"search,omitempty"`
	SearchAnchor    *NavigationScope       `json:"search_anchor,omitempty"`
	ShowHidden      bool                   `json:"show_hidden"`
	ShowTransfers   bool                   `json:"show_transfers"`
	Sort            domain.SortSpec        `json:"sort"`
}

// ReturnFrame contains one analytical parent without presentation or selection state.
type ReturnFrame struct {
	Kind  ReturnKind      `json:"kind"`
	State AnalyticalState `json:"state"`
}

// ViewState contains the complete durable state needed by stateless clients.
type ViewState struct {
	Version uint8           `json:"version"`
	Current AnalyticalState `json:"current"`
	Returns []ReturnFrame   `json:"returns,omitempty"`
}

// DefaultViewState returns the canonical initial analytical view.
func DefaultViewState() ViewState {
	return NewSession().ViewState()
}

// Clone returns a view state whose nested values are independent.
func (state ViewState) Clone() ViewState {
	state.Current = state.Current.Clone()
	state.Returns = append([]ReturnFrame(nil), state.Returns...)
	for index := range state.Returns {
		state.Returns[index].State = state.Returns[index].State.Clone()
	}
	return state
}

// Clone returns an analytical state whose nested values are independent.
func (state AnalyticalState) Clone() AnalyticalState {
	state.SubGrouping = cloneDimension(state.SubGrouping)
	state.Drilldowns = stableDrilldowns(state.Drilldowns)
	state.DateRange = cloneDateRange(state.DateRange)
	state.SearchAnchor = cloneNavigationScope(state.SearchAnchor)
	return state
}

// Validate checks the complete durable state without requiring resolved display labels.
func (state ViewState) Validate() error {
	if state.Version != ViewStateSchemaVersion {
		return errors.New("validate view state: unsupported schema version")
	}
	if len(state.Returns) > MaxReturnFrames {
		return fmt.Errorf("validate view state: %w", ErrTooManyReturnFrames)
	}
	if err := state.Current.validate(); err != nil {
		return fmt.Errorf("validate view state: current: %w", err)
	}
	for index, frame := range state.Returns {
		if frame.Kind != ReturnNavigation && frame.Kind != ReturnSubgroup {
			return fmt.Errorf("validate view state: return frame %d: invalid kind", index)
		}
		if err := frame.State.validate(); err != nil {
			return fmt.Errorf("validate view state: return frame %d: %w", index, err)
		}
	}
	return nil
}

func (state AnalyticalState) validate() error {
	if !state.Dimension.Valid() {
		return errors.New("invalid dimension")
	}
	if state.SubGrouping != nil && !state.SubGrouping.Valid() {
		return errors.New("invalid subgroup dimension")
	}
	if (state.Search == "") != (state.SearchAnchor == nil) {
		return errors.New("search and search anchor must be present together")
	}
	for _, drilldown := range state.Drilldowns {
		if drilldown.Label != "" {
			return errors.New("durable drill-down contains a display label")
		}
	}
	if state.SearchAnchor != nil {
		if err := state.SearchAnchor.validate(); err != nil {
			return fmt.Errorf("invalid search anchor: %w", err)
		}
	}
	query := analyticalQuerySpec(state)
	if err := query.Validate(); err != nil {
		return err
	}
	return nil
}

func (scope NavigationScope) validate() error {
	if !scope.Mode.Valid() || !scope.Dimension.Valid() {
		return errors.New("invalid mode or dimension")
	}
	if scope.SubGrouping != nil && !scope.SubGrouping.Valid() {
		return errors.New("invalid subgroup dimension")
	}
	if scope.DrilldownSize < 0 || scope.DrilldownSize > len(dimensionOrder) {
		return errors.New("invalid drill-down size")
	}
	return nil
}

// ViewState exports durable state while omitting cursor, scroll, selection, and display labels.
func (session Session) ViewState() ViewState {
	state := ViewState{
		Version: ViewStateSchemaVersion,
		Current: analyticalStateFromSession(session),
	}
	if len(session.history) > 0 {
		state.Returns = make([]ReturnFrame, len(session.history))
	}
	for index, entry := range session.history {
		state.Returns[index] = ReturnFrame{
			Kind:  returnKind(entry.kind),
			State: analyticalStateFromSnapshot(entry.snapshot),
		}
	}
	return state
}

// NewSessionFromViewState reconstructs analytical history with empty transient state.
func NewSessionFromViewState(state ViewState) (Session, error) {
	if err := state.Validate(); err != nil {
		return Session{}, err
	}
	session := sessionFromAnalyticalState(state.Current)
	session.history = make([]historyEntry, len(state.Returns))
	for index, frame := range state.Returns {
		kind := historyNavigation
		if frame.Kind == ReturnSubgroup {
			kind = historySubGrouping
		}
		session.history[index] = historyEntry{
			snapshot: snapshotFromAnalyticalState(frame.State),
			position: ViewPosition{},
			kind:     kind,
		}
	}
	return session, nil
}

func analyticalStateFromSession(session Session) AnalyticalState {
	return AnalyticalState{
		Mode: session.Mode, Dimension: session.Dimension,
		SubGrouping: cloneDimension(session.SubGrouping), TimeGranularity: session.TimeGranularity,
		Drilldowns: stableDrilldowns(session.Drilldowns), DateRange: cloneDateRange(session.DateRange),
		Search: session.Search, SearchAnchor: navigationScopeFromMarker(session.searchAnchor),
		ShowHidden: session.ShowHidden, ShowTransfers: session.ShowTransfers, Sort: session.Sort,
	}
}

func analyticalStateFromSnapshot(snapshot sessionSnapshot) AnalyticalState {
	return AnalyticalState{
		Mode: snapshot.mode, Dimension: snapshot.dimension,
		SubGrouping: cloneDimension(snapshot.subGrouping), TimeGranularity: snapshot.timeGranularity,
		Drilldowns: stableDrilldowns(snapshot.drilldowns), DateRange: cloneDateRange(snapshot.dateRange),
		Search: snapshot.search, SearchAnchor: navigationScopeFromMarker(snapshot.searchAnchor),
		ShowHidden: snapshot.showHidden, ShowTransfers: snapshot.showTransfers, Sort: snapshot.sort,
	}
}

func sessionFromAnalyticalState(state AnalyticalState) Session {
	return Session{
		Mode: state.Mode, Dimension: state.Dimension,
		SubGrouping: cloneDimension(state.SubGrouping), TimeGranularity: state.TimeGranularity,
		Drilldowns: stableDrilldowns(state.Drilldowns), DateRange: cloneDateRange(state.DateRange),
		Search: state.Search, searchAnchor: markerFromNavigationScope(state.SearchAnchor),
		ShowHidden: state.ShowHidden, ShowTransfers: state.ShowTransfers, Sort: state.Sort,
		SelectedTransactionIDs: make(map[string]struct{}),
		SelectedAggregateKeys:  make(map[string]struct{}),
	}
}

func snapshotFromAnalyticalState(state AnalyticalState) sessionSnapshot {
	return sessionSnapshot{
		mode: state.Mode, dimension: state.Dimension,
		subGrouping: cloneDimension(state.SubGrouping), timeGranularity: state.TimeGranularity,
		drilldowns: stableDrilldowns(state.Drilldowns), dateRange: cloneDateRange(state.DateRange),
		search: state.Search, searchAnchor: markerFromNavigationScope(state.SearchAnchor),
		showHidden: state.ShowHidden, showTransfers: state.ShowTransfers, sort: state.Sort,
		selectedTransactionIDs: make(map[string]struct{}),
		selectedAggregateKeys:  make(map[string]struct{}),
	}
}

func analyticalQuerySpec(state AnalyticalState) domain.QuerySpec {
	mode := state.Mode
	groupBy := state.Dimension
	if state.SubGrouping != nil {
		mode = domain.ResultModeAggregate
		groupBy = *state.SubGrouping
	}
	return domain.QuerySpec{
		DateRange: cloneDateRange(state.DateRange), Search: state.Search,
		ShowHidden: state.ShowHidden, ShowTransfers: state.ShowTransfers,
		Drilldowns: stableDrilldowns(state.Drilldowns), Mode: mode, GroupBy: groupBy,
		TimeGranularity: state.TimeGranularity, Sort: state.Sort,
	}
}

func stableDrilldowns(drilldowns []domain.Drilldown) []domain.Drilldown {
	cloned := cloneDrilldowns(drilldowns)
	for index := range cloned {
		cloned[index].Label = ""
	}
	return cloned
}

func navigationScopeFromMarker(marker *navigationMarker) *NavigationScope {
	if marker == nil {
		return nil
	}
	return &NavigationScope{
		Mode: marker.mode, Dimension: marker.dimension,
		SubGrouping: cloneDimension(marker.subGrouping), DrilldownSize: marker.drilldownSize,
	}
}

func markerFromNavigationScope(scope *NavigationScope) *navigationMarker {
	if scope == nil {
		return nil
	}
	return &navigationMarker{
		mode: scope.Mode, dimension: scope.Dimension,
		subGrouping: cloneDimension(scope.SubGrouping), drilldownSize: scope.DrilldownSize,
	}
}

func cloneNavigationScope(scope *NavigationScope) *NavigationScope {
	if scope == nil {
		return nil
	}
	cloned := *scope
	cloned.SubGrouping = cloneDimension(scope.SubGrouping)
	return &cloned
}

func returnKind(kind historyKind) ReturnKind {
	if kind == historySubGrouping {
		return ReturnSubgroup
	}
	return ReturnNavigation
}
