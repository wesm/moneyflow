package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
)

// SimpleFINOnboardingView supplies guarded, credential-blind setup transitions.
type SimpleFINOnboardingView interface {
	Start(context.Context, simplefinonboarding.StartRequest) (simplefinonboarding.Snapshot, error)
	Status(context.Context, simplefinonboarding.StatusRequest) (simplefinonboarding.Snapshot, error)
	Submit(context.Context, simplefinonboarding.SubmitRequest) (simplefinonboarding.Snapshot, error)
	Cancel(context.Context, simplefinonboarding.CancelRequest) (simplefinonboarding.Snapshot, error)
	TakeOpenedProfile(context.Context, simplefinonboarding.StatusRequest) (simplefinonboarding.OpenedProfile, error)
	Shutdown(context.Context) error
}

type shellSimpleFINSnapshotMsg struct {
	snapshot simplefinonboarding.Snapshot
	entry    *profilecatalog.Entry
	guard    *onboardingPollGuard
	start    *shellRequestGuard
	cancel   bool
	err      error
}
type shellSimpleFINPollMsg struct{ guard onboardingPollGuard }
type shellSimpleFINOpenedMsg struct {
	profile ShellOpenedProfile
	guard   onboardingPollGuard
	err     error
}

func (shell Shell) acceptsSimpleFINGuard(guard onboardingPollGuard) bool {
	return shell.screen == shellOnboarding && shell.onboardingKind == "simplefin" && shell.haveSimpleFINSnapshot &&
		shell.simplefinSnapshot.AttemptID == guard.attemptID && shell.simplefinSnapshot.StateVersion == guard.stateVersion
}

func (shell Shell) applySimpleFINMessage(message shellSimpleFINSnapshotMsg) (tea.Model, tea.Cmd) {
	if message.start != nil && !shell.acceptsShellRequest(*message.start) {
		return shell, nil
	}
	if message.cancel {
		// Progress may advance while Cancel is in flight. Its reply belongs to
		// the attempt even when the version used for cancellation is now stale.
		if message.guard == nil || shell.screen != shellOnboarding || shell.onboardingKind != "simplefin" ||
			!shell.haveSimpleFINSnapshot || shell.simplefinSnapshot.AttemptID != message.guard.attemptID {
			return shell, nil
		}
		shell.canceling = false
	} else if message.guard != nil && !shell.acceptsSimpleFINGuard(*message.guard) {
		return shell, nil
	}
	if message.err != nil {
		if message.start != nil {
			return shell.handleOnboardingStartFailure(message.err)
		}
		if simplefinonboarding.CodeOf(message.err) == "onboarding_stale" {
			return shell, shell.pollSimpleFIN(onboardingPollGuard{attemptID: shell.simplefinSnapshot.AttemptID, stateVersion: shell.simplefinSnapshot.StateVersion})
		}
		shell.status = "SimpleFIN setup could not continue. Cancel and reopen the profile to retry."
		return shell, nil
	}
	if message.snapshot.ProtocolVersion != simplefinonboarding.ProtocolVersion {
		shell.status = "SimpleFIN setup requires another Moneyflow version."
		return shell, nil
	}
	if message.entry != nil {
		entry := *message.entry
		shell.selected = &entry
	}
	if !shell.haveSimpleFINSnapshot {
		shell.simplefin = newSimpleFINForm()
	}
	if message.snapshot.State == simplefinonboarding.StateImporting && shell.simplefin.started.IsZero() {
		shell.simplefin.started = time.Now()
	}
	shell.simplefinSnapshot, shell.haveSimpleFINSnapshot = message.snapshot, true
	shell.status = ""
	if message.cancel {
		shell.cancelQueued = false
	}
	if shell.cancelQueued && !shell.canceling {
		return shell.cancelSimpleFINOnboarding()
	}
	guard := onboardingPollGuard{attemptID: message.snapshot.AttemptID, stateVersion: message.snapshot.StateVersion}
	switch message.snapshot.State {
	case simplefinonboarding.StateInspect, simplefinonboarding.StateClaiming, simplefinonboarding.StateSavingSession, simplefinonboarding.StateImporting:
		return shell, tea.Tick(onboardingPollInterval, func(time.Time) tea.Msg { return shellSimpleFINPollMsg{guard} })
	case simplefinonboarding.StateComplete:
		return shell, func() tea.Msg {
			opened, err := shell.dependencies.SimpleFINOnboarding.TakeOpenedProfile(shell.ctx, simplefinonboarding.StatusRequest{ProfileID: message.snapshot.ProfileID, AttemptID: message.snapshot.AttemptID})
			return shellSimpleFINOpenedMsg{profile: ShellOpenedProfile{ID: opened.ID, Paths: opened.Paths, Service: opened.Service, Close: opened.Close, ProviderKind: "simplefin"}, guard: guard, err: err}
		}
	case simplefinonboarding.StateCanceled:
		// A claim may have succeeded. The coordinator decides whether the profile
		// can be removed; never perform the generic catalog rollback here.
		shell.createdID = ""
		shell.simplefin.input.Clear()
		shell.haveSimpleFINSnapshot = false
		shell.status = "Setup stopped. Reopen this profile to use its saved connection."
		if message.snapshot.Failure != nil {
			shell.status = message.snapshot.Failure.Message
		}
		return shell.finishCanceledOnboarding()
	}
	return shell, nil
}

func (shell Shell) pollSimpleFIN(guard onboardingPollGuard) tea.Cmd {
	request := simplefinonboarding.StatusRequest{ProfileID: shell.simplefinSnapshot.ProfileID, AttemptID: shell.simplefinSnapshot.AttemptID}
	return func() tea.Msg {
		snapshot, err := shell.dependencies.SimpleFINOnboarding.Status(shell.ctx, request)
		return shellSimpleFINSnapshotMsg{snapshot: snapshot, guard: &guard, err: err}
	}
}

func (shell Shell) routeSimpleFINKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if !shell.haveSimpleFINSnapshot || shell.canceling {
		return shell, nil
	}
	snapshot := shell.simplefinSnapshot
	request := simplefinonboarding.SubmitRequest{ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID, ExpectedStateVersion: snapshot.StateVersion}
	if simplefinNeedsInput(snapshot) {
		if !shell.simplefin.settingsReady {
			var submit bool
			var command tea.Cmd
			var marker *onboarding.SubmitRequest
			// Reuse the money form's navigation and parsing before requesting a credential.
			shell.simplefin.settings, marker, command = shell.simplefin.settings.update(message)
			submit = marker != nil
			if !submit {
				return shell, command
			}
			if _, ok := shell.simplefin.importSettings(); !ok {
				return shell, nil
			}
			shell.simplefin.settingsReady = true
			return shell, shell.simplefin.input.Focus()
		}
		if message.Keystroke() != "enter" {
			var command tea.Cmd
			shell.simplefin.input, command = shell.simplefin.input.Update(message)
			return shell, command
		}
		settings, ok := shell.simplefin.importSettings()
		if !ok {
			return shell, nil
		}
		value := shell.simplefin.input.Value()
		if value == "" || len(value) > 8192 {
			shell.status = "Paste a setup token or Access URL (at most 8 KiB)."
			return shell, nil
		}
		request.Action, request.Settings, request.Input = simplefinonboarding.ActionConnect, settings, []byte(value)
		shell.simplefin.input.Clear()
	} else if snapshot.Failure != nil && snapshot.Failure.CanRetry && (message.Keystroke() == "enter" || message.Keystroke() == "r") {
		request.Action = simplefinonboarding.ActionRetryImport
		if snapshot.Failure.Code == "session_save_failed" {
			request.Action = simplefinonboarding.ActionRetrySave
		}
	} else {
		return shell, nil
	}
	guard := onboardingPollGuard{attemptID: snapshot.AttemptID, stateVersion: snapshot.StateVersion}
	return shell, func() tea.Msg {
		defer clear(request.Input)
		next, err := shell.dependencies.SimpleFINOnboarding.Submit(shell.ctx, request)
		return shellSimpleFINSnapshotMsg{snapshot: next, guard: &guard, err: err}
	}
}

func (shell Shell) cancelSimpleFINOnboarding() (tea.Model, tea.Cmd) {
	if shell.canceling {
		return shell, nil
	}
	shell.cancelQueued = true
	if !shell.haveSimpleFINSnapshot {
		return shell, nil
	}
	shell.canceling = true
	snapshot := shell.simplefinSnapshot
	guard := onboardingPollGuard{attemptID: snapshot.AttemptID, stateVersion: snapshot.StateVersion}
	return shell, func() tea.Msg {
		next, err := shell.dependencies.SimpleFINOnboarding.Cancel(shell.ctx, simplefinonboarding.CancelRequest{ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID, ExpectedStateVersion: snapshot.StateVersion})
		return shellSimpleFINSnapshotMsg{snapshot: next, guard: &guard, cancel: true, err: err}
	}
}

func (shell Shell) openSimpleFINMessage(message shellSimpleFINOpenedMsg) (tea.Model, tea.Cmd) {
	if !shell.acceptsSimpleFINGuard(message.guard) || shell.canceling || shell.cancelQueued {
		if message.profile.Close != nil {
			shell.err = message.profile.Close()
		}
		return shell, nil
	}
	if message.err != nil {
		shell.status = "The connected profile could not be opened."
		return shell, nil
	}
	session := shell.initialSession.Clone()
	if shell.resume != nil {
		session = shell.resume.session
	}
	if err := shell.enterFinance(message.profile, session); err != nil {
		shell.err = err
		return shell, nil
	}
	if shell.resume != nil {
		shell.finance.cursor, shell.finance.scroll = shell.resume.cursor, shell.resume.scroll
		shell.finance.clampCursor()
	}
	shell.resume, shell.selected, shell.createdID = nil, nil, ""
	shell.haveSimpleFINSnapshot, shell.canceling, shell.cancelQueued = false, false, false
	updated, command := shell.finance.Update(tea.WindowSizeMsg{Width: shell.width, Height: shell.height})
	finance := updated.(Model)
	shell.finance = &finance
	return shell, tea.Batch(command, shell.finance.Init())
}
