package tui

import (
	"context"
	"errors"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

const (
	minimumWidth  = 80
	minimumHeight = 24
)

// Options contains presentation and initial-view choices resolved at the process boundary.
type Options struct {
	Theme            ThemeName
	ColorMode        ColorMode
	Version          string
	InitialDateRange *domain.DateRange
	ProfileRoot      string
	Temporary        bool
	EncodeViewQuery  ViewQueryEncoder
	// Now supplies renderer-local scheduling time. The application owns durable refresh time.
	Now func() time.Time
}

type overlayKind uint8

const (
	overlayNone overlayKind = iota
	overlaySearch
	overlayFilters
	overlayHelp
	overlayTransactionInfo
	overlayMerchantEditor
	overlayCategoryEditor
	overlayCategoryManager
	overlayGroupManager
	overlayReview
	overlayProviderConfirmation
	overlayProviderWrite
	overlayDuplicates
	overlayDeleteConfirmation
	overlayExport
	overlayQuit
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
	ctx                context.Context
	service            *app.Service
	session            app.Session
	options            Options
	palette            Palette
	bindings           []binding
	result             domain.QueryResult
	width              int
	height             int
	cursor             int
	scroll             int
	status             string
	err                error
	overlay            overlayKind
	search             searchState
	filters            filterState
	help               helpState
	transactionInfo    transactionInfoState
	merchant           merchantEditorState
	category           categoryEditorState
	categoryManager    taxonomyManagerState
	groupManager       taxonomyManagerState
	review             reviewState
	quit               quitState
	pending            app.PendingSummary
	caps               map[app.ActionID]app.Capability
	selection          app.SelectionValue
	provider           providerTUIState
	providerWrite      providerWriteTUIState
	duplicates         duplicateState
	deleteConfirmation deleteConfirmationState
	export             exportState
	profileKind        string
	amazonMatchColumn  bool
	amazonMatches      map[string]*app.AmazonMatchIndicator
	now                func() time.Time
	clockAt            time.Time
}

// NewModel validates presentation options and evaluates the initial session.
func NewModel(ctx context.Context, service *app.Service, session app.Session, options Options) (Model, error) {
	if ctx == nil {
		return Model{}, errors.New("new model: context is nil")
	}
	if service == nil {
		return Model{}, errors.New("new model: service is nil")
	}
	if options.Theme == "" {
		options.Theme = ThemeDefault
	}
	if options.ColorMode == "" {
		options.ColorMode = ColorModeNone
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	palette, err := PaletteFor(options.Theme, options.ColorMode)
	if err != nil {
		return Model{}, err
	}
	if _, err = service.Refresh(ctx); err != nil {
		return Model{}, err
	}
	result, err := service.QueryContext(ctx, session)
	if err != nil {
		return Model{}, err
	}
	model := Model{
		ctx:         ctx,
		service:     service,
		session:     session,
		options:     options,
		palette:     palette,
		bindings:    defaultBindings(),
		result:      result,
		width:       minimumWidth,
		height:      minimumHeight,
		selection:   app.EmptySelection(),
		now:         options.Now,
		clockAt:     options.Now(),
		profileKind: service.ProfileKind(),
	}
	model.syncProfileMetadata()
	model.refreshAmazonPresentation()
	return model, nil
}

// Init starts the renderer clock and the bounded provider loop when refresh is available.
func (model Model) Init() tea.Cmd {
	if model.profileKind == "amazon" {
		return clockTickCommand()
	}
	if _, available := model.capability(app.ActionRefreshProvider); !available &&
		model.providerWrite.status.Phase == "" {
		return clockTickCommand()
	}
	return tea.Batch(clockTickCommand(), model.providerStatusCommand(model.now()))
}

// View renders the owned cell frame into Bubble Tea's alternate screen.
func (model Model) View() tea.View {
	view := tea.NewView(model.RenderScreen().Frame.RenderANSI())
	view.AltScreen = true
	return view
}

func (model *Model) refresh() {
	_, err := model.service.Refresh(model.ctx)
	if err != nil {
		model.err = err
		model.clampCursor()
		return
	}
	result, err := model.service.QueryContext(model.ctx, model.session)
	model.err = err
	if err == nil {
		model.result = result
	}
	model.syncProfileMetadata()
	model.refreshAmazonPresentation()
	model.clampCursor()
}

func (model *Model) refreshAmazonPresentation() {
	model.amazonMatchColumn = false
	model.amazonMatches = nil
	if model.result.DetailRows == nil || model.profileKind == "amazon" {
		return
	}
	transactions := make([]domain.Transaction, len(model.result.DetailRows))
	for index, row := range model.result.DetailRows {
		transactions[index] = row.Transaction
	}
	visible, indicators, err := model.service.AmazonMatchIndicators(model.ctx, transactions)
	if err != nil {
		model.status = "Amazon matches could not be loaded."
		return
	}
	model.amazonMatchColumn = visible
	model.amazonMatches = indicators
}

func (model *Model) syncProfileMetadata() {
	model.profileKind = model.service.ProfileKind()
	for index := range model.bindings {
		if model.bindings[index].action == app.ActionRefreshProvider {
			model.bindings[index].description = model.actionDescription(app.ActionRefreshProvider)
		}
	}
	capabilities := model.service.CapabilitiesForState(model.session.ViewState())
	model.caps = make(map[app.ActionID]app.Capability, len(capabilities))
	for _, capability := range capabilities {
		model.caps[capability.Action] = capability
	}
	model.pending = model.service.Pending()
	if model.profileKind != "amazon" {
		connection, err := model.service.ProviderConnection(model.ctx)
		model.provider.bound = err == nil && connection.Bound
		if status, statusErr := model.service.ProviderWriteStatus(model.ctx); statusErr == nil {
			model.providerWrite.status = status
		}
		if _, available := model.capability(app.ActionRefreshProvider); available {
			if status, statusErr := model.service.ProviderStatus(model.ctx); statusErr == nil {
				model.provider.status = status
			}
		}
	}
}

func (model Model) capability(action app.ActionID) (app.Capability, bool) {
	capability, exists := model.caps[action]
	return capability, exists && capability.Available
}

func capabilityMessage(capability app.Capability) string {
	if capability.Reason != "" {
		return capability.Reason
	}
	return "This action is not available for the current profile."
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
