package parity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/tui"
)

const semanticSchemaVersion = 1

// FrameScenarioDocument names deterministic Python and Go terminal states.
type FrameScenarioDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Fixture       string          `json:"fixture"`
	Scenarios     []FrameScenario `json:"scenarios"`
}

// FrameScenario contains one initial session and terminal key sequence.
type FrameScenario struct {
	Name    string       `json:"name"`
	Width   int          `json:"width"`
	Height  int          `json:"height"`
	Theme   string       `json:"theme"`
	Fixture string       `json:"fixture,omitempty"`
	Initial FrameInitial `json:"initial"`
	Keys    []string     `json:"keys"`
}

// FrameInitial is the serializable renderer-neutral starting state.
type FrameInitial struct {
	Mode                   domain.ResultMode      `json:"mode"`
	Dimension              domain.Dimension       `json:"dimension"`
	SubGrouping            *domain.Dimension      `json:"sub_grouping,omitempty"`
	TimeGranularity        domain.TimeGranularity `json:"time_granularity"`
	Sort                   domain.SortSpec        `json:"sort"`
	Search                 string                 `json:"search,omitempty"`
	ShowHidden             bool                   `json:"show_hidden"`
	ShowTransfers          bool                   `json:"show_transfers"`
	DateRange              *domain.DateRange      `json:"date_range,omitempty"`
	Drilldowns             []domain.Drilldown     `json:"drilldowns"`
	SelectedTransactionIDs []string               `json:"selected_transaction_ids"`
	SelectedAggregateKeys  []string               `json:"selected_aggregate_keys"`
}

// SemanticFrame contains only Python-authoritative content and layout invariants.
type SemanticFrame struct {
	SchemaVersion int              `json:"schema_version"`
	Name          string           `json:"name"`
	Width         int              `json:"width"`
	Height        int              `json:"height"`
	Regions       []SemanticRegion `json:"regions"`
	Columns       []int            `json:"columns"`
	VisibleRowIDs []string         `json:"visible_row_ids"`
	Breadcrumb    string           `json:"breadcrumb"`
	Stats         string           `json:"stats"`
	Flags         []string         `json:"flags"`
	SelectionIDs  []string         `json:"selection_ids"`
	Hints         string           `json:"hints"`
	Overlay       []string         `json:"overlay"`
}

// SemanticRegion is a style-free rectangular crop.
type SemanticRegion struct {
	Name   string   `json:"name"`
	Origin Origin   `json:"origin"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Lines  []string `json:"lines"`
}

// Origin is an absolute terminal cell position.
type Origin struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// LoadFrameScenarios strictly loads committed frame scenarios.
func LoadFrameScenarios(path string) (FrameScenarioDocument, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller selects a committed fixture path.
	if err != nil {
		return FrameScenarioDocument{}, fmt.Errorf("load frame scenarios: %w", err)
	}
	var document FrameScenarioDocument
	if err := decodeStrict(data, &document); err != nil {
		return FrameScenarioDocument{}, fmt.Errorf("load frame scenarios: %w", err)
	}
	if document.SchemaVersion != semanticSchemaVersion || document.Fixture == "" || len(document.Scenarios) == 0 {
		return FrameScenarioDocument{}, errors.New("load frame scenarios: unsupported or empty document")
	}
	seen := make(map[string]struct{}, len(document.Scenarios))
	for index, scenario := range document.Scenarios {
		if scenario.Name == "" || scenario.Width < 1 || scenario.Height < 1 || scenario.Keys == nil {
			return FrameScenarioDocument{}, fmt.Errorf("load frame scenarios: scenarios[%d] is invalid", index)
		}
		if _, exists := seen[scenario.Name]; exists {
			return FrameScenarioDocument{}, fmt.Errorf("load frame scenarios: duplicate name %q", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
		if err := queryForInitial(scenario.Initial).Validate(); err != nil {
			return FrameScenarioDocument{}, fmt.Errorf("load frame scenarios: scenarios[%d]: %w", index, err)
		}
		if scenario.Initial.Drilldowns == nil || scenario.Initial.SelectedTransactionIDs == nil ||
			scenario.Initial.SelectedAggregateKeys == nil {
			return FrameScenarioDocument{}, fmt.Errorf("load frame scenarios: scenarios[%d]: lists are required", index)
		}
	}
	return document, nil
}

// LoadSemanticFrame strictly loads one committed Python semantic artifact.
func LoadSemanticFrame(path string) (SemanticFrame, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller selects a committed fixture path.
	if err != nil {
		return SemanticFrame{}, fmt.Errorf("load semantic frame: %w", err)
	}
	var frame SemanticFrame
	if err := decodeStrict(data, &frame); err != nil {
		return SemanticFrame{}, fmt.Errorf("load semantic frame: %w", err)
	}
	if err := frame.Validate(); err != nil {
		return SemanticFrame{}, fmt.Errorf("load semantic frame: %w", err)
	}
	return frame, nil
}

// Validate rejects malformed semantic frame geometry.
func (frame SemanticFrame) Validate() error {
	if frame.SchemaVersion != semanticSchemaVersion || frame.Name == "" || frame.Width < 1 || frame.Height < 1 {
		return errors.New("unsupported or incomplete document")
	}
	if frame.Regions == nil || frame.Columns == nil || frame.VisibleRowIDs == nil || frame.Flags == nil ||
		frame.SelectionIDs == nil || frame.Overlay == nil {
		return errors.New("all semantic lists are required")
	}
	for index, region := range frame.Regions {
		if region.Name == "" || region.Width < 0 || region.Height < 0 || len(region.Lines) != region.Height ||
			region.Origin.X < 0 || region.Origin.Y < 0 || region.Origin.X+region.Width > frame.Width ||
			region.Origin.Y+region.Height > frame.Height {
			return fmt.Errorf("region %d has invalid geometry", index)
		}
	}
	if len(frame.Flags) != len(frame.VisibleRowIDs) {
		return errors.New("flags must correspond to visible rows")
	}
	return nil
}

// ProjectSemantic crops style-free regions from the Go-owned rendered frame.
func ProjectSemantic(name string, screen tui.RenderedScreen) SemanticFrame {
	regions := make([]SemanticRegion, 0, 6)
	for _, overlayName := range []string{
		"search_semantic", "filter_semantic", "help_semantic", "export_semantic",
	} {
		if region, ok := namedRegion(screen.Regions, overlayName); ok {
			regions = append(regions, projectRegion(screen.Frame, "overlay", region.Rect))
			goto projected
		}
	}
	for _, wanted := range []string{"breadcrumb", "stats", "table_header", "table_body", "hints"} {
		if region, ok := namedRegion(screen.Regions, wanted); ok {
			if wanted == "table_body" && region.Rect.Height > len(screen.VisibleRowIDs) {
				region.Rect.Height = len(screen.VisibleRowIDs)
			}
			regions = append(regions, projectRegion(screen.Frame, wanted, region.Rect))
		}
	}

projected:
	selectionIDs := append([]string{}, screen.SelectionIDs...)
	sort.Strings(selectionIDs)
	return SemanticFrame{
		SchemaVersion: semanticSchemaVersion,
		Name:          name,
		Width:         screen.Frame.Width(),
		Height:        screen.Frame.Height(),
		Regions:       regions,
		Columns:       append([]int(nil), screen.Columns...),
		VisibleRowIDs: append([]string(nil), screen.VisibleRowIDs...),
		Breadcrumb:    screen.Breadcrumb,
		Stats:         screen.Stats,
		Flags:         append([]string(nil), screen.Flags...),
		SelectionIDs:  selectionIDs,
		Hints:         screen.Hints,
		Overlay:       append([]string{}, screen.Overlay...),
	}
}

// SessionFromFrameInitial creates the shared application state for a scenario.
func SessionFromFrameInitial(initial FrameInitial) (app.Session, error) {
	session := app.NewSession()
	session.Mode = initial.Mode
	session.Dimension = initial.Dimension
	session.SubGrouping = initial.SubGrouping
	session.TimeGranularity = initial.TimeGranularity
	session.Sort = initial.Sort
	session.Drilldowns = append([]domain.Drilldown(nil), initial.Drilldowns...)
	session.SelectedTransactionIDs = stringSet(initial.SelectedTransactionIDs)
	session.SelectedAggregateKeys = stringSet(initial.SelectedAggregateKeys)
	session.SetSearch(initial.Search)
	if err := session.SetFilters(app.Filters{
		DateRange: initial.DateRange, ShowHidden: initial.ShowHidden, ShowTransfers: initial.ShowTransfers,
	}); err != nil {
		return app.Session{}, err
	}
	return session, nil
}

func decodeStrict(data []byte, output any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON value")
	}
	return nil
}

func queryForInitial(initial FrameInitial) domain.QuerySpec {
	mode, groupBy := initial.Mode, initial.Dimension
	if initial.SubGrouping != nil {
		mode, groupBy = domain.ResultModeAggregate, *initial.SubGrouping
	}
	return domain.QuerySpec{
		DateRange: initial.DateRange, Search: initial.Search, ShowHidden: initial.ShowHidden,
		ShowTransfers: initial.ShowTransfers, Drilldowns: initial.Drilldowns, Mode: mode,
		GroupBy: groupBy, TimeGranularity: initial.TimeGranularity, Sort: initial.Sort,
	}
}

func namedRegion(regions []tui.NamedRegion, name string) (tui.NamedRegion, bool) {
	for _, region := range regions {
		if region.Name == name {
			return region, true
		}
	}
	return tui.NamedRegion{}, false
}

func projectRegion(frame tui.Frame, name string, rect tui.Rect) SemanticRegion {
	crop := frame.Crop(rect)
	lines := crop.PlainLines()
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return SemanticRegion{
		Name: name, Origin: Origin{X: rect.X, Y: rect.Y}, Width: rect.Width, Height: rect.Height,
		Lines: lines,
	}
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
