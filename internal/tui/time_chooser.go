package tui

import (
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/domain"
)

type timeChooserState struct {
	selected int
	now      time.Time
	custom   bool
	input    *textinput.Model
	err      string
}

func (model *Model) openTimeChooser() {
	model.timeChooser = timeChooserState{now: model.now()}
	model.overlay = overlayTimeChooser
	model.status = ""
}

func (model *Model) routeTimeChooser(message tea.KeyPressMsg) tea.Cmd {
	chooser := &model.timeChooser
	if message.Keystroke() == "esc" {
		if chooser.input != nil {
			chooser.input.Blur()
		}
		model.overlay = overlayNone
		return nil
	}
	if chooser.custom {
		if message.Keystroke() == "enter" {
			month, err := time.Parse("2006-01", chooser.input.Value())
			if err != nil || month.Year() < 1 {
				chooser.err = "Enter a valid month as YYYY-MM"
				return nil
			}
			model.applyCalendarMonth(month)
			return nil
		}
		var command tea.Cmd
		*chooser.input, command = chooser.input.Update(message)
		chooser.err = ""
		return command
	}
	switch message.Keystroke() {
	case "down", "j", "tab":
		chooser.selected = (chooser.selected + 1) % 4
	case "up", "k", "shift+tab":
		chooser.selected = (chooser.selected + 3) % 4
	case "enter":
		switch chooser.selected {
		case 0:
			model.applyCalendarMonth(chooser.now)
		case 1:
			model.applyCalendarMonth(time.Date(chooser.now.Year(), chooser.now.Month()-1, 1, 0, 0, 0, 0, chooser.now.Location()))
		case 2:
			chooser.custom = true
			input := textinput.New()
			chooser.input = &input
			chooser.input.Prompt = ""
			chooser.input.Placeholder = chooser.now.Format("2006-01")
			chooser.input.CharLimit = len("YYYY-MM")
			chooser.input.SetWidth(20)
			return chooser.input.Focus()
		case 3:
			model.applyTimeRange(nil)
		}
	}
	return nil
}

func (model *Model) applyCalendarMonth(month time.Time) {
	dateRange, err := calendarMonthRange(month)
	if err != nil {
		model.timeChooser.err = "Choose a month between 0001-01 and 9999-12"
		return
	}
	model.applyTimeRange(&dateRange)
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
	if month, ok := calendarMonth(model.session.DateRange); ok {
		dateRange, err := calendarMonthRange(month.AddDate(0, delta, 0))
		if err == nil {
			model.applyTimeRange(&dateRange)
		}
		return
	}
	if model.session.NavigatePeriod(delta) {
		model.resetAndRefresh()
	}
}

func calendarMonthRange(month time.Time) (domain.DateRange, error) {
	start, err := domain.NewDate(month.Year(), month.Month(), 1)
	if err != nil {
		return domain.DateRange{}, err
	}
	last := time.Date(month.Year(), month.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	end, err := domain.NewDate(last.Year(), last.Month(), last.Day())
	return domain.DateRange{Start: start, End: end}, err
}

func calendarMonth(dateRange *domain.DateRange) (time.Time, bool) {
	if dateRange == nil || dateRange.Start.Day() != 1 {
		return time.Time{}, false
	}
	month := time.Date(dateRange.Start.Year(), dateRange.Start.Month(), 1, 0, 0, 0, 0, time.UTC)
	fullMonth, err := calendarMonthRange(month)
	return month, err == nil && fullMonth == *dateRange
}

func (model Model) renderTimeChooser(screen *RenderedScreen) {
	palette := model.palette
	palette.Text.Background = palette.Panel.Background
	palette.Muted.Background = palette.Panel.Background
	palette.Warning.Background = palette.Panel.Background
	rect := responsiveOverlayRect(model.width, model.height, 56, 15)
	fillRect(&screen.Frame, rect, palette.Panel)
	title := "Choose time"
	if model.timeChooser.custom {
		title = "Choose month"
	}
	overlayTitle(&screen.Frame, rect, title, palette.Heading)
	x, width := rect.X+2, rect.Width-4
	if model.timeChooser.custom {
		screen.Frame.PutText(x, rect.Y+3, "Month (YYYY-MM)", palette.Text)
		value := model.timeChooser.input.Value()
		if value == "" {
			value = model.timeChooser.input.Placeholder
		}
		screen.Frame.PutText(x, rect.Y+5, padRight(value, 20), palette.Selection)
		if model.timeChooser.err != "" {
			screen.Frame.PutText(x, rect.Y+8, Truncate(model.timeChooser.err, width), palette.Warning)
		}
	} else {
		now := model.timeChooser.now
		last := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location())
		for index, choice := range []string{
			"This month     " + now.Format("Jan 2006"),
			"Last month     " + last.Format("Jan 2006"),
			"Choose month…",
			"All time",
		} {
			filterLine(&screen.Frame, x, rect.Y+3+index*2, width, model.timeChooser.selected == index, padRight(choice, width-2), "", palette)
		}
	}
	footer := "↑/↓ Choose   Enter Apply   Esc Cancel"
	if model.timeChooser.custom {
		footer = "Enter Apply   Esc Cancel"
	}
	putCentered(&screen.Frame, Rect{X: rect.X, Y: rect.Y + rect.Height - 2, Width: rect.Width, Height: 1}, footer, palette.Muted)
	screen.Regions = append(screen.Regions, NamedRegion{Name: "time_chooser", Rect: rect})
}
