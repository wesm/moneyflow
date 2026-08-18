package parity

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

func TestOnboardingScenariosStrictValidation(t *testing.T) {
	t.Parallel()
	valid := `{"schema_version":1,"scenarios":[{"name":"account_selector","width":100,"height":30,"screen":"account_selector","keys":[]}]}`
	document, err := DecodeOnboardingScenarios(strings.NewReader(valid))
	require.NoError(t, err)
	require.Len(t, document.Scenarios, 1)

	for name, input := range map[string]string{
		"version":  strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		"unknown":  strings.Replace(valid, `"keys":[]`, `"keys":[],"extra":true`, 1),
		"screen":   strings.Replace(valid, `"account_selector"`, `"unknown"`, 2),
		"geometry": strings.Replace(valid, `"width":100`, `"width":0`, 1),
		"trailing": valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, decodeErr := DecodeOnboardingScenarios(strings.NewReader(input))
			assert.Error(t, decodeErr)
		})
	}
}

func TestOnboardingCommittedArtifactsAreEmbeddedAndStrict(t *testing.T) {
	t.Parallel()
	document, err := LoadOnboardingScenarios(filepath.Join(
		"..", "..", "testdata", "parity", "onboarding_scenarios.json",
	))
	require.NoError(t, err)
	require.Len(t, document.Scenarios, 4)
	for _, scenario := range document.Scenarios {
		path := filepath.Join("onboarding_semantic_frames", scenario.Name+".json")
		data, readErr := paritydata.Onboarding.ReadFile(path)
		require.NoError(t, readErr)
		frame, decodeErr := DecodeOnboardingSemanticFrame(strings.NewReader(string(data)))
		require.NoError(t, decodeErr)
		assert.Equal(t, scenario.Name, frame.Name)
		assert.Equal(t, scenario.Width, frame.Width)
		assert.Equal(t, scenario.Height, frame.Height)
	}
}

func TestOnboardingSemanticFrameValidatesBounds(t *testing.T) {
	t.Parallel()
	valid := `{"schema_version":1,"name":"credential_unlock","width":100,"height":30,"lines":["Unlock"],"focus":"password-input","fields":["password-input"],"hints":["Esc=Back"]}`
	frame, err := DecodeOnboardingSemanticFrame(strings.NewReader(valid))
	require.NoError(t, err)
	assert.Equal(t, "credential_unlock", frame.Name)

	for name, input := range map[string]string{
		"version": strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		"lines":   strings.Replace(valid, `"lines":["Unlock"]`, `"lines":null`, 1),
		"fields":  strings.Replace(valid, `"fields":["password-input"]`, `"fields":null`, 1),
		"unknown": strings.Replace(valid, `"hints":["Esc=Back"]`, `"hints":["Esc=Back"],"secret":"bad"`, 1),
		"bounds":  strings.Replace(valid, `"height":30`, `"height":0`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, decodeErr := DecodeOnboardingSemanticFrame(strings.NewReader(input))
			assert.Error(t, decodeErr)
		})
	}
}
