package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const insightsDescription = "Return chart data for one view: KPIs, cash flow, composition, " +
	"calendar, and per-row trend series. Insights use committed values; get_transactions and " +
	"search_transactions return effective values with pending edits applied."

// InsightsDocument is chart data for one view, computed from committed values in one currency.
type InsightsDocument struct {
	Header
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
	Today    string `json:"today"`
	// Latest is the newest transaction in the currency; absent when there is none.
	Latest  *string                  `json:"latest,omitempty"`
	Ranges  InsightsRangesDocument   `json:"ranges"`
	Summary *InsightsSummaryDocument `json:"summary,omitempty"`
	// Series has one entry per distinct requested key, in request order.
	Series []InsightsSeriesDocument `json:"series"`
}

// InsightsPeriodDocument identifies one calendar period.
type InsightsPeriodDocument struct {
	Granularity string `json:"granularity"`
	Year        int    `json:"year"`
	Month       int    `json:"month,omitempty"`
	Day         int    `json:"day,omitempty"`
}

// InsightsPeriodRangeDocument is one chart period. Value lists elsewhere align with these.
type InsightsPeriodRangeDocument struct {
	Period  InsightsPeriodDocument `json:"period"`
	Start   string                 `json:"start"`
	End     string                 `json:"end"`
	Partial bool                   `json:"partial"`
	Absent  bool                   `json:"absent"`
}

// InsightsHeatmapDocument is the day range a calendar covers.
type InsightsHeatmapDocument struct {
	Layout string `json:"layout"`
	Start  string `json:"start"`
	End    string `json:"end"`
}

// InsightsRangesDocument holds the scope, chart context, trend, and comparison ranges.
type InsightsRangesDocument struct {
	Kind               string                        `json:"kind"`
	Scope              *DateRangeDocument            `json:"scope,omitempty"`
	ContextGranularity string                        `json:"context_granularity"`
	Context            []InsightsPeriodRangeDocument `json:"context"`
	Trend              []InsightsPeriodRangeDocument `json:"trend"`
	Comparison         *DateRangeDocument            `json:"comparison,omitempty"`
	ComparisonLabel    string                        `json:"comparison_label,omitempty"`
	Heatmap            *InsightsHeatmapDocument      `json:"heatmap,omitempty"`
}

// InsightsChangeDocument is one comparison computed by the server.
type InsightsChangeDocument struct {
	Kind          string `json:"kind"`
	PercentTenths int    `json:"percent_tenths"`
	Grew          bool   `json:"grew"`
	Favorable     bool   `json:"favorable"`
	// Amount is current minus prior, present only for the amount kind.
	Amount *Money `json:"amount,omitempty"`
}

// InsightsTotalsDocument holds exact totals for one range.
type InsightsTotalsDocument struct {
	In          Money `json:"in"`
	Out         Money `json:"out"`
	Net         Money `json:"net"`
	Count       int   `json:"count"`
	HiddenCount int   `json:"hidden_count"`
	// PendingCount counts transactions the bank still lists as pending, not pending edits.
	PendingCount int `json:"pending_count"`
}

// InsightsKPIsDocument holds the scope totals and their comparison.
type InsightsKPIsDocument struct {
	Scope                 InsightsTotalsDocument  `json:"scope"`
	Comparison            *InsightsTotalsDocument `json:"comparison,omitempty"`
	IncomeChange          *InsightsChangeDocument `json:"income_change,omitempty"`
	SpendingChange        *InsightsChangeDocument `json:"spending_change,omitempty"`
	NetChange             *InsightsChangeDocument `json:"net_change,omitempty"`
	SavingsRateTenths     *int                    `json:"savings_rate_tenths,omitempty"`
	AveragePerTransaction *Money                  `json:"average_per_transaction,omitempty"`
}

// InsightsCashflowDocument holds one value per ranges.context period.
type InsightsCashflowDocument struct {
	Labels []string `json:"labels"`
	In     []Money  `json:"in"`
	Out    []Money  `json:"out"`
	Net    []Money  `json:"net"`
	Count  []int    `json:"count"`
}

// InsightsInOutDocument holds aligned In and Out values.
type InsightsInOutDocument struct {
	In  []Money `json:"in"`
	Out []Money `json:"out"`
}

// InsightsCompositionEntryDocument is one ranked row, or the entry folding the rest.
type InsightsCompositionEntryDocument struct {
	Key         string `json:"key,omitempty"`
	Label       string `json:"label"`
	Net         Money  `json:"net"`
	ShareTenths int    `json:"share_tenths"`
	Others      int    `json:"others,omitempty"`
}

// InsightsCompositionDocument ranks rows by their share of spending and of income.
type InsightsCompositionDocument struct {
	Spending []InsightsCompositionEntryDocument `json:"spending"`
	Income   []InsightsCompositionEntryDocument `json:"income"`
}

// InsightsSummaryDocument holds the view-wide charts. Optional parts appear where they apply.
type InsightsSummaryDocument struct {
	KPIs              InsightsKPIsDocument          `json:"kpis"`
	Cashflow          InsightsCashflowDocument      `json:"cashflow"`
	Composition       *InsightsCompositionDocument  `json:"composition,omitempty"`
	PeriodComposition []InsightsCompositionDocument `json:"period_composition,omitempty"`
	Calendar          *InsightsInOutDocument        `json:"calendar,omitempty"`
}

// InsightsSeriesDocument is one requested row's chart data.
type InsightsSeriesDocument struct {
	Key      string                  `json:"key"`
	Trend    []Money                 `json:"trend"`
	Context  InsightsInOutDocument   `json:"context"`
	PriorNet *Money                  `json:"prior_net,omitempty"`
	VsPrior  *InsightsChangeDocument `json:"vs_prior,omitempty"`
}

// insightsDocument computes committed chart data for the view the input describes.
func insightsDocument(
	ctx context.Context, service *app.Service, input GetInsightsInput,
) (InsightsDocument, error) {
	if _, err := service.Refresh(ctx); err != nil {
		return InsightsDocument{}, err
	}
	request, err := insightsRequest(input, service.Currencies())
	if err != nil {
		return InsightsDocument{}, newInsightsInputError(service.Revision(), err)
	}
	result, err := service.Insights(ctx, request)
	var webErr *app.WebError
	if errors.As(err, &webErr) && webErr.Code == app.WebInvalidRequest {
		reason := errors.Unwrap(webErr)
		if reason == nil {
			reason = webErr
		}
		return InsightsDocument{}, newInsightsInputError(service.Revision(), reason)
	}
	if err != nil {
		return InsightsDocument{}, err
	}
	return insightsResultDocument(result), nil
}

// newInsightsInputError tells the agent why its insights request was rejected. Only input
// errors carry their reason; other failures keep their generic documents.
func newInsightsInputError(revision uint64, reason error) error {
	return &app.AppError{
		Code: app.AppInvalidOperation, CurrentRevision: revision,
		Detail: "The insights request is invalid: " + reason.Error(),
	}
}

// insightsRequest builds the analytical state and turns keys into row identities of the
// view's grouping.
func insightsRequest(
	input GetInsightsInput, currencies []domain.CurrencyCount,
) (app.InsightsRequest, error) {
	scope, err := insightsCurrency(input, currencies)
	if err != nil {
		return app.InsightsRequest{}, err
	}
	session, grouping, err := insightsSession(input, scope)
	if err != nil {
		return app.InsightsRequest{}, err
	}
	rows, err := insightsRows(input.Keys, grouping, scope)
	if err != nil {
		return app.InsightsRequest{}, err
	}
	return app.InsightsRequest{
		State: session.ViewState().Current, Rows: rows, Summary: defaultTrue(input.Summary),
	}, nil
}

// insightsSession navigates the shared session as the TUI and web do. It returns the
// dimension the view's rows are grouped by, or "" for a transaction view.
func insightsSession(
	input GetInsightsInput, scope domain.CurrencyScope,
) (app.Session, domain.Dimension, error) {
	session := app.NewSession()
	dates, err := insightsDateRange(input.StartDate, input.EndDate)
	if err != nil {
		return session, "", err
	}
	if err = session.SetCurrency(&scope); err != nil {
		return session, "", err
	}
	err = session.SetFilters(app.Filters{
		DateRange: dates, ShowHidden: defaultTrue(input.IncludeHidden),
		ShowTransfers: input.IncludeTransfers,
	})
	if err != nil {
		return session, "", err
	}
	grouping, err := applyInsightsGrouping(&session, input, scope)
	if err != nil {
		return session, "", err
	}
	if input.Search != "" {
		session.SetSearch(input.Search)
	}
	return session, grouping, nil
}

func defaultTrue(value *bool) bool {
	return value == nil || *value
}

// insightsCurrency returns the requested currency, or the profile's only one.
func insightsCurrency(
	input GetInsightsInput, currencies []domain.CurrencyCount,
) (domain.CurrencyScope, error) {
	if (input.Currency == "") != (input.Scale == nil) {
		return domain.CurrencyScope{}, errors.New("currency and scale must be provided together")
	}
	if input.Currency != "" {
		return requestedInsightsCurrency(input, currencies)
	}
	if len(currencies) == 0 {
		return domain.CurrencyScope{}, errors.New("the profile has no transactions")
	}
	if len(currencies) > 1 {
		return domain.CurrencyScope{}, errors.New(
			"the profile has several currencies; provide currency and scale")
	}
	return domain.CurrencyScope{Currency: currencies[0].Currency, Scale: currencies[0].Scale}, nil
}

// requestedInsightsCurrency accepts the requested pair when the profile has it. An empty
// profile has no pairs, so any pair charts as zeros.
func requestedInsightsCurrency(
	input GetInsightsInput, currencies []domain.CurrencyCount,
) (domain.CurrencyScope, error) {
	scope := domain.CurrencyScope{Currency: domain.Currency(input.Currency), Scale: *input.Scale}
	if len(currencies) == 0 {
		return scope, nil
	}
	for _, have := range currencies {
		if have.Currency == scope.Currency && have.Scale == scope.Scale {
			return scope, nil
		}
	}
	return domain.CurrencyScope{}, fmt.Errorf(
		"the profile has no %s transactions at scale %d", scope.Currency, scope.Scale)
}

// insightsDateRange parses an inclusive range; omitting both dates means All.
func insightsDateRange(startText, endText string) (*domain.DateRange, error) {
	if startText == "" && endText == "" {
		return nil, nil
	}
	if startText == "" || endText == "" {
		return nil, errors.New("start_date and end_date must be provided together")
	}
	start, err := domain.ParseDate(startText)
	if err != nil {
		return nil, fmt.Errorf("start_date: %w", err)
	}
	end, err := domain.ParseDate(endText)
	if err != nil {
		return nil, fmt.Errorf("end_date: %w", err)
	}
	return &domain.DateRange{Start: start, End: end}, nil
}

// applyInsightsGrouping drills into the ID filters, then groups the result by group_by.
// Without filters, group_by is the top-level grouping; without either, the view lists
// transactions.
func applyInsightsGrouping(
	session *app.Session, input GetInsightsInput, scope domain.CurrencyScope,
) (domain.Dimension, error) {
	groupBy := domain.Dimension(input.GroupBy)
	drills := insightsDrills(input)
	if len(drills) == 0 && groupBy == "" {
		session.ShowAllDetail()
		return "", nil
	}
	if len(drills) == 0 {
		return groupBy, session.SetGrouping(groupBy)
	}
	if err := drillInsightsFilters(session, drills, scope); err != nil {
		return "", err
	}
	if groupBy == "" {
		return "", nil
	}
	return groupBy, session.SetSubGrouping(groupBy)
}

// drillInsightsFilters groups by each filter's dimension and drills into its row.
func drillInsightsFilters(
	session *app.Session, drills []domain.Drilldown, scope domain.CurrencyScope,
) error {
	for index, drill := range drills {
		group := session.SetSubGrouping
		if index == 0 {
			group = session.SetGrouping
		}
		if err := group(drill.Dimension); err != nil {
			return err
		}
		// Durable view state carries no display labels, so the ID stands in for one.
		row := domain.AggregateRow{
			Dimension: drill.Dimension, Key: drill.Key, Label: drill.Key,
			Total: domain.Money{Currency: scope.Currency, Scale: scope.Scale},
		}
		if err := session.Drill(row, app.ViewPosition{}); err != nil {
			return err
		}
	}
	return nil
}

// insightsDrills lists the ID filters in drill order.
func insightsDrills(input GetInsightsInput) []domain.Drilldown {
	filters := []domain.Drilldown{
		{Dimension: domain.DimensionMerchant, Key: input.MerchantID},
		{Dimension: domain.DimensionCategory, Key: input.CategoryID},
		{Dimension: domain.DimensionGroup, Key: input.GroupID},
		{Dimension: domain.DimensionAccount, Key: input.AccountID},
	}
	drills := filters[:0]
	for _, filter := range filters {
		if filter.Key != "" {
			drills = append(drills, filter)
		}
	}
	return drills
}

// insightsRows turns aggregate keys into row identities of the grouping and currency.
func insightsRows(
	keys []string, grouping domain.Dimension, scope domain.CurrencyScope,
) ([]string, error) {
	if len(keys) > 0 && grouping == "" {
		return nil, errors.New("keys require group_by")
	}
	rows := make([]string, len(keys))
	for index, key := range keys {
		rows[index] = app.AggregateIdentity(domain.AggregateRow{
			Dimension: grouping, Key: key,
			Total: domain.Money{Currency: scope.Currency, Scale: scope.Scale},
		})
	}
	return rows, nil
}

func insightsResultDocument(result app.InsightsResult) InsightsDocument {
	insights := result.Insights
	money := insightsMoney{currency: insights.Currency, scale: insights.Scale}
	document := InsightsDocument{
		Header:   NewHeader(StatusOK, result.Revision),
		Currency: string(insights.Currency), Scale: insights.Scale, Today: insights.Today.String(),
		Ranges: insightsRangesDocument(insights.Ranges),
		Series: make([]InsightsSeriesDocument, len(insights.Series)),
	}
	if !insights.Latest.IsZero() {
		latest := insights.Latest.String()
		document.Latest = &latest
	}
	if insights.Summary != nil {
		summary := money.summary(*insights.Summary)
		document.Summary = &summary
	}
	for index, series := range insights.Series {
		document.Series[index] = money.series(series)
	}
	return document
}

func insightsRangesDocument(ranges app.InsightsRanges) InsightsRangesDocument {
	document := InsightsRangesDocument{
		Kind: string(ranges.Kind), Scope: optionalDateRangeDocument(ranges.Scope),
		ContextGranularity: string(ranges.ContextGranularity),
		Context:            periodRangeDocuments(ranges.Context),
		Trend:              periodRangeDocuments(ranges.Trend),
		Comparison:         optionalDateRangeDocument(ranges.Comparison),
		ComparisonLabel:    ranges.ComparisonLabel,
	}
	if ranges.Heatmap != nil {
		document.Heatmap = &InsightsHeatmapDocument{
			Layout: string(ranges.Heatmap.Layout),
			Start:  ranges.Heatmap.Start.String(), End: ranges.Heatmap.End.String(),
		}
	}
	return document
}

func periodRangeDocuments(periods []app.InsightsPeriodRange) []InsightsPeriodRangeDocument {
	documents := make([]InsightsPeriodRangeDocument, len(periods))
	for index, period := range periods {
		documents[index] = InsightsPeriodRangeDocument{
			Period: InsightsPeriodDocument{
				Granularity: string(period.Period.Granularity), Year: period.Period.Year,
				Month: period.Period.Month, Day: period.Period.Day,
			},
			Start: period.Start.String(), End: period.End.String(),
			Partial: period.Partial, Absent: period.Absent,
		}
	}
	return documents
}

func optionalDateRangeDocument(dates *domain.DateRange) *DateRangeDocument {
	if dates == nil {
		return nil
	}
	return &DateRangeDocument{Start: dates.Start.String(), End: dates.End.String()}
}

// insightsMoney converts the insights' minor units in their one currency to money documents.
type insightsMoney struct {
	currency domain.Currency
	scale    uint8
}

func (money insightsMoney) value(minor int64) Money {
	return MoneyDocument(domain.Money{Minor: minor, Currency: money.currency, Scale: money.scale})
}

func (money insightsMoney) optional(minor *int64) *Money {
	if minor == nil {
		return nil
	}
	value := money.value(*minor)
	return &value
}

func (money insightsMoney) summary(summary app.InsightsSummary) InsightsSummaryDocument {
	document := InsightsSummaryDocument{
		KPIs: money.kpis(summary.KPIs), Cashflow: money.cashflow(summary.Cashflow),
	}
	if summary.Composition != nil {
		composition := money.composition(*summary.Composition)
		document.Composition = &composition
	}
	if summary.PeriodComposition != nil {
		document.PeriodComposition = make([]InsightsCompositionDocument, len(summary.PeriodComposition))
		for index, period := range summary.PeriodComposition {
			document.PeriodComposition[index] = money.composition(period.Composition)
		}
	}
	if summary.Calendar != nil {
		calendar := InsightsInOutDocument{
			In: make([]Money, len(summary.Calendar)), Out: make([]Money, len(summary.Calendar)),
		}
		for index, day := range summary.Calendar {
			calendar.In[index], calendar.Out[index] = money.value(day.In), money.value(day.Out)
		}
		document.Calendar = &calendar
	}
	return document
}

func (money insightsMoney) kpis(kpis app.InsightsKPIs) InsightsKPIsDocument {
	document := InsightsKPIsDocument{
		Scope:                 money.totals(kpis.Scope),
		IncomeChange:          money.change(kpis.IncomeChange),
		SpendingChange:        money.change(kpis.SpendingChange),
		NetChange:             money.change(kpis.NetChange),
		SavingsRateTenths:     kpis.SavingsRateTenths,
		AveragePerTransaction: money.optional(kpis.AveragePerTransaction),
	}
	if kpis.Comparison != nil {
		comparison := money.totals(*kpis.Comparison)
		document.Comparison = &comparison
	}
	return document
}

func (money insightsMoney) totals(totals app.InsightsTotals) InsightsTotalsDocument {
	return InsightsTotalsDocument{
		In: money.value(totals.In), Out: money.value(totals.Out), Net: money.value(totals.Net),
		Count: totals.Count, HiddenCount: totals.HiddenCount, PendingCount: totals.PendingCount,
	}
}

func (money insightsMoney) change(change *app.InsightsChange) *InsightsChangeDocument {
	if change == nil {
		return nil
	}
	document := &InsightsChangeDocument{
		Kind: string(change.Kind), PercentTenths: change.PercentTenths,
		Grew: change.Grew, Favorable: change.Favorable,
	}
	if change.Kind == app.InsightsChangeAmount {
		document.Amount = money.optional(&change.Amount)
	}
	return document
}

func (money insightsMoney) cashflow(periods []app.InsightsPeriod) InsightsCashflowDocument {
	document := InsightsCashflowDocument{
		Labels: make([]string, len(periods)), In: make([]Money, len(periods)),
		Out: make([]Money, len(periods)), Net: make([]Money, len(periods)),
		Count: make([]int, len(periods)),
	}
	for index, period := range periods {
		document.Labels[index], document.Count[index] = period.Label, period.Count
		document.In[index], document.Out[index] = money.value(period.In), money.value(period.Out)
		document.Net[index] = money.value(period.Net)
	}
	return document
}

func (money insightsMoney) composition(
	composition app.InsightsComposition,
) InsightsCompositionDocument {
	return InsightsCompositionDocument{
		Spending: money.compositionEntries(composition.Spending),
		Income:   money.compositionEntries(composition.Income),
	}
}

func (money insightsMoney) compositionEntries(
	entries []app.InsightsCompositionEntry,
) []InsightsCompositionEntryDocument {
	documents := make([]InsightsCompositionEntryDocument, len(entries))
	for index, entry := range entries {
		documents[index] = InsightsCompositionEntryDocument{
			Key: entry.Key, Label: entry.Label, Net: money.value(entry.Net),
			ShareTenths: entry.ShareTenths, Others: entry.Others,
		}
	}
	return documents
}

func (money insightsMoney) series(series app.InsightsSeries) InsightsSeriesDocument {
	document := InsightsSeriesDocument{
		Key: series.Key, Trend: make([]Money, len(series.Trend)),
		Context: InsightsInOutDocument{
			In: make([]Money, len(series.Context)), Out: make([]Money, len(series.Context)),
		},
		PriorNet: money.optional(series.PriorNet), VsPrior: money.change(series.VsPrior),
	}
	for index, point := range series.Trend {
		document.Trend[index] = money.value(point.Net)
	}
	for index, cell := range series.Context {
		document.Context.In[index] = money.value(cell.In)
		document.Context.Out[index] = money.value(cell.Out)
	}
	return document
}
