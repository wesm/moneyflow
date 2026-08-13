// Package parity decodes committed cross-implementation behavioral contracts.
package parity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const interactionSchemaVersion = 1

// InteractionDocument contains deterministic renderer-neutral scenarios.
type InteractionDocument struct {
	SchemaVersion int                   `json:"schema_version"`
	Scenarios     []InteractionScenario `json:"scenarios"`
}

// InteractionScenario starts from explicit state and applies ordered operations.
type InteractionScenario struct {
	Name    string            `json:"name"`
	Initial InteractionState  `json:"initial"`
	Steps   []InteractionStep `json:"steps"`
}

// InteractionState is the serializable visible portion of an app session.
type InteractionState struct {
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
	ResultIDs              []string               `json:"result_ids"`
	Breadcrumb             string                 `json:"breadcrumb"`
}

// RowIdentity resolves a visible row without depending on its index.
type RowIdentity struct {
	Kind      string           `json:"kind"`
	Dimension domain.Dimension `json:"dimension,omitempty"`
	Key       string           `json:"key"`
	Currency  domain.Currency  `json:"currency,omitempty"`
	Scale     uint8            `json:"scale,omitempty"`
}

// InteractionStep applies one named state transition and records its complete result.
type InteractionStep struct {
	Operation string            `json:"operation"`
	Target    *RowIdentity      `json:"target,omitempty"`
	Position  *app.ViewPosition `json:"position,omitempty"`
	Delta     *int              `json:"delta,omitempty"`
	Search    *string           `json:"search,omitempty"`
	Filters   *app.Filters      `json:"filters,omitempty"`
	Expected  *InteractionState `json:"expected"`
	Returned  *app.ViewPosition `json:"returned_position,omitempty"`
}

// LoadInteractionDocument loads a strict committed scenario file.
func LoadInteractionDocument(path string) (InteractionDocument, error) {
	data, err := os.ReadFile(path) //nolint:gosec // caller selects a fixture path.
	if err != nil {
		return InteractionDocument{}, fmt.Errorf("load interactions: %w", err)
	}
	return DecodeInteractionDocument(bytes.NewReader(data))
}

// DecodeInteractionDocument rejects unknown fields, trailing values, and ambiguous operations.
func DecodeInteractionDocument(reader io.Reader) (InteractionDocument, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var document InteractionDocument
	if err := decoder.Decode(&document); err != nil {
		return InteractionDocument{}, fmt.Errorf("decode interactions: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return InteractionDocument{}, errors.New("decode interactions: trailing JSON value")
	}
	if document.SchemaVersion != interactionSchemaVersion {
		return InteractionDocument{}, fmt.Errorf(
			"decode interactions: unsupported schema version %d", document.SchemaVersion,
		)
	}
	if len(document.Scenarios) == 0 {
		return InteractionDocument{}, errors.New("decode interactions: no scenarios")
	}
	for scenarioIndex, scenario := range document.Scenarios {
		if err := validateScenario(scenario); err != nil {
			return InteractionDocument{}, fmt.Errorf("decode interactions: scenarios[%d]: %w", scenarioIndex, err)
		}
	}
	return document, nil
}

func validateScenario(scenario InteractionScenario) error {
	if scenario.Name == "" || len(scenario.Steps) == 0 {
		return errors.New("name and steps are required")
	}
	if err := validateState(scenario.Initial, false); err != nil {
		return fmt.Errorf("initial: %w", err)
	}
	for index, step := range scenario.Steps {
		if !validOperation(step.Operation) {
			return fmt.Errorf("steps[%d]: unknown operation %q", index, step.Operation)
		}
		if step.Expected == nil {
			return fmt.Errorf("steps[%d]: expected state is required", index)
		}
		if err := validateState(*step.Expected, true); err != nil {
			return fmt.Errorf("steps[%d].expected: %w", index, err)
		}
		if operationNeedsTarget(step.Operation) && (step.Target == nil || step.Target.Key == "") {
			return fmt.Errorf("steps[%d]: operation %q requires a stable target", index, step.Operation)
		}
		if !operationNeedsTarget(step.Operation) && step.Target != nil {
			return fmt.Errorf("steps[%d]: operation %q does not accept a target", index, step.Operation)
		}
		if step.Target != nil && step.Target.Kind != "aggregate" && step.Target.Kind != "transaction" {
			return fmt.Errorf("steps[%d]: invalid target kind %q", index, step.Target.Kind)
		}
		if step.Operation == "drill" && step.Target.Kind != "aggregate" {
			return fmt.Errorf("steps[%d]: drill requires an aggregate target", index)
		}
		if step.Target != nil && step.Target.Kind == "aggregate" && !step.Target.Dimension.Valid() {
			return fmt.Errorf("steps[%d]: aggregate target requires a dimension", index)
		}
		if step.Target != nil && step.Target.Kind == "transaction" &&
			(step.Target.Dimension != "" || step.Target.Currency != "" || step.Target.Scale != 0) {
			return fmt.Errorf("steps[%d]: transaction target contains aggregate fields", index)
		}
		if step.Operation == "set_filters" && step.Filters == nil {
			return fmt.Errorf("steps[%d]: set_filters requires filters", index)
		}
		if step.Operation != "set_filters" && step.Filters != nil {
			return fmt.Errorf("steps[%d]: operation %q does not accept filters", index, step.Operation)
		}
		if step.Operation == "set_search" && step.Search == nil {
			return fmt.Errorf("steps[%d]: set_search requires search", index)
		}
		if step.Operation != "set_search" && step.Search != nil {
			return fmt.Errorf("steps[%d]: operation %q does not accept search", index, step.Operation)
		}
		if step.Operation == "navigate_period" && (step.Delta == nil || *step.Delta == 0) {
			return fmt.Errorf("steps[%d]: navigate_period requires nonzero delta", index)
		}
		if step.Operation != "navigate_period" && step.Delta != nil {
			return fmt.Errorf("steps[%d]: operation %q does not accept delta", index, step.Operation)
		}
		if step.Operation == "drill" && step.Position == nil {
			return fmt.Errorf("steps[%d]: drill requires position", index)
		}
		if step.Operation != "drill" && step.Position != nil {
			return fmt.Errorf("steps[%d]: operation %q does not accept position", index, step.Operation)
		}
		if step.Operation == "back" && step.Returned == nil {
			return fmt.Errorf("steps[%d]: back requires returned_position", index)
		}
		if step.Operation != "back" && step.Returned != nil {
			return fmt.Errorf("steps[%d]: operation %q does not accept returned_position", index, step.Operation)
		}
	}
	return nil
}

func validateState(state InteractionState, requireResults bool) error {
	query := domain.QuerySpec{
		DateRange:       state.DateRange,
		Search:          state.Search,
		ShowHidden:      state.ShowHidden,
		ShowTransfers:   state.ShowTransfers,
		Drilldowns:      state.Drilldowns,
		Mode:            state.Mode,
		GroupBy:         state.Dimension,
		TimeGranularity: state.TimeGranularity,
		Sort:            state.Sort,
	}
	if state.SubGrouping != nil {
		query.Mode = domain.ResultModeAggregate
		query.GroupBy = *state.SubGrouping
	}
	if err := query.Validate(); err != nil {
		return err
	}
	if state.Drilldowns == nil || state.SelectedTransactionIDs == nil || state.SelectedAggregateKeys == nil {
		return errors.New("drilldowns and selection lists are required")
	}
	if requireResults && state.ResultIDs == nil {
		return errors.New("result_ids is required")
	}
	return nil
}

func validOperation(operation string) bool {
	switch operation {
	case "cycle_grouping", "show_detail", "switch_accounts", "cycle_sort", "reverse_sort",
		"toggle_time_granularity", "drill", "back", "navigate_period", "clear_time_period",
		"cycle_subgroup", "set_search", "set_filters", "toggle_selection", "select_all":
		return true
	default:
		return false
	}
}

func operationNeedsTarget(operation string) bool {
	return operation == "drill" || operation == "toggle_selection"
}
