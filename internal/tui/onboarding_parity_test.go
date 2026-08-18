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
			goScreen, renderErr := tui.RenderOnboardingPreviewForTest(
				scenario.Screen, scenario.Width, scenario.Height, false,
			)
			require.NoError(t, renderErr)
			semantics := tui.OnboardingPreviewSemanticsForTest(scenario.Screen)

			assert.Equal(t, python.Width, goScreen.Frame.Width())
			assert.Equal(t, python.Height, goScreen.Frame.Height())
			assert.Equal(t, python.Focus, semantics.Focus)
			assert.Equal(t, python.Fields, semantics.Fields)
			assertOnboardingConcepts(t, scenario.Screen, strings.Join(python.Lines, "\n"))
			assertOnboardingConcepts(t, scenario.Screen, strings.Join(goScreen.Frame.PlainLines(), "\n"))
			assertCredentialBlindPreview(t, goScreen, semantics)
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
	screen tui.RenderedScreen,
	semantics tui.OnboardingPreviewSemantics,
) {
	t.Helper()
	if len(semantics.Fields) == 0 {
		return
	}
	rendered := strings.Join(screen.Frame.PlainLines(), "\n")
	assert.NotContains(t, rendered, "example@example.com")
	assert.NotContains(t, rendered, "correct horse battery staple")
	assert.NotContains(t, rendered, "JBSWY3DPEHPK3PXP")
}
