package app

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	// DefaultWindowLimit is the normal browser row-window size.
	DefaultWindowLimit = 200
	// MaxWindowLimit bounds one browser row window.
	MaxWindowLimit = 400
	// MaxWindowOffset bounds random-access row offsets.
	MaxWindowOffset = 1_000_000
	// PlotRatioScale is the maximum absolute chart coordinate within a money partition.
	PlotRatioScale = 10_000
	// MaxCommittedSearchBytes bounds a transition before it can enter durable URL state.
	MaxCommittedSearchBytes = 2 << 10
	// MaxDurableEntityKeyBytes bounds a drill transition before it can enter durable URL state.
	MaxDurableEntityKeyBytes = 512
)

// WebErrorCode is a stable machine-readable projection or transition failure.
type WebErrorCode string

const (
	// WebInvalidRequest identifies invalid windows, arguments, or transitions.
	WebInvalidRequest WebErrorCode = "invalid_web_request"
	// WebStaleViewTarget identifies a durable stable key that no longer resolves.
	WebStaleViewTarget WebErrorCode = "stale_view_target"
	// WebNoChange identifies an action that cannot change the current state.
	WebNoChange WebErrorCode = "no_change"
)

// WebError separates a safe public detail from its diagnostic cause.
type WebError struct {
	Code   WebErrorCode
	Detail string
	cause  error
}

// Error returns only the safe public detail.
func (webErr *WebError) Error() string {
	return webErr.Detail
}

// Unwrap returns the diagnostic cause.
func (webErr *WebError) Unwrap() error {
	return webErr.cause
}

// WindowRequest asks for one bounded row range. A zero value uses the default limit.
type WindowRequest struct {
	Offset int
	Limit  int
}

// RowTarget identifies one row without trusting a browser index.
type RowTarget struct {
	Kind     IdentityKind
	Identity string
}

// TransitionRequest applies exactly one renderer-neutral action.
type TransitionRequest struct {
	Action  ActionID
	Target  *RowTarget
	Search  *string
	Filters *Filters
}

// Window describes the returned row range.
type Window struct {
	Offset int
	Limit  int
	Count  int
}

// BreadcrumbSegment is one server-derived visible navigation component.
type BreadcrumbSegment struct {
	Dimension domain.Dimension
	Label     string
}

// WebDetailRow decorates a detail row with a stable identity and global index.
type WebDetailRow struct {
	Index    int
	Identity string
	Row      domain.DetailRow
}

// WebAggregateRow decorates an aggregate row with a stable identity and global index.
type WebAggregateRow struct {
	Index    int
	Identity string
	Row      domain.AggregateRow
}

// ChartMark contains renderer-neutral exact chart geometry for one aggregate row.
type ChartMark struct {
	Index     int
	Identity  string
	Label     string
	Amount    domain.Money
	PlotRatio int
}

// ChartProjection complements the current table window without owning analytical state.
type ChartProjection struct {
	Marks   []ChartMark
	Summary []domain.CurrencyStats
}

// WebProjection is the complete renderer-neutral output for one browser row window.
type WebProjection struct {
	State          ViewState
	Selection      SelectionValue
	Breadcrumbs    []BreadcrumbSegment
	BreadcrumbText string
	Filters        Filters
	Actions        []ActionID
	TotalRows      int
	Window         Window
	DetailRows     []WebDetailRow
	AggregateRows  []WebAggregateRow
	Statistics     []domain.CurrencyStats
	Chart          ChartProjection
	Status         string
}

// ProjectView resolves durable state and returns one deterministic row/chart projection.
func (service *Service) ProjectView(
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (WebProjection, error) {
	if err := state.Validate(); err != nil {
		return WebProjection{}, invalidWebRequest(err)
	}
	window, err := normalizeWindow(window)
	if err != nil {
		return WebProjection{}, err
	}
	resolvedSession, breadcrumbs, err := service.resolveViewSession(state.Current)
	if err != nil {
		return WebProjection{}, err
	}
	result, err := service.Query(resolvedSession)
	if err != nil {
		return WebProjection{}, invalidWebRequest(err)
	}
	snapshot, err := service.ResolveSelection(state.Current, selection)
	if err != nil {
		return WebProjection{}, err
	}
	decorateWebSelection(&result, snapshot)

	projection := WebProjection{
		State: state.Clone(), Selection: selection,
		Breadcrumbs: breadcrumbs, BreadcrumbText: resolvedSession.Breadcrumb(result.DateRange),
		Filters: Filters{
			DateRange:  cloneDateRange(state.Current.DateRange),
			ShowHidden: state.Current.ShowHidden, ShowTransfers: state.Current.ShowTransfers,
		},
		Actions: webActionIDs(), TotalRows: resultRowCount(result),
		Statistics: append([]domain.CurrencyStats(nil), result.Statistics...),
	}
	projection.Window = windowResult(window, projection.TotalRows)
	projection.DetailRows = detailWindow(result.DetailRows, projection.Window)
	projection.AggregateRows = aggregateWindow(result.AggregateRows, projection.Window)
	if result.DetailRows != nil {
		projection.Chart.Summary = append([]domain.CurrencyStats(nil), result.Statistics...)
	} else {
		projection.Chart.Marks = chartMarks(projection.AggregateRows)
	}
	if projection.TotalRows == 0 {
		projection.Status = "No transactions match the current view."
	}
	return projection, nil
}

// TransitionView applies one server-authoritative transition and projects its result.
func (service *Service) TransitionView(
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
	window WindowRequest,
) (ViewState, SelectionValue, WebProjection, error) {
	if err := state.Validate(); err != nil {
		return rejectedTransition(state, selection, invalidWebRequest(err))
	}
	if _, err := normalizeWindow(window); err != nil {
		return rejectedTransition(state, selection, err)
	}
	definition, ok := ActionByID(transition.Action)
	if !ok || !definition.Web || !definition.Implemented ||
		(definition.Scope != ScopeAnalytical && definition.Scope != ScopeSelection) {
		return rejectedTransition(
			state,
			selection,
			invalidWebRequest(errors.New("action is not a server transition")),
		)
	}
	if err := validateTransitionArguments(transition); err != nil {
		return rejectedTransition(state, selection, err)
	}
	if definition.Scope == ScopeSelection {
		nextSelection, err := service.applySelectionTransition(state.Current, selection, transition)
		if err != nil {
			return rejectedTransition(state, selection, err)
		}
		if nextSelection == selection {
			return rejectedTransition(state, selection, noChangeWeb(errors.New("selection did not change")))
		}
		projection, err := service.ProjectView(state, nextSelection, window)
		if err != nil {
			return rejectedTransition(state, selection, err)
		}
		return state.Clone(), nextSelection, projection, nil
	}

	session, err := NewSessionFromViewState(state)
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	resolvedCurrent, _, err := service.resolveViewSession(state.Current)
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	session.Drilldowns = resolvedCurrent.Drilldowns
	selectionDocument, err := decodeSelection(selection)
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	if len(selectionDocument.Returns) != 0 && len(selectionDocument.Returns) != len(state.Returns) {
		return rejectedTransition(
			state,
			selection,
			invalidSelection(errors.New("return selections do not match return frames")),
		)
	}
	snapshot, err := service.resolveSelectionPayload(state.Current, selectionDocument.payload())
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	applySnapshotToSession(&session, snapshot)
	for index, payload := range selectionDocument.Returns {
		returnSnapshot, resolveErr := service.resolveSelectionPayload(state.Returns[index].State, payload)
		if resolveErr != nil {
			return rejectedTransition(state, selection, resolveErr)
		}
		applySnapshotToHistory(&session.history[index].snapshot, returnSnapshot)
	}
	if err := service.applyAnalyticalTransition(&session, transition); err != nil {
		return rejectedTransition(state, selection, err)
	}
	nextState := session.ViewState()
	if reflect.DeepEqual(state, nextState) {
		return rejectedTransition(state, selection, noChangeWeb(errors.New("action did not change view")))
	}
	nextSelection, err := service.selectionFromSession(nextState, selection, selectionDocument, session)
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	projection, err := service.ProjectView(nextState, nextSelection, window)
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	return nextState, nextSelection, projection, nil
}

func validateTransitionArguments(transition TransitionRequest) error {
	wantTarget := transition.Action == ActionDrill || transition.Action == ActionToggleSelection
	wantSearch := transition.Action == ActionApplySearch
	wantFilters := transition.Action == ActionApplyFilters
	if (transition.Target != nil) != wantTarget ||
		(transition.Search != nil) != wantSearch ||
		(transition.Filters != nil) != wantFilters {
		return invalidWebRequest(errors.New("action arguments do not match action"))
	}
	if transition.Search != nil &&
		(!utf8.ValidString(*transition.Search) || len(*transition.Search) > MaxCommittedSearchBytes) {
		return invalidWebRequest(errors.New("committed search exceeds durable-state limit or is not UTF-8"))
	}
	return nil
}

func (service *Service) applySelectionTransition(
	state AnalyticalState,
	selection SelectionValue,
	transition TransitionRequest,
) (SelectionValue, error) {
	switch transition.Action {
	case ActionToggleSelection:
		if err := service.validateRowTarget(state, *transition.Target); err != nil {
			return selection, err
		}
		return service.ToggleSelection(
			state,
			selection,
			transition.Target.Kind,
			transition.Target.Identity,
		)
	case ActionToggleSelectAll:
		return service.ToggleAllSelection(state, selection)
	default:
		return selection, invalidWebRequest(errors.New("unsupported selection action"))
	}
}

func (service *Service) applyAnalyticalTransition(
	session *Session,
	transition TransitionRequest,
) error {
	switch transition.Action {
	case ActionCycleGrouping:
		session.CycleGrouping()
	case ActionShowDetail:
		session.ShowAllDetail()
	case ActionSwitchAccounts:
		session.SwitchAccounts()
	case ActionDrill:
		row, err := service.resolveAggregateTarget(*session, *transition.Target)
		if err != nil {
			return err
		}
		if err := session.Drill(row, ViewPosition{}); err != nil {
			return invalidWebRequest(err)
		}
	case ActionBack:
		if _, changed := session.Back(); !changed {
			return noChangeWeb(errors.New("view has no parent"))
		}
	case ActionToggleTime:
		session.ToggleTimeGranularity()
	case ActionClearTime:
		if !session.ClearTimePeriod() {
			return noChangeWeb(errors.New("view has no active time period"))
		}
	case ActionPreviousPeriod:
		if !session.NavigatePeriod(-1) {
			return noChangeWeb(errors.New("view has no previous time period"))
		}
	case ActionNextPeriod:
		if !session.NavigatePeriod(1) {
			return noChangeWeb(errors.New("view has no next time period"))
		}
	case ActionCycleSort:
		session.CycleSort()
	case ActionReverseSort:
		session.ReverseSort()
	case ActionApplySearch:
		session.SetSearch(*transition.Search)
	case ActionApplyFilters:
		if err := session.SetFilters(*transition.Filters); err != nil {
			return invalidWebRequest(err)
		}
	default:
		return invalidWebRequest(errors.New("unsupported analytical action"))
	}
	return nil
}

func (service *Service) resolveAggregateTarget(
	session Session,
	target RowTarget,
) (domain.AggregateRow, error) {
	if target.Kind != IdentityAggregate || target.Identity == "" {
		return domain.AggregateRow{}, invalidWebRequest(errors.New("drill target kind is invalid"))
	}
	result, err := service.Query(session)
	if err != nil {
		return domain.AggregateRow{}, invalidWebRequest(err)
	}
	for _, row := range result.AggregateRows {
		if AggregateIdentity(row) == target.Identity {
			if len(row.Key) > MaxDurableEntityKeyBytes {
				return domain.AggregateRow{}, invalidWebRequest(
					errors.New("drill target key exceeds durable-state limit"),
				)
			}
			return row, nil
		}
	}
	return domain.AggregateRow{}, staleWebTarget(errors.New("aggregate target no longer resolves"))
}

func (service *Service) validateRowTarget(state AnalyticalState, target RowTarget) error {
	if target.Kind != identityKindForState(state) || target.Identity == "" {
		return invalidWebRequest(errors.New("selection target kind is invalid"))
	}
	session, _, err := service.resolveViewSession(state)
	if err != nil {
		return err
	}
	result, err := service.Query(session)
	if err != nil {
		return invalidWebRequest(err)
	}
	if target.Kind == IdentityTransaction {
		for _, row := range result.DetailRows {
			if row.Transaction.ID == target.Identity {
				return nil
			}
		}
	} else {
		for _, row := range result.AggregateRows {
			if AggregateIdentity(row) == target.Identity {
				return nil
			}
		}
	}
	return staleWebTarget(errors.New("selection target no longer resolves"))
}

func applySnapshotToSession(session *Session, snapshot SelectionSnapshot) {
	session.SelectedTransactionIDs = make(map[string]struct{})
	session.SelectedAggregateKeys = make(map[string]struct{})
	if snapshot.Kind == IdentityTransaction {
		session.SelectedTransactionIDs = cloneSet(snapshot.IDs)
		return
	}
	session.SelectedAggregateKeys = cloneSet(snapshot.IDs)
}

func applySnapshotToHistory(snapshot *sessionSnapshot, selection SelectionSnapshot) {
	snapshot.selectedTransactionIDs = make(map[string]struct{})
	snapshot.selectedAggregateKeys = make(map[string]struct{})
	if selection.Kind == IdentityTransaction {
		snapshot.selectedTransactionIDs = cloneSet(selection.IDs)
		return
	}
	snapshot.selectedAggregateKeys = cloneSet(selection.IDs)
}

func (service *Service) selectionFromSession(
	state ViewState,
	old SelectionValue,
	oldDocument selectionDocument,
	session Session,
) (SelectionValue, error) {
	if len(session.history) != len(state.Returns) {
		return old, invalidSelection(errors.New("session history does not match return frames"))
	}
	returns := make([]selectionPayload, len(session.history))
	for index, entry := range session.history {
		returnState := state.Returns[index].State
		returnKind := identityKindForState(returnState)
		returnTarget := entry.snapshot.selectedAggregateKeys
		if returnKind == IdentityTransaction {
			returnTarget = entry.snapshot.selectedTransactionIDs
		}
		payload, err := service.smallestSelectionPayload(returnState, returnKind, returnTarget)
		if err != nil {
			return old, err
		}
		returns[index] = payload
	}

	kind := identityKindForState(state.Current)
	target := session.SelectedAggregateKeys
	if kind == IdentityTransaction {
		target = session.SelectedTransactionIDs
	}
	if len(target) == 0 && len(returns) == 0 {
		return EmptySelection(), nil
	}
	return service.smallestSelectionWithReturns(
		state.Current, old, oldDocument, kind, target, returns,
	)
}

func rejectedTransition(
	state ViewState,
	selection SelectionValue,
	err error,
) (ViewState, SelectionValue, WebProjection, error) {
	return state.Clone(), selection, WebProjection{}, err
}

func normalizeWindow(window WindowRequest) (WindowRequest, error) {
	if window.Offset < 0 || window.Offset > MaxWindowOffset {
		return WindowRequest{}, invalidWebRequest(errors.New("window offset is out of range"))
	}
	if window.Limit == 0 {
		window.Limit = DefaultWindowLimit
	}
	if window.Limit < 1 || window.Limit > MaxWindowLimit {
		return WindowRequest{}, invalidWebRequest(errors.New("window limit is out of range"))
	}
	return window, nil
}

func windowResult(request WindowRequest, total int) Window {
	count := 0
	if request.Offset < total {
		count = min(request.Limit, total-request.Offset)
	}
	return Window{Offset: request.Offset, Limit: request.Limit, Count: count}
}

func (service *Service) resolveViewSession(
	state AnalyticalState,
) (Session, []BreadcrumbSegment, error) {
	session := sessionFromAnalyticalState(state)
	segments := make([]BreadcrumbSegment, 0, len(session.Drilldowns)+1)
	resolved := make([]domain.Drilldown, 0, len(session.Drilldowns))
	for index := range session.Drilldowns {
		drilldown := session.Drilldowns[index]
		if drilldown.Dimension == domain.DimensionTime {
			segments = append(segments, BreadcrumbSegment{
				Dimension: drilldown.Dimension,
				Label:     formatBreadcrumbPeriod(*drilldown.Period),
			})
			resolved = append(resolved, drilldown)
			continue
		}
		label, err := service.resolveDrillLabel(state, resolved, drilldown)
		if err != nil {
			return Session{}, nil, fmt.Errorf("resolve drill-down %d: %w", index, err)
		}
		drilldown.Label = label
		resolved = append(resolved, drilldown)
		segments = append(segments, BreadcrumbSegment{
			Dimension: drilldown.Dimension,
			Label:     label,
		})
	}
	session.Drilldowns = resolved
	if len(segments) == 0 {
		segments = append(segments, BreadcrumbSegment{
			Dimension: state.Dimension,
			Label:     session.Breadcrumb(nil),
		})
	}
	return session, segments, nil
}

func (service *Service) resolveDrillLabel(
	state AnalyticalState,
	prefix []domain.Drilldown,
	target domain.Drilldown,
) (string, error) {
	spec := domain.QuerySpec{
		ShowHidden: true, ShowTransfers: true,
		Drilldowns: cloneDrilldowns(prefix), Mode: domain.ResultModeAggregate,
		GroupBy: target.Dimension, TimeGranularity: state.TimeGranularity,
		Sort: domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc},
	}
	result, err := analytics.Query(service.transactions, spec)
	if err != nil {
		return "", invalidWebRequest(err)
	}
	label := ""
	for _, row := range result.AggregateRows {
		if row.Dimension != target.Dimension || row.Key != target.Key ||
			row.Total.Currency != target.Currency || row.Total.Scale != target.Scale {
			continue
		}
		if label != "" && label != row.Label {
			return "", staleWebTarget(errors.New("stable key resolves to conflicting labels"))
		}
		label = row.Label
	}
	if label == "" {
		return "", staleWebTarget(errors.New("stable key no longer resolves"))
	}
	return label, nil
}

func decorateWebSelection(result *domain.QueryResult, selection SelectionSnapshot) {
	for index := range result.DetailRows {
		_, result.DetailRows[index].Flags.Selected = selection.IDs[result.DetailRows[index].Transaction.ID]
	}
	for index := range result.AggregateRows {
		identity := AggregateIdentity(result.AggregateRows[index])
		_, result.AggregateRows[index].Flags.Selected = selection.IDs[identity]
	}
}

func detailWindow(rows []domain.DetailRow, window Window) []WebDetailRow {
	if rows == nil {
		return nil
	}
	result := make([]WebDetailRow, window.Count)
	for index := range result {
		globalIndex := window.Offset + index
		result[index] = WebDetailRow{
			Index: globalIndex, Identity: rows[globalIndex].Transaction.ID,
			Row: rows[globalIndex],
		}
	}
	return result
}

func aggregateWindow(rows []domain.AggregateRow, window Window) []WebAggregateRow {
	if rows == nil {
		return nil
	}
	result := make([]WebAggregateRow, window.Count)
	for index := range result {
		globalIndex := window.Offset + index
		result[index] = WebAggregateRow{
			Index: globalIndex, Identity: AggregateIdentity(rows[globalIndex]),
			Row: rows[globalIndex],
		}
	}
	return result
}

func chartMarks(rows []WebAggregateRow) []ChartMark {
	if rows == nil {
		return nil
	}
	maxima := make(map[moneyPartition]*big.Int)
	for _, row := range rows {
		partition := moneyPartition{currency: row.Row.Total.Currency, scale: row.Row.Total.Scale}
		absolute := new(big.Int).Abs(big.NewInt(row.Row.Total.Minor))
		if current := maxima[partition]; current == nil || absolute.Cmp(current) > 0 {
			maxima[partition] = absolute
		}
	}
	marks := make([]ChartMark, len(rows))
	for index, row := range rows {
		partition := moneyPartition{currency: row.Row.Total.Currency, scale: row.Row.Total.Scale}
		marks[index] = ChartMark{
			Index: row.Index, Identity: row.Identity, Label: row.Row.Label, Amount: row.Row.Total,
			PlotRatio: exactPlotRatio(row.Row.Total.Minor, maxima[partition]),
		}
	}
	return marks
}

type moneyPartition struct {
	currency domain.Currency
	scale    uint8
}

func exactPlotRatio(minor int64, maximum *big.Int) int {
	if maximum == nil || maximum.Sign() == 0 || minor == 0 {
		return 0
	}
	numerator := new(big.Int).Mul(big.NewInt(minor), big.NewInt(PlotRatioScale))
	ratio := new(big.Int).Quo(numerator, maximum)
	return int(ratio.Int64())
}

func resultRowCount(result domain.QueryResult) int {
	if result.DetailRows != nil {
		return len(result.DetailRows)
	}
	return len(result.AggregateRows)
}

func webActionIDs() []ActionID {
	actions := ReadOnlyActions()
	result := make([]ActionID, 0, len(actions))
	for _, action := range actions {
		if action.Web && action.Implemented {
			result = append(result, action.ID)
		}
	}
	return result
}

func invalidWebRequest(cause error) *WebError {
	return &WebError{
		Code: WebInvalidRequest, Detail: "The web view request is invalid.", cause: cause,
	}
}

func staleWebTarget(cause error) *WebError {
	return &WebError{
		Code: WebStaleViewTarget, Detail: "The selected view target is no longer available.",
		cause: cause,
	}
}

func noChangeWeb(cause error) *WebError {
	return &WebError{
		Code: WebNoChange, Detail: "The requested action would not change the view.", cause: cause,
	}
}
