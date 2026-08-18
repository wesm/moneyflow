package parity

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const onboardingSchemaVersion = 1

// OnboardingScenarioDocument names deterministic Python onboarding screens.
type OnboardingScenarioDocument struct {
	SchemaVersion int                  `json:"schema_version"`
	Scenarios     []OnboardingScenario `json:"scenarios"`
}

// OnboardingScenario identifies one bounded screen and key sequence.
type OnboardingScenario struct {
	Name   string   `json:"name"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Screen string   `json:"screen"`
	Keys   []string `json:"keys"`
}

// OnboardingSemanticFrame contains only style-free, credential-blind screen semantics.
type OnboardingSemanticFrame struct {
	SchemaVersion int      `json:"schema_version"`
	Name          string   `json:"name"`
	Width         int      `json:"width"`
	Height        int      `json:"height"`
	Lines         []string `json:"lines"`
	Focus         string   `json:"focus"`
	Fields        []string `json:"fields"`
	Hints         []string `json:"hints"`
}

// LoadOnboardingScenarios loads one strict scenario document.
func LoadOnboardingScenarios(path string) (OnboardingScenarioDocument, error) {
	file, err := os.Open(path) //nolint:gosec // caller selects a committed fixture path.
	if err != nil {
		return OnboardingScenarioDocument{}, fmt.Errorf("load onboarding scenarios: %w", err)
	}
	defer func() { _ = file.Close() }()
	return DecodeOnboardingScenarios(file)
}

// DecodeOnboardingScenarios rejects unknown fields, duplicate names, and unbounded screens.
func DecodeOnboardingScenarios(reader io.Reader) (OnboardingScenarioDocument, error) {
	var document OnboardingScenarioDocument
	if err := decodeOnboardingStrict(reader, &document); err != nil {
		return OnboardingScenarioDocument{}, fmt.Errorf("decode onboarding scenarios: %w", err)
	}
	if document.SchemaVersion != onboardingSchemaVersion || len(document.Scenarios) == 0 {
		return OnboardingScenarioDocument{}, errors.New("decode onboarding scenarios: unsupported or empty document")
	}
	seen := make(map[string]struct{}, len(document.Scenarios))
	for index, scenario := range document.Scenarios {
		if scenario.Name == "" || scenario.Width < 80 || scenario.Width > 240 ||
			scenario.Height < 24 || scenario.Height > 100 || scenario.Keys == nil ||
			!validOnboardingScreen(scenario.Screen) {
			return OnboardingScenarioDocument{}, fmt.Errorf("decode onboarding scenarios: scenarios[%d] is invalid", index)
		}
		if _, exists := seen[scenario.Name]; exists {
			return OnboardingScenarioDocument{}, fmt.Errorf("decode onboarding scenarios: duplicate name %q", scenario.Name)
		}
		seen[scenario.Name] = struct{}{}
		for _, key := range scenario.Keys {
			if key == "" {
				return OnboardingScenarioDocument{}, fmt.Errorf("decode onboarding scenarios: scenarios[%d] has empty key", index)
			}
		}
	}
	return document, nil
}

// LoadOnboardingSemanticFrame loads one committed credential-blind artifact.
func LoadOnboardingSemanticFrame(path string) (OnboardingSemanticFrame, error) {
	file, err := os.Open(path) //nolint:gosec // caller selects a committed fixture path.
	if err != nil {
		return OnboardingSemanticFrame{}, fmt.Errorf("load onboarding semantic frame: %w", err)
	}
	defer func() { _ = file.Close() }()
	return DecodeOnboardingSemanticFrame(file)
}

// DecodeOnboardingSemanticFrame rejects unknown fields and impossible geometry.
func DecodeOnboardingSemanticFrame(reader io.Reader) (OnboardingSemanticFrame, error) {
	var frame OnboardingSemanticFrame
	if err := decodeOnboardingStrict(reader, &frame); err != nil {
		return OnboardingSemanticFrame{}, fmt.Errorf("decode onboarding semantic frame: %w", err)
	}
	if err := frame.Validate(); err != nil {
		return OnboardingSemanticFrame{}, fmt.Errorf("decode onboarding semantic frame: %w", err)
	}
	return frame, nil
}

// Validate checks stable bounds without interpreting renderer styling.
func (frame OnboardingSemanticFrame) Validate() error {
	if frame.SchemaVersion != onboardingSchemaVersion || frame.Name == "" ||
		frame.Width < 80 || frame.Width > 240 || frame.Height < 24 || frame.Height > 100 {
		return errors.New("unsupported or incomplete document")
	}
	if frame.Lines == nil || frame.Fields == nil || frame.Hints == nil || len(frame.Lines) > frame.Height {
		return errors.New("semantic lists or geometry are invalid")
	}
	for _, field := range frame.Fields {
		if field == "" {
			return errors.New("field identity is empty")
		}
	}
	return nil
}

func validOnboardingScreen(screen string) bool {
	switch screen {
	case "account_selector", "provider_selector", "credential_setup", "credential_unlock":
		return true
	default:
		return false
	}
}

func decodeOnboardingStrict(reader io.Reader, output any) error {
	decoder := json.NewDecoder(reader)
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
