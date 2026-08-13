package tui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestLayoutSupportedSizesExposeStableRegions(t *testing.T) {
	t.Parallel()

	for _, size := range []struct{ width, height int }{{150, 50}, {120, 30}, {80, 24}} {
		model := newTestModel(t, app.NewSession())
		model.width, model.height = size.width, size.height
		screen := model.RenderScreen()
		assert.Equal(t, size.width, screen.Frame.Width())
		assert.Equal(t, size.height, screen.Frame.Height())
		for _, name := range []string{"breadcrumb", "stats", "table_header", "table_body", "hints"} {
			region, ok := findRegion(screen.Regions, name)
			assert.True(t, ok, name)
			assert.LessOrEqual(t, region.Rect.X+region.Rect.Width, size.width)
			assert.LessOrEqual(t, region.Rect.Y+region.Rect.Height, size.height)
		}
		body, _ := findRegion(screen.Regions, "table_body")
		assert.GreaterOrEqual(t, body.Rect.Height, 1)
	}
}

func TestDetailDrillColumnWidthsUseFixedDimensionPrecedence(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.result.AggregateRows = nil
	model.result.DetailRows = []domain.DetailRow{}
	model.session.Drilldowns = []domain.Drilldown{
		{Dimension: domain.DimensionCategory, Key: "category", Label: "A very long category label"},
		{Dimension: domain.DimensionMerchant, Key: "merchant", Label: "Shop"},
	}

	columns := model.columns(120)
	for _, column := range columns {
		if column.Key == "merchant" {
			assert.Equal(t, len([]rune("Shop"))+2, column.Width)
			return
		}
	}
	t.Fatal("merchant column missing")
}

func TestLayoutBelowMinimumShowsResizeOnly(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.width, model.height = 79, 23
	screen := model.RenderScreen()
	_, hasResize := findRegion(screen.Regions, "resize")
	_, hasTable := findRegion(screen.Regions, "table_body")
	assert.True(t, hasResize)
	assert.False(t, hasTable)
	assert.Contains(t, screen.Frame.RenderANSI(), "80x24")
}

func findRegion(regions []NamedRegion, name string) (NamedRegion, bool) {
	for _, region := range regions {
		if region.Name == name {
			return region, true
		}
	}
	return NamedRegion{}, false
}
