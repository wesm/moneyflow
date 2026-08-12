package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThemeNamesAndUnknownTheme(t *testing.T) {
	t.Parallel()

	want := []ThemeName{
		ThemeDefault, ThemeBerg, ThemeNord, ThemeGruvbox, ThemeDracula, ThemeSolarizedDark, ThemeMonokai,
	}
	assert.Equal(t, want, ThemeNames())
	for _, name := range want {
		palette, err := PaletteFor(name, ColorModeTrueColor)
		require.NoError(t, err)
		assert.NotEmpty(t, palette.Background.Background)
		assert.NotEmpty(t, palette.Text.Foreground)
	}
	_, err := PaletteFor("unknown", ColorModeTrueColor)
	assert.Error(t, err)
}

func TestThemeResolutionIsDeterministic(t *testing.T) {
	t.Parallel()

	trueColor, err := PaletteFor(ThemeNord, ColorModeTrueColor)
	require.NoError(t, err)
	assert.Equal(t, "#3b4252", trueColor.Background.Background)
	assert.Equal(t, "#eceff4", trueColor.Text.Foreground)

	ansi256, err := PaletteFor(ThemeNord, ColorModeANSI256)
	require.NoError(t, err)
	assert.Regexp(t, `^ansi256:[0-9]+$`, ansi256.Text.Foreground)
	ansi16, err := PaletteFor(ThemeNord, ColorModeANSI16)
	require.NoError(t, err)
	assert.Regexp(t, `^ansi16:[0-9]+$`, ansi16.Text.Foreground)
	plain, err := PaletteFor(ThemeNord, ColorModeNone)
	require.NoError(t, err)
	assert.Empty(t, plain.Text.Foreground)
	assert.True(t, plain.Heading.Bold)
}

func TestResolveColorMode(t *testing.T) {
	t.Parallel()

	profile := TerminalProfile{TrueColor: true, Colors: 1 << 24}
	assert.Equal(t, ColorModeNone, ResolveColorMode(ColorModeAuto, map[string]string{"NO_COLOR": "1"}, profile))
	assert.Equal(t, ColorModeTrueColor, ResolveColorMode(ColorModeTrueColor, map[string]string{"NO_COLOR": "1"}, profile))
	assert.Equal(t, ColorModeTrueColor, ResolveColorMode(ColorModeAuto, nil, profile))
	assert.Equal(t, ColorModeANSI256, ResolveColorMode(ColorModeAuto, nil, TerminalProfile{Colors: 256}))
	assert.Equal(t, ColorModeANSI16, ResolveColorMode(ColorModeAuto, nil, TerminalProfile{Colors: 16}))
	assert.Equal(t, ColorModeNone, ResolveColorMode(ColorModeAuto, nil, TerminalProfile{}))
}
