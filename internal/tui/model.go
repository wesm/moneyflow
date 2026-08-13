package tui

import (
	"errors"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	minimumWidth  = 80
	minimumHeight = 24
)

// Options contains presentation choices resolved at the process boundary.
type Options struct {
	Theme     ThemeName
	ColorMode ColorMode
}

type overlayKind uint8

const (
	overlayNone overlayKind = iota
	overlaySearch
	overlayFilters
	overlayHelp
)

type searchState struct {
	input           textinput.Model
	originalSession app.Session
	originalCursor  int
	originalScroll  int
	err             string
}

// Model owns terminal-only state around a renderer-neutral application session.
type Model struct {
	service  *app.Service
	session  app.Session
	options  Options
	palette  Palette
	bindings []binding
	result   domain.QueryResult
	width    int
	height   int
	cursor   int
	scroll   int
	status   string
	err      error
	overlay  overlayKind
	search   searchState
	filters  filterState
	help     helpState
}

// NewModel validates presentation options and evaluates the initial session.
func NewModel(service *app.Service, session app.Session, options Options) (Model, error) {
	if service == nil {
		return Model{}, errors.New("new model: service is nil")
	}
	if options.Theme == "" {
		options.Theme = ThemeDefault
	}
	if options.ColorMode == "" {
		options.ColorMode = ColorModeNone
	}
	palette, err := PaletteFor(options.Theme, options.ColorMode)
	if err != nil {
		return Model{}, err
	}
	result, err := service.Query(session)
	if err != nil {
		return Model{}, err
	}
	return Model{
		service:  service,
		session:  session,
		options:  options,
		palette:  palette,
		bindings: defaultBindings(),
		result:   result,
		width:    minimumWidth,
		height:   minimumHeight,
	}, nil
}

// Init has no asynchronous work in the fixture-backed slice.
func (model Model) Init() tea.Cmd { return nil }

// View renders the owned cell frame into Bubble Tea's alternate screen.
func (model Model) View() tea.View {
	view := tea.NewView(model.RenderScreen().Frame.RenderANSI())
	view.AltScreen = true
	return view
}

func (model *Model) refresh() {
	result, err := model.service.Query(model.session)
	model.err = err
	if err == nil {
		model.result = result
	}
	model.clampCursor()
}

func (model *Model) refreshPreserving(identity string) {
	model.refresh()
	if identity == "" {
		return
	}
	for index := 0; index < model.rowCount(); index++ {
		if model.rowIdentity(index) == identity {
			model.cursor = index
			model.ensureCursorVisible()
			return
		}
	}
}

func (model *Model) rowCount() int {
	if model.result.DetailRows != nil {
		return len(model.result.DetailRows)
	}
	return len(model.result.AggregateRows)
}

func (model *Model) rowIdentity(index int) string {
	if index < 0 || index >= model.rowCount() {
		return ""
	}
	if model.result.DetailRows != nil {
		return model.result.DetailRows[index].Transaction.ID
	}
	return app.AggregateIdentity(model.result.AggregateRows[index])
}

func (model *Model) clampCursor() {
	count := model.rowCount()
	if count == 0 {
		model.cursor, model.scroll = 0, 0
		return
	}
	if model.cursor < 0 {
		model.cursor = 0
	}
	if model.cursor >= count {
		model.cursor = count - 1
	}
	model.ensureCursorVisible()
}

func (model *Model) ensureCursorVisible() {
	visible := model.visibleRows()
	if visible < 1 {
		visible = 1
	}
	if model.cursor < model.scroll {
		model.scroll = model.cursor
	}
	if model.cursor >= model.scroll+visible {
		model.scroll = model.cursor - visible + 1
	}
	maximum := model.rowCount() - visible
	if maximum < 0 {
		maximum = 0
	}
	if model.scroll > maximum {
		model.scroll = maximum
	}
	if model.scroll < 0 {
		model.scroll = 0
	}
}

func (model Model) visibleRows() int {
	if model.height < minimumHeight {
		return 0
	}
	return model.height - 7
}
