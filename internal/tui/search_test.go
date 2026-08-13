package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestSearchIncrementalAcceptCancelAndClear(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('/'))
	require.Equal(t, overlaySearch, model.overlay)
	for _, character := range "GROC" {
		model = press(t, model, keyRune(character))
	}
	assert.Equal(t, "GROC", model.session.Search)
	require.NotEmpty(t, model.result.AggregateRows)
	for _, row := range model.result.AggregateRows {
		assert.Contains(t, row.Label, "Grocer")
	}

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, "GROC", model.session.Search)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Empty(t, model.session.Search)
	assert.Equal(t, overlayNone, model.overlay)

	model = press(t, model, keyRune('/'))
	model = press(t, model, keyRune('x'))
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Empty(t, model.session.Search)
	assert.Equal(t, overlayNone, model.overlay)
}

func TestSearchInvalidAndEmptyResultsStaySafe(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('/'))
	model = press(t, model, keyRune('['))
	assert.Equal(t, overlaySearch, model.overlay)
	assert.NotEmpty(t, model.search.err)
	assert.NotContains(t, model.search.err, "[")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlaySearch, model.overlay)

	model.search.input.SetValue("does-not-match")
	model.updateSearch()
	assert.Empty(t, model.result.AggregateRows)
	assert.Zero(t, model.cursor)
	assert.Zero(t, model.scroll)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
}
