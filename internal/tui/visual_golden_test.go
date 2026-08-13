package tui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/parity"
	"github.com/wesm/moneyflow/internal/tui"
)

type visualScenario struct {
	name      string
	scenario  parity.FrameScenario
	theme     tui.ThemeName
	colorMode tui.ColorMode
}

func TestVisualGoldens(t *testing.T) {
	root := filepath.Join("..", "..")
	document, err := parity.LoadFrameScenarios(filepath.Join(root, "testdata", "parity", "frame_scenarios.json"))
	require.NoError(t, err)
	transactions, err := fixture.Load(filepath.Join(root, document.Fixture))
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	artifactDirectory := filepath.Join(root, "testdata", "parity", "go_frames")
	update := os.Getenv("MONEYFLOW_UPDATE_GO_FRAMES") == "1"
	previewDirectory := os.Getenv("MONEYFLOW_GO_FRAME_PREVIEW_DIR")
	scenarios := visualGoldenScenarios(document)

	if update {
		require.NoError(t, os.MkdirAll(artifactDirectory, 0o750))
		removeStaleVisualArtifacts(t, artifactDirectory, scenarios)
	}
	if previewDirectory != "" {
		require.NoError(t, os.MkdirAll(previewDirectory, 0o750)) //nolint:gosec // explicit preview path.
	}

	for _, visual := range scenarios {
		t.Run(visual.name, func(t *testing.T) {
			session, sessionErr := parity.SessionFromFrameInitial(visual.scenario.Initial)
			require.NoError(t, sessionErr)
			model, modelErr := tui.NewModel(service, session, tui.Options{
				Theme: visual.theme, ColorMode: visual.colorMode,
			})
			require.NoError(t, modelErr)
			model = updateModel(t, model, tea.WindowSizeMsg{
				Width: visual.scenario.Width, Height: visual.scenario.Height,
			})
			for _, keyName := range visual.scenario.Keys {
				model = updateModel(t, model, semanticKey(keyName))
			}
			frame := model.RenderScreen().Frame
			artifact, encodeErr := parity.EncodeVisual(visual.name, frame)
			require.NoError(t, encodeErr)
			path := filepath.Join(artifactDirectory, visual.name+".json")
			if update {
				data, marshalErr := parity.MarshalVisual(artifact)
				require.NoError(t, marshalErr)
				require.NoError(t, os.WriteFile(path, data, 0o600))
			} else {
				committed, loadErr := parity.LoadVisual(path)
				require.NoError(t, loadErr, "visual artifact missing; use make parity-update-go deliberately")
				require.NoError(t, parity.CompareVisual(committed, frame))
			}
			if previewDirectory != "" {
				writeVisualPreviews(t, previewDirectory, visual.name, frame)
			}
		})
	}
}

func visualGoldenScenarios(document parity.FrameScenarioDocument) []visualScenario {
	result := make([]visualScenario, 0, len(document.Scenarios)+len(tui.ThemeNames()))
	var merchant parity.FrameScenario
	for _, scenario := range document.Scenarios {
		result = append(result, visualScenario{
			name: scenario.Name, scenario: scenario,
			theme: tui.ThemeName(scenario.Theme), colorMode: tui.ColorModeTrueColor,
		})
		if scenario.Name == "merchant" {
			merchant = scenario
		}
	}
	for _, theme := range tui.ThemeNames() {
		if theme == tui.ThemeDefault {
			continue
		}
		result = append(result, visualScenario{
			name: "merchant_theme_" + string(theme), scenario: merchant,
			theme: theme, colorMode: tui.ColorModeTrueColor,
		})
	}
	result = append(result, visualScenario{
		name: "merchant_no_color", scenario: merchant,
		theme: tui.ThemeDefault, colorMode: tui.ColorModeNone,
	})
	return result
}

func removeStaleVisualArtifacts(t testing.TB, directory string, scenarios []visualScenario) {
	t.Helper()
	expected := make(map[string]struct{}, len(scenarios))
	for _, scenario := range scenarios {
		expected[scenario.name+".json"] = struct{}{}
	}
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		if _, ok := expected[entry.Name()]; !ok {
			require.NoError(t, os.Remove(filepath.Join(directory, entry.Name())))
		}
	}
}

func writeVisualPreviews(t testing.TB, directory string, name string, frame tui.Frame) {
	t.Helper()
	plain := []byte(fmt.Sprintf("%s\n", joinLines(frame.PlainLines())))
	require.NoError(t, os.WriteFile( //nolint:gosec // explicit test-only preview directory.
		filepath.Join(directory, name+".txt"), plain, 0o600,
	))
	require.NoError(t, os.WriteFile( //nolint:gosec // explicit test-only preview directory.
		filepath.Join(directory, name+".ansi"),
		[]byte(frame.RenderANSI()+"\n"), 0o600,
	))
}

func joinLines(lines []string) string {
	result := ""
	for index, line := range lines {
		if index > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
