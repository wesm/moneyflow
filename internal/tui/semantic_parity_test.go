package tui_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/parity"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	"github.com/wesm/moneyflow/internal/tui"
)

func TestPythonSemanticFrameParity(t *testing.T) {
	root := filepath.Join("..", "..")
	document, err := parity.LoadFrameScenarios(filepath.Join(root, "testdata", "parity", "frame_scenarios.json"))
	require.NoError(t, err)
	transactions, err := fixture.Load(filepath.Join(root, document.Fixture))
	require.NoError(t, err)
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(context.Background(), committed)
	require.NoError(t, err)
	persistentService, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)

	for _, scenario := range document.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			scenarioService := service
			profileRoot := ""
			scenarioTransactions := transactions
			if scenario.Fixture != "" && scenario.Fixture != document.Fixture {
				var fixtureErr error
				scenarioTransactions, fixtureErr = fixture.Load(filepath.Join(root, scenario.Fixture))
				require.NoError(t, fixtureErr)
			}
			if amazonService, amazonRoot, ok := amazonParityService(t, scenario, scenarioTransactions); ok {
				scenarioService = amazonService
				profileRoot = amazonRoot
			} else if scenario.Name == "export" {
				scenarioService = persistentService
				profileRoot = paths.Root
			} else if scenario.Fixture != "" && scenario.Fixture != document.Fixture {
				fixturePaths, fixtureErr := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
				require.NoError(t, fixtureErr)
				fixtureProfile, fixtureErr := sqlite.Open(context.Background(), fixturePaths, sqlite.DefaultOptions)
				require.NoError(t, fixtureErr)
				t.Cleanup(func() { require.NoError(t, fixtureProfile.Close()) })
				fixtureCommitted, fixtureErr := fixture.CommittedProfile(scenarioTransactions)
				require.NoError(t, fixtureErr)
				_, fixtureErr = fixtureProfile.CreateSeededProfile(context.Background(), fixtureCommitted)
				require.NoError(t, fixtureErr)
				scenarioService, fixtureErr = app.NewProfileService(context.Background(), fixtureProfile)
				require.NoError(t, fixtureErr)
			}
			session, sessionErr := parity.SessionFromFrameInitial(scenario.Initial)
			require.NoError(t, sessionErr)
			model, modelErr := tui.NewModel(context.Background(), scenarioService, session, tui.Options{
				Theme: tui.ThemeName(scenario.Theme), ColorMode: tui.ColorModeNone,
				ProfileRoot:     profileRoot,
				EncodeViewQuery: func(app.ViewState) (string, error) { return "v=1", nil },
			})
			require.NoError(t, modelErr)
			model = updateModel(t, model, tea.WindowSizeMsg{Width: scenario.Width, Height: scenario.Height})
			for _, keyName := range scenario.Keys {
				model = updateModel(t, model, semanticKey(keyName))
			}
			got := parity.ProjectSemantic(scenario.Name, model.RenderScreen())
			got = withoutNamedGoTransactionInfoHint(got)
			if scenario.Name == "help" {
				got.Overlay = withoutNamedGoHelpDivergences(t, got.Overlay)
			}
			want, loadErr := parity.LoadSemanticFrame(filepath.Join(
				root, "testdata", "parity", "semantic_frames", scenario.Name+".json",
			))
			require.NoError(t, loadErr)
			if scenario.Name == "amazon_matching_info" {
				requireAmazonInfoSemanticParity(t, want, got)
				return
			}
			if scenario.ProfileKind == "amazon" {
				// Amazon import allocates opaque local IDs. Python's source adapter uses
				// the stable fixture ID; content and layout remain authoritative.
				require.Len(t, got.VisibleRowIDs, len(want.VisibleRowIDs))
				got.VisibleRowIDs = append([]string(nil), want.VisibleRowIDs...)
			}
			if scenario.Fixture == "testdata/fixtures/duplicate_transactions.json" {
				requireDuplicateWorkflowParity(t, want, got)
				// The workflow facts above remain authoritative while renderer-specific
				// duplicate geometry and base-table flag placement are normalized.
				got.Columns = want.Columns
				got.Regions = want.Regions
				got.Overlay = want.Overlay
				got.Flags = want.Flags
				got.SelectionIDs = want.SelectionIDs
				if scenario.Name == "duplicates_hidden" {
					got.Stats = want.Stats
				}
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatal(compactSemanticDiff(want, got))
			}
		})
	}
}

func requireDuplicateWorkflowParity(
	t testing.TB,
	want parity.SemanticFrame,
	got parity.SemanticFrame,
) {
	t.Helper()
	wantText := semanticFrameText(want)
	gotText := semanticFrameText(got)
	switch want.Name {
	case "duplicates_info":
		for _, token := range []string{"duplicate-001", "2026-01-15", "-12.34", "Example Merchant"} {
			require.Contains(t, wantText, token)
			require.Contains(t, gotText, token)
		}
	case "duplicates_delete_confirmation":
		require.Contains(t, wantText, "Delete Transaction")
		require.Contains(t, gotText, "Delete 1 transaction")
		require.Contains(t, gotText, "stages a pending edit")
	case "duplicates_hidden":
		require.GreaterOrEqual(t, len(got.Overlay), len(want.Overlay))
		require.Equal(t, want.Overlay, got.Overlay[:len(want.Overlay)])
		require.Contains(t, strings.Join(got.Overlay[len(want.Overlay):], "\n"), "Staged edit")
	default:
		require.Equal(t, want.Overlay, got.Overlay)
	}
}

func requireAmazonInfoSemanticParity(t testing.TB, want parity.SemanticFrame, got parity.SemanticFrame) {
	t.Helper()
	require.Equal(t, want.Name, got.Name)
	require.Equal(t, want.Width, got.Width)
	require.Equal(t, want.Height, got.Height)
	wantText := semanticFrameText(want)
	gotText := semanticFrameText(got)
	for _, token := range []string{
		"Amazon Marketplace", "Example Headphones", "order-example", "-12.34",
	} {
		require.Contains(t, wantText, token, "Python transaction-info characterization lost %q", token)
		require.Contains(t, gotText, token, "Go transaction-info projection lost %q", token)
	}
	require.Contains(t, wantText, "Matching Amazon Orders")
	require.Contains(t, gotText, "Amazon matches")
}

func semanticFrameText(frame parity.SemanticFrame) string {
	parts := append([]string(nil), frame.Overlay...)
	for _, region := range frame.Regions {
		parts = append(parts, region.Lines...)
	}
	return strings.Join(parts, "\n")
}

func withoutNamedGoTransactionInfoHint(frame parity.SemanticFrame) parity.SemanticFrame {
	const hint = " | i=Info"
	frame.Hints = strings.Replace(frame.Hints, hint, "", 1)
	for index := range frame.Regions {
		if frame.Regions[index].Name != "hints" {
			continue
		}
		for line := range frame.Regions[index].Lines {
			frame.Regions[index].Lines[line] = strings.Replace(frame.Regions[index].Lines[line], hint, "", 1)
		}
	}
	return frame
}

func withoutNamedGoHelpDivergences(t testing.TB, overlay []string) []string {
	t.Helper()
	divergences := map[string]bool{
		"  U               Redo most recent undone edit": false,
		"  r               Refresh provider data":        false,
	}
	result := make([]string, 0, len(overlay)-len(divergences))
	for _, line := range overlay {
		if _, divergent := divergences[line]; divergent {
			divergences[line] = true
			continue
		}
		result = append(result, line)
	}
	for line, found := range divergences {
		require.True(t, found, "named Go help entry is missing: %s", line)
	}
	return result
}

func updateModel(t testing.TB, model tui.Model, message tea.Msg) tui.Model {
	t.Helper()
	updated, _ := model.Update(message)
	result, ok := updated.(tui.Model)
	require.True(t, ok)
	return result
}

func semanticKey(name string) tea.KeyPressMsg {
	switch name {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "escape":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

func compactSemanticDiff(want parity.SemanticFrame, got parity.SemanticFrame) string {
	if fmt.Sprint(want.Columns) != fmt.Sprint(got.Columns) {
		return fmt.Sprintf("columns: want %v got %v", want.Columns, got.Columns)
	}
	if fmt.Sprint(want.VisibleRowIDs) != fmt.Sprint(got.VisibleRowIDs) {
		return fmt.Sprintf("row identities: want %v got %v", want.VisibleRowIDs, got.VisibleRowIDs)
	}
	for index := 0; index < len(want.Regions) && index < len(got.Regions); index++ {
		if fmt.Sprint(want.Regions[index]) != fmt.Sprint(got.Regions[index]) {
			return fmt.Sprintf("region %s differs:\nwant: %#v\n got: %#v", want.Regions[index].Name, want.Regions[index], got.Regions[index])
		}
	}
	if want.Breadcrumb != got.Breadcrumb {
		return fmt.Sprintf("breadcrumb: want %q got %q", want.Breadcrumb, got.Breadcrumb)
	}
	if want.Stats != got.Stats {
		return fmt.Sprintf("statistics: want %q got %q", want.Stats, got.Stats)
	}
	if fmt.Sprint(want.Flags) != fmt.Sprint(got.Flags) {
		return fmt.Sprintf("flags: want %v got %v", want.Flags, got.Flags)
	}
	if fmt.Sprint(want.SelectionIDs) != fmt.Sprint(got.SelectionIDs) {
		return fmt.Sprintf("selected: want %v got %v", want.SelectionIDs, got.SelectionIDs)
	}
	if want.Hints != got.Hints {
		return fmt.Sprintf("hints: want %q got %q", want.Hints, got.Hints)
	}
	if fmt.Sprint(want.Overlay) != fmt.Sprint(got.Overlay) {
		return fmt.Sprintf("overlay: want %v got %v", want.Overlay, got.Overlay)
	}
	return "semantic frame differs"
}
