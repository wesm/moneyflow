package onboarding

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/provider"
	"github.com/wesm/moneyflow/internal/provider/ynab"
)

const choiceIDPrefix = "choice_"

type ynabCredentialMaterial struct {
	token    []byte
	password []byte
}

func newYNABCredentialMaterial(input *YNABCredentialInput) (*ynabCredentialMaterial, error) {
	if input == nil || len(input.AccessToken) == 0 || len(input.AccountPassword) == 0 ||
		len(input.AccountPassword) != len(input.Confirmation) ||
		subtle.ConstantTimeCompare(input.AccountPassword, input.Confirmation) != 1 {
		return nil, newError(CodeCredentialInputInvalid, errors.New("YNAB credentials are invalid"))
	}
	token := strings.TrimSpace(string(input.AccessToken))
	if token == "" || token != string(input.AccessToken) {
		return nil, newError(CodeCredentialInputInvalid, errors.New("YNAB token is invalid"))
	}
	return &ynabCredentialMaterial{
		token:    append([]byte(nil), input.AccessToken...),
		password: append([]byte(nil), input.AccountPassword...),
	}, nil
}

func (material *ynabCredentialMaterial) clear() {
	if material == nil {
		return
	}
	clear(material.token)
	clear(material.password)
}

func (coordinator *Coordinator) inspectYNAB(
	_ context.Context,
	attemptID string,
	connection app.ProviderConnectionState,
) {
	if coordinator.monthToDate(attemptID) {
		coordinator.fail(attemptID, string(CodeCredentialInputInvalid),
			"Month-to-date import is not available for YNAB.", false, false)
		return
	}
	if !connection.Bound && !connection.Pristine {
		coordinator.setStableState(attemptID, StateLocalOnly, &Failure{
			Code:    string(CodeOnboardingLocalOnly),
			Message: "This profile contains local data and can only be opened offline.",
		})
		return
	}
	if connection.Bound && connection.Kind != "ynab" {
		coordinator.setStableState(attemptID, StateIdentityMismatch, &Failure{
			Code:    string(provider.CodeIdentityMismatch),
			Message: "This Moneyflow profile is bound to a different provider.", CanReenter: false,
		})
		return
	}
	runtime, ok := coordinator.ynabRuntime(attemptID)
	if !ok {
		return
	}
	exists, err := runtime.YNABVault.Exists()
	if err != nil {
		coordinator.fail(attemptID, genericFailureCode,
			"The YNAB credential vault could not be inspected.", true, false)
		return
	}
	state := StateCredentialsRequired
	if exists {
		state = StateUnlockRequired
	}
	coordinator.setStableState(attemptID, state, nil)
}

func (coordinator *Coordinator) authenticateYNABFromVault(
	ctx context.Context,
	attemptID string,
	accountPassword []byte,
) {
	defer clear(accountPassword)
	runtime, ok := coordinator.ynabRuntime(attemptID)
	if !ok {
		return
	}
	credentials, err := runtime.YNABVault.Load(accountPassword)
	if err != nil {
		if errors.Is(err, ynab.ErrCredentialUnlock) {
			coordinator.setStableState(attemptID, StateUnlockRequired, &Failure{
				Code:    string(CodeCredentialUnlockFailed),
				Message: "The Moneyflow account password was not accepted.", CanReenter: true,
			})
			return
		}
		coordinator.fail(attemptID, genericFailureCode,
			"The YNAB credential vault could not be opened.", true, true)
		return
	}
	if !coordinator.retainYNABCredentials(attemptID, credentials, accountPassword, true) {
		return
	}
	coordinator.fetchYNABPlan(ctx, attemptID, credentials.PlanID)
}

func (coordinator *Coordinator) authenticateYNABNew(
	ctx context.Context,
	attemptID string,
	material *ynabCredentialMaterial,
) {
	defer material.clear()
	runtime, ok := coordinator.ynabRuntime(attemptID)
	if !ok {
		return
	}
	client, err := runtime.NewYNABClient(material.token)
	if err != nil || client == nil {
		coordinator.fail(attemptID, genericFailureCode,
			"Authentication with YNAB could not start.", false, true)
		return
	}
	plans, err := client.ListPlans(ctx)
	if err != nil {
		coordinator.failYNABProvider(attemptID, err, true)
		return
	}
	if len(plans) == 0 {
		coordinator.fail(attemptID, string(provider.CodeDataInvalid),
			"No YNAB budgets are available to this token.", false, true)
		return
	}
	if !coordinator.retainYNABToken(attemptID, material.token, material.password) {
		return
	}
	slices.SortFunc(plans, func(left, right ynab.PlanSummary) int {
		if order := strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name)); order != 0 {
			return order
		}
		return strings.Compare(left.ID, right.ID)
	})
	if len(plans) == 1 {
		coordinator.fetchYNABPlan(ctx, attemptID, plans[0].ID)
		return
	}
	coordinator.publishYNABChoices(attemptID, plans)
}

func (coordinator *Coordinator) selectYNABRemoteProfile(
	ctx context.Context,
	attemptID string,
	choiceID string,
) {
	coordinator.mu.Lock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || current.flow.ynabPlans == nil {
		coordinator.mu.Unlock()
		return
	}
	plan, exists := current.flow.ynabPlans[choiceID]
	current.remoteProfiles = nil
	current.flow.ynabPlans = nil
	coordinator.mu.Unlock()
	if !exists {
		coordinator.fail(attemptID, string(CodeCredentialInputInvalid),
			"The selected YNAB budget is no longer available.", false, true)
		return
	}
	coordinator.fetchYNABPlan(ctx, attemptID, plan.ID)
}

func (coordinator *Coordinator) fetchYNABPlan(
	ctx context.Context,
	attemptID string,
	planID string,
) {
	runtime, credentials, token, connection, vaultSaved, ok := coordinator.ynabInputs(attemptID)
	if !ok {
		return
	}
	defer clear(token)
	client, err := runtime.NewYNABClient(token)
	if err != nil || client == nil {
		coordinator.fail(attemptID, genericFailureCode, "The YNAB request could not start.", true, false)
		return
	}
	plan, err := client.FetchPlan(ctx, planID)
	if err != nil {
		coordinator.failYNABProvider(attemptID, err, true)
		return
	}
	snapshot, err := ynab.Normalize(plan, runtime.Now().UTC())
	if err != nil {
		coordinator.failYNABProvider(attemptID, err, false)
		return
	}
	settings := Settings{
		Currency: domain.Currency(plan.CurrencyFormat.ISOCode),
		// Normalize above rejects decimal digits outside the exact uint8 range 0..9.
		Scale: uint8(*plan.CurrencyFormat.DecimalDigits), // #nosec G115
	}
	if connection.Bound {
		if connection.RemoteProfileID != plan.ID {
			coordinator.failYNABProvider(attemptID, provider.NewError(provider.CodeIdentityMismatch), false)
			return
		}
		if connection.Currency != settings.Currency || connection.Scale != settings.Scale {
			coordinator.failYNABProvider(attemptID, provider.NewError(provider.CodeMoneyMismatch), false)
			return
		}
	}
	if vaultSaved && credentials.PlanID != plan.ID {
		coordinator.failYNABProvider(attemptID, provider.NewError(provider.CodeIdentityMismatch), false)
		return
	}
	settingsChanged := vaultSaved && (credentials.Currency != settings.Currency || credentials.Scale != settings.Scale)
	if connection.Bound && settingsChanged {
		coordinator.failYNABProvider(attemptID, provider.NewError(provider.CodeMoneyMismatch), false)
		return
	}
	credentials.PlanID = plan.ID
	credentials.Currency = settings.Currency
	credentials.Scale = settings.Scale
	result := provider.SnapshotResult{
		Identity: provider.ProfileIdentity{Kind: "ynab", RemoteID: plan.ID}, Snapshot: snapshot,
	}
	coordinator.mu.Lock()
	current, exists := coordinator.attempts[attemptID]
	if !exists || current.state == StateCanceled {
		coordinator.mu.Unlock()
		return
	}
	current.flow.ynabCredentials = &credentials
	current.flow.ynabPlan = &plan
	current.flow.ynabSnapshot = &result
	current.settings = &settings
	coordinator.mu.Unlock()
	if vaultSaved && !settingsChanged {
		coordinator.mu.Lock()
		if current, exists := coordinator.attempts[attemptID]; exists {
			clear(current.flow.ynabPassword)
			current.flow.ynabPassword = nil
		}
		coordinator.mu.Unlock()
		coordinator.startYNABImport(attemptID)
		return
	}
	coordinator.setStableState(attemptID, StateSettingsRequired, nil)
}

func (coordinator *Coordinator) confirmAndImportYNAB(ctx context.Context, attemptID string) {
	runtime, credentials, password, ok := coordinator.ynabSaveInputs(attemptID)
	if !ok {
		return
	}
	defer clear(password)
	if err := runtime.YNABVault.Save(credentials, password); err != nil {
		coordinator.fail(attemptID, genericFailureCode,
			"The YNAB credential vault could not be saved. Re-enter credentials to try saving again.", false, true)
		return
	}
	coordinator.mu.Lock()
	if current, exists := coordinator.attempts[attemptID]; exists {
		clear(current.flow.ynabPassword)
		current.flow.ynabPassword = nil
		current.flow.ynabVaultSaved = true
	}
	coordinator.mu.Unlock()
	coordinator.importYNABProfile(ctx, attemptID)
}

func (coordinator *Coordinator) startYNABImport(attemptID string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || current.flow.opened == nil ||
		current.flow.ynabCredentials == nil || current.flow.ynabSnapshot == nil {
		return
	}
	coordinator.transitionLocked(current, StateImporting, nil)
	current.flow.retryState = StateImporting
	coordinator.startJobLocked(current, coordinator.importYNABProfile)
}

func (coordinator *Coordinator) importYNABProfile(ctx context.Context, attemptID string) {
	runtime, credentials, snapshot, opened, renderer, ok := coordinator.ynabImportInputs(attemptID)
	if !ok {
		return
	}
	writeStatus, err := opened.Service.ProviderWriteStatus(ctx)
	if err != nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	initial := &snapshot
	connection, err := opened.Service.ProviderConnection(ctx)
	if err != nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	if connection.Bound {
		// Only first import may reuse the onboarding snapshot. A bound profile
		// can be written by another process during unlock; fetch under the
		// refresh lease so those writes cannot be undone by pre-write data.
		initial = nil
	}
	readSource, writeSource, err := runtime.NewYNABSource(credentials, initial)
	if err != nil || readSource == nil || writeSource == nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	startedAt := coordinator.now()
	if err = opened.Service.ConfigureProvider(app.ProviderRuntime{
		ReadSource: readSource, WriteSource: writeSource, Provider: "ynab", Currency: credentials.Currency, Scale: credentials.Scale,
		Renderer: renderer, InstanceID: runtime.InstanceID, Now: runtime.Now,
		Progress: func(update provider.Progress) {
			coordinator.observeProgress(attemptID, startedAt, update)
		},
	}); err != nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	if writeStatus.Phase != "" {
		// Unlock restores the runtime, not the frozen journal. The owning renderer
		// resumes eligible work; CLI connection never dispatches a durable batch.
		coordinator.completeImport(attemptID, 0, startedAt)
		return
	}
	result, err := opened.Service.RefreshProvider(ctx, app.ProviderRefreshRequest{
		Manual: true, State: app.DefaultViewState(), Selection: app.EmptySelection(),
	})
	if err != nil {
		coordinator.importFailure(attemptID, err)
		return
	}
	coordinator.completeImport(attemptID, result.Status.Summary.ImportedTransactions, startedAt)
}

func (coordinator *Coordinator) ynabRuntime(attemptID string) (Runtime, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || current.flow.runtime.ProviderKind != "ynab" {
		return Runtime{}, false
	}
	return current.flow.runtime, true
}

func (coordinator *Coordinator) retainYNABToken(attemptID string, token, password []byte) bool {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return false
	}
	clear(current.flow.ynabToken)
	clear(current.flow.ynabPassword)
	current.flow.ynabToken = append([]byte(nil), token...)
	current.flow.ynabPassword = append([]byte(nil), password...)
	current.flow.ynabVaultSaved = false
	return true
}

func (coordinator *Coordinator) retainYNABCredentials(
	attemptID string,
	credentials ynab.StoredCredentials,
	password []byte,
	vaultSaved bool,
) bool {
	if !coordinator.retainYNABToken(attemptID, []byte(credentials.AccessToken), password) {
		return false
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current := coordinator.attempts[attemptID]
	current.flow.ynabCredentials = &credentials
	current.flow.ynabVaultSaved = vaultSaved
	return true
}

func (coordinator *Coordinator) ynabInputs(
	attemptID string,
) (Runtime, ynab.StoredCredentials, []byte, app.ProviderConnectionState, bool, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || len(current.flow.ynabToken) == 0 {
		return Runtime{}, ynab.StoredCredentials{}, nil, app.ProviderConnectionState{}, false, false
	}
	credentials := ynab.StoredCredentials{AccessToken: string(current.flow.ynabToken)}
	if current.flow.ynabCredentials != nil {
		credentials = *current.flow.ynabCredentials
	}
	return current.flow.runtime, credentials, append([]byte(nil), current.flow.ynabToken...),
		current.flow.connection, current.flow.ynabVaultSaved, true
}

func (coordinator *Coordinator) ynabSaveInputs(
	attemptID string,
) (Runtime, ynab.StoredCredentials, []byte, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled || current.flow.ynabCredentials == nil ||
		len(current.flow.ynabPassword) == 0 {
		return Runtime{}, ynab.StoredCredentials{}, nil, false
	}
	return current.flow.runtime, *current.flow.ynabCredentials,
		append([]byte(nil), current.flow.ynabPassword...), true
}

func (coordinator *Coordinator) ynabImportInputs(
	attemptID string,
) (Runtime, ynab.StoredCredentials, provider.SnapshotResult, OpenedProfile, string, bool) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state != StateImporting || current.flow.opened == nil ||
		current.flow.ynabCredentials == nil || current.flow.ynabSnapshot == nil {
		return Runtime{}, ynab.StoredCredentials{}, provider.SnapshotResult{}, OpenedProfile{}, "", false
	}
	return current.flow.runtime, *current.flow.ynabCredentials, *current.flow.ynabSnapshot,
		*current.flow.opened, current.flow.renderer, true
}

func (coordinator *Coordinator) publishYNABChoices(attemptID string, plans []ynab.PlanSummary) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	current, ok := coordinator.attempts[attemptID]
	if !ok || current.state == StateCanceled {
		return
	}
	current.flow.ynabPlans = make(map[string]ynab.PlanSummary, len(plans))
	current.remoteProfiles = make([]RemoteProfileChoice, 0, len(plans))
	for _, plan := range plans {
		choiceID, err := coordinator.newChoiceIDLocked(current.flow.ynabPlans)
		if err != nil {
			coordinator.transitionLocked(current, StateFailed, &Failure{
				Code: genericFailureCode, Message: "The YNAB budget choices could not be prepared.",
			})
			return
		}
		current.flow.ynabPlans[choiceID] = plan
		current.remoteProfiles = append(current.remoteProfiles, RemoteProfileChoice{
			ChoiceID: choiceID, DisplayName: plan.Name, LastModified: plan.LastModifiedOn,
		})
	}
	coordinator.transitionLocked(current, StateRemoteProfileRequired, nil)
}

func (coordinator *Coordinator) newChoiceIDLocked(existing map[string]ynab.PlanSummary) (string, error) {
	for range 16 {
		buffer := make([]byte, attemptIDBytes)
		if _, err := io.ReadFull(coordinator.random, buffer); err != nil {
			return "", err
		}
		id := choiceIDPrefix + attemptIDEncoding.EncodeToString(buffer)
		if _, duplicate := existing[id]; !duplicate {
			return id, nil
		}
	}
	return "", errors.New("choice ID collision limit reached")
}

func (coordinator *Coordinator) failYNABProvider(attemptID string, err error, canRetry bool) {
	code, ok := provider.CodeOf(err)
	if !ok {
		coordinator.fail(attemptID, genericFailureCode, "The YNAB request failed.", canRetry, false)
		return
	}
	message := "The YNAB request failed."
	switch code {
	case provider.CodeReconnectRequired:
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()
		if current, exists := coordinator.attempts[attemptID]; exists && current.state != StateCanceled {
			(ynabFlow{}).ReauthenticateLocked(current)
			coordinator.transitionLocked(current, StateCredentialsRequired, &Failure{
				Code: string(code), Message: "Enter a new YNAB token to reconnect.", CanReenter: true,
			})
		}
		return
	case provider.CodeIdentityMismatch:
		message = "The YNAB budget does not match this Moneyflow profile."
	case provider.CodeMoneyMismatch:
		message = "The YNAB budget currency does not match this Moneyflow profile."
	case provider.CodeRateLimited:
		message = "YNAB temporarily limited requests."
	case provider.CodeUnavailable:
		message = "YNAB is temporarily unavailable."
	case provider.CodeDataInvalid:
		message = "YNAB returned data that Moneyflow could not use."
		if reason, known := provider.DataInvalidReasonOf(err); known {
			message += " " + provider.DataInvalidDetail(reason)
		}
		if !canRetry {
			message += " Cancel and restart setup after correcting the provider data."
		}
	}
	coordinator.fail(attemptID, string(code), message, canRetry, code == provider.CodeIdentityMismatch)
}
