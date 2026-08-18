package tui_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/parity"
	"github.com/wesm/moneyflow/internal/tui"
)

func TestOnboardingSemanticParity(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	document, err := parity.LoadOnboardingScenarios(filepath.Join(
		root, "testdata", "parity", "onboarding_scenarios.json",
	))
	require.NoError(t, err)
	for _, scenario := range document.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Parallel()
			python, loadErr := parity.LoadOnboardingSemanticFrame(filepath.Join(
				root, "testdata", "parity", "onboarding_semantic_frames", scenario.Name+".json",
			))
			require.NoError(t, loadErr)
			preview, renderErr := tui.PopulatedOnboardingPreviewForTest(
				scenario.Screen, scenario.Width, scenario.Height, false,
			)
			require.NoError(t, renderErr)

			assert.Equal(t, python.Width, preview.Screen.Frame.Width())
			assert.Equal(t, python.Height, preview.Screen.Frame.Height())
			assert.Equal(t, python.Focus, preview.Semantics.Focus)
			assert.Equal(t, python.Fields, preview.Semantics.Fields)
			assertOnboardingConcepts(t, scenario.Screen, strings.Join(python.Lines, "\n"))
			assertOnboardingConcepts(t, scenario.Screen, strings.Join(
				preview.Screen.Frame.PlainLines(), "\n",
			))
			assertCredentialBlindPreview(t, preview)
		})
	}
}

func assertOnboardingConcepts(t testing.TB, screen string, rendered string) {
	t.Helper()
	rendered = strings.ToLower(rendered)
	concepts := map[string][]string{
		"account_selector":  {"select", "account", "example profile", "monarch", "add", "demo", "exit"},
		"provider_selector": {"select", "finance", "monarch", "ynab", "simplefin", "cancel"},
		"credential_setup":  {"monarch", "email", "password", "totp"},
		"credential_unlock": {"unlock", "credentials", "password"},
	}
	for _, concept := range concepts[screen] {
		assert.Contains(t, rendered, concept)
	}
}

func assertCredentialBlindPreview(
	t testing.TB,
	preview tui.OnboardingPreview,
) {
	t.Helper()
	if len(preview.Semantics.Fields) == 0 {
		return
	}
	rendered := strings.Join(preview.Screen.Frame.PlainLines(), "\n")
	for _, visible := range preview.VisibleValues {
		assert.Contains(t, rendered, visible)
	}
	for _, secret := range preview.SecretValues {
		assert.NotContains(t, rendered, secret)
	}
	assert.Contains(t, rendered, "•", "populated secret fields must render as masks")
}
