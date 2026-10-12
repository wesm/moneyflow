package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wesm/moneyflow/internal/analytics"
	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

// InsightsSchemaVersion identifies the insights wire contract.
const InsightsSchemaVersion = "1"

// InsightsBody asks for the chart data of one view. The server derives every range from the
// query and its clock.
type InsightsBody struct {
	Query string `json:"query" maxLength:"65536"`
	// Rows are aggregate row identities of the view's grouping. More than 200 rows is an
	// invalid request.
	Rows    []string `json:"rows,omitempty" nullable:"false" maxLength:"4096" doc:"Aggregate row identities of the view's grouping, at most 200. Duplicates are ignored."`
	Summary bool     `json:"summary,omitempty"`
}

// InsightsPeriodRange is one chart period. Value arrays elsewhere align with these entries.
type InsightsPeriodRange struct {
	Period Period `json:"period"`
	Start  string `json:"start" format:"date"`
	End    string `json:"end" format:"date"`
	// Partial marks a period that contains today before its last day, or a custom-range edge
	// period that the scope covers only in part.
	Partial bool `json:"partial"`
	// Absent marks a trend month before the currency's first transaction.
	Absent bool `json:"absent"`
}

// InsightsHeatmap is the day range a calendar heatmap draws.
type InsightsHeatmap struct {
	Layout string `json:"layout" enum:"months,weeks"`
	Start  string `json:"start" format:"date"`
	End    string `json:"end" format:"date"`
}

// InsightsRanges are the scope, chart context, trend, and comparison ranges of one view.
type InsightsRanges struct {
	Kind string `json:"kind" enum:"day,month,year,custom,all"`
	// Scope is absent for All.
	Scope              *DateRange            `json:"scope,omitempty"`
	ContextGranularity string                `json:"context_granularity" enum:"year,month,day"`
	Context            []InsightsPeriodRange `json:"context" nullable:"false"`
	// Trend is empty in the Time grouping.
	Trend []InsightsPeriodRange `json:"trend" nullable:"false"`
	// Comparison and ComparisonLabel are absent for All.
	Comparison      *DateRange       `json:"comparison,omitempty"`
	ComparisonLabel string           `json:"comparison_label,omitempty"`
	Heatmap         *InsightsHeatmap `json:"heatmap,omitempty"`
}

// InsightsChange is one server-computed comparison.
type InsightsChange struct {
	Kind          string `json:"kind" enum:"none,new,percent,amount"`
	PercentTenths int    `json:"percent_tenths"`
	Grew          bool   `json:"grew"`
	Favorable     bool   `json:"favorable"`
	// Amount is current minus prior in minor units, present only for the amount kind.
	Amount *string `json:"amount,omitempty"`
}

// InsightsTotals are exact totals for one range. Hidden transactions count but add no money.
type InsightsTotals struct {
	In          string `json:"in"`
	Out         string `json:"out"`
	Net         string `json:"net"`
	Count       int    `json:"count"`
	HiddenCount int    `json:"hidden_count"`
	// PendingCount counts transactions the bank still lists as pending, not pending edits.
	PendingCount int `json:"pending_count"`
}

// InsightsKPIs feed the KPI strip.
type InsightsKPIs struct {
	Scope InsightsTotals `json:"scope"`
	// Comparison and the three changes are absent when the scope has no comparison.
	Comparison     *InsightsTotals `json:"comparison,omitempty"`
	IncomeChange   *InsightsChange `json:"income_change,omitempty"`
	SpendingChange *InsightsChange `json:"spending_change,omitempty"`
	NetChange      *InsightsChange `json:"net_change,omitempty"`
	// SavingsRateTenths is present at top level without search when In is positive.
	SavingsRateTenths *int `json:"savings_rate_tenths,omitempty"`
	// AveragePerTransaction is present inside a drill when a transaction is not hidden.
	AveragePerTransaction *string `json:"average_per_transaction,omitempty"`
}

// InsightsCashflow holds one value per ranges.context period.
type InsightsCashflow struct {
	Labels []string `json:"labels" nullable:"false"`
	In     []string `json:"in" nullable:"false"`
	Out    []string `json:"out" nullable:"false"`
	Net    []string `json:"net" nullable:"false"`
	Count  []int    `json:"count" nullable:"false"`
}

// InsightsInOut holds aligned In and Out values.
type InsightsInOut struct {
	In  []string `json:"in" nullable:"false"`
	Out []string `json:"out" nullable:"false"`
}

// InsightsCompositionEntry is one ranked row, or the entry that folds the remaining rows.
type InsightsCompositionEntry struct {
	// Key is the aggregate key; absent for the others entry.
	Key string `json:"key,omitempty"`
	// Identity is the table row identity of a ranked entry in summary.composition; absent
	// for the others entry and in period_composition, whose keys are category groups.
	Identity    string `json:"identity,omitempty"`
	Label       string `json:"label"`
	Net         string `json:"net"`
	ShareTenths int    `json:"share_tenths"`
	// Others counts the rows folded into this entry; absent for a ranked row.
	Others int `json:"others,omitempty"`
}

// InsightsComposition ranks rows by net share: Spending lists negative Net and Income
// positive Net, each with the top seven and an others entry.
type InsightsComposition struct {
	Spending []InsightsCompositionEntry `json:"spending" nullable:"false"`
	Income   []InsightsCompositionEntry `json:"income" nullable:"false"`
}

// InsightsSummary holds the chart row's data. Each optional part is present where it applies.
type InsightsSummary struct {
	KPIs InsightsKPIs `json:"kpis"`
	// Cashflow aligns with ranges.context.
	Cashflow InsightsCashflow `json:"cashflow"`
	// Composition is present in the merchant, category, group, and account groupings.
	Composition *InsightsComposition `json:"composition,omitempty"`
	// PeriodComposition is present in the Time grouping, one category-group composition per
	// ranges.context period.
	PeriodComposition []InsightsComposition `json:"period_composition,omitempty" nullable:"false"`
	// Calendar holds one value per day from ranges.heatmap.start through ranges.heatmap.end,
	// present in transaction views and Day scopes.
	Calendar *InsightsInOut `json:"calendar,omitempty"`
}

// InsightsSeries is one requested table row's chart data.
type InsightsSeries struct {
	Identity string `json:"identity"`
	// Trend is Net per ranges.trend month.
	Trend []string `json:"trend" nullable:"false"`
	// Context is In and Out per ranges.context period.
	Context InsightsInOut `json:"context"`
	// PriorNet and VsPrior are absent when the scope has no comparison.
	PriorNet *string         `json:"prior_net,omitempty"`
	VsPrior  *InsightsChange `json:"vs_prior,omitempty"`
}

// InsightsResponse is chart data for one view, computed from committed values in one
// currency. Money values are minor-unit decimal strings in that currency and scale.
type InsightsResponse struct {
	Version  string `json:"version"`
	Revision string `json:"revision" pattern:"^[0-9]+$"`
	// Query is the canonical view query.
	Query    string `json:"query"`
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
	Today    string `json:"today" format:"date"`
	// Latest is the newest transaction in the currency; absent when there is none.
	Latest  *string          `json:"latest,omitempty" format:"date"`
	Ranges  InsightsRanges   `json:"ranges"`
	Summary *InsightsSummary `json:"summary,omitempty"`
	// Series has one entry per distinct requested row, in request order.
	Series []InsightsSeries `json:"series" nullable:"false"`
}

type insightsInput struct {
	ProfileID string `path:"profile_id"`
	Body      InsightsBody
}

type insightsOutput struct{ Body InsightsResponse }

func (server *Server) registerInsightsEndpoint() {
	huma.Register(server.api, huma.Operation{
		OperationID: "projectInsights", Method: http.MethodPost,
		Path:    server.profilePath("view/insights"),
		Summary: "Project chart data for one view from committed values",
		Errors:  []int{400, 413, 422, 500, 503},
	}, func(ctx context.Context, input *insightsInput) (*insightsOutput, error) {
		service := profileService(ctx)
		if _, err := service.Refresh(ctx); err != nil {
			return nil, problemFromError(err)
		}
		response, err := projectInsights(ctx, service, input.Body)
		if err != nil {
			return nil, problemFromError(err)
		}
		return &insightsOutput{Body: response}, nil
	})
}

// projectInsights decodes the view query, computes insights, and converts them to the wire.
func projectInsights(
	ctx context.Context, service *app.Service, body InsightsBody,
) (InsightsResponse, error) {
	state, canonical, err := DecodeViewQuery(body.Query)
	if err != nil {
		return InsightsResponse{}, err
	}
	result, err := service.Insights(ctx, app.InsightsRequest{
		State: state.Current, Rows: body.Rows, Summary: body.Summary,
	})
	if err != nil {
		return InsightsResponse{}, err
	}
	return insightsToWire(result, canonical), nil
}

func insightsToWire(result app.InsightsResult, canonical string) InsightsResponse {
	insights := result.Insights
	response := InsightsResponse{
		Version: InsightsSchemaVersion, Revision: strconv.FormatUint(result.Revision, 10),
		Query: canonical, Currency: string(insights.Currency), Scale: insights.Scale,
		Today: insights.Today.String(), Ranges: insightsRangesToWire(insights.Ranges),
		Series: insightsSeriesToWire(result),
	}
	if !insights.Latest.IsZero() {
		latest := insights.Latest.String()
		response.Latest = &latest
	}
	if insights.Summary != nil {
		summary := insightsSummaryToWire(*insights.Summary, result.RowIdentity)
		response.Summary = &summary
	}
	return response
}

// insightsSeriesToWire labels each series with the row identity of its key.
func insightsSeriesToWire(result app.InsightsResult) []InsightsSeries {
	series := result.Insights.Series
	wire := make([]InsightsSeries, len(series))
	for index, entry := range series {
		wire[index] = InsightsSeries{
			Identity: result.RowIdentity(entry.Key),
			Trend:    make([]string, len(entry.Trend)),
			Context:  cellsToWire(entry.Context),
			PriorNet: optionalMinor(entry.PriorNet),
			VsPrior:  optionalChange(entry.VsPrior),
		}
		for point, trend := range entry.Trend {
			wire[index].Trend[point] = minorText(trend.Net)
		}
	}
	return wire
}

func insightsRangesToWire(ranges analytics.Ranges) InsightsRanges {
	wire := InsightsRanges{
		Kind: string(ranges.Kind), Scope: optionalDateRange(ranges.Scope),
		ContextGranularity: string(ranges.ContextGranularity),
		Context:            periodRangesToWire(ranges.Context),
		Trend:              periodRangesToWire(ranges.Trend),
		Comparison:         optionalDateRange(ranges.Comparison),
		ComparisonLabel:    ranges.ComparisonLabel,
	}
	if ranges.Heatmap != nil {
		wire.Heatmap = &InsightsHeatmap{
			Layout: string(ranges.Heatmap.Layout),
			Start:  ranges.Heatmap.Start.String(), End: ranges.Heatmap.End.String(),
		}
	}
	return wire
}

func periodRangesToWire(periods []analytics.PeriodRange) []InsightsPeriodRange {
	wire := make([]InsightsPeriodRange, len(periods))
	for index, period := range periods {
		wire[index] = InsightsPeriodRange{
			Period: periodToWire(period.Period),
			Start:  period.Start.String(), End: period.End.String(),
			Partial: period.Partial, Absent: period.Absent,
		}
	}
	return wire
}

func periodToWire(period domain.Period) Period {
	return Period{
		Granularity: string(period.Granularity), Year: period.Year,
		Month: period.Month, Day: period.Day,
	}
}

// insightsSummaryToWire converts the summary. rowIdentity links ranked composition entries to
// table rows; period compositions rank category groups, which are not table rows.
func insightsSummaryToWire(
	summary analytics.InsightsSummary, rowIdentity func(string) string,
) InsightsSummary {
	wire := InsightsSummary{
		KPIs:     insightsKPIsToWire(summary.KPIs),
		Cashflow: cashflowToWire(summary.Cashflow),
	}
	if summary.Composition != nil {
		composition := compositionToWire(*summary.Composition, rowIdentity)
		wire.Composition = &composition
	}
	if summary.PeriodComposition != nil {
		wire.PeriodComposition = make([]InsightsComposition, len(summary.PeriodComposition))
		for index, period := range summary.PeriodComposition {
			wire.PeriodComposition[index] = compositionToWire(period.Composition, nil)
		}
	}
	if summary.Calendar != nil {
		calendar := InsightsInOut{
			In: make([]string, len(summary.Calendar)), Out: make([]string, len(summary.Calendar)),
		}
		for index, day := range summary.Calendar {
			calendar.In[index], calendar.Out[index] = minorText(day.In), minorText(day.Out)
		}
		wire.Calendar = &calendar
	}
	return wire
}

func insightsKPIsToWire(kpis analytics.InsightsKPIs) InsightsKPIs {
	wire := InsightsKPIs{
		Scope:                 insightsTotalsToWire(kpis.Scope),
		IncomeChange:          optionalChange(kpis.IncomeChange),
		SpendingChange:        optionalChange(kpis.SpendingChange),
		NetChange:             optionalChange(kpis.NetChange),
		SavingsRateTenths:     kpis.SavingsRateTenths,
		AveragePerTransaction: optionalMinor(kpis.AveragePerTransaction),
	}
	if kpis.Comparison != nil {
		comparison := insightsTotalsToWire(*kpis.Comparison)
		wire.Comparison = &comparison
	}
	return wire
}

func insightsTotalsToWire(totals analytics.InsightsTotals) InsightsTotals {
	return InsightsTotals{
		In: minorText(totals.In), Out: minorText(totals.Out), Net: minorText(totals.Net),
		Count: totals.Count, HiddenCount: totals.HiddenCount, PendingCount: totals.PendingCount,
	}
}

func cashflowToWire(periods []analytics.InsightsPeriod) InsightsCashflow {
	wire := InsightsCashflow{
		Labels: make([]string, len(periods)), In: make([]string, len(periods)),
		Out: make([]string, len(periods)), Net: make([]string, len(periods)),
		Count: make([]int, len(periods)),
	}
	for index, period := range periods {
		wire.Labels[index], wire.Count[index] = period.Label, period.Count
		wire.In[index], wire.Out[index] = minorText(period.In), minorText(period.Out)
		wire.Net[index] = minorText(period.Net)
	}
	return wire
}

// compositionToWire converts a composition; a nil rowIdentity leaves entries unlinked.
func compositionToWire(
	composition analytics.Composition, rowIdentity func(string) string,
) InsightsComposition {
	return InsightsComposition{
		Spending: compositionEntriesToWire(composition.Spending, rowIdentity),
		Income:   compositionEntriesToWire(composition.Income, rowIdentity),
	}
}

func compositionEntriesToWire(
	entries []analytics.CompositionEntry, rowIdentity func(string) string,
) []InsightsCompositionEntry {
	wire := make([]InsightsCompositionEntry, len(entries))
	for index, entry := range entries {
		wire[index] = InsightsCompositionEntry{
			Key: entry.Key, Label: entry.Label, Net: minorText(entry.Net),
			ShareTenths: entry.ShareTenths, Others: entry.Others,
		}
		if rowIdentity != nil && entry.Others == 0 {
			wire[index].Identity = rowIdentity(entry.Key)
		}
	}
	return wire
}

func cellsToWire(cells []analytics.CrossTabCell) InsightsInOut {
	wire := InsightsInOut{In: make([]string, len(cells)), Out: make([]string, len(cells))}
	for index, cell := range cells {
		wire.In[index], wire.Out[index] = minorText(cell.In), minorText(cell.Out)
	}
	return wire
}

func optionalDateRange(dates *domain.DateRange) *DateRange {
	if dates == nil {
		return nil
	}
	return &DateRange{Start: dates.Start.String(), End: dates.End.String()}
}

func optionalChange(change *analytics.Change) *InsightsChange {
	if change == nil {
		return nil
	}
	wire := &InsightsChange{
		Kind: string(change.Kind), PercentTenths: change.PercentTenths,
		Grew: change.Grew, Favorable: change.Favorable,
	}
	if change.Kind == analytics.ChangeAmount {
		wire.Amount = optionalMinor(&change.Amount)
	}
	return wire
}

func optionalMinor(value *int64) *string {
	if value == nil {
		return nil
	}
	text := minorText(*value)
	return &text
}

func minorText(value int64) string {
	return strconv.FormatInt(value, 10)
}
