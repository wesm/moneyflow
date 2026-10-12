package app

import (
	"context"
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
	Action    ActionID
	Target    *RowTarget
	Search    *string
	Filters   *Filters
	Dimension *domain.Dimension
	Currency  *domain.CurrencyScope
	SortField *domain.SortField
	Period    *PeriodTarget
	Depth     *int
}

// PeriodTarget sets the view's period. A nil Range means All.
type PeriodTarget struct {
	Range *domain.DateRange
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
	Index       int
	Identity    string
	Row         domain.DetailRow
	AmazonMatch *AmazonMatchIndicator
	Staged      *StagedValues
}

// AmazonMatchIndicator is the bounded table-cell summary for one best match.
type AmazonMatchIndicator struct {
	Class        analytics.AmazonMatchClass
	Confidence   analytics.AmazonMatchConfidence
	FirstProduct string
	TotalMatches int
}

// WebAggregateRow decorates an aggregate row with a stable identity and global index.
type WebAggregateRow struct {
	Index    int
	Identity string
	Row      domain.AggregateRow
	// SavingsRateTenths is a Time row's Net ÷ In in tenths of a percent; nil unless In > 0.
	SavingsRateTenths *int
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
	Revision          uint64
	ProfileKind       string
	AmazonSettings    *AmazonProjectionSettings
	Pending           PendingSummary
	Capabilities      []Capability
	State             ViewState
	Selection         SelectionValue
	SelectionCount    int
	SelectionSummary  []domain.CurrencyStats
	Breadcrumbs       []BreadcrumbSegment
	BreadcrumbText    string
	Filters           Filters
	Actions           []ActionID
	TotalRows         int
	Window            Window
	DetailRows        []WebDetailRow
	AmazonMatchColumn bool
	AggregateRows     []WebAggregateRow
	Statistics        []domain.CurrencyStats
	Currencies        []domain.CurrencyCount
	Chart             ChartProjection
	Status            string
	AuditWarning      string
}

// AmazonProjectionSettings carries immutable import money settings without source facts.
type AmazonProjectionSettings struct {
	Currency domain.Currency
	Scale    uint8
}

// ProjectView resolves durable state and returns one deterministic row/chart projection.
func (service *Service) ProjectView(
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (WebProjection, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	return service.projectViewLocked(state, selection, window)
}

func (service *Service) projectViewLocked(
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
) (WebProjection, error) {
	return service.projectViewWith(state, selection, window, service.Currencies())
}

// projectViewWith projects a view with the profile's currencies, which the caller computed
// once for the request. The projection's state carries the derived Time granularity.
func (service *Service) projectViewWith(
	state ViewState,
	selection SelectionValue,
	window WindowRequest,
	currencies []domain.CurrencyCount,
) (WebProjection, error) {
	if err := state.Validate(); err != nil {
		return WebProjection{}, invalidWebRequest(err)
	}
	window, err := normalizeWindow(window)
	if err != nil {
		return WebProjection{}, err
	}
	resolvedSession, breadcrumbs, result, err := service.queryProjection(&state.Current, currencies)
	if err != nil {
		return WebProjection{}, err
	}
	snapshot, err := service.resolveSelection(state.Current, selection, currencies)
	if err != nil {
		return WebProjection{}, err
	}
	decorateWebSelection(&result, snapshot)
	summary, err := service.projectionSelectionSummary(result, snapshot)
	if err != nil {
		return WebProjection{}, invalidWebRequest(err)
	}

	actions := webActionIDs()
	var revision uint64
	var pending PendingSummary
	var capabilities []Capability
	if snapshot, snapshotErr := service.effectiveSnapshot(); snapshotErr == nil {
		revision = snapshot.Revision
		pending = pendingSummary(snapshot)
		capabilities = service.capabilitiesForStateSnapshot(snapshot, state)
		for _, capability := range capabilities {
			if capability.Available {
				actions = appendActionID(actions, capability.Action)
			}
		}
	}
	projection := WebProjection{
		Revision: revision, ProfileKind: service.ProfileKind(), Pending: pending, Capabilities: capabilities,
		State: state.Clone(), Selection: selection, SelectionCount: len(snapshot.IDs),
		SelectionSummary: summary,
		Breadcrumbs:      breadcrumbs,
		BreadcrumbText:   resolvedSession.Breadcrumb(result.DateRange),
		Filters: Filters{
			DateRange:  cloneDateRange(state.Current.DateRange),
			ShowHidden: state.Current.ShowHidden, ShowTransfers: state.Current.ShowTransfers,
		},
		Actions:    actions,
		Statistics: append([]domain.CurrencyStats(nil), result.Statistics...),
		Currencies: currencies,
	}
	service.mu.RLock()
	projection.AuditWarning = service.auditWarning
	if service.amazonSettings != nil {
		projection.AmazonSettings = &AmazonProjectionSettings{
			Currency: service.amazonSettings.Currency, Scale: service.amazonSettings.Scale,
		}
	}
	service.mu.RUnlock()
	if err = service.projectRows(&projection, result, window); err != nil {
		return WebProjection{}, invalidWebRequest(err)
	}
	if projection.TotalRows == 0 {
		projection.Status = "No transactions match the current view."
	}
	return projection, nil
}

// queryProjection derives the view's Time granularity, then resolves its breadcrumbs and the
// rows the browser shows. Rows are complete before selection decoration, so a selected empty
// Time row shows as selected.
func (service *Service) queryProjection(
	state *AnalyticalState, currencies []domain.CurrencyCount,
) (Session, []BreadcrumbSegment, domain.QueryResult, error) {
	periods, err := service.deriveTimeRows(state, currencies)
	if err != nil {
		return Session{}, nil, domain.QueryResult{}, invalidWebRequest(err)
	}
	session, breadcrumbs, err := service.ResolveViewSession(*state)
	if err != nil {
		return Session{}, nil, domain.QueryResult{}, err
	}
	result, err := service.Query(session)
	if err != nil {
		return Session{}, nil, domain.QueryResult{}, invalidWebRequest(err)
	}
	result.AggregateRows = mergeTimeRows(*state, result.AggregateRows, periods)
	return session, breadcrumbs, result, nil
}

// projectRows sets the row count, the row window, and the chart of a projection.
func (service *Service) projectRows(
	projection *WebProjection, result domain.QueryResult, window WindowRequest,
) error {
	var err error
	projection.TotalRows = resultRowCount(result)
	projection.Window = windowResult(window, projection.TotalRows)
	projection.DetailRows = detailWindow(result.DetailRows, projection.Window)
	service.decorateStagedValues(projection.DetailRows)
	if err = service.decorateAmazonMatchColumn(projection, result.DetailRows); err != nil {
		return err
	}
	projection.AggregateRows = aggregateWindow(result.AggregateRows, projection.Window)
	if err = decorateSavingsRates(projection.AggregateRows); err != nil {
		return err
	}
	if result.DetailRows != nil {
		projection.Chart.Summary = append([]domain.CurrencyStats(nil), result.Statistics...)
	} else {
		projection.Chart.Marks = chartMarks(projection.AggregateRows)
	}
	return nil
}

func (service *Service) decorateAmazonMatchColumn(
	projection *WebProjection,
	allRows []domain.DetailRow,
) error {
	service.mu.RLock()
	matcher := service.amazonMatcher
	rawLabels := service.rawProviderMerchantLabelsLocked()
	service.mu.RUnlock()
	if matcher == nil || len(allRows) == 0 {
		return nil
	}
	for _, row := range allRows {
		if !isAmazonMerchantLabel(row.Transaction.Merchant.Name) &&
			!isAmazonMerchantLabel(rawLabels[row.Transaction.Merchant.ID]) {
			return nil
		}
	}
	transactions := make([]domain.Transaction, len(projection.DetailRows))
	for index, row := range projection.DetailRows {
		transactions[index] = row.Row.Transaction
	}
	visible, indicators, err := service.AmazonMatchIndicators(context.Background(), transactions)
	if err != nil || !visible {
		return err
	}
	projection.AmazonMatchColumn = true
	for index := range projection.DetailRows {
		projection.DetailRows[index].AmazonMatch = indicators[projection.DetailRows[index].Identity]
	}
	return nil
}

// AmazonMatchIndicators returns the best bounded match only when every row qualifies.
func (service *Service) AmazonMatchIndicators(
	ctx context.Context,
	transactions []domain.Transaction,
) (bool, map[string]*AmazonMatchIndicator, error) {
	service.mu.RLock()
	matcher := service.amazonMatcher
	rawLabels := service.rawProviderMerchantLabelsLocked()
	service.mu.RUnlock()
	if matcher == nil || len(transactions) == 0 {
		return false, nil, nil
	}
	inputs := make([]AmazonMatchInput, len(transactions))
	for index, transaction := range transactions {
		inputs[index] = AmazonMatchInput{
			Transaction: transaction, RawProviderMerchantLabel: rawLabels[transaction.Merchant.ID],
		}
	}
	matchedBatch, err := matcher.MatchBatch(ctx, inputs, 1)
	if err != nil {
		return false, nil, err
	}
	indicators := make(map[string]*AmazonMatchIndicator, len(transactions))
	for index, transaction := range transactions {
		matched := matchedBatch[index]
		if !matched.Qualified {
			return false, nil, nil
		}
		if len(matched.Result.Matches) == 0 {
			continue
		}
		best := matched.Result.Matches[0]
		indicators[transaction.ID] = &AmazonMatchIndicator{
			Class: best.Class, Confidence: best.Confidence,
			FirstProduct: best.FirstProduct, TotalMatches: matched.Result.Total,
		}
	}
	return true, indicators, nil
}

// TransitionView applies one server-authoritative transition and projects its result.
func (service *Service) TransitionView(
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
	window WindowRequest,
) (ViewState, SelectionValue, WebProjection, error) {
	service.interactions.Lock()
	defer service.interactions.Unlock()
	return service.transitionViewLocked(state, selection, transition, window)
}

func (service *Service) transitionViewLocked(
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
	window WindowRequest,
) (ViewState, SelectionValue, WebProjection, error) {
	result, err := service.applyTransition(state, selection, transition, window)
	if err != nil {
		return rejectedTransition(state, selection, err)
	}
	return result.state, result.selection, result.projection, nil
}

// transitionResult is an accepted transition's next state, selection, and projection.
type transitionResult struct {
	state      ViewState
	selection  SelectionValue
	projection WebProjection
}

// applyTransition validates a transition and applies it to the derived current state, so a
// stored granularity that the period no longer gives does not count as a change.
func (service *Service) applyTransition(
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
	window WindowRequest,
) (transitionResult, error) {
	if err := state.Validate(); err != nil {
		return transitionResult{}, invalidWebRequest(err)
	}
	if _, err := normalizeWindow(window); err != nil {
		return transitionResult{}, err
	}
	definition, ok := ActionByID(transition.Action)
	if !ok || !isServerTransition(definition) {
		return transitionResult{}, invalidWebRequest(errors.New("action is not a server transition"))
	}
	if err := validateTransitionArguments(transition); err != nil {
		return transitionResult{}, err
	}
	currencies := service.Currencies()
	state, err := service.deriveViewState(state, currencies)
	if err != nil {
		return transitionResult{}, err
	}
	if definition.Scope == ScopeSelection {
		return service.applySelectionTransitionView(state, selection, transition, window, currencies)
	}
	return service.applyAnalyticalTransitionView(state, selection, transition, window, currencies)
}

func isServerTransition(definition ActionDefinition) bool {
	return definition.Web && definition.Implemented &&
		(definition.Scope == ScopeAnalytical || definition.Scope == ScopeSelection)
}

func (service *Service) applySelectionTransitionView(
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
	window WindowRequest,
	currencies []domain.CurrencyCount,
) (transitionResult, error) {
	nextSelection, err := service.applySelectionTransition(
		state.Current, selection, transition, currencies,
	)
	if err != nil {
		return transitionResult{}, err
	}
	if nextSelection == selection {
		return transitionResult{}, noChangeWeb(errors.New("selection did not change"))
	}
	nextSelection, err = BindSelectionRevision(nextSelection, service.Revision())
	if err != nil {
		return transitionResult{}, err
	}
	projection, err := service.projectViewWith(state, nextSelection, window, currencies)
	if err != nil {
		return transitionResult{}, err
	}
	return transitionResult{
		state: state.Clone(), selection: nextSelection, projection: projection,
	}, nil
}

func (service *Service) applyAnalyticalTransitionView(
	state ViewState,
	selection SelectionValue,
	transition TransitionRequest,
	window WindowRequest,
	currencies []domain.CurrencyCount,
) (transitionResult, error) {
	session, err := service.transitionSession(state, selection, currencies)
	if err != nil {
		return transitionResult{}, err
	}
	if err := service.applyAnalyticalTransition(&session, transition, currencies); err != nil {
		return transitionResult{}, err
	}
	nextState, err := service.deriveViewState(session.ViewState(), currencies)
	if err != nil {
		return transitionResult{}, err
	}
	nextSelection, err := service.selectionFromSession(
		nextState.Current, selection, session, currencies,
	)
	if err != nil {
		return transitionResult{}, err
	}
	if reflect.DeepEqual(state, nextState) && nextSelection == selection {
		return transitionResult{}, noChangeWeb(errors.New("action did not change view"))
	}
	projection, err := service.projectViewWith(nextState, nextSelection, window, currencies)
	if err != nil {
		return transitionResult{}, err
	}
	return transitionResult{state: nextState, selection: nextSelection, projection: projection}, nil
}

// transitionSession rebuilds the session of a view state with resolved drill labels and the
// current selection.
func (service *Service) transitionSession(
	state ViewState, selection SelectionValue, currencies []domain.CurrencyCount,
) (Session, error) {
	session, err := NewSessionFromViewState(state)
	if err != nil {
		return Session{}, err
	}
	resolvedCurrent, _, err := service.ResolveViewSession(state.Current)
	if err != nil {
		return Session{}, err
	}
	session.Drilldowns = resolvedCurrent.Drilldowns
	snapshot, err := service.resolveSelection(state.Current, selection, currencies)
	if err != nil {
		return Session{}, err
	}
	applySnapshotToSession(&session, snapshot)
	return session, nil
}

// transitionArguments records which optional TransitionRequest fields are set.
type transitionArguments struct {
	target, search, filters, dimension, currency, sortField, period, depth bool
}

func argumentsOf(transition TransitionRequest) transitionArguments {
	return transitionArguments{
		target:    transition.Target != nil,
		search:    transition.Search != nil,
		filters:   transition.Filters != nil,
		dimension: transition.Dimension != nil,
		currency:  transition.Currency != nil,
		sortField: transition.SortField != nil,
		period:    transition.Period != nil,
		depth:     transition.Depth != nil,
	}
}

func requiredArguments(action ActionID) transitionArguments {
	switch action {
	case ActionDrill, ActionToggleSelection:
		return transitionArguments{target: true}
	case ActionApplySearch:
		return transitionArguments{search: true}
	case ActionApplyFilters:
		return transitionArguments{filters: true}
	case ActionSetGrouping, ActionSetSubGrouping:
		return transitionArguments{dimension: true}
	case ActionSetCurrency:
		return transitionArguments{currency: true}
	case ActionSetSort:
		return transitionArguments{sortField: true}
	case ActionSetPeriod:
		return transitionArguments{period: true}
	case ActionBackTo:
		return transitionArguments{depth: true}
	default:
		return transitionArguments{}
	}
}

func validateTransitionArguments(transition TransitionRequest) error {
	if argumentsOf(transition) != requiredArguments(transition.Action) {
		return invalidWebRequest(errors.New("action arguments do not match action"))
	}
	if transition.Search != nil &&
		(!utf8.ValidString(*transition.Search) || len(*transition.Search) > MaxCommittedSearchBytes) {
		return invalidWebRequest(errors.New("committed search exceeds durable-state limit or is not UTF-8"))
	}
	if transition.Depth != nil && *transition.Depth < 0 {
		return invalidWebRequest(errors.New("breadcrumb depth is negative"))
	}
	return nil
}

func (service *Service) applySelectionTransition(
	state AnalyticalState,
	selection SelectionValue,
	transition TransitionRequest,
	currencies []domain.CurrencyCount,
) (SelectionValue, error) {
	switch transition.Action {
	case ActionToggleSelection:
		if err := service.validateRowTarget(state, *transition.Target, currencies); err != nil {
			return selection, err
		}
		return service.toggleSelection(
			state,
			selection,
			transition.Target.Kind,
			transition.Target.Identity,
			currencies,
		)
	case ActionToggleSelectAll:
		return service.toggleAllSelection(state, selection, currencies)
	default:
		return selection, invalidWebRequest(errors.New("unsupported selection action"))
	}
}

func (service *Service) applyAnalyticalTransition(
	session *Session,
	transition TransitionRequest,
	currencies []domain.CurrencyCount,
) error {
	switch transition.Action {
	case ActionCycleGrouping:
		session.CycleGrouping()
	case ActionShowDetail:
		session.ShowAllDetail()
	case ActionSwitchAccounts:
		session.SwitchAccounts()
	case ActionDrill:
		row, err := service.resolveAggregateTarget(*session, *transition.Target, currencies)
		if err != nil {
			return err
		}
		if err := session.Drill(row, ViewPosition{}); err != nil {
			return invalidWebRequest(err)
		}
	case ActionBack, ActionBackTo:
		return applyBackTransition(session, transition)
	case ActionClearTime, ActionPreviousPeriod, ActionNextPeriod, ActionSetPeriod:
		return applyPeriodTransition(session, transition)
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
		return applyDirectTransition(session, transition)
	}
	return nil
}

func applyBackTransition(session *Session, transition TransitionRequest) error {
	if transition.Action == ActionBack {
		if _, changed := session.Back(); !changed {
			return noChangeWeb(errors.New("view has no parent"))
		}
		return nil
	}
	depth := *transition.Depth
	if depth > len(session.Drilldowns) {
		return invalidWebRequest(errors.New("breadcrumb depth is deeper than the view"))
	}
	if !session.BackTo(depth) {
		return noChangeWeb(errors.New("view is already at that level"))
	}
	return nil
}

func applyDirectTransition(session *Session, transition TransitionRequest) error {
	var err error
	switch transition.Action {
	case ActionSetGrouping:
		err = session.SetGrouping(*transition.Dimension)
	case ActionSetSubGrouping:
		err = session.SetSubGrouping(*transition.Dimension)
	case ActionSetCurrency:
		err = session.SetCurrency(transition.Currency)
	case ActionSetSort:
		err = session.SetSort(*transition.SortField)
	default:
		err = errors.New("unsupported analytical action")
	}
	if err != nil {
		return invalidWebRequest(err)
	}
	return nil
}

func shiftWebPeriod(session *Session, delta int, unchanged string) error {
	changed, err := session.ShiftPeriod(delta)
	if err != nil {
		return invalidWebRequest(err)
	}
	if !changed {
		return noChangeWeb(errors.New(unchanged))
	}
	return nil
}

func (service *Service) resolveAggregateTarget(
	session Session,
	target RowTarget,
	currencies []domain.CurrencyCount,
) (domain.AggregateRow, error) {
	if target.Kind != IdentityAggregate || target.Identity == "" {
		return domain.AggregateRow{}, invalidWebRequest(errors.New("drill target kind is invalid"))
	}
	result, err := service.webQuery(analyticalStateFromSession(session), currencies)
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

func (service *Service) validateRowTarget(
	state AnalyticalState, target RowTarget, currencies []domain.CurrencyCount,
) error {
	if target.Kind != identityKindForState(state) || target.Identity == "" {
		return invalidWebRequest(errors.New("selection target kind is invalid"))
	}
	if _, _, err := service.ResolveViewSession(state); err != nil {
		return err
	}
	result, err := service.webQuery(state, currencies)
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

func (service *Service) selectionFromSession(
	state AnalyticalState,
	old SelectionValue,
	session Session,
	currencies []domain.CurrencyCount,
) (SelectionValue, error) {
	kind := identityKindForState(state)
	target := session.SelectedAggregateKeys
	if kind == IdentityTransaction {
		target = session.SelectedTransactionIDs
	}
	if len(target) == 0 {
		return EmptySelection(), nil
	}
	return service.smallestSelection(state, old, kind, target, currencies)
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

// ResolveViewSession resolves saved drill identities to committed breadcrumb labels.
func (service *Service) ResolveViewSession(
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
	service.mu.RLock()
	transactions := append([]domain.Transaction(nil), service.browseTransactionsLocked()...)
	service.mu.RUnlock()
	result, err := analytics.Query(transactions, spec)
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
		if known, ok := service.knownEmptyDrillLabel(target); ok {
			return known, nil
		}
		return "", staleWebTarget(errors.New("stable key no longer resolves"))
	}
	return label, nil
}

func (service *Service) knownEmptyDrillLabel(target domain.Drilldown) (string, bool) {
	snapshot, err := service.effectiveSnapshot()
	if err != nil {
		return "", false
	}
	identity := domain.DrillIdentity{
		Dimension: target.Dimension, Currency: target.Currency, Scale: target.Scale, Key: target.Key,
	}
	snapshot.Effective = snapshot.Committed
	if ClassifyKnownDrill(snapshot, identity) == DrillInvalid {
		return "", false
	}
	id := domain.EntityID(target.Key)
	switch target.Dimension {
	case domain.DimensionAccount:
		for _, value := range snapshot.Effective.Accounts {
			if value.ID == id {
				return value.Label, true
			}
		}
	case domain.DimensionMerchant:
		for _, value := range snapshot.Effective.Merchants {
			if value.ID == id {
				return value.Label, true
			}
		}
	case domain.DimensionCategory:
		for _, value := range snapshot.Effective.Categories {
			if value.ID == id {
				return value.Label, true
			}
		}
	case domain.DimensionGroup:
		for _, value := range snapshot.Effective.Groups {
			if value.ID == id {
				return value.Label, true
			}
		}
	}
	return "Unavailable", true
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

func appendActionID(actions []ActionID, candidate ActionID) []ActionID {
	for _, action := range actions {
		if action == candidate {
			return actions
		}
	}
	return append(actions, candidate)
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
