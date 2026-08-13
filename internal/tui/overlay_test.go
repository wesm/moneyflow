package tui

import (
	"math/rand"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/app"
)

func TestOverlayResponsiveRegions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key    rune
		width  int
		height int
		region string
	}{{'/', 150, 30, "search_overlay"}, {'f', 80, 24, "filter_overlay"}, {'?', 150, 50, "help_overlay"}}
	for _, test := range cases {
		model := newTestModel(t, app.NewSession())
		model.width, model.height = test.width, test.height
		model = press(t, model, keyRune(test.key))
		screen := model.RenderScreen()
		region, ok := findRegion(screen.Regions, test.region)
		assert.True(t, ok, test.region)
		assertRectInside(t, region.Rect, test.width, test.height)
		assert.NotEmpty(t, screen.Frame.PlainLines())
		model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
		assert.Equal(t, overlayNone, model.overlay)
	}

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('/'))
	model.width, model.height = 40, 10
	screen := model.RenderScreen()
	_, hasResize := findRegion(screen.Regions, "resize")
	_, hasOverlay := findRegion(screen.Regions, "search_overlay")
	assert.True(t, hasResize)
	assert.False(t, hasOverlay)
}

func TestResponsiveRandomMessageSequencesStayBounded(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic state-machine coverage.
	messages := []tea.Msg{
		keyRune('j'), keyRune('k'), keyRune('/'), keyRune('f'), keyRune('?'),
		tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyTab},
		tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, keyRune('x'),
	}
	model := newTestModel(t, app.NewSession())
	for range 500 {
		if random.Intn(4) == 0 {
			model = pressMessage(t, model, tea.WindowSizeMsg{
				Width: random.Intn(180), Height: random.Intn(60),
			})
		} else {
			model = pressMessage(t, model, messages[random.Intn(len(messages))])
		}
		screen := model.RenderScreen()
		assert.Equal(t, model.width, screen.Frame.Width())
		assert.Equal(t, model.height, screen.Frame.Height())
		for _, region := range screen.Regions {
			assertRectInside(t, region.Rect, model.width, model.height)
		}
		if model.rowCount() == 0 {
			assert.Zero(t, model.cursor)
		} else {
			assert.GreaterOrEqual(t, model.cursor, 0)
			assert.Less(t, model.cursor, model.rowCount())
		}
	}
}

func pressMessage(t testing.TB, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}

func assertRectInside(t testing.TB, rect Rect, width int, height int) {
	t.Helper()
	assert.GreaterOrEqual(t, rect.X, 0)
	assert.GreaterOrEqual(t, rect.Y, 0)
	assert.GreaterOrEqual(t, rect.Width, 0)
	assert.GreaterOrEqual(t, rect.Height, 0)
	assert.LessOrEqual(t, rect.X+rect.Width, width)
	assert.LessOrEqual(t, rect.Y+rect.Height, height)
}
