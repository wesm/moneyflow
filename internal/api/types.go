package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	// APISchemaVersion identifies the HTTP compatibility contract.
	APISchemaVersion = "1"
	// ProjectionSchemaVersion identifies the browser projection shape.
	ProjectionSchemaVersion = "1"
	// MaxViewBodyBytes bounds a complete structured request body.
	MaxViewBodyBytes = 2 << 20
)

// Money is exact money text safe for JavaScript clients.
type Money struct {
	Minor    string `json:"minor"`
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
	Decimal  string `json:"decimal"`
	Display  string `json:"display"`
}

// Window is one bounded request range.
type Window struct {
	Offset int `json:"offset" minimum:"0" maximum:"1000000" default:"0"`
	Limit  int `json:"limit" minimum:"1" maximum:"400" default:"200"`
}

// ViewBody asks for one normalized view projection.
type ViewBody struct {
	Query     string `json:"query" maxLength:"65536"`
	Selection string `json:"selection,omitempty" maxLength:"1468006"`
	Window    Window `json:"window"`
}

// TransitionTarget identifies a projected row by its server-issued stable identity.
type TransitionTarget struct {
	Kind     app.IdentityKind `json:"kind"`
	Identity string           `json:"identity" maxLength:"4096"`
}

// DateRange is an inclusive pair of ISO calendar dates.
type DateRange struct {
	Start string `json:"start" format:"date"`
	End   string `json:"end" format:"date"`
}

// TransitionPeriod sets the view's period. Both bounds make an inclusive range; neither
// means all time.
type TransitionPeriod struct {
	Start *string `json:"start,omitempty" format:"date"`
	End   *string `json:"end,omitempty" format:"date"`
}

// TransitionFilters contains staged predicates accepted by filters.apply.
type TransitionFilters struct {
	DateRange     *DateRange `json:"date_range,omitempty"`
	ShowHidden    bool       `json:"show_hidden"`
	ShowTransfers bool       `json:"show_transfers"`
}

// TransitionBody asks the server to apply exactly one renderer-neutral action.
type TransitionBody struct {
	Query     string             `json:"query" maxLength:"65536"`
	Selection string             `json:"selection,omitempty" maxLength:"1468006"`
	Action    app.ActionID       `json:"action"`
	Target    *TransitionTarget  `json:"target,omitempty"`
	Search    *string            `json:"search,omitempty" maxLength:"2048"`
	Filters   *TransitionFilters `json:"filters,omitempty"`
	Dimension *string            `json:"dimension,omitempty" enum:"merchant,category,group,account,time"`
	Currency  *CurrencyScope     `json:"currency,omitempty"`
	SortField *string            `json:"sort_field,omitempty" enum:"count,amount,date,merchant,category,group,account,time_period"`
	Period    *TransitionPeriod  `json:"period,omitempty"`
	Depth     *int               `json:"depth,omitempty" minimum:"0"`
	Window    Window             `json:"window"`
}

// Flags contains the read-only row state exposed to the browser.
type Flags struct {
	Selected bool `json:"selected"`
	Hidden   bool `json:"hidden"`
	Pending  bool `json:"pending"`
}

// DetailRow contains only fields rendered by the read-only interface.
type DetailRow struct {
	Index       int                   `json:"index"`
	Identity    string                `json:"identity"`
	Date        string                `json:"date"`
	Account     string                `json:"account"`
	Merchant    string                `json:"merchant"`
	Category    string                `json:"category"`
	Group       string                `json:"group"`
	Amount      Money                 `json:"amount"`
	Flags       Flags                 `json:"flags"`
	AmazonMatch *AmazonMatchIndicator `json:"amazon_match,omitempty"`
	Staged      *StagedValues         `json:"staged,omitempty"`
}

// StagedValues lists the pending values of a row that differ from its committed ones, using
// the row's own display strings. A category change carries the new group too.
type StagedValues struct {
	Merchant *string `json:"merchant,omitempty"`
	Category *string `json:"category,omitempty"`
	Group    *string `json:"group,omitempty"`
	Hidden   *bool   `json:"hidden,omitempty"`
	Deleted  bool    `json:"deleted"`
}

// AmazonMatchIndicator is the bounded summary rendered in qualifying transaction tables.
type AmazonMatchIndicator struct {
	Class        string `json:"class"`
	Confidence   string `json:"confidence"`
	FirstProduct string `json:"first_product"`
	TotalMatches int    `json:"total_matches"`
}

// Period is a typed time label with no locale-dependent parsing.
type Period struct {
	Granularity string `json:"granularity"`
	Year        int    `json:"year"`
	Month       int    `json:"month,omitempty"`
	Day         int    `json:"day,omitempty"`
}

// AggregateRow contains one stable aggregate partition. Group is a category row's group label;
// other dimensions and ungrouped categories omit it.
type AggregateRow struct {
	Index              int     `json:"index"`
	Identity           string  `json:"identity"`
	Dimension          string  `json:"dimension"`
	Label              string  `json:"label"`
	Group              string  `json:"group,omitempty"`
	Count              int     `json:"count"`
	In                 Money   `json:"in"`
	Out                Money   `json:"out"`
	Total              Money   `json:"total"`
	Period             *Period `json:"period,omitempty"`
	TopCategory        string  `json:"top_category,omitempty"`
	TopCategoryPercent int     `json:"top_category_percent,omitempty"`
	ShareTenths        int     `json:"share_tenths"`
	Flags              Flags   `json:"flags"`
	// SavingsRateTenths is a Time row's net over income in tenths of a percent.
	SavingsRateTenths *int `json:"savings_rate_tenths,omitempty"`
	// NetRatio is a Time row's |Net| over the largest |Net| of the whole result, in basis points
	// and signed like Net. It does not depend on the returned window.
	NetRatio *int `json:"net_ratio,omitempty" minimum:"-10000" maximum:"10000"`
}

// Statistics contains exact totals for one currency and scale partition.
type Statistics struct {
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
	Count    int    `json:"count"`
	In       Money  `json:"in"`
	Out      Money  `json:"out"`
	Net      Money  `json:"net"`
}

// ChartMark contains the only numeric geometry value consumed by chart code.
type ChartMark struct {
	Index     int    `json:"index"`
	Identity  string `json:"identity"`
	Label     string `json:"label"`
	Amount    Money  `json:"amount"`
	PlotRatio int    `json:"plot_ratio" minimum:"-10000" maximum:"10000"`
}

// CurrencyScope names one currency and the scale of its minor units.
type CurrencyScope struct {
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
}

// CurrencyCount describes one currency in a profile with its transaction count and date span.
type CurrencyCount struct {
	Currency string `json:"currency"`
	Scale    uint8  `json:"scale"`
	Count    int    `json:"count"`
	First    string `json:"first" format:"date"`
	Latest   string `json:"latest" format:"date"`
}

// Chart contains either aggregate marks or detail summaries.
type Chart struct {
	Marks   []ChartMark  `json:"marks,omitempty"`
	Summary []Statistics `json:"summary,omitempty"`
}

// Breadcrumb contains one server-derived label.
type Breadcrumb struct {
	Dimension string `json:"dimension"`
	Label     string `json:"label"`
}

// ActiveDateRange is an inclusive ISO date range.
type ActiveDateRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ActiveFilters contains active independent predicates.
type ActiveFilters struct {
	DateRange     *ActiveDateRange `json:"date_range,omitempty"`
	ShowHidden    bool             `json:"show_hidden"`
	ShowTransfers bool             `json:"show_transfers"`
}

// Capability describes one available renderer-neutral action.
type Capability struct {
	ID          app.ActionID `json:"id"`
	KeyDisplay  string       `json:"key_display,omitempty"`
	Description string       `json:"description"`
	Category    string       `json:"category"`
	Available   bool         `json:"available"`
	Reason      string       `json:"reason,omitempty"`
}

// Warning is a safe announced non-fatal condition.
type Warning struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// ReturnedWindow describes the actual range in a projection.
type ReturnedWindow struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
	Count  int `json:"count"`
}

// ViewMetadata contains server-derived analytical labels and sort state.
type ViewMetadata struct {
	Mode            string `json:"mode"`
	Grouping        string `json:"grouping"`
	TimeGranularity string `json:"time_granularity"`
	Search          string `json:"search,omitempty"`
	SortField       string `json:"sort_field"`
	SortDirection   string `json:"sort_direction"`
	SubGrouping     string `json:"sub_grouping,omitempty"`
	// Currency is the single currency the view is scoped to; absent when it shows all.
	Currency *CurrencyScope `json:"currency,omitempty"`
}

// Projection is the strict browser-facing view contract.
type Projection struct {
	APISchemaVersion        string          `json:"api_schema_version"`
	ProjectionSchemaVersion string          `json:"projection_schema_version"`
	Revision                string          `json:"revision" pattern:"^[0-9]+$"`
	ProfileKind             string          `json:"profile_kind" enum:"monarch,ynab,simplefin,amazon,local,csv"`
	AmazonSettings          *AmazonSettings `json:"amazon_settings,omitempty"`
	Pending                 PendingSummary  `json:"pending"`
	CanonicalQuery          string          `json:"canonical_query"`
	Selection               string          `json:"selection"`
	SelectionCount          int             `json:"selection_count"`
	SelectionSummary        []Statistics    `json:"selection_summary,omitempty" nullable:"false"`
	View                    ViewMetadata    `json:"view"`
	Breadcrumbs             []Breadcrumb    `json:"breadcrumbs"`
	BreadcrumbText          string          `json:"breadcrumb_text"`
	Filters                 ActiveFilters   `json:"filters"`
	Capabilities            []Capability    `json:"capabilities"`
	TotalRows               int             `json:"total_rows"`
	Window                  ReturnedWindow  `json:"window"`
	DetailRows              []DetailRow     `json:"detail_rows,omitempty"`
	AmazonMatchColumn       bool            `json:"amazon_match_column"`
	AggregateRows           []AggregateRow  `json:"aggregate_rows,omitempty"`
	Statistics              []Statistics    `json:"statistics"`
	Currencies              []CurrencyCount `json:"currencies" nullable:"false"`
	Chart                   Chart           `json:"chart"`
	Status                  string          `json:"status,omitempty"`
	Warnings                []Warning       `json:"warnings,omitempty"`
}

// AmazonSettings carries immutable money parsing settings for repeat imports.
type AmazonSettings struct {
	Currency string `json:"currency" minLength:"3" maxLength:"3"`
	Scale    uint8  `json:"scale" maximum:"9"`
}

func projectionToWire(
	projection app.WebProjection,
	canonical string,
	warnings []Warning,
) Projection {
	wire := Projection{
		APISchemaVersion: APISchemaVersion, ProjectionSchemaVersion: ProjectionSchemaVersion,
		Revision: strconv.FormatUint(projection.Revision, 10), ProfileKind: projection.ProfileKind,
		Pending:        pendingToWire(projection.Pending),
		CanonicalQuery: canonical, Selection: string(projection.Selection),
		SelectionCount: projection.SelectionCount,
		View:           viewMetadataToWire(projection.State.Current),
		BreadcrumbText: projection.BreadcrumbText, Filters: filtersToWire(projection.Filters),
		TotalRows: projection.TotalRows,
		Window: ReturnedWindow{
			Offset: projection.Window.Offset, Limit: projection.Window.Limit,
			Count: projection.Window.Count,
		},
		Status: projection.Status, Warnings: append([]Warning(nil), warnings...),
		AmazonMatchColumn: projection.AmazonMatchColumn,
	}
	if projection.AuditWarning != "" {
		wire.Warnings = append(wire.Warnings, Warning{Code: "audit_completion_failed", Detail: projection.AuditWarning})
	}
	if projection.AmazonSettings != nil {
		wire.AmazonSettings = &AmazonSettings{
			Currency: string(projection.AmazonSettings.Currency), Scale: projection.AmazonSettings.Scale,
		}
	}
	for _, breadcrumb := range projection.Breadcrumbs {
		wire.Breadcrumbs = append(wire.Breadcrumbs, Breadcrumb{
			Dimension: string(breadcrumb.Dimension), Label: breadcrumb.Label,
		})
	}
	wire.Capabilities = capabilitiesToWire(projection)
	wire.Currencies = currenciesToWire(projection.Currencies)
	for _, stats := range projection.SelectionSummary {
		wire.SelectionSummary = append(wire.SelectionSummary, statisticsToWire(stats))
	}
	appendRowsToWire(&wire, projection)
	return wire
}

func appendRowsToWire(wire *Projection, projection app.WebProjection) {
	for _, row := range projection.DetailRows {
		wire.DetailRows = append(wire.DetailRows, detailRowToWire(row))
	}
	for _, row := range projection.AggregateRows {
		wire.AggregateRows = append(wire.AggregateRows, aggregateRowToWire(row))
	}
	for _, stats := range projection.Statistics {
		wire.Statistics = append(wire.Statistics, statisticsToWire(stats))
	}
	for _, mark := range projection.Chart.Marks {
		wire.Chart.Marks = append(wire.Chart.Marks, ChartMark{
			Index: mark.Index, Identity: mark.Identity, Label: mark.Label,
			Amount: moneyToWire(mark.Amount), PlotRatio: mark.PlotRatio,
		})
	}
	for _, stats := range projection.Chart.Summary {
		wire.Chart.Summary = append(wire.Chart.Summary, statisticsToWire(stats))
	}
}

func viewMetadataToWire(state app.AnalyticalState) ViewMetadata {
	view := ViewMetadata{
		Mode:            string(state.Mode),
		Grouping:        string(state.Dimension),
		TimeGranularity: string(state.TimeGranularity),
		Search:          state.Search,
		SortField:       string(state.Sort.Field),
		SortDirection:   string(state.Sort.Direction),
	}
	if state.SubGrouping != nil {
		view.SubGrouping = string(*state.SubGrouping)
	}
	if state.Currency != nil {
		scope := currencyScopeToWire(*state.Currency)
		view.Currency = &scope
	}
	return view
}

func currencyScopeToWire(scope domain.CurrencyScope) CurrencyScope {
	return CurrencyScope{Currency: string(scope.Currency), Scale: scope.Scale}
}

func currenciesToWire(currencies []domain.CurrencyCount) []CurrencyCount {
	wire := make([]CurrencyCount, 0, len(currencies))
	for _, currency := range currencies {
		wire = append(wire, CurrencyCount{
			Currency: string(currency.Currency), Scale: currency.Scale, Count: currency.Count,
			First: currency.First.String(), Latest: currency.Latest.String(),
		})
	}
	return wire
}

// capabilitiesToWire lists every action the web offers, with keyless ones carrying no key.
func capabilitiesToWire(projection app.WebProjection) []Capability {
	availableActions := make(map[app.ActionID]struct{}, len(projection.Actions))
	for _, actionID := range projection.Actions {
		availableActions[actionID] = struct{}{}
	}
	dynamicCapabilities := make(map[app.ActionID]app.Capability, len(projection.Capabilities))
	for _, capability := range projection.Capabilities {
		dynamicCapabilities[capability.Action] = capability
	}
	var capabilities []Capability
	for _, definition := range app.ReadOnlyActions() {
		if !definition.Web {
			continue
		}
		_, available := availableActions[definition.ID]
		reason := ""
		if dynamic, exists := dynamicCapabilities[definition.ID]; exists {
			available = dynamic.Available
			reason = dynamic.Reason
		}
		description := definition.Description
		if projection.ProfileKind == "amazon" && definition.ID == app.ActionRefreshProvider {
			description = "Import Amazon order history"
		}
		capabilities = append(capabilities, Capability{
			ID: definition.ID, KeyDisplay: definition.KeyDisplay,
			Description: description, Category: definition.Category,
			Available: available, Reason: reason,
		})
	}
	return capabilities
}

func moneyToWire(money domain.Money) Money {
	return Money{
		Minor: strconv.FormatInt(money.Minor, 10), Currency: string(money.Currency),
		Scale: money.Scale, Decimal: money.DecimalString(), Display: formatMoney(money),
	}
}

func detailRowToWire(row app.WebDetailRow) DetailRow {
	transaction := row.Row.Transaction
	wire := DetailRow{
		Index: row.Index, Identity: row.Identity, Date: transaction.Date.String(),
		Account: transaction.Account.Name, Merchant: transaction.Merchant.Name,
		Category: transaction.Category.Name, Group: transaction.Category.Group,
		Amount: moneyToWire(transaction.Amount), Flags: flagsToWire(row.Row.Flags),
	}
	if row.AmazonMatch != nil {
		wire.AmazonMatch = &AmazonMatchIndicator{
			Class: string(row.AmazonMatch.Class), Confidence: string(row.AmazonMatch.Confidence),
			FirstProduct: row.AmazonMatch.FirstProduct, TotalMatches: row.AmazonMatch.TotalMatches,
		}
	}
	if row.Staged != nil {
		wire.Staged = stagedToWire(*row.Staged)
	}
	return wire
}

func stagedToWire(staged app.StagedValues) *StagedValues {
	wire := StagedValues{Deleted: staged.Deleted}
	if staged.Hidden != nil {
		hidden := *staged.Hidden
		wire.Hidden = &hidden
	}
	if staged.Merchant != nil {
		merchant := staged.Merchant.Name
		wire.Merchant = &merchant
	}
	if staged.Category != nil {
		category, group := staged.Category.Name, staged.Category.Group
		wire.Category, wire.Group = &category, &group
	}
	return &wire
}

func aggregateRowToWire(row app.WebAggregateRow) AggregateRow {
	wire := AggregateRow{
		Index: row.Index, Identity: row.Identity, Dimension: string(row.Row.Dimension),
		Label: row.Row.Label, Group: row.Row.Group, Count: row.Row.Count,
		Total: moneyToWire(row.Row.Total), In: moneyToWire(row.Row.In), Out: moneyToWire(row.Row.Out),
		TopCategory: row.Row.TopCategory, TopCategoryPercent: row.Row.TopCategoryPercent,
		ShareTenths: row.Row.ShareTenths, Flags: flagsToWire(row.Row.Flags),
		SavingsRateTenths: row.SavingsRateTenths, NetRatio: row.NetRatio,
	}
	if row.Row.Period != nil {
		period := periodToWire(*row.Row.Period)
		wire.Period = &period
	}
	return wire
}

func statisticsToWire(stats domain.CurrencyStats) Statistics {
	return Statistics{
		Currency: string(stats.Currency), Scale: stats.Scale, Count: stats.Count,
		In: moneyToWire(stats.In), Out: moneyToWire(stats.Out), Net: moneyToWire(stats.Net),
	}
}

func filtersToWire(filters app.Filters) ActiveFilters {
	wire := ActiveFilters{
		ShowHidden: filters.ShowHidden, ShowTransfers: filters.ShowTransfers,
	}
	if filters.DateRange != nil {
		wire.DateRange = &ActiveDateRange{
			From: filters.DateRange.Start.String(), To: filters.DateRange.End.String(),
		}
	}
	return wire
}

func flagsToWire(flags domain.RowFlags) Flags {
	return Flags{Selected: flags.Selected, Hidden: flags.Hidden, Pending: flags.Pending}
}

func formatMoney(money domain.Money) string {
	decimal := money.DecimalString()
	sign := "+"
	if strings.HasPrefix(decimal, "-") {
		sign = "-"
		decimal = strings.TrimPrefix(decimal, "-")
	}
	parts := strings.SplitN(decimal, ".", 2)
	integer := parts[0]
	for index := len(integer) - 3; index > 0; index -= 3 {
		integer = integer[:index] + "," + integer[index:]
	}
	if len(parts) == 1 {
		return sign + integer
	}
	return fmt.Sprintf("%s%s.%s", sign, integer, parts[1])
}
