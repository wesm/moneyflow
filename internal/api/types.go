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

// TransitionBody asks the server to apply exactly one renderer-neutral action.
type TransitionBody struct {
	Query     string         `json:"query" maxLength:"65536"`
	Selection string         `json:"selection,omitempty" maxLength:"1468006"`
	Action    app.ActionID   `json:"action"`
	Target    *app.RowTarget `json:"target,omitempty"`
	Search    *string        `json:"search,omitempty" maxLength:"2048"`
	Filters   *app.Filters   `json:"filters,omitempty"`
	Window    Window         `json:"window"`
}

// Flags contains the read-only row state exposed to the browser.
type Flags struct {
	Selected bool `json:"selected"`
	Hidden   bool `json:"hidden"`
	Pending  bool `json:"pending"`
}

// DetailRow contains only fields rendered by the read-only interface.
type DetailRow struct {
	Index    int    `json:"index"`
	Identity string `json:"identity"`
	Date     string `json:"date"`
	Account  string `json:"account"`
	Merchant string `json:"merchant"`
	Category string `json:"category"`
	Group    string `json:"group"`
	Amount   Money  `json:"amount"`
	Flags    Flags  `json:"flags"`
}

// Period is a typed time label with no locale-dependent parsing.
type Period struct {
	Granularity string `json:"granularity"`
	Year        int    `json:"year"`
	Month       int    `json:"month,omitempty"`
	Day         int    `json:"day,omitempty"`
}

// AggregateRow contains one stable aggregate partition.
type AggregateRow struct {
	Index              int     `json:"index"`
	Identity           string  `json:"identity"`
	Dimension          string  `json:"dimension"`
	Label              string  `json:"label"`
	Count              int     `json:"count"`
	Total              Money   `json:"total"`
	Period             *Period `json:"period,omitempty"`
	TopCategory        string  `json:"top_category,omitempty"`
	TopCategoryPercent int     `json:"top_category_percent,omitempty"`
	ShareTenths        int     `json:"share_tenths"`
	Flags              Flags   `json:"flags"`
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

// Projection is the strict browser-facing view contract.
type Projection struct {
	APISchemaVersion        string         `json:"api_schema_version"`
	ProjectionSchemaVersion string         `json:"projection_schema_version"`
	CanonicalQuery          string         `json:"canonical_query"`
	Selection               string         `json:"selection"`
	Breadcrumbs             []Breadcrumb   `json:"breadcrumbs"`
	BreadcrumbText          string         `json:"breadcrumb_text"`
	Filters                 ActiveFilters  `json:"filters"`
	Capabilities            []Capability   `json:"capabilities"`
	TotalRows               int            `json:"total_rows"`
	Window                  ReturnedWindow `json:"window"`
	DetailRows              []DetailRow    `json:"detail_rows,omitempty"`
	AggregateRows           []AggregateRow `json:"aggregate_rows,omitempty"`
	Statistics              []Statistics   `json:"statistics"`
	Chart                   Chart          `json:"chart"`
	Status                  string         `json:"status,omitempty"`
	Warnings                []Warning      `json:"warnings,omitempty"`
}

func projectionToWire(
	projection app.WebProjection,
	canonical string,
	warnings []Warning,
) Projection {
	wire := Projection{
		APISchemaVersion: APISchemaVersion, ProjectionSchemaVersion: ProjectionSchemaVersion,
		CanonicalQuery: canonical, Selection: string(projection.Selection),
		BreadcrumbText: projection.BreadcrumbText, Filters: filtersToWire(projection.Filters),
		TotalRows: projection.TotalRows,
		Window: ReturnedWindow{
			Offset: projection.Window.Offset, Limit: projection.Window.Limit,
			Count: projection.Window.Count,
		},
		Status: projection.Status, Warnings: append([]Warning(nil), warnings...),
	}
	for _, breadcrumb := range projection.Breadcrumbs {
		wire.Breadcrumbs = append(wire.Breadcrumbs, Breadcrumb{
			Dimension: string(breadcrumb.Dimension), Label: breadcrumb.Label,
		})
	}
	for _, actionID := range projection.Actions {
		definition, ok := app.ActionByID(actionID)
		if ok {
			wire.Capabilities = append(wire.Capabilities, Capability{
				ID: actionID, KeyDisplay: definition.KeyDisplay,
				Description: definition.Description,
			})
		}
	}
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
	return wire
}

func moneyToWire(money domain.Money) Money {
	return Money{
		Minor: strconv.FormatInt(money.Minor, 10), Currency: string(money.Currency),
		Scale: money.Scale, Decimal: money.DecimalString(), Display: formatMoney(money),
	}
}

func detailRowToWire(row app.WebDetailRow) DetailRow {
	transaction := row.Row.Transaction
	return DetailRow{
		Index: row.Index, Identity: row.Identity, Date: transaction.Date.String(),
		Account: transaction.Account.Name, Merchant: transaction.Merchant.Name,
		Category: transaction.Category.Name, Group: transaction.Category.Group,
		Amount: moneyToWire(transaction.Amount), Flags: flagsToWire(row.Row.Flags),
	}
}

func aggregateRowToWire(row app.WebAggregateRow) AggregateRow {
	wire := AggregateRow{
		Index: row.Index, Identity: row.Identity, Dimension: string(row.Row.Dimension),
		Label: row.Row.Label, Count: row.Row.Count, Total: moneyToWire(row.Row.Total),
		TopCategory: row.Row.TopCategory, TopCategoryPercent: row.Row.TopCategoryPercent,
		ShareTenths: row.Row.ShareTenths, Flags: flagsToWire(row.Row.Flags),
	}
	if row.Row.Period != nil {
		wire.Period = &Period{
			Granularity: string(row.Row.Period.Granularity), Year: row.Row.Period.Year,
			Month: row.Row.Period.Month, Day: row.Row.Period.Day,
		}
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
