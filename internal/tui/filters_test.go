package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestFilterCurrentValuesFocusApplyAndCancel(t *testing.T) {
	t.Parallel()

	start, err := domain.ParseDate("2024-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2024-12-31")
	require.NoError(t, err)
	session := app.NewSession()
	require.NoError(t, session.SetFilters(app.Filters{
		DateRange: &domain.DateRange{Start: start, End: end}, ShowHidden: false, ShowTransfers: true,
	}))
	model := newTestModel(t, session)
	model = press(t, model, keyRune('f'))
	require.Equal(t, overlayFilters, model.overlay)
	assert.Equal(t, "2024-01-01", model.filters.start.Value())
	assert.Equal(t, "2024-12-31", model.filters.end.Value())
	assert.False(t, model.filters.showHidden)
	assert.True(t, model.filters.showTransfers)

	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, filterEnd, model.filters.focus)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab})
	assert.Equal(t, filterHidden, model.filters.focus)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	assert.True(t, model.filters.showHidden)

	model.filters.focus = filterApply
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayNone, model.overlay)
	assert.True(t, model.session.ShowHidden)
	assert.True(t, model.session.ShowTransfers)

	model = press(t, model, keyRune('f'))
	model.filters.showHidden = false
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEscape})
	assert.Equal(t, overlayNone, model.overlay)
	assert.True(t, model.session.ShowHidden)
}

func TestFilterDateValidationKeepsOverlayOpen(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model = press(t, model, keyRune('f'))
	model.filters.start.SetValue("2025-12-31")
	model.filters.end.SetValue("2025-01-01")
	model.filters.focus = filterApply
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, overlayFilters, model.overlay)
	assert.Equal(t, "Start date must not be after end date", model.filters.err)
	assert.NotContains(t, model.filters.err, "2025")

	model.filters.start.SetValue("not-a-date")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	assert.Equal(t, "Dates must use YYYY-MM-DD", model.filters.err)
}
