package api

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	// MaxEncodedViewQuery bounds both received and canonical bookmark queries.
	MaxEncodedViewQuery = 64 << 10
	// MaxSearchBytes bounds committed UTF-8 regular-expression search text.
	MaxSearchBytes = 2 << 10
	// MaxEntityKeyBytes bounds one decoded stable drill-down key.
	MaxEntityKeyBytes = 512
	// MaxDrilldowns is the number of unique supported analytical dimensions.
	MaxDrilldowns = 5
)

var scalarViewFields = map[string]struct{}{
	"v": {}, "mode": {}, "group": {}, "subgroup": {}, "time": {},
	"from": {}, "to": {}, "hidden": {}, "transfers": {}, "q": {},
	"search-at": {}, "sort": {},
}

// DecodeViewQuery parses untrusted bookmark state and returns its canonical encoding.
func DecodeViewQuery(raw string) (app.ViewState, string, error) {
	if strings.HasPrefix(raw, "?") {
		return app.ViewState{}, "", invalidView(errors.New("query includes a leading question mark"))
	}
	if len(raw) > MaxEncodedViewQuery {
		return app.ViewState{}, "", invalidView(errors.New("encoded query exceeds limit"))
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return app.ViewState{}, "", invalidView(fmt.Errorf("parse query: %w", err))
	}
	state, err := decodeTopLevel(values)
	if err != nil {
		return app.ViewState{}, "", invalidView(err)
	}
	canonical, err := encodeViewState(state)
	if err != nil {
		return app.ViewState{}, "", invalidView(err)
	}
	if len(canonical) > MaxEncodedViewQuery {
		return app.ViewState{}, "", invalidView(errors.New("canonical query exceeds limit"))
	}
	return state, canonical, nil
}

// EncodeViewQuery validates durable state and encodes one canonical bookmark query.
func EncodeViewQuery(state app.ViewState) (string, error) {
	if err := state.Validate(); err != nil {
		if errors.Is(err, app.ErrTooManyReturnFrames) {
			return "", newSafeError(
				CodeViewStateTooLarge,
				"The requested view is too large to bookmark.",
				err,
			)
		}
		return "", invalidView(err)
	}
	if err := validateViewBounds(state); err != nil {
		return "", newSafeError(
			CodeViewStateTooLarge,
			"The requested view is too large to bookmark.",
			err,
		)
	}
	encoded, err := encodeViewState(state)
	if err != nil {
		return "", invalidView(err)
	}
	if len(encoded) > MaxEncodedViewQuery {
		return "", newSafeError(
			CodeViewStateTooLarge,
			"The requested view is too large to bookmark.",
			errors.New("canonical query exceeds limit"),
		)
	}
	return encoded, nil
}

func decodeTopLevel(values url.Values) (app.ViewState, error) {
	if err := validateFields(values, true); err != nil {
		return app.ViewState{}, err
	}
	if version, ok := scalar(values, "v"); ok && version != "1" {
		return app.ViewState{}, errors.New("unsupported schema version")
	}
	current, err := decodeAnalytical(values)
	if err != nil {
		return app.ViewState{}, err
	}
	state := app.ViewState{Version: app.ViewStateSchemaVersion, Current: current}
	for index, encodedFrame := range values["return"] {
		kindText, frameQuery, ok := strings.Cut(encodedFrame, ":")
		if !ok || frameQuery == "" {
			return app.ViewState{}, fmt.Errorf("return frame %d is malformed", index)
		}
		kind := app.ReturnKind(kindText)
		if kind != app.ReturnNavigation && kind != app.ReturnSubgroup {
			return app.ViewState{}, fmt.Errorf("return frame %d has invalid kind", index)
		}
		frameValues, parseErr := url.ParseQuery(frameQuery)
		if parseErr != nil {
			return app.ViewState{}, fmt.Errorf("parse return frame %d: %w", index, parseErr)
		}
		if err := validateFields(frameValues, false); err != nil {
			return app.ViewState{}, fmt.Errorf("return frame %d: %w", index, err)
		}
		frameState, decodeErr := decodeAnalytical(frameValues)
		if decodeErr != nil {
			return app.ViewState{}, fmt.Errorf("return frame %d: %w", index, decodeErr)
		}
		state.Returns = append(state.Returns, app.ReturnFrame{Kind: kind, State: frameState})
	}
	if err := state.Validate(); err != nil {
		return app.ViewState{}, err
	}
	if err := validateViewBounds(state); err != nil {
		return app.ViewState{}, err
	}
	return state, nil
}

func decodeAnalytical(values url.Values) (app.AnalyticalState, error) {
	state := app.DefaultViewState().Current
	if value, ok := scalar(values, "mode"); ok {
		state.Mode = domain.ResultMode(value)
	}
	if value, ok := scalar(values, "group"); ok {
		state.Dimension = domain.Dimension(value)
	}
	if value, ok := scalar(values, "subgroup"); ok {
		dimension := domain.Dimension(value)
		state.SubGrouping = &dimension
	}
	if value, ok := scalar(values, "time"); ok {
		state.TimeGranularity = domain.TimeGranularity(value)
	}
	from, hasFrom := scalar(values, "from")
	to, hasTo := scalar(values, "to")
	if hasFrom != hasTo {
		return app.AnalyticalState{}, errors.New("date range requires both bounds")
	}
	if hasFrom {
		start, err := domain.ParseDate(from)
		if err != nil {
			return app.AnalyticalState{}, fmt.Errorf("parse start date: %w", err)
		}
		end, err := domain.ParseDate(to)
		if err != nil {
			return app.AnalyticalState{}, fmt.Errorf("parse end date: %w", err)
		}
		state.DateRange = &domain.DateRange{Start: start, End: end}
	}
	if value, ok := scalar(values, "hidden"); ok {
		parsed, err := parseFlag(value)
		if err != nil {
			return app.AnalyticalState{}, fmt.Errorf("parse hidden flag: %w", err)
		}
		state.ShowHidden = parsed
	}
	if value, ok := scalar(values, "transfers"); ok {
		parsed, err := parseFlag(value)
		if err != nil {
			return app.AnalyticalState{}, fmt.Errorf("parse transfers flag: %w", err)
		}
		state.ShowTransfers = parsed
	}
	if value, ok := scalar(values, "q"); ok {
		state.Search = value
	}
	if value, ok := scalar(values, "search-at"); ok {
		scope, err := parseNavigationScope(value)
		if err != nil {
			return app.AnalyticalState{}, err
		}
		state.SearchAnchor = &scope
	}
	for index, value := range values["drill"] {
		drilldown, err := parseDrilldown(value)
		if err != nil {
			return app.AnalyticalState{}, fmt.Errorf("parse drill-down %d: %w", index, err)
		}
		state.Drilldowns = append(state.Drilldowns, drilldown)
	}
	if value, ok := scalar(values, "sort"); ok {
		sort, err := parseSort(value)
		if err != nil {
			return app.AnalyticalState{}, err
		}
		state.Sort = sort
	} else {
		state.Sort = defaultSort(state)
	}
	return state, nil
}

func encodeViewState(state app.ViewState) (string, error) {
	values, err := encodeAnalytical(state.Current)
	if err != nil {
		return "", err
	}
	values.Set("v", strconv.Itoa(int(app.ViewStateSchemaVersion)))
	for _, frame := range state.Returns {
		frameValues, frameErr := encodeAnalytical(frame.State)
		if frameErr != nil {
			return "", frameErr
		}
		values.Add("return", string(frame.Kind)+":"+frameValues.Encode())
	}
	return values.Encode(), nil
}

func encodeAnalytical(state app.AnalyticalState) (url.Values, error) {
	values := url.Values{}
	if state.Mode != domain.ResultModeAggregate {
		values.Set("mode", string(state.Mode))
	}
	if state.Dimension != domain.DimensionMerchant {
		values.Set("group", string(state.Dimension))
	}
	if state.SubGrouping != nil {
		values.Set("subgroup", string(*state.SubGrouping))
	}
	if state.TimeGranularity != domain.TimeGranularityYear {
		values.Set("time", string(state.TimeGranularity))
	}
	if state.DateRange != nil {
		values.Set("from", state.DateRange.Start.String())
		values.Set("to", state.DateRange.End.String())
	}
	if !state.ShowHidden {
		values.Set("hidden", "0")
	}
	if state.ShowTransfers {
		values.Set("transfers", "1")
	}
	if state.Search != "" {
		values.Set("q", state.Search)
	}
	if state.SearchAnchor != nil {
		values.Set("search-at", formatNavigationScope(*state.SearchAnchor))
	}
	for _, drilldown := range state.Drilldowns {
		encoded, err := formatDrilldown(drilldown)
		if err != nil {
			return nil, err
		}
		values.Add("drill", encoded)
	}
	if state.Sort != defaultSort(state) {
		values.Set("sort", string(state.Sort.Field)+":"+string(state.Sort.Direction))
	}
	return values, nil
}

func validateFields(values url.Values, topLevel bool) error {
	for field, entries := range values {
		if field == "return" {
			if !topLevel {
				return errors.New("nested return frames are not allowed")
			}
			if len(entries) > app.MaxReturnFrames {
				return errors.New("too many return frames")
			}
			continue
		}
		if field == "drill" {
			if len(entries) > MaxDrilldowns {
				return errors.New("too many drill-downs")
			}
			continue
		}
		if _, ok := scalarViewFields[field]; !ok || (!topLevel && field == "v") {
			return fmt.Errorf("unknown field %q", field)
		}
		if len(entries) != 1 {
			return fmt.Errorf("field %q must appear once", field)
		}
	}
	return nil
}

func validateViewBounds(state app.ViewState) error {
	if len(state.Returns) > app.MaxReturnFrames {
		return errors.New("too many return frames")
	}
	if err := validateAnalyticalBounds(state.Current); err != nil {
		return err
	}
	for index, frame := range state.Returns {
		if err := validateAnalyticalBounds(frame.State); err != nil {
			return fmt.Errorf("return frame %d: %w", index, err)
		}
	}
	return nil
}

func validateAnalyticalBounds(state app.AnalyticalState) error {
	if !utf8.ValidString(state.Search) || len(state.Search) > MaxSearchBytes {
		return errors.New("search exceeds limit or is not UTF-8")
	}
	if len(state.Drilldowns) > MaxDrilldowns {
		return errors.New("too many drill-downs")
	}
	for _, drilldown := range state.Drilldowns {
		if !utf8.ValidString(drilldown.Key) || len(drilldown.Key) > MaxEntityKeyBytes {
			return errors.New("entity key exceeds limit or is not UTF-8")
		}
	}
	return nil
}

func scalar(values url.Values, field string) (string, bool) {
	entries, ok := values[field]
	if !ok || len(entries) == 0 {
		return "", false
	}
	return entries[0], true
}

func parseFlag(value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, errors.New("flag must be 0 or 1")
	}
}

func parseSort(value string) (domain.SortSpec, error) {
	field, direction, ok := strings.Cut(value, ":")
	if !ok || strings.Contains(direction, ":") {
		return domain.SortSpec{}, errors.New("sort is malformed")
	}
	return domain.SortSpec{Field: domain.SortField(field), Direction: domain.SortDirection(direction)}, nil
}

func defaultSort(state app.AnalyticalState) domain.SortSpec {
	if state.SubGrouping != nil {
		if *state.SubGrouping == domain.DimensionTime {
			return domain.SortSpec{Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc}
		}
		return domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}
	}
	if state.Mode == domain.ResultModeDetail {
		return domain.SortSpec{Field: domain.SortFieldDate, Direction: domain.SortDirectionDesc}
	}
	if state.Dimension == domain.DimensionTime {
		return domain.SortSpec{Field: domain.SortFieldTimePeriod, Direction: domain.SortDirectionAsc}
	}
	return domain.SortSpec{Field: domain.SortFieldAmount, Direction: domain.SortDirectionDesc}
}

func parseDrilldown(value string) (domain.Drilldown, error) {
	dimensionText, target, ok := strings.Cut(value, ":")
	if !ok || target == "" {
		return domain.Drilldown{}, errors.New("drill-down is malformed")
	}
	dimension := domain.Dimension(dimensionText)
	if dimension == domain.DimensionTime {
		period, err := parsePeriod(target)
		if err != nil {
			return domain.Drilldown{}, err
		}
		return domain.Drilldown{Dimension: dimension, Period: &period}, nil
	}
	return domain.Drilldown{Dimension: dimension, Key: target}, nil
}

func formatDrilldown(drilldown domain.Drilldown) (string, error) {
	if drilldown.Dimension == domain.DimensionTime {
		if drilldown.Period == nil {
			return "", errors.New("time drill-down has no period")
		}
		return "time:" + formatPeriod(*drilldown.Period), nil
	}
	return string(drilldown.Dimension) + ":" + drilldown.Key, nil
}

func parsePeriod(value string) (domain.Period, error) {
	granularityText, dateText, ok := strings.Cut(value, ":")
	if !ok || strings.Contains(dateText, ":") {
		return domain.Period{}, errors.New("time period is malformed")
	}
	parts := strings.Split(dateText, "-")
	period := domain.Period{Granularity: domain.TimeGranularity(granularityText)}
	var err error
	if len(parts) > 0 {
		period.Year, err = strconv.Atoi(parts[0])
	}
	if err == nil && len(parts) > 1 {
		period.Month, err = strconv.Atoi(parts[1])
	}
	if err == nil && len(parts) > 2 {
		period.Day, err = strconv.Atoi(parts[2])
	}
	if err != nil || len(parts) == 0 || len(parts) > 3 || period.Validate() != nil || formatPeriod(period) != value {
		return domain.Period{}, errors.New("time period is invalid")
	}
	return period, nil
}

func formatPeriod(period domain.Period) string {
	switch period.Granularity {
	case domain.TimeGranularityYear:
		return fmt.Sprintf("year:%04d", period.Year)
	case domain.TimeGranularityMonth:
		return fmt.Sprintf("month:%04d-%02d", period.Year, period.Month)
	default:
		return fmt.Sprintf("day:%04d-%02d-%02d", period.Year, period.Month, period.Day)
	}
}

func parseNavigationScope(value string) (app.NavigationScope, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 4 {
		return app.NavigationScope{}, errors.New("search anchor is malformed")
	}
	scope := app.NavigationScope{
		Mode: domain.ResultMode(parts[0]), Dimension: domain.Dimension(parts[1]),
	}
	if parts[2] != "_" {
		dimension := domain.Dimension(parts[2])
		scope.SubGrouping = &dimension
	}
	depth, err := strconv.Atoi(parts[3])
	if err != nil {
		return app.NavigationScope{}, errors.New("search anchor depth is malformed")
	}
	scope.DrilldownSize = depth
	if formatNavigationScope(scope) != value {
		return app.NavigationScope{}, errors.New("search anchor is not canonical")
	}
	return scope, nil
}

func formatNavigationScope(scope app.NavigationScope) string {
	subgroup := "_"
	if scope.SubGrouping != nil {
		subgroup = string(*scope.SubGrouping)
	}
	return fmt.Sprintf(
		"%s:%s:%s:%d",
		scope.Mode,
		scope.Dimension,
		subgroup,
		scope.DrilldownSize,
	)
}

func invalidView(cause error) *SafeError {
	return newSafeError(CodeInvalidViewState, "The view URL is invalid.", cause)
}
