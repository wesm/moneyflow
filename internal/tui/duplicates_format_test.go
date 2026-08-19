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
	assert.Contains(t, rendered, "1 duplicate group")
	assert.Contains(t, rendered, "2 transactions")
	assert.Contains(t, rendered, model.duplicates.projection.Groups[0].Rows[0].MatchingLabel)
	assert.Contains(t, rendered, model.duplicates.projection.Groups[0].Rows[0].Transaction.Category.Name)
	assert.Contains(t, rendered, model.duplicates.projection.Groups[0].Rows[0].Transaction.Account.Name)
	assert.Contains(t, rendered, FormatAmount(model.duplicates.projection.Groups[0].Rows[0].Transaction.Amount))
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
