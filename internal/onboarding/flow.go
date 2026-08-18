package onboarding

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/monarch"
)

const genericFailureCode = "store_error"

var errImportConfigAbsent = errors.New("import configuration is absent")

func (coordinator *Coordinator) beginFlow(current *attempt, request StartRequest) {
	if coordinator.openProfile == nil {
		return
	}
	if request.Settings != nil {
		current.flow.explicitConfig = &monarch.ImportConfig{
			Currency: request.Settings.Currency,
			Scale:    request.Settings.Scale,
		}
	}
	current.flow.renderer = request.Renderer
	current.flow.monthToDate = request.MonthToDate
	coordinator.startJobLocked(current, coordinator.inspectAndValidate)
}

func (coordinator *Coordinator) inspectAndValidate(ctx context.Context, attemptID string) {
	profileID, ok := coordinator.attemptProfileID(attemptID)
	if !ok {
		return
	}
	opened, err := coordinator.openProfile(ctx, profileID)
	if err != nil {
		coordinator.fail(attemptID, genericFailureCode, "The profile could not be opened.", false, false)
		return
	}
	if opened.ID != profileID || opened.Service == nil || opened.Close == nil ||
		opened.Paths.Root == "" {
		_ = opened.Close()
		coordinator.fail(attemptID, genericFailureCode, "The profile could not be opened.", false, false)
		return
	}
	providerLock, err := home.TryLock(
		opened.Paths.Root, home.LockProviderConnect, home.LockExclusive,
	)
	if err != nil {
		_ = opened.Close()
		if errors.Is(err, home.ErrLockBusy) {
			coordinator.fail(
				attemptID,
				string(CodeProviderConnectInProgress),
				"Another provider connection is already in progress.",
				true,
				false,
			)
			return
		}
		coordinator.fail(
			attemptID, genericFailureCode, "The provider connection could not start.", true, false,
		)
		return
	}
	runtime, err := coordinator.runtimeFactory(opened.Paths)
	if err != nil || runtime.Sessions == nil || runtime.Credentials == nil ||
		runtime.NewConnector == nil || runtime.NewSource == nil ||
		strings.TrimSpace(runtime.InstanceID) == "" {
		_ = providerLock.Release()
		_ = opened.Close()
		coordinator.fail(
			attemptID, genericFailureCode, "The provider connection could not start.", true, false,
		)
		return
	}
	if runtime.Now == nil {
		runtime.Now = time.Now
	}
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		_ = providerLock.Release()
		_ = opened.Close()
		coordinator.fail(
			attemptID, genericFailureCode, "The profile state could not be inspected.", true, false,
		)
		return
	}
	if !coordinator.installFlow(attemptID, opened, providerLock, runtime, connection) {
		_ = providerLock.Release()
		_ = opened.Close()
		return
	}
	if coordinator.monthToDate(attemptID) && !connection.Pristine {
		coordinator.setStableState(attemptID, StateLocalOnly, &Failure{
			Code: string(CodeOnboardingLocalOnly),
			Message: "month-to-date import requires a pristine profile; " +
				"run without --mtd to refresh the complete profile.",
		})
		return
	}
	if !connection.Bound && !connection.Pristine {
		coordinator.setStableState(attemptID, StateLocalOnly, &Failure{
			Code:    string(CodeOnboardingLocalOnly),
			Message: "This profile contains local data and can only be opened offline.",
		})
		return
	}

	var binding *monarch.ImportConfig
	if connection.Bound {
		binding = &monarch.ImportConfig{Currency: connection.Currency, Scale: connection.Scale}
	}
	session, _, loadErr := runtime.Sessions.Load()
	sessionPresent := loadErr == nil
	if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
		coordinator.fail(
			attemptID, genericFailureCode, "The saved provider session could not be read.", true, false,
		)
		return
	}
	var sessionConfig *monarch.ImportConfig
	if sessionPresent {
		sessionConfig = &session.Import
	}
	explicit := coordinator.explicitConfig(attemptID)
	selected, selectErr := selectImportConfig(binding, sessionConfig, explicit)
	if selectErr != nil && !errors.Is(selectErr, errImportConfigAbsent) {
		coordinator.fail(
			attemptID,
			string(CodeCredentialInputInvalid),
			"The import currency and scale are invalid.",
			false,
			true,
		)
		return
	}
	if binding != nil && sessionConfig != nil && *binding != *sessionConfig {
		coordinator.fail(
			attemptID,
			string(CodeCredentialInputInvalid),
			"The saved session conflicts with the profile money interpretation.",
			false,
			true,
		)
		return
	}
	if explicit != nil {
		canonical := binding
		if canonical == nil {
			canonical = sessionConfig
		}
		if canonical != nil && *explicit != *canonical {
			coordinator.fail(
				attemptID,
				string(CodeCredentialInputInvalid),
				"The import currency and scale conflict with saved profile state.",
				false,
				true,
			)
			return
		}
	}
	if selectErr == nil {
		coordinator.setSelectedConfig(attemptID, selected)
	}
	if !sessionPresent {
		coordinator.routeToInput(attemptID)
		return
	}
	if selectErr != nil {
		coordinator.setStableState(attemptID, StateSettingsRequired, nil)
		return
	}
	connector, err := runtime.NewConnector(selected)
	if err != nil || connector == nil {
		coordinator.fail(
			attemptID, genericFailureCode, "The saved provider session could not be checked.", true, false,
		)
		return
	}
	if !coordinator.setValidationState(attemptID, session) {
		return
	}
	identity, err := connector.Validate(ctx, session)
	if err != nil {
		if code, codeOK := provider.CodeOf(err); codeOK && code == provider.CodeReconnectRequired {
			coordinator.clearRetainedSession(attemptID)
			coordinator.routeToInput(attemptID)
			return
		}
		coordinator.failProvider(attemptID, err, true)
		return
	}
	if !validIdentity(connection.Kind, connection.RemoteProfileID, connection.Bound, identity) {
		coordinator.setStableState(attemptID, StateIdentityMismatch, &Failure{
			Code:       string(provider.CodeIdentityMismatch),
			Message:    "The Monarch account does not match this Moneyflow profile.",
			CanReenter: true,
		})
		return
	}
	coordinator.retainValidatedSession(attemptID, session, identity)
	coordinator.startImport(attemptID)
}

func (coordinator *Coordinator) monthToDate(attemptID string) bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	return ok && current.flow.monthToDate
}

func selectImportConfig(
	binding *monarch.ImportConfig,
	session *monarch.ImportConfig,
	input *monarch.ImportConfig,
) (monarch.ImportConfig, error) {
	for _, candidate := range []*monarch.ImportConfig{binding, session, input} {
		if candidate == nil {
			continue
		}
		if err := candidate.Validate(); err != nil {
			return monarch.ImportConfig{}, err
		}
		return *candidate, nil
	}
	return monarch.ImportConfig{}, errImportConfigAbsent
}

func validIdentity(
	boundKind string,
	boundRemoteID string,
	bound bool,
	identity provider.ProfileIdentity,
) bool {
	if identity.Kind != "monarch" || strings.TrimSpace(identity.RemoteID) == "" {
		return false
	}
	return !bound || (boundKind == identity.Kind && boundRemoteID == identity.RemoteID)
}

func (coordinator *Coordinator) routeToInput(attemptID string) {
	coordinator.mu.Lock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		coordinator.mu.Unlock()
		return
	}
	if current.flow.selectedConfig == nil {
		coordinator.transitionLocked(current, StateSettingsRequired, nil)
		coordinator.mu.Unlock()
		return
	}
	runtime := current.flow.runtime
	coordinator.mu.Unlock()
	exists, err := runtime.Credentials.Exists()
	if err != nil {
		coordinator.fail(
			attemptID, genericFailureCode, "The credential vault could not be inspected.", true, false,
		)
		return
	}
	state := StateCredentialsRequired
	if exists {
		state = StateUnlockRequired
	}
	coordinator.setStableState(attemptID, state, nil)
}

func (coordinator *Coordinator) installFlow(
	attemptID string,
	opened OpenedProfile,
	providerLock *home.Lock,
	runtime Runtime,
	connection app.ProviderConnectionState,
) bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, exists := coordinator.attempts[attemptID]
	if !exists || current.state == StateCanceled {
		return false
	}
	current.flow.opened = &opened
	current.flow.providerLock = providerLock
	current.flow.runtime = runtime
	current.flow.connection = connection
	return true
}

func (coordinator *Coordinator) attemptProfileID(attemptID string) (string, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return "", false
	}
	return current.profileID, true
}

func (coordinator *Coordinator) explicitConfig(attemptID string) *monarch.ImportConfig {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.flow.explicitConfig == nil {
		return nil
	}
	cloned := *current.flow.explicitConfig
	return &cloned
}

func (coordinator *Coordinator) setSelectedConfig(attemptID string, config monarch.ImportConfig) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return
	}
	current.flow.selectedConfig = &config
	current.settings = &Settings{Currency: config.Currency, Scale: config.Scale}
}

func (coordinator *Coordinator) setValidationState(
	attemptID string,
	session monarch.Session,
) bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return false
	}
	current.flow.retainedSession = &session
	coordinator.transitionLocked(current, StateValidateSession, nil)
	return true
}

func (coordinator *Coordinator) clearRetainedSession(attemptID string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if current, ok := coordinator.attempts[attemptID]; ok {
		current.flow.retainedSession = nil
		current.flow.identity = nil
	}
}

func (coordinator *Coordinator) retainValidatedSession(
	attemptID string,
	session monarch.Session,
	identity provider.ProfileIdentity,
) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if current, ok := coordinator.attempts[attemptID]; ok && current.state != StateCanceled {
		current.flow.retainedSession = &session
		current.flow.identity = &identity
	}
}

func (coordinator *Coordinator) setStableState(
	attemptID string,
	state State,
	failure *Failure,
) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return
	}
	coordinator.transitionLocked(current, state, failure)
}

func (coordinator *Coordinator) fail(
	attemptID string,
	code string,
	message string,
	canRetry bool,
	canReenter bool,
) {
	coordinator.setStableState(attemptID, StateFailed, &Failure{
		Code: code, Message: message, CanRetry: canRetry, CanReenter: canReenter,
	})
}

func (coordinator *Coordinator) failProvider(attemptID string, err error, canRetry bool) {
	code, ok := provider.CodeOf(err)
	if !ok {
		coordinator.fail(
			attemptID, genericFailureCode, "The provider request failed.", canRetry, false,
		)
		return
	}
	coordinator.fail(
		attemptID,
		string(code),
		providerFailureMessage(code),
		canRetry,
		code == provider.CodeIdentityMismatch,
	)
}

func providerFailureMessage(code provider.ErrorCode) string {
	switch code {
	case provider.CodeReconnectRequired:
		return "Reconnect to Monarch to continue."
	case provider.CodeIdentityMismatch:
		return "The Monarch account does not match this Moneyflow profile."
	case provider.CodeRateLimited:
		return "Monarch temporarily limited requests."
	case provider.CodeUnavailable:
		return "Monarch is temporarily unavailable."
	case provider.CodeDataInvalid:
		return "Monarch returned data that Moneyflow could not use."
	default:
		return "The provider request failed."
	}
}
