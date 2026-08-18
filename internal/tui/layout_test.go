package tui

import (
	"strings"
	"testing"
	"time"

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

func TestChromeShowsVersionCurrentTimeAndLastUpdateAtSupportedSizes(t *testing.T) {
	t.Parallel()

	current := time.Date(2026, time.August, 18, 9, 41, 0, 0, time.Local)
	lastUpdate := time.Date(2026, time.August, 18, 9, 5, 0, 0, time.Local)
	for _, size := range []struct{ width, height int }{{150, 50}, {80, 24}} {
		model := newTestModel(t, app.NewSession())
		model.width, model.height = size.width, size.height
		model.options.Version = "v9.8.7"
		model.clockAt = current
		model.provider.status.LastSuccess = lastUpdate

		screen := model.RenderScreen()
		line := screen.Frame.PlainLines()[0]
		assert.Contains(t, line, "moneyflow v9.8.7")
		assert.Contains(t, line, "Last update 9:05 AM")
		assert.Contains(t, line, "9:41 AM")
		region, ok := findRegion(screen.Regions, "chrome")
		assert.True(t, ok)
		assert.Equal(t, 0, region.Rect.Y)
	}
}

func TestChromeShowsUnknownLastUpdateForLocalProfile(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.options.Version = "dev"
	model.clockAt = time.Date(2026, time.August, 18, 9, 41, 0, 0, time.Local)

	line := strings.TrimSpace(model.RenderScreen().Frame.PlainLines()[0])
	assert.Contains(t, line, "moneyflow dev")
	assert.Contains(t, line, "Last update —")
}

func TestDetailDrillColumnWidthsUseFixedDimensionPrecedence(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, app.NewSession())
	model.result.AggregateRows = nil
	model.result.DetailRows = []domain.DetailRow{}
	model.session.Drilldowns = []domain.Drilldown{
		{Dimension: domain.DimensionCategory, Currency: "USD", Scale: 2, Key: "category", Label: "A very long category label"},
		{Dimension: domain.DimensionMerchant, Currency: "USD", Scale: 2, Key: "merchant", Label: "Shop"},
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
