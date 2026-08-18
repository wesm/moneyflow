package tui

import (
	"context"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
)

func TestModelConstructionAndInitialView(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	assert.NotNil(t, model.Init())
	view := model.View()
	assert.True(t, view.AltScreen)
	assert.NotEmpty(t, view.Content)
	assert.Equal(t, 80, model.width)
	assert.Equal(t, 24, model.height)

	_, err := NewModel(context.Background(), model.service, app.NewSession(), Options{Theme: "missing", ColorMode: ColorModeNone})
	assert.Error(t, err)
}

func TestModelResizeOwnsViewportOnly(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	before := model.session
	updated, command := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	require.Nil(t, command)
	model = updated.(Model)
	assert.Equal(t, 120, model.width)
	assert.Equal(t, 30, model.height)
	assert.Equal(t, before.QuerySpec(), model.session.QuerySpec())
}

func newTestModel(t testing.TB, session app.Session) Model {
	t.Helper()
	transactions, err := fixture.Load(filepath.Join("..", "..", "testdata", "parity", "transactions.json"))
	require.NoError(t, err)
	service, err := app.NewService(transactions)
	require.NoError(t, err)
	model, err := NewModel(context.Background(), service, session, Options{Theme: ThemeDefault, ColorMode: ColorModeNone})
	require.NoError(t, err)
	return model
}
