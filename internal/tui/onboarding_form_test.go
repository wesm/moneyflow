package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

func TestSettingsFormShowsDefaultsAndBuildsVersionedRequest(t *testing.T) {
	t.Parallel()
	form, command := newSettingsForm()
	require.NotNil(t, command)
	assert.Equal(t, "USD", form.currency.Value())
	assert.Equal(t, "2", form.scale.Value())
	snapshot := formSnapshot(onboarding.StateSettingsRequired)
	request, ok := form.submit(snapshot)
	require.True(t, ok)
	assert.Equal(t, onboarding.ActionConfirmSettings, request.Action)
	assert.Equal(t, snapshot.StateVersion, request.ExpectedStateVersion)
	assert.Equal(t, domain.Currency("USD"), request.Settings.Currency)
	assert.Equal(t, uint8(2), request.Settings.Scale)
}

func TestUnlockSubmitClearsSecretImmediately(t *testing.T) {
	t.Parallel()
	form, _ := newUnlockForm()
	form.password.SetValue("temporary-account-password")
	request, ok := form.submit(formSnapshot(onboarding.StateUnlockRequired))
	require.True(t, ok)
	assert.Equal(t, onboarding.ActionUnlock, request.Action)
	assert.Equal(t, []byte("temporary-account-password"), request.Unlock.AccountPassword)
	assert.Empty(t, form.password.Value())
	assert.NotContains(t, fmt.Sprintf("%#v", form), "temporary-account-password")
}

func TestCredentialSubmitClearsEverySecretField(t *testing.T) {
	t.Parallel()
	form, _ := newCredentialForm()
	form.email.SetValue("user@example.com")
	form.password.SetValue("temporary-provider-password")
	form.totp.SetValue("JBSWY3DPEHPK3PXP")
	form.accountPassword.SetValue("temporary-account-password")
	form.confirmation.SetValue("temporary-account-password")

	request, ok := form.submit(formSnapshot(onboarding.StateCredentialsRequired))
	require.True(t, ok)
	assert.Equal(t, onboarding.ActionSubmitCredentials, request.Action)
	assert.Equal(t, []byte("user@example.com"), request.Credentials.Email)
	assert.Empty(t, form.password.Value())
	assert.Empty(t, form.totp.Value())
	assert.Empty(t, form.accountPassword.Value())
	assert.Empty(t, form.confirmation.Value())
	assert.Equal(t, "user@example.com", form.email.Value())
	assert.NotContains(t, fmt.Sprintf("%#v", form), "temporary-provider-password")
	assert.NotContains(t, fmt.Sprintf("%#v", form), "temporary-account-password")
}

func TestCredentialMismatchKeepsOnlyNonsecretEmail(t *testing.T) {
	t.Parallel()
	form, _ := newCredentialForm()
	form.email.SetValue("user@example.com")
	form.password.SetValue("provider-secret")
	form.totp.SetValue("JBSWY3DPEHPK3PXP")
	form.accountPassword.SetValue("first-secret")
	form.confirmation.SetValue("second-secret")

	_, ok := form.submit(formSnapshot(onboarding.StateCredentialsRequired))
	assert.False(t, ok)
	assert.Equal(t, "Moneyflow account passwords do not match.", form.status)
	assert.Equal(t, "user@example.com", form.email.Value())
	assert.Empty(t, form.password.Value())
	assert.Empty(t, form.totp.Value())
	assert.Empty(t, form.accountPassword.Value())
	assert.Empty(t, form.confirmation.Value())
}

func TestCredentialFormTabAndShiftTabMoveFocus(t *testing.T) {
	t.Parallel()
	form, _ := newCredentialForm()
	assert.Zero(t, form.focused)
	assert.True(t, form.email.Focused())
	assert.False(t, form.password.input.Focused())
	assert.False(t, form.totp.input.Focused())
	assert.False(t, form.accountPassword.input.Focused())
	assert.False(t, form.confirmation.input.Focused())
	form, _ = form.updateFocus(keyMessage("tab"))
	assert.Equal(t, 1, form.focused)
	form, _ = form.updateFocus(keyMessage("shift+tab"))
	assert.Zero(t, form.focused)
}

func TestShellStartsOnboardingAndRendersSettingsForm(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	entry := profileEntryForOnboarding()
	dependencies.Catalog = fakeCatalogView{entries: []profilecatalog.Entry{entry}}
	state.onboardingSnapshot = formSnapshot(onboarding.StateSettingsRequired)
	state.onboardingSnapshot.ProfileID = entry.ID
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)

	updated, command := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, command)
	shell = updateShell(t, shell, command())
	assert.Equal(t, 1, state.onboardingStarts)
	rendered := strings.Join(shell.RenderScreen().Frame.PlainLines(), "\n")
	assert.Contains(t, rendered, "Import currency")
	assert.Contains(t, rendered, "USD")
	assert.Contains(t, rendered, "Minor-unit scale")
}

func TestShellCredentialSubmitClearsSecretsBeforeCoordinatorRuns(t *testing.T) {
	t.Parallel()
	dependencies, state := fakeShellDependencies(t)
	shell, err := NewShell(context.Background(), dependencies, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	shell.screen = shellOnboarding
	snapshot := formSnapshot(onboarding.StateCredentialsRequired)
	shell = updateShell(t, shell, shellOnboardingSnapshotMsg{snapshot: snapshot})
	shell.credentials.email.SetValue("user@example.com")
	shell.credentials.password.SetValue("provider-secret")
	shell.credentials.totp.SetValue("JBSWY3DPEHPK3PXP")
	shell.credentials.accountPassword.SetValue("account-secret")
	shell.credentials.confirmation.SetValue("account-secret")
	shell.credentials.focused = 4

	updated, command := shell.Update(keyMessage("enter"))
	shell = updated.(Shell)
	require.NotNil(t, command)
	assert.Empty(t, shell.credentials.password.Value())
	assert.Empty(t, shell.credentials.totp.Value())
	assert.Empty(t, shell.credentials.accountPassword.Value())
	assert.Empty(t, shell.credentials.confirmation.Value())
	shell = updateShell(t, shell, command())
	assert.Equal(t, 1, state.onboardingSubmits)
	assert.Equal(t, onboarding.ActionSubmitCredentials, state.lastSubmit.Action)
	assert.NotContains(t, fmt.Sprintf("%#v", shell), "provider-secret")
	assert.NotContains(t, fmt.Sprintf("%#v", shell), "user@example.com")
}

func profileEntryForOnboarding() profilecatalog.Entry {
	return profilecatalog.Entry{
		Key: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		DisplayName: "Example Profile", ProviderKind: "monarch",
		Status: profilecatalog.StatusSetupIncomplete,
	}
}

func formSnapshot(state onboarding.State) onboarding.Snapshot {
	return onboarding.Snapshot{
		ProtocolVersion: onboarding.ProtocolVersion,
		AttemptID:       "attempt_example", ProfileID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
		StateVersion: 7, State: state, ProviderKind: "monarch",
	}
}
