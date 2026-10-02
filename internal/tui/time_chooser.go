package tui

import (
	"slices"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/domain"
)

var timeResolutions = []domain.TimeGranularity{
	domain.TimeGranularityYear, domain.TimeGranularityMonth, domain.TimeGranularityDay,
}

type timeChooserState struct {
	now        time.Time
	anchor     time.Time
	resolution domain.TimeGranularity
	selected   int
	input      *textinput.Model
	err        string
}

func (model *Model) openTimeChooser() {
	now := model.now()
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = len("YYYY-MM-DD")
	input.SetWidth(12)
	input.Focus()
	model.timeChooser = timeChooserState{
		now: now, anchor: now, resolution: domain.TimeGranularityMonth, input: &input,
	}
	model.overlay = overlayTimeChooser
	model.status = ""
}

func (model *Model) routeTimeChooser(message tea.KeyPressMsg) tea.Cmd {
	chooser := &model.timeChooser
	key := message.Keystroke()
	switch key {
	case "esc":
		chooser.input.Blur()
		model.overlay = overlayNone
	case "a":
		model.applyTimeRange(nil)
	case "tab", "shift+tab", "down", "j", "up", "k", "enter":
		if !chooser.readInput() {
			return nil
		}
		switch key {
		case "tab", "shift+tab":
			delta := 1
			if key == "shift+tab" {
				delta = -1
			}
			index := slices.Index(timeResolutions, chooser.resolution)
			chooser.resolution = timeResolutions[(index+delta+len(timeResolutions))%len(timeResolutions)]
			chooser.selected = 0
		case "down", "j", "up", "k":
			delta := -1
			if key == "up" || key == "k" {
				delta = 1
			}
			anchor := shiftCalendarPeriod(chooser.anchor, chooser.resolution, delta)
			if _, err := calendarPeriodRange(anchor, chooser.resolution); err == nil {
				chooser.anchor = anchor
				chooser.selected = min(4, max(0, chooser.selected-delta))
			}
		case "enter":
			dateRange, err := calendarPeriodRange(chooser.anchor, chooser.resolution)
			if err == nil {
				model.applyTimeRange(&dateRange)
			}
		}
		chooser.input.SetValue("")
	default:
		var command tea.Cmd
		*chooser.input, command = chooser.input.Update(message)
		chooser.err = ""
		if chooser.input.Value() != "" {
			// Preview complete input immediately. Incomplete input stays editable.
			if anchor, resolution, ok := parseCalendarPeriod(chooser.input.Value()); ok {
				chooser.anchor, chooser.resolution = anchor, resolution
				chooser.selected = 0
			}
		}
		return command
	}
	return nil
}

func (chooser *timeChooserState) readInput() bool {
	if chooser.input.Value() == "" {
		return true
	}
	anchor, resolution, ok := parseCalendarPeriod(chooser.input.Value())
	if !ok {
		chooser.err = "Use YYYY, YYYY-MM, or YYYY-MM-DD (years 0001–9999)"
		return false
	}
	chooser.anchor, chooser.resolution = anchor, resolution
	chooser.err = ""
	return true
}

func parseCalendarPeriod(value string) (time.Time, domain.TimeGranularity, bool) {
	for index, layout := range []string{"2006", "2006-01", "2006-01-02"} {
		if len(value) != len(layout) {
			continue
		}
		anchor, err := time.Parse(layout, value)
		if err == nil && anchor.Year() >= 1 && anchor.Format(layout) == value {
			return anchor, timeResolutions[index], true
		}
	}
	return time.Time{}, "", false
}

func (model *Model) applyTimeRange(dateRange *domain.DateRange) {
	if err := model.session.SetTimeRange(dateRange); err != nil {
		model.status = "The time range could not be applied."
		return
	}
	if model.timeChooser.input != nil {
		model.timeChooser.input.Blur()
	}
	model.overlay = overlayNone
	model.resetAndRefresh()
}

func (model *Model) navigateTimePeriod(delta int) {
	if anchor, resolution, ok := calendarPeriod(model.session.DateRange); ok {
		dateRange, err := calendarPeriodRange(shiftCalendarPeriod(anchor, resolution, delta), resolution)
		if err == nil {
			model.applyTimeRange(&dateRange)
		}
		return
	}
	if model.session.NavigatePeriod(delta) {
		model.resetAndRefresh()
	}
}

// shiftCalendarPeriod retains the day when possible and clamps it at month end.
// Dates use UTC here because a time selection represents calendar dates, not instants.
func shiftCalendarPeriod(anchor time.Time, resolution domain.TimeGranularity, delta int) time.Time {
	year, month, day := anchor.Date()
	if resolution == domain.TimeGranularityDay {
		return time.Date(year, month, day+delta, 0, 0, 0, 0, time.UTC)
	}
	if resolution == domain.TimeGranularityYear {
		year += delta
	} else {
		month += time.Month(delta)
	}
	lastDay := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(year, month, min(day, lastDay), 0, 0, 0, 0, time.UTC)
}

func calendarPeriodRange(anchor time.Time, resolution domain.TimeGranularity) (domain.DateRange, error) {
	year, month, day := anchor.Date()
	endMonth, endDay := month, day
	switch resolution {
	case domain.TimeGranularityYear:
		month, day, endMonth, endDay = time.January, 1, time.December, 31
	case domain.TimeGranularityMonth:
		day = 1
		endDay = time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	}
	start, err := domain.NewDate(year, month, day)
	if err != nil {
		return domain.DateRange{}, err
	}
	end, err := domain.NewDate(year, endMonth, endDay)
	return domain.DateRange{Start: start, End: end}, err
}

func calendarPeriod(dateRange *domain.DateRange) (time.Time, domain.TimeGranularity, bool) {
	if dateRange != nil {
		start := dateRange.Start
		anchor := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		for _, resolution := range timeResolutions {
			fullPeriod, err := calendarPeriodRange(anchor, resolution)
			if err == nil && fullPeriod == *dateRange {
				return anchor, resolution, true
			}
		}
	}
	return time.Time{}, "", false
}

func calendarPeriodLabel(anchor time.Time, resolution domain.TimeGranularity) string {
	switch resolution {
	case domain.TimeGranularityYear:
		return anchor.Format("2006")
	case domain.TimeGranularityMonth:
		return anchor.Format("Jan 2006")
	default:
		return anchor.Format("Jan 2, 2006")
	}
}

func (model Model) renderTimeChooser(screen *RenderedScreen) {
	palette := model.palette
	palette.Text.Background = palette.Panel.Background
	palette.Muted.Background = palette.Panel.Background
	palette.Warning.Background = palette.Panel.Background
	rect := responsiveOverlayRect(model.width, model.height, 64, 19)
	fillRect(&screen.Frame, rect, palette.Panel)
	overlayTitle(&screen.Frame, rect, "Choose time", palette.Heading)
	x, width := rect.X+2, rect.Width-4
	chooser := model.timeChooser
	tabX := x
	for index, label := range []string{"Year", "Month", "Day"} {
		style := palette.Muted
		if chooser.resolution == timeResolutions[index] {
			label = "[" + label + "]"
			style = palette.Selection
		} else {
			label = " " + label + " "
		}
		screen.Frame.PutText(tabX, rect.Y+2, label, style)
		tabX += len(label) + 3
	}
	for index := range 5 {
		anchor := shiftCalendarPeriod(chooser.anchor, chooser.resolution, chooser.selected-index)
		if _, err := calendarPeriodRange(anchor, chooser.resolution); err != nil {
			continue
		}
		label := calendarPeriodLabel(anchor, chooser.resolution)
		relative := ""
		current := calendarPeriodLabel(chooser.now, chooser.resolution)
		previous := calendarPeriodLabel(shiftCalendarPeriod(chooser.now, chooser.resolution, -1), chooser.resolution)
		switch label {
		case current:
			relative = "This " + string(chooser.resolution)
			if chooser.resolution == domain.TimeGranularityDay {
				relative = "Today"
			}
		case previous:
			relative = "Last " + string(chooser.resolution)
			if chooser.resolution == domain.TimeGranularityDay {
				relative = "Yesterday"
			}
		}
		label = padRight(label, 20) + relative
		filterLine(&screen.Frame, x, rect.Y+4+index, width, chooser.selected == index, padRight(label, width-2), "", palette)
	}
	dateRange, _ := calendarPeriodRange(chooser.anchor, chooser.resolution)
	preview := "Selected: " + dateRange.Start.String() + " → " + dateRange.End.String()
	if value := chooser.input.Value(); value != "" {
		if _, _, ok := parseCalendarPeriod(value); !ok {
			preview = "Selected: finish typing a valid period"
		}
	}
	screen.Frame.PutText(x, rect.Y+10, preview, palette.Text)
	screen.Frame.PutText(x, rect.Y+12, "Jump: ", palette.Text)
	screen.Frame.PutText(x+6, rect.Y+12, padRight(chooser.input.Value(), 12), palette.Selection)
	if chooser.input.Value() != "" {
		screen.Cursor = tea.NewCursor(x+6+chooser.input.Position(), rect.Y+12)
	}
	screen.Frame.PutText(x, rect.Y+13, "Type YYYY, YYYY-MM, or YYYY-MM-DD", palette.Muted)
	if chooser.err != "" {
		screen.Frame.PutText(x, rect.Y+14, Truncate(chooser.err, width), palette.Warning)
	}
	for index, footer := range []string{
		"Tab/Shift+Tab Resolution   ↑/↓ Period",
		"Enter Apply   a All time   Esc Cancel",
	} {
		putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 3 + index, Width: rect.Width, Height: 1}, footer, palette.Muted)
	}
	screen.Regions = append(screen.Regions, NamedRegion{Name: "time_chooser", Rect: rect})
}
