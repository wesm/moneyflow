package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
)

func TestDuplicateOverlayFormattingIsBoundedAndComplete(t *testing.T) {
	t.Parallel()

	model := press(t, newDuplicateModel(t).model, keyRune('D'))
	model.width, model.height = 80, 24
	screen := model.RenderScreen()
	region, ok := findRegion(screen.Regions, "duplicate_overlay")
	require.True(t, ok)
	assertRectInside(t, region.Rect, 80, 24)
	rendered := strings.Join(screen.Overlay, "\n")
	assert.Contains(t, rendered, "Found 1 potential duplicates in 1 groups")
	assert.Equal(t, 2, strings.Count(rendered, "#1"))
	assert.Contains(t, rendered, "selected=0")
	assert.Contains(t, rendered, "hidden=0")
	assert.Contains(t, rendered, model.duplicates.projection.Groups[0].Rows[0].MatchingLabel)
	assert.Contains(t, rendered, model.duplicates.projection.Groups[0].Rows[0].Transaction.Account.Name)
	assert.Contains(t, rendered, FormatAmount(model.duplicates.projection.Groups[0].Rows[0].Transaction.Amount))
	assert.Contains(t, screen.Frame.RenderANSI(), "Catego")
}

func TestDuplicateActionBindingsAreImplemented(t *testing.T) {
	t.Parallel()

	duplicates, ok := app.ActionByID(app.ActionFindDuplicates)
	require.True(t, ok)
	assert.True(t, duplicates.Implemented)
	assert.Equal(t, []string{"D"}, duplicates.Keys)
	deletion, ok := app.ActionByID(app.ActionDeleteTransaction)
	require.True(t, ok)
	assert.True(t, deletion.Implemented)
	assert.Equal(t, []string{"x"}, deletion.Keys)
}
