package tui_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/parity"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/tui"
)

type visualScenario struct {
	name             string
	scenario         parity.FrameScenario
	theme            tui.ThemeName
	colorMode        tui.ColorMode
	durable          bool
	onboardingScreen string
	allStatuses      bool
}

func TestVisualGoldens(t *testing.T) {
	root := filepath.Join("..", "..")
	document, err := parity.LoadFrameScenarios(filepath.Join(root, "testdata", "parity", "frame_scenarios.json"))
	require.NoError(t, err)
	transactions, err := fixture.Load(filepath.Join(root, document.Fixture))
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
			var frame tui.Frame
			if visual.onboardingScreen != "" {
				screen, renderErr := tui.RenderOnboardingPreviewForTest(
					visual.onboardingScreen,
					visual.scenario.Width,
					visual.scenario.Height,
					visual.allStatuses,
				)
				require.NoError(t, renderErr)
				frame = screen.Frame
			} else {
				service := visualGoldenService(t, transactions, visual.durable)
				session, sessionErr := parity.SessionFromFrameInitial(visual.scenario.Initial)
				require.NoError(t, sessionErr)
				model, modelErr := tui.NewModel(context.Background(), service, session, tui.Options{
					Theme: visual.theme, ColorMode: visual.colorMode, Version: "v2-test",
					Now: func() time.Time {
						return time.Date(2026, time.August, 18, 9, 41, 0, 0, time.Local)
					},
				})
				require.NoError(t, modelErr)
				model = updateModel(t, model, tea.WindowSizeMsg{
					Width: visual.scenario.Width, Height: visual.scenario.Height,
				})
				for _, keyName := range visual.scenario.Keys {
					if keyName == "external_undo" {
						_, mutationErr := service.Undo(context.Background(), service.Revision())
						require.NoError(t, mutationErr)
						continue
					}
					model = updateModel(t, model, semanticKey(keyName))
				}
				frame = model.RenderScreen().Frame
			}
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
	result := make([]visualScenario, 0, len(document.Scenarios)+len(tui.ThemeNames())+14)
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
	result = append(result, goOnlyEditingScenarios(merchant)...)
	result = append(result, onboardingVisualScenarios()...)
	return result
}

func onboardingVisualScenarios() []visualScenario {
	result := make([]visualScenario, 0, 6)
	for _, screen := range []string{
		"account_selector", "provider_selector", "credential_setup", "credential_unlock",
	} {
		result = append(result, visualScenario{
			name:             screen,
			scenario:         parity.FrameScenario{Width: 100, Height: 30},
			onboardingScreen: screen,
		})
	}
	result = append(result,
		visualScenario{
			name:             "account_selector_minimum",
			scenario:         parity.FrameScenario{Width: 80, Height: 24},
			onboardingScreen: "account_selector",
		},
		visualScenario{
			name:             "account_selector_statuses",
			scenario:         parity.FrameScenario{Width: 100, Height: 30},
			onboardingScreen: "account_selector", allStatuses: true,
		},
	)
	return result
}

func goOnlyEditingScenarios(initial parity.FrameScenario) []visualScenario {
	definitions := []struct {
		name string
		keys []string
	}{
		{"category_manager", []string{"C"}},
		{"group_manager", []string{"G"}},
		{"transaction_info", []string{"d", "i"}},
		{"transaction_info_aggregate", []string{"i"}},
		{"redo_pending", []string{"h", "u"}},
		{"active_inactive_review", []string{"h", "down", "h", "u", "w"}},
		{"commit_redo_warning", []string{"h", "down", "h", "u", "w", "c"}},
		{"durable_pending_quit", []string{"h", "q"}},
		{"stale_review_conflict", []string{"h", "w", "c", "external_undo", "enter"}},
		{"provider_refresh_unbound", []string{"r"}},
	}
	result := make([]visualScenario, 0, len(definitions))
	for _, definition := range definitions {
		scenario := initial
		scenario.Name = definition.name
		scenario.Keys = append([]string(nil), definition.keys...)
		result = append(result, visualScenario{
			name: definition.name, scenario: scenario, theme: tui.ThemeDefault,
			colorMode: tui.ColorModeTrueColor, durable: true,
		})
	}
	return result
}

func visualGoldenService(
	t testing.TB,
	transactions []domain.Transaction,
	durable bool,
) *app.Service {
	t.Helper()
	if !durable {
		service, err := app.NewService(transactions)
		require.NoError(t, err)
		return service
	}
	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	return service
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
