package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ThemeName identifies one explicit moneyflow palette.
type ThemeName string

// Supported themes, in user-visible order.
const (
	ThemeDefault       ThemeName = "default"
	ThemeBerg          ThemeName = "berg"
	ThemeNord          ThemeName = "nord"
	ThemeGruvbox       ThemeName = "gruvbox"
	ThemeDracula       ThemeName = "dracula"
	ThemeSolarizedDark ThemeName = "solarized-dark"
	ThemeMonokai       ThemeName = "monokai"
)

// ColorMode fixes terminal color resolution at the process boundary.
type ColorMode string

// Supported color modes.
const (
	ColorModeAuto      ColorMode = "auto"
	ColorModeTrueColor ColorMode = "truecolor"
	ColorModeANSI256   ColorMode = "256"
	ColorModeANSI16    ColorMode = "16"
	ColorModeNone      ColorMode = "none"
)

// Style is the renderer-independent appearance stored in terminal cells.
type Style struct {
	Foreground string
	Background string
	Bold       bool
	Dim        bool
	Reverse    bool
}

// Palette assigns styles to the semantic roles shared by every screen.
type Palette struct {
	Background Style
	Panel      Style
	Border     Style
	Heading    Style
	Text       Style
	Muted      Style
	Selection  Style
	Positive   Style
	Warning    Style
	Hidden     Style
}

// TerminalProfile is the capability probe supplied by the process boundary.
type TerminalProfile struct {
	TrueColor bool
	Colors    int
}

type themeColors struct {
	background string
	panel      string
	primary    string
	accent     string
	text       string
	muted      string
	positive   string
	warning    string
}

var themes = map[ThemeName]themeColors{
	ThemeDefault: {
		background: "#121826", panel: "#0e1626", primary: "#1460aa", accent: "#5aa9e6",
		text: "#e6edf3", muted: "#8b98a5", positive: "#65b584", warning: "#d6ad60",
	},
	ThemeBerg: {
		background: "#222222", panel: "#2a2a2a", primary: "#8b6508", accent: "#cd9b3d",
		text: "#c5b5a0", muted: "#8a7f6f", positive: "#7a9971", warning: "#d4a76a",
	},
	ThemeNord: {
		background: "#3b4252", panel: "#434c5e", primary: "#5e8d9a", accent: "#88c0d0",
		text: "#eceff4", muted: "#d8dee9", positive: "#a3be8c", warning: "#ebcb8b",
	},
	ThemeGruvbox: {
		background: "#282828", panel: "#3c3836", primary: "#a87c1a", accent: "#d87c31",
		text: "#d5c4a1", muted: "#a89984", positive: "#a3af5a", warning: "#c28a1d",
	},
	ThemeDracula: {
		background: "#2f3241", panel: "#44475a", primary: "#8b6fc8", accent: "#c75a94",
		text: "#e5e5e5", muted: "#7c8db5", positive: "#6bd16b", warning: "#e9e87c",
	},
	ThemeSolarizedDark: {
		background: "#073642", panel: "#073642", primary: "#268bd2", accent: "#3fbfb3",
		text: "#839496", muted: "#586e75", positive: "#859900", warning: "#b58900",
	},
	ThemeMonokai: {
		background: "#2d2e27", panel: "#3e3d32", primary: "#4a7a85", accent: "#e59c6a",
		text: "#e5e5e5", muted: "#888877", positive: "#98c379", warning: "#d4c48a",
	},
}

// ThemeNames returns all supported themes in stable display order.
func ThemeNames() []ThemeName {
	return []ThemeName{
		ThemeDefault, ThemeBerg, ThemeNord, ThemeGruvbox, ThemeDracula, ThemeSolarizedDark, ThemeMonokai,
	}
}

// PaletteFor resolves a theme to deterministic terminal color tokens.
func PaletteFor(name ThemeName, mode ColorMode) (Palette, error) {
	colors, exists := themes[name]
	if !exists {
		return Palette{}, fmt.Errorf("palette: unknown theme %q", name)
	}
	if mode == ColorModeAuto {
		mode = ColorModeTrueColor
	}
	if !mode.validResolved() {
		return Palette{}, fmt.Errorf("palette: invalid color mode %q", mode)
	}
	resolve := func(color string) string { return resolveColor(color, mode) }
	background := resolve(colors.background)
	panel := resolve(colors.panel)
	text := resolve(colors.text)
	muted := resolve(colors.muted)
	return Palette{
		Background: Style{Foreground: text, Background: background},
		Panel:      Style{Foreground: text, Background: panel},
		Border:     Style{Foreground: resolve(colors.primary), Background: background},
		Heading:    Style{Foreground: resolve(colors.accent), Background: panel, Bold: true},
		Text:       Style{Foreground: text, Background: background},
		Muted:      Style{Foreground: muted, Background: background, Dim: true},
		Selection:  Style{Foreground: text, Background: resolve(colors.accent), Reverse: mode == ColorModeNone},
		Positive:   Style{Foreground: resolve(colors.positive), Background: background},
		Warning:    Style{Foreground: resolve(colors.warning), Background: background, Bold: true},
		Hidden:     Style{Foreground: muted, Background: background, Dim: true},
	}, nil
}

// ResolveColorMode applies an explicit choice or deterministic boundary detection.
func ResolveColorMode(explicit ColorMode, env map[string]string, profile TerminalProfile) ColorMode {
	if explicit != "" && explicit != ColorModeAuto {
		if explicit.validResolved() {
			return explicit
		}
		return ColorModeNone
	}
	if _, disabled := env["NO_COLOR"]; disabled {
		return ColorModeNone
	}
	if profile.TrueColor || profile.Colors >= 1<<24 {
		return ColorModeTrueColor
	}
	if profile.Colors >= 256 {
		return ColorModeANSI256
	}
	if profile.Colors >= 16 {
		return ColorModeANSI16
	}
	return ColorModeNone
}

func (mode ColorMode) validResolved() bool {
	return mode == ColorModeTrueColor || mode == ColorModeANSI256 || mode == ColorModeANSI16 || mode == ColorModeNone
}

func resolveColor(color string, mode ColorMode) string {
	if mode == ColorModeNone {
		return ""
	}
	if mode == ColorModeTrueColor {
		return strings.ToLower(color)
	}
	r, g, b, err := parseHex(color)
	if err != nil {
		return ""
	}
	if mode == ColorModeANSI16 {
		return fmt.Sprintf("ansi16:%d", nearestPalette(r, g, b, ansi16Palette()))
	}
	return fmt.Sprintf("ansi256:%d", nearestPalette(r, g, b, ansi256Palette()))
}

type rgb struct{ r, g, b int }

func parseHex(value string) (int, int, int, error) {
	if len(value) != 7 || value[0] != '#' {
		return 0, 0, 0, errors.New("invalid hex color")
	}
	parsed, err := strconv.ParseUint(value[1:], 16, 24)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(parsed >> 16), int((parsed >> 8) & 0xff), int(parsed & 0xff), nil
}

func nearestPalette(r int, g int, b int, palette []rgb) int {
	best, bestDistance := 0, int(^uint(0)>>1)
	for index, candidate := range palette {
		dr, dg, db := r-candidate.r, g-candidate.g, b-candidate.b
		distance := dr*dr + dg*dg + db*db
		if distance < bestDistance {
			best, bestDistance = index, distance
		}
	}
	return best
}

func ansi16Palette() []rgb {
	return []rgb{
		{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
		{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
		{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
		{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
	}
}

func ansi256Palette() []rgb {
	palette := append([]rgb(nil), ansi16Palette()...)
	levels := []int{0, 95, 135, 175, 215, 255}
	for _, r := range levels {
		for _, g := range levels {
			for _, b := range levels {
				palette = append(palette, rgb{r, g, b})
			}
		}
	}
	for index := 0; index < 24; index++ {
		level := 8 + index*10
		palette = append(palette, rgb{level, level, level})
	}
	return palette
}
