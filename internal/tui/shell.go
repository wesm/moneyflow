package tui

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

type shellScreen uint8

const (
	shellSelector shellScreen = iota + 1
	shellProvider
	shellName
	shellRecovery
	shellOnboarding
	shellFinance
)

// CatalogView supplies local-only profile rows without opening financial data.
type CatalogView interface {
	List(context.Context) ([]profilecatalog.Entry, error)
}

// ProfileLifecycle supplies durable create, rollback, and recovery operations.
type ProfileLifecycle interface {
	ActivateForProvider(context.Context, string, string) (profilecatalog.Entry, error)
	Create(context.Context, profilecatalog.CreateRequest) (profilecatalog.Entry, error)
	CancelNewProfile(context.Context, string) (bool, error)
	RecoveryPlan(context.Context, string) (profilecatalog.RecoveryPlan, error)
	Recreate(context.Context, profilecatalog.RecoveryRequest) (profilecatalog.RecoveryResult, error)
}

// OnboardingView is the renderer-neutral attempt surface used by later shell screens.
type OnboardingView interface {
	Start(context.Context, onboarding.StartRequest) (onboarding.Snapshot, error)
	Status(context.Context, onboarding.StatusRequest) (onboarding.Snapshot, error)
	Submit(context.Context, onboarding.SubmitRequest) (onboarding.Snapshot, error)
	Cancel(context.Context, onboarding.CancelRequest) (onboarding.Snapshot, error)
	TakeOpenedProfile(context.Context, onboarding.StatusRequest) (onboarding.OpenedProfile, error)
}

// ShellOpenedProfile is one profile-scoped finance service owned by the shell.
type ShellOpenedProfile struct {
	ID        string
	Paths     home.Paths
	Service   *app.Service
	Temporary bool
	Close     func() error
}

// ShellProfileOpener opens one selected profile while preserving its lifecycle lock.
type ShellProfileOpener func(context.Context, string) (ShellOpenedProfile, error)

// ShellDemoOpener opens one uniquely temporary synthetic profile.
type ShellDemoOpener func(context.Context) (ShellOpenedProfile, error)

// ShellDependencies are application boundaries injected by the command composition root.
type ShellDependencies struct {
	Catalog     CatalogView
	Profiles    ProfileLifecycle
	OpenProfile ShellProfileOpener
	OpenDemo    ShellDemoOpener
	Onboarding  OnboardingView
	Preselected *ShellOpenedProfile
}

type shellOwnedProfile struct {
	profile ShellOpenedProfile
	once    sync.Once
	err     error
}

func (owned *shellOwnedProfile) close() error {
	if owned == nil {
		return nil
	}
	owned.once.Do(func() {
		if owned.profile.Close != nil {
			owned.err = owned.profile.Close()
		}
	})
	return owned.err
}

// Shell owns profile-neutral navigation around the existing finance model.
type Shell struct {
	ctx            context.Context
	dependencies   ShellDependencies
	options        Options
	initialSession app.Session
	palette        Palette
	screen         shellScreen
	entries        []profilecatalog.Entry
	selector       profileSelectorState
	providers      providerSelectorState
	selected       *profilecatalog.Entry
	name           profileNameState
	recovery       profileRecoveryState
	createdID      string
	snapshot       onboarding.Snapshot
	haveSnapshot   bool
	settings       settingsForm
	unlock         unlockForm
	credentials    credentialForm
	canceling      bool
	cancelQueued   bool
	requestID      uint64
	resume         *financeResumeState
	finance        *Model
	opened         *shellOwnedProfile
	width          int
	height         int
	status         string
	err            error
}

type financeResumeState struct {
	profileID string
	session   app.Session
	cursor    int
	scroll    int
}

// String keeps generic model diagnostics free of profile and credential values.
func (shell Shell) String() string {
	return fmt.Sprintf("Moneyflow TUI shell (screen=%d, size=%dx%d)", shell.screen, shell.width, shell.height)
}

// GoString keeps %#v diagnostics free of profile and credential values.
func (shell Shell) GoString() string {
	return fmt.Sprintf("tui.Shell{screen:%d, width:%d, height:%d}", shell.screen, shell.width, shell.height)
}

type switchProfileMsg struct{}

type shellProfileOpenedMsg struct {
	profile ShellOpenedProfile
	resume  *financeResumeState
	guard   shellRequestGuard
	err     error
}

type shellProfileCreatedMsg struct {
	entry profilecatalog.Entry
	guard shellRequestGuard
	err   error
}

type shellProfileCanceledMsg struct {
	removed bool
	guard   shellRequestGuard
	err     error
}

type shellRecoveryPlanMsg struct {
	plan  profilecatalog.RecoveryPlan
	guard shellRequestGuard
	err   error
}

type shellProfileRecreatedMsg struct {
	result profilecatalog.RecoveryResult
	guard  shellRequestGuard
	err    error
}

type shellOnboardingSnapshotMsg struct {
	snapshot onboarding.Snapshot
	entry    *profilecatalog.Entry
	guard    *onboardingPollGuard
	start    *shellRequestGuard
	err      error
}

type shellRequestGuard struct {
	id         uint64
	screen     shellScreen
	profileKey string
}

type onboardingPollGuard struct {
	attemptID    string
	stateVersion uint64
}

type shellOnboardingPollMsg struct {
	guard onboardingPollGuard
}

type shellOnboardingOpenedMsg struct {
	profile ShellOpenedProfile
	guard   onboardingPollGuard
	err     error
}

type shellOnboardingCancelMsg struct {
	snapshot onboarding.Snapshot
	attempt  string
	err      error
}

const onboardingPollInterval = 100 * time.Millisecond

// NewShell validates dependencies and starts either at selection or a preselected finance view.
func NewShell(ctx context.Context, dependencies ShellDependencies, options Options) (Shell, error) {
	if ctx == nil {
		return Shell{}, errors.New("new TUI shell: context is nil")
	}
	if options.Theme == "" {
		options.Theme = ThemeDefault
	}
	if options.ColorMode == "" {
		options.ColorMode = ColorModeNone
	}
	palette, err := PaletteFor(options.Theme, options.ColorMode)
	if err != nil {
		return Shell{}, err
	}
	initialSession := app.NewSession()
	if err = initialSession.SetFilters(app.Filters{
		DateRange:     options.InitialDateRange,
		ShowHidden:    initialSession.ShowHidden,
		ShowTransfers: initialSession.ShowTransfers,
	}); err != nil {
		return Shell{}, fmt.Errorf("new TUI shell: initial filters: %w", err)
	}
	shell := Shell{
		ctx: ctx, dependencies: dependencies, options: options, palette: palette,
		initialSession: initialSession,
		screen:         shellSelector, width: minimumWidth, height: minimumHeight,
	}
	if dependencies.Preselected != nil {
		if err = shell.enterFinance(*dependencies.Preselected, shell.initialSession.Clone()); err != nil {
			return Shell{}, err
		}
		return shell, nil
	}
	if dependencies.Catalog == nil || dependencies.Profiles == nil || dependencies.Onboarding == nil ||
		dependencies.OpenProfile == nil || dependencies.OpenDemo == nil {
		return Shell{}, errors.New("new TUI shell: dependencies are incomplete")
	}
	shell.entries, err = dependencies.Catalog.List(ctx)
	if err != nil {
		return Shell{}, err
	}
	shell.selector = newProfileSelector(shell.entries)
	return shell, nil
}

// Init delegates only to an active finance model.
func (shell Shell) Init() tea.Cmd {
	if shell.screen == shellFinance && shell.finance != nil {
		return shell.finance.Init()
	}
	return nil
}

// Update routes terminal events to the current child state.
func (shell Shell) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case shellProfileOpenedMsg:
		if !shell.acceptsShellRequest(message.guard) {
			if message.profile.Close != nil {
				shell.err = message.profile.Close()
			}
			return shell, nil
		}
		if message.err != nil {
			shell.status = "The selected profile could not be opened."
			shell.selector.status = shell.status
			return shell, nil
		}
		session := shell.initialSession.Clone()
		if message.resume != nil {
			session = message.resume.session
		}
		if err := shell.enterFinance(message.profile, session); err != nil {
			shell.status = "The selected profile could not be opened."
			shell.err = err
			return shell, nil
		}
		if message.resume != nil {
			shell.finance.cursor = message.resume.cursor
			shell.finance.scroll = message.resume.scroll
			shell.finance.clampCursor()
			shell.resume = nil
		}
		updated, command := shell.finance.Update(tea.WindowSizeMsg{Width: shell.width, Height: shell.height})
		finance := updated.(Model)
		shell.finance = &finance
		return shell, tea.Batch(command, shell.finance.Init())
	case shellProfileCreatedMsg:
		if !shell.acceptsShellRequest(message.guard) {
			return shell, nil
		}
		shell.name.busy = false
		if message.err != nil {
			shell.name.status = profileCreateMessage(message.err)
			return shell, nil
		}
		entry := message.entry
		shell.selected = &entry
		shell.createdID = entry.ID
		shell.screen = shellOnboarding
		shell.status = "Continue setting up " + entry.DisplayName + "."
		return shell, shell.beginOnboarding(entry)
	case shellProfileCanceledMsg:
		if !shell.acceptsShellRequest(message.guard) {
			return shell, nil
		}
		shell.selector.status = ""
		if message.err != nil {
			shell.status = "The incomplete profile could not be removed."
			shell.err = message.err
		} else if message.removed {
			shell.status = "Incomplete profile removed."
		} else {
			shell.status = "Profile kept because setup created durable state."
		}
		shell.createdID = ""
		shell.selected = nil
		shell.screen = shellSelector
		shell.refreshEntries()
		return shell, nil
	case shellRecoveryPlanMsg:
		if !shell.acceptsShellRequest(message.guard) {
			return shell, nil
		}
		if message.err != nil {
			shell.recovery.status = recoveryMessage(message.err)
			shell.err = message.err
			return shell, nil
		}
		shell.recovery.applyPlan(message.plan)
		return shell, nil
	case shellProfileRecreatedMsg:
		if !shell.acceptsShellRequest(message.guard) {
			return shell, nil
		}
		shell.recovery.busy = false
		if message.err != nil {
			shell.recovery.status = recoveryMessage(message.err)
			shell.err = message.err
			return shell, nil
		}
		if shell.selected != nil {
			entry := *shell.selected
			entry.Status = profilecatalog.StatusSetupIncomplete
			if shell.recovery.plan != nil {
				entry.ID = shell.recovery.plan.ProfileID
			}
			shell.selected = &entry
		}
		shell.status = "Profile recreated. The previous database is in the backup at " + message.result.BackupPath
		shell.screen = shellOnboarding
		if shell.selected == nil {
			return shell, nil
		}
		return shell, shell.beginOnboarding(*shell.selected)
	case shellOnboardingSnapshotMsg:
		if message.start != nil && !shell.acceptsShellRequest(*message.start) {
			return shell, nil
		}
		if message.guard != nil && !shell.acceptsOnboardingGuard(*message.guard) {
			return shell, nil
		}
		if message.err != nil {
			if message.start != nil {
				return shell.handleOnboardingStartFailure(message.err)
			}
			shell.status = "Profile setup could not continue."
			shell.err = message.err
			return shell, nil
		}
		if message.entry != nil {
			entry := *message.entry
			shell.selected = &entry
		}
		if message.snapshot.ProtocolVersion != onboarding.ProtocolVersion {
			shell.status = "Profile setup requires another Moneyflow version."
			return shell, nil
		}
		shell.applyOnboardingSnapshot(message.snapshot)
		return shell.nextOnboardingStep()
	case shellOnboardingCancelMsg:
		if shell.screen != shellOnboarding || !shell.haveSnapshot ||
			shell.snapshot.AttemptID != message.attempt {
			return shell, nil
		}
		shell.canceling = false
		if message.err != nil {
			if onboarding.CodeOf(message.err) == onboarding.CodeOnboardingStale {
				shell.cancelQueued = true
				guard := onboardingPollGuard{
					attemptID:    shell.snapshot.AttemptID,
					stateVersion: shell.snapshot.StateVersion,
				}
				return shell, shell.pollOnboarding(guard)
			}
			shell.cancelQueued = false
			shell.status = "Profile setup could not be canceled. Try again."
			shell.err = message.err
			return shell, nil
		}
		shell.cancelQueued = false
		shell.applyOnboardingSnapshot(message.snapshot)
		return shell.nextOnboardingStep()
	case shellOnboardingPollMsg:
		if !shell.acceptsOnboardingGuard(message.guard) {
			return shell, nil
		}
		return shell, shell.pollOnboarding(message.guard)
	case shellOnboardingOpenedMsg:
		if !shell.acceptsCompletedProfile(message.guard) {
			if message.profile.Close != nil {
				shell.err = message.profile.Close()
			}
			return shell, nil
		}
		if message.err != nil {
			shell.status = "The connected profile could not enter the finance view."
			shell.err = message.err
			return shell, nil
		}
		session := shell.initialSession.Clone()
		if shell.resume != nil {
			session = shell.resume.session
		}
		if err := shell.enterFinance(message.profile, session); err != nil {
			shell.status = "The connected profile could not enter the finance view."
			shell.err = err
			return shell, nil
		}
		if shell.resume != nil {
			shell.finance.cursor = shell.resume.cursor
			shell.finance.scroll = shell.resume.scroll
			shell.finance.clampCursor()
		}
		shell.resume = nil
		shell.createdID = ""
		shell.selected = nil
		shell.haveSnapshot = false
		shell.canceling = false
		shell.cancelQueued = false
		updated, command := shell.finance.Update(tea.WindowSizeMsg{Width: shell.width, Height: shell.height})
		finance := updated.(Model)
		shell.finance = &finance
		return shell, tea.Batch(command, shell.finance.Init())
	case switchProfileMsg:
		shell.invalidateShellRequests()
		shell.leaveFinance()
		return shell, nil
	case tea.WindowSizeMsg:
		shell.width = max(message.Width, 0)
		shell.height = max(message.Height, 0)
		if shell.finance != nil {
			updated, command := shell.finance.Update(message)
			finance := updated.(Model)
			shell.finance = &finance
			return shell, command
		}
		return shell, nil
	case tea.KeyPressMsg:
		if message.Keystroke() == "ctrl+c" {
			shell.err = shell.Close()
			return shell, tea.Quit
		}
		if shell.screen == shellSelector && (message.Keystroke() == "esc" || message.Keystroke() == "q") {
			selection := shell.selector.update(message)
			return shell.routeProfileSelection(selection)
		}
		if shell.screen == shellSelector {
			selection := shell.selector.update(message)
			return shell.routeProfileSelection(selection)
		}
		if shell.screen == shellProvider {
			selection := shell.providers.update(message)
			if selection.back {
				shell.invalidateShellRequests()
				shell.screen = shellSelector
			} else if selection.provider == providerMonarch {
				shell.screen = shellName
				shell.name, _ = newProfileNameState()
				shell.status = ""
			}
			return shell, nil
		}
		if shell.screen == shellName {
			if shell.name.busy {
				return shell, nil
			}
			if message.Keystroke() == "esc" {
				shell.invalidateShellRequests()
				shell.screen = shellProvider
				return shell, nil
			}
			var name string
			var command tea.Cmd
			shell.name, name, command = shell.name.update(message)
			if name != "" {
				shell.name.busy = true
				guard := shell.beginShellRequest(shellName, "")
				return shell, func() tea.Msg {
					entry, err := shell.dependencies.Profiles.Create(shell.ctx, profilecatalog.CreateRequest{
						DisplayName: name, ProviderKind: "monarch",
					})
					return shellProfileCreatedMsg{entry: entry, guard: guard, err: err}
				}
			}
			return shell, command
		}
		if shell.screen == shellRecovery {
			return shell.routeRecoveryKey(message)
		}
		if shell.screen == shellOnboarding && message.Keystroke() == "esc" {
			return shell.cancelOnboarding()
		}
		if shell.screen == shellOnboarding {
			return shell.routeOnboardingKey(message)
		}
		if message.Keystroke() == "esc" && shell.screen != shellFinance {
			shell.invalidateShellRequests()
			shell.screen = shellSelector
			shell.selected = nil
			return shell, nil
		}
	}
	if shell.screen == shellFinance && shell.finance != nil {
		updated, command := shell.finance.Update(message)
		finance := updated.(Model)
		shell.finance = &finance
		if shell.finance.provider.reconnectRequested {
			return shell.startFinanceReconnect()
		}
		return shell, command
	}
	return shell, nil
}

func (shell Shell) routeProfileSelection(selection profileSelection) (tea.Model, tea.Cmd) {
	switch selection.action {
	case selectorNone:
		return shell, nil
	case selectorExit:
		shell.err = shell.Close()
		return shell, tea.Quit
	case selectorDemo:
		guard := shell.beginShellRequest(shellSelector, "")
		return shell, func() tea.Msg {
			profile, err := shell.dependencies.OpenDemo(shell.ctx)
			return shellProfileOpenedMsg{profile: profile, guard: guard, err: err}
		}
	case selectorAdd:
		shell.invalidateShellRequests()
		shell.providers = newProviderSelector()
		shell.screen = shellProvider
		return shell, nil
	case selectorOpen:
		profileID := entrySelector(selection.entry)
		guard := shell.beginShellRequest(shellSelector, profileID)
		return shell, func() tea.Msg {
			profile, err := shell.dependencies.OpenProfile(shell.ctx, profileID)
			return shellProfileOpenedMsg{profile: profile, guard: guard, err: err}
		}
	case selectorOnboarding:
		entry := selection.entry
		shell.selected = &entry
		shell.screen = shellOnboarding
		shell.status = "Profile setup will continue here."
		return shell, shell.beginOnboarding(entry)
	case selectorLocalOnly:
		shell.invalidateShellRequests()
		entry := selection.entry
		shell.selected = &entry
		shell.screen = shellRecovery
		shell.recovery = newProfileRecoveryState(entry)
		shell.status = ""
	case selectorRecovery:
		entry := selection.entry
		shell.selected = &entry
		shell.screen = shellRecovery
		shell.recovery = newProfileRecoveryState(entry)
		selector := entry.Key
		if selector == "" {
			selector = entry.ID
		}
		guard := shell.beginShellRequest(shellRecovery, selector)
		return shell, func() tea.Msg {
			plan, err := shell.dependencies.Profiles.RecoveryPlan(shell.ctx, selector)
			return shellRecoveryPlanMsg{plan: plan, guard: guard, err: err}
		}
	case selectorGuidance:
		shell.invalidateShellRequests()
		entry := selection.entry
		shell.selected = &entry
		shell.screen = shellRecovery
		shell.recovery = newProfileRecoveryState(entry)
		if entry.Status == profilecatalog.StatusRequiresNewer {
			shell.status = "This profile requires a newer Moneyflow. No data was changed."
		} else {
			shell.status = "This profile metadata requires another Moneyflow version. No data was changed."
		}
	}
	return shell, nil
}

func (shell *Shell) beginOnboarding(entry profilecatalog.Entry) tea.Cmd {
	selector := entrySelector(entry)
	guard := shell.beginShellRequest(shellOnboarding, selector)
	return func() tea.Msg {
		if entry.ID == "" {
			activated, err := shell.dependencies.Profiles.ActivateForProvider(
				shell.ctx, selector, "monarch",
			)
			if err != nil {
				return shellOnboardingSnapshotMsg{start: &guard, err: err}
			}
			entry = activated
		}
		profileID := entry.ID
		if profileID == "" {
			return shellOnboardingSnapshotMsg{
				start: &guard, err: errors.New("profile setup ID is unavailable"),
			}
		}
		snapshot, err := shell.dependencies.Onboarding.Start(shell.ctx, onboarding.StartRequest{
			ProfileID: profileID, Renderer: "tui",
		})
		return shellOnboardingSnapshotMsg{snapshot: snapshot, entry: &entry, start: &guard, err: err}
	}
}

func (shell *Shell) beginShellRequest(screen shellScreen, profileKey string) shellRequestGuard {
	shell.requestID++
	return shellRequestGuard{id: shell.requestID, screen: screen, profileKey: profileKey}
}

func (shell *Shell) invalidateShellRequests() {
	shell.requestID++
}

func (shell Shell) acceptsShellRequest(guard shellRequestGuard) bool {
	if guard.id == 0 || guard.id != shell.requestID || guard.screen != shell.screen {
		return false
	}
	if guard.profileKey == "" {
		return true
	}
	if shell.selected != nil && entrySelector(*shell.selected) == guard.profileKey {
		return true
	}
	return shell.screen == shellSelector
}

func (shell Shell) acceptsOnboardingGuard(guard onboardingPollGuard) bool {
	return shell.screen == shellOnboarding && shell.haveSnapshot &&
		shell.snapshot.AttemptID == guard.attemptID &&
		shell.snapshot.StateVersion == guard.stateVersion
}

func (shell Shell) acceptsCompletedProfile(guard onboardingPollGuard) bool {
	return shell.acceptsOnboardingGuard(guard) && shell.snapshot.State == onboarding.StateComplete &&
		!shell.canceling && !shell.cancelQueued
}

func (shell Shell) pollOnboarding(guard onboardingPollGuard) tea.Cmd {
	request := onboarding.StatusRequest{
		ProfileID: shell.snapshot.ProfileID, AttemptID: shell.snapshot.AttemptID,
	}
	return func() tea.Msg {
		snapshot, err := shell.dependencies.Onboarding.Status(shell.ctx, request)
		return shellOnboardingSnapshotMsg{snapshot: snapshot, guard: &guard, err: err}
	}
}

func (shell Shell) nextOnboardingStep() (tea.Model, tea.Cmd) {
	if shell.cancelQueued && !shell.canceling {
		return shell.cancelOnboarding()
	}
	switch shell.snapshot.State {
	case onboarding.StateInspect, onboarding.StateValidateSession,
		onboarding.StateAuthenticating, onboarding.StateImporting:
		guard := onboardingPollGuard{
			attemptID: shell.snapshot.AttemptID, stateVersion: shell.snapshot.StateVersion,
		}
		return shell, tea.Tick(onboardingPollInterval, func(time.Time) tea.Msg {
			return shellOnboardingPollMsg{guard: guard}
		})
	case onboarding.StateComplete:
		guard := onboardingPollGuard{
			attemptID: shell.snapshot.AttemptID, stateVersion: shell.snapshot.StateVersion,
		}
		request := onboarding.StatusRequest{
			ProfileID: shell.snapshot.ProfileID, AttemptID: shell.snapshot.AttemptID,
		}
		return shell, func() tea.Msg {
			opened, err := shell.dependencies.Onboarding.TakeOpenedProfile(shell.ctx, request)
			return shellOnboardingOpenedMsg{
				profile: ShellOpenedProfile{
					ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close,
				},
				guard: guard, err: err,
			}
		}
	case onboarding.StateCanceled:
		return shell.finishCanceledOnboarding()
	default:
		return shell, nil
	}
}

func (shell Shell) cancelOnboarding() (tea.Model, tea.Cmd) {
	if shell.canceling {
		return shell, nil
	}
	if !shell.haveSnapshot || shell.snapshot.AttemptID == "" {
		shell.cancelQueued = true
		shell.status = "Cancellation requested; waiting for profile setup to start…"
		return shell, nil
	}
	shell.canceling = true
	shell.cancelQueued = true
	shell.status = "Cancellation requested; waiting for Monarch work to stop…"
	request := onboarding.CancelRequest{
		ProfileID: shell.snapshot.ProfileID, AttemptID: shell.snapshot.AttemptID,
		ExpectedStateVersion: shell.snapshot.StateVersion,
	}
	return shell, func() tea.Msg {
		snapshot, err := shell.dependencies.Onboarding.Cancel(shell.ctx, request)
		return shellOnboardingCancelMsg{snapshot: snapshot, attempt: request.AttemptID, err: err}
	}
}

func (shell Shell) handleOnboardingStartFailure(startErr error) (tea.Model, tea.Cmd) {
	shell.haveSnapshot = false
	shell.canceling = false
	shell.cancelQueued = false
	if shell.createdID != "" {
		profileID := shell.createdID
		shell.status = "Removing incomplete profile…"
		shell.err = startErr
		guard := shell.beginShellRequest(shellOnboarding, profileID)
		return shell, func() tea.Msg {
			removed, err := shell.dependencies.Profiles.CancelNewProfile(shell.ctx, profileID)
			return shellProfileCanceledMsg{removed: removed, guard: guard, err: err}
		}
	}
	if shell.resume != nil {
		resume := *shell.resume
		guard := shell.beginShellRequest(shellOnboarding, resume.profileID)
		return shell, func() tea.Msg {
			profile, err := shell.dependencies.OpenProfile(shell.ctx, resume.profileID)
			return shellProfileOpenedMsg{profile: profile, resume: &resume, guard: guard, err: err}
		}
	}
	shell.status = "Profile setup could not start."
	shell.err = startErr
	shell.selected = nil
	shell.screen = shellSelector
	shell.refreshEntries()
	return shell, nil
}

func (shell Shell) finishCanceledOnboarding() (tea.Model, tea.Cmd) {
	shell.canceling = false
	shell.cancelQueued = false
	shell.haveSnapshot = false
	if shell.createdID != "" {
		profileID := shell.createdID
		shell.status = "Removing incomplete profile…"
		guard := shell.beginShellRequest(shellOnboarding, profileID)
		return shell, func() tea.Msg {
			removed, err := shell.dependencies.Profiles.CancelNewProfile(shell.ctx, profileID)
			return shellProfileCanceledMsg{removed: removed, guard: guard, err: err}
		}
	}
	if shell.resume != nil {
		resume := *shell.resume
		shell.status = "Returning to the profile without reconnecting…"
		guard := shell.beginShellRequest(shellOnboarding, resume.profileID)
		return shell, func() tea.Msg {
			profile, err := shell.dependencies.OpenProfile(shell.ctx, resume.profileID)
			return shellProfileOpenedMsg{profile: profile, resume: &resume, guard: guard, err: err}
		}
	}
	shell.selected = nil
	shell.invalidateShellRequests()
	shell.screen = shellSelector
	shell.refreshEntries()
	return shell, nil
}

func (shell Shell) startFinanceReconnect() (tea.Model, tea.Cmd) {
	if shell.finance == nil || shell.opened == nil || shell.opened.profile.ID == "" {
		shell.status = "This profile cannot start reconnect from the current view."
		return shell, nil
	}
	resume := &financeResumeState{
		profileID: shell.opened.profile.ID,
		session:   shell.finance.session, cursor: shell.finance.cursor, scroll: shell.finance.scroll,
	}
	entry := profilecatalog.Entry{
		ID: resume.profileID, Key: resume.profileID,
		DisplayName: "Moneyflow profile", ProviderKind: "monarch",
	}
	for _, candidate := range shell.entries {
		if candidate.ID == resume.profileID || candidate.Key == resume.profileID {
			entry = candidate
			break
		}
	}
	if err := shell.Close(); err != nil {
		shell.status = "The profile could not be closed for reconnect."
		shell.err = err
		return shell, nil
	}
	shell.resume = resume
	shell.selected = &entry
	shell.screen = shellOnboarding
	shell.haveSnapshot = false
	shell.canceling = false
	shell.cancelQueued = false
	shell.status = "Reconnect Monarch to continue refreshing this profile."
	return shell, shell.beginOnboarding(entry)
}

func (shell *Shell) applyOnboardingSnapshot(snapshot onboarding.Snapshot) {
	shell.snapshot = snapshot
	shell.haveSnapshot = true
	shell.status = ""
	switch snapshot.State {
	case onboarding.StateSettingsRequired:
		shell.settings, _ = newSettingsForm()
		if snapshot.Failure != nil {
			shell.settings.status = snapshot.Failure.Message
		}
	case onboarding.StateUnlockRequired:
		shell.unlock, _ = newUnlockForm()
		if snapshot.Failure != nil {
			shell.unlock.status = snapshot.Failure.Message
		}
	case onboarding.StateCredentialsRequired:
		shell.credentials, _ = newCredentialForm()
		if snapshot.Failure != nil {
			shell.credentials.status = snapshot.Failure.Message
		}
	default:
		if snapshot.Failure != nil {
			shell.status = snapshot.Failure.Message
		}
	}
}

func (shell Shell) routeOnboardingKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !shell.haveSnapshot {
		return shell, nil
	}
	switch shell.snapshot.State {
	case onboarding.StateSettingsRequired:
		var submit *onboarding.SubmitRequest
		var command tea.Cmd
		shell.settings, submit, command = shell.settings.update(message)
		if submit == nil {
			return shell, command
		}
		request, ok := shell.settings.submit(shell.snapshot)
		if !ok {
			return shell, nil
		}
		return shell, shell.submitOnboarding(request)
	case onboarding.StateUnlockRequired:
		var submit bool
		var command tea.Cmd
		shell.unlock, submit, command = shell.unlock.update(message)
		if !submit {
			return shell, command
		}
		request, ok := shell.unlock.submit(shell.snapshot)
		if !ok {
			return shell, nil
		}
		return shell, shell.submitOnboarding(request)
	case onboarding.StateCredentialsRequired:
		var submit bool
		var command tea.Cmd
		shell.credentials, submit, command = shell.credentials.update(message)
		if !submit {
			return shell, command
		}
		request, ok := shell.credentials.submit(shell.snapshot)
		if !ok {
			return shell, nil
		}
		return shell, shell.submitOnboarding(request)
	case onboarding.StateFailed:
		if shell.snapshot.Failure != nil && shell.snapshot.Failure.CanRetry &&
			(message.Keystroke() == "enter" || message.Keystroke() == "r") {
			return shell, shell.submitOnboarding(onboarding.SubmitRequest{
				ProfileID: shell.snapshot.ProfileID, AttemptID: shell.snapshot.AttemptID,
				ExpectedStateVersion: shell.snapshot.StateVersion, Action: onboarding.ActionRetry,
			})
		}
	case onboarding.StateIdentityMismatch:
		if shell.snapshot.Failure != nil && shell.snapshot.Failure.CanReenter &&
			(message.Keystroke() == "enter" || message.Keystroke() == "r") {
			return shell, shell.submitOnboarding(onboarding.SubmitRequest{
				ProfileID: shell.snapshot.ProfileID, AttemptID: shell.snapshot.AttemptID,
				ExpectedStateVersion: shell.snapshot.StateVersion,
				Action:               onboarding.ActionReauthenticate,
			})
		}
	default:
		return shell, nil
	}
	return shell, nil
}

func (shell Shell) submitOnboarding(request onboarding.SubmitRequest) tea.Cmd {
	guard := onboardingPollGuard{
		attemptID: request.AttemptID, stateVersion: request.ExpectedStateVersion,
	}
	return func() tea.Msg {
		snapshot, err := shell.dependencies.Onboarding.Submit(shell.ctx, request)
		return shellOnboardingSnapshotMsg{snapshot: snapshot, guard: &guard, err: err}
	}
}

func (shell Shell) routeRecoveryKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if message.Keystroke() == "esc" {
		shell.invalidateShellRequests()
		shell.screen = shellSelector
		shell.selected = nil
		return shell, nil
	}
	if message.Keystroke() != "enter" || shell.selected == nil {
		return shell, nil
	}
	if shell.recovery.entry.Status == profilecatalog.StatusLocalOnly {
		profileID := entrySelector(*shell.selected)
		guard := shell.beginShellRequest(shellRecovery, profileID)
		return shell, func() tea.Msg {
			profile, err := shell.dependencies.OpenProfile(shell.ctx, profileID)
			return shellProfileOpenedMsg{profile: profile, guard: guard, err: err}
		}
	}
	if !shell.recovery.confirm() {
		return shell, nil
	}
	plan := *shell.recovery.plan
	shell.recovery.busy = true
	guard := shell.beginShellRequest(shellRecovery, entrySelector(*shell.selected))
	return shell, func() tea.Msg {
		result, err := shell.dependencies.Profiles.Recreate(shell.ctx, profilecatalog.RecoveryRequest{
			Plan: plan, Confirmed: true,
		})
		return shellProfileRecreatedMsg{result: result, guard: guard, err: err}
	}
}

func entrySelector(entry profilecatalog.Entry) string {
	if entry.ID != "" {
		return entry.ID
	}
	return entry.Key
}

func profileCreateMessage(err error) string {
	switch profilecatalog.CodeOf(err) {
	case profilecatalog.CodeProfileNameConflict:
		return "That profile name is already in use."
	case profilecatalog.CodeProfileBusy:
		return "The profile catalog is busy. Try again."
	default:
		return "The profile name could not be saved."
	}
}

func recoveryMessage(err error) string {
	switch profilecatalog.CodeOf(err) {
	case profilecatalog.CodeProfileBusy:
		return "This profile is open elsewhere. Close it before recovery."
	case profilecatalog.CodeRecoveryIncomplete:
		return "Profile recovery is incomplete. Restart Moneyflow to continue it."
	default:
		return "The profile could not be recreated. No data was discarded."
	}
}

// View renders the current child into the alternate screen.
func (shell Shell) View() tea.View {
	if shell.screen == shellFinance && shell.finance != nil {
		return shell.finance.View()
	}
	view := tea.NewView(shell.RenderScreen().Frame.RenderANSI())
	view.AltScreen = true
	return view
}

// RenderScreen returns a deterministic profile-neutral frame or the finance frame.
func (shell Shell) RenderScreen() RenderedScreen {
	if shell.screen == shellFinance && shell.finance != nil {
		return shell.finance.RenderScreen()
	}
	return shell.renderSelectorPlaceholder()
}

// Close releases the active profile at most once.
func (shell *Shell) Close() error {
	if shell == nil || shell.opened == nil {
		return nil
	}
	err := shell.opened.close()
	shell.opened = nil
	shell.finance = nil
	return err
}

func (shell *Shell) enterFinance(opened ShellOpenedProfile, session app.Session) error {
	if opened.Service == nil || opened.Close == nil {
		return errors.New("enter finance TUI: opened profile is incomplete")
	}
	options := shell.options
	options.ProfileRoot = opened.Paths.Root
	options.Temporary = opened.Temporary
	finance, err := NewModel(shell.ctx, opened.Service, session, options)
	if err != nil {
		return errors.Join(err, opened.Close())
	}
	shell.opened = &shellOwnedProfile{profile: opened}
	shell.finance = &finance
	shell.screen = shellFinance
	return nil
}

func (shell *Shell) leaveFinance() {
	if err := shell.Close(); err != nil {
		shell.status = "The profile could not be closed cleanly."
		shell.err = err
	}
	shell.screen = shellSelector
	shell.refreshEntries()
}

func (shell *Shell) refreshEntries() {
	if shell.dependencies.Catalog == nil {
		return
	}
	if entries, err := shell.dependencies.Catalog.List(shell.ctx); err == nil {
		shell.entries = entries
		shell.selector.replace(entries)
	} else {
		shell.status = "The profile catalog could not be refreshed."
		shell.err = err
	}
}
