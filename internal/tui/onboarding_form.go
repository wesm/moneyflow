package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/onboarding"
)

type settingsForm struct {
	currency textinput.Model
	scale    textinput.Model
	focused  int
	status   string
	readonly bool
}

func newSettingsForm() (settingsForm, tea.Cmd) {
	currency := textinput.New()
	currency.Prompt = "Import currency: "
	currency.SetValue("USD")
	currency.SetWidth(16)
	scale := textinput.New()
	scale.Prompt = "Minor-unit scale: "
	scale.SetValue("2")
	scale.SetWidth(8)
	form := settingsForm{currency: currency, scale: scale}
	return form, form.currency.Focus()
}

func newSettingsFormForSnapshot(snapshot onboarding.Snapshot) (settingsForm, tea.Cmd) {
	form, command := newSettingsForm()
	if snapshot.ProviderKind != "ynab" || snapshot.Settings == nil {
		return form, command
	}
	form.currency.SetValue(string(snapshot.Settings.Currency))
	form.scale.SetValue(strconv.Itoa(int(snapshot.Settings.Scale)))
	form.currency.Blur()
	form.scale.Blur()
	form.readonly = true
	return form, nil
}

func (form *settingsForm) submit(snapshot onboarding.Snapshot) (onboarding.SubmitRequest, bool) {
	currency := strings.ToUpper(strings.TrimSpace(form.currency.Value()))
	scale, err := strconv.ParseUint(strings.TrimSpace(form.scale.Value()), 10, 8)
	if currency == "" || err != nil || scale > 9 {
		form.status = "Enter a currency and a minor-unit scale from 0 to 9."
		return onboarding.SubmitRequest{}, false
	}
	form.status = ""
	return onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionConfirmSettings,
		Settings: &onboarding.SettingsInput{Currency: domain.Currency(currency), Scale: uint8(scale)},
	}, true
}

func (form settingsForm) update(message tea.KeyPressMsg) (settingsForm, *onboarding.SubmitRequest, tea.Cmd) {
	if form.readonly {
		if message.Keystroke() == "enter" {
			return form, &onboarding.SubmitRequest{}, nil
		}
		return form, nil, nil
	}
	if message.Keystroke() == "tab" || message.Keystroke() == "shift+tab" ||
		message.Keystroke() == "up" || message.Keystroke() == "down" {
		step := 1
		if message.Keystroke() == "shift+tab" || message.Keystroke() == "up" {
			step = -1
		}
		form.focused = (form.focused + step + 2) % 2
		return form, nil, form.focus()
	}
	if message.Keystroke() == "enter" {
		return form, &onboarding.SubmitRequest{}, nil
	}
	var command tea.Cmd
	if form.focused == 0 {
		form.currency, command = form.currency.Update(message)
	} else {
		form.scale, command = form.scale.Update(message)
	}
	return form, nil, command
}

type ynabCredentialForm struct {
	token           secretInput
	accountPassword secretInput
	confirmation    secretInput
	focused         int
	status          string
}

func newYNABCredentialForm() (ynabCredentialForm, tea.Cmd) {
	token, _ := newSecretInput("YNAB personal access token")
	accountPassword, _ := newSecretInput("Moneyflow account password")
	confirmation, _ := newSecretInput("Confirm Moneyflow account password")
	form := ynabCredentialForm{
		token: token, accountPassword: accountPassword, confirmation: confirmation,
	}
	return form, form.focus()
}

func (form *ynabCredentialForm) submit(snapshot onboarding.Snapshot) (onboarding.SubmitRequest, bool) {
	token := []byte(form.token.Value())
	accountPassword := []byte(form.accountPassword.Value())
	confirmation := []byte(form.confirmation.Value())
	form.clearSecrets()
	if len(token) == 0 || len(accountPassword) == 0 || len(confirmation) == 0 {
		form.status = "Complete every credential field."
		return onboarding.SubmitRequest{}, false
	}
	if string(accountPassword) != string(confirmation) {
		form.status = "Moneyflow account passwords do not match."
		return onboarding.SubmitRequest{}, false
	}
	form.status = ""
	return onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionSubmitCredentials,
		YNABCredentials: &onboarding.YNABCredentialInput{
			AccessToken: token, AccountPassword: accountPassword, Confirmation: confirmation,
		},
	}, true
}

func (form ynabCredentialForm) update(message tea.KeyPressMsg) (ynabCredentialForm, bool, tea.Cmd) {
	switch message.Keystroke() {
	case "tab", "down", "shift+tab", "up":
		step := 1
		if message.Keystroke() == "shift+tab" || message.Keystroke() == "up" {
			step = -1
		}
		form.focused = (form.focused + step + 3) % 3
		return form, false, form.focus()
	case "enter":
		if form.focused < 2 {
			form.focused++
			return form, false, form.focus()
		}
		return form, true, nil
	}
	var command tea.Cmd
	switch form.focused {
	case 0:
		form.token, command = form.token.Update(message)
	case 1:
		form.accountPassword, command = form.accountPassword.Update(message)
	default:
		form.confirmation, command = form.confirmation.Update(message)
	}
	return form, false, command
}

func (form *ynabCredentialForm) focus() tea.Cmd {
	form.token.Blur()
	form.accountPassword.Blur()
	form.confirmation.Blur()
	switch form.focused {
	case 0:
		return form.token.Focus()
	case 1:
		return form.accountPassword.Focus()
	default:
		return form.confirmation.Focus()
	}
}

func (form *ynabCredentialForm) clearSecrets() {
	form.token.Clear()
	form.accountPassword.Clear()
	form.confirmation.Clear()
}

func (form ynabCredentialForm) GoString() string {
	return fmt.Sprintf(
		"tui.ynabCredentialForm{token:%#v, accountPassword:%#v, confirmation:%#v, focused:%d, status:%q}",
		form.token, form.accountPassword, form.confirmation, form.focused, form.status,
	)
}

type remoteProfileForm struct {
	choices []onboarding.RemoteProfileChoice
	cursor  int
	status  string
}

func newRemoteProfileForm(choices []onboarding.RemoteProfileChoice) remoteProfileForm {
	return remoteProfileForm{choices: append([]onboarding.RemoteProfileChoice(nil), choices...)}
}

func (form remoteProfileForm) update(message tea.KeyPressMsg) (remoteProfileForm, bool) {
	if len(form.choices) == 0 {
		return form, false
	}
	switch message.Keystroke() {
	case "up", "k", "shift+tab":
		form.cursor = (form.cursor - 1 + len(form.choices)) % len(form.choices)
	case "down", "j", "tab":
		form.cursor = (form.cursor + 1) % len(form.choices)
	case "home":
		form.cursor = 0
	case "enter":
		return form, true
	}
	return form, false
}

func (form *remoteProfileForm) submit(snapshot onboarding.Snapshot) (onboarding.SubmitRequest, bool) {
	if form.cursor < 0 || form.cursor >= len(form.choices) {
		form.status = "Select a YNAB budget."
		return onboarding.SubmitRequest{}, false
	}
	form.status = ""
	return onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionSelectRemoteProfile,
		RemoteProfileChoiceID: form.choices[form.cursor].ChoiceID,
	}, true
}

func (form *settingsForm) focus() tea.Cmd {
	form.currency.Blur()
	form.scale.Blur()
	if form.focused == 0 {
		return form.currency.Focus()
	}
	return form.scale.Focus()
}

type unlockForm struct {
	password secretInput
	status   string
}

func newUnlockForm() (unlockForm, tea.Cmd) {
	password, command := newSecretInput("Moneyflow account password")
	return unlockForm{password: password}, command
}

func (form *unlockForm) submit(snapshot onboarding.Snapshot) (onboarding.SubmitRequest, bool) {
	password := []byte(form.password.Value())
	form.password.Clear()
	if len(password) == 0 {
		form.status = "Enter your Moneyflow account password."
		return onboarding.SubmitRequest{}, false
	}
	form.status = ""
	return onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionUnlock,
		Unlock: &onboarding.UnlockInput{AccountPassword: password},
	}, true
}

func (form unlockForm) update(message tea.KeyPressMsg) (unlockForm, bool, tea.Cmd) {
	if message.Keystroke() == "enter" {
		return form, true, nil
	}
	var command tea.Cmd
	form.password, command = form.password.Update(message)
	return form, false, command
}

func (form unlockForm) GoString() string {
	return fmt.Sprintf("tui.unlockForm{password:%#v, status:%q}", form.password, form.status)
}

type credentialForm struct {
	email           textinput.Model
	password        secretInput
	totp            secretInput
	accountPassword secretInput
	confirmation    secretInput
	focused         int
	status          string
}

func newCredentialForm() (credentialForm, tea.Cmd) {
	email := textinput.New()
	email.Prompt = "Monarch email: "
	email.Placeholder = "user@example.com"
	email.SetWidth(52)
	password, _ := newSecretInput("Monarch password")
	totp, _ := newSecretInput("Monarch TOTP secret")
	accountPassword, _ := newSecretInput("Moneyflow account password")
	confirmation, _ := newSecretInput("Confirm Moneyflow account password")
	form := credentialForm{
		email: email, password: password, totp: totp,
		accountPassword: accountPassword, confirmation: confirmation,
	}
	return form, form.focus()
}

func (form *credentialForm) submit(snapshot onboarding.Snapshot) (onboarding.SubmitRequest, bool) {
	email := strings.TrimSpace(form.email.Value())
	password := []byte(form.password.Value())
	totp := []byte(form.totp.Value())
	accountPassword := []byte(form.accountPassword.Value())
	confirmation := []byte(form.confirmation.Value())
	form.clearSecrets()
	if email == "" || len(password) == 0 || len(totp) == 0 || len(accountPassword) == 0 {
		form.status = "Complete every credential field."
		return onboarding.SubmitRequest{}, false
	}
	if string(accountPassword) != string(confirmation) {
		form.status = "Moneyflow account passwords do not match."
		return onboarding.SubmitRequest{}, false
	}
	form.status = ""
	return onboarding.SubmitRequest{
		ProfileID: snapshot.ProfileID, AttemptID: snapshot.AttemptID,
		ExpectedStateVersion: snapshot.StateVersion, Action: onboarding.ActionSubmitCredentials,
		MonarchCredentials: &onboarding.CredentialInput{
			Email: []byte(email), Password: password, TOTPSecret: totp,
			AccountPassword: accountPassword, Confirmation: confirmation,
		},
	}, true
}

func (form credentialForm) updateFocus(message tea.KeyPressMsg) (credentialForm, tea.Cmd) {
	step := 0
	switch message.Keystroke() {
	case "tab", "down":
		step = 1
	case "shift+tab", "up":
		step = -1
	}
	if step == 0 {
		return form, nil
	}
	form.focused = (form.focused + step + 5) % 5
	return form, form.focus()
}

func (form credentialForm) update(message tea.KeyPressMsg) (credentialForm, bool, tea.Cmd) {
	if message.Keystroke() == "tab" || message.Keystroke() == "shift+tab" ||
		message.Keystroke() == "up" || message.Keystroke() == "down" {
		form, command := form.updateFocus(message)
		return form, false, command
	}
	if message.Keystroke() == "enter" {
		if form.focused < 4 {
			form.focused++
			return form, false, form.focus()
		}
		return form, true, nil
	}
	var command tea.Cmd
	switch form.focused {
	case 0:
		form.email, command = form.email.Update(message)
	case 1:
		form.password, command = form.password.Update(message)
	case 2:
		form.totp, command = form.totp.Update(message)
	case 3:
		form.accountPassword, command = form.accountPassword.Update(message)
	case 4:
		form.confirmation, command = form.confirmation.Update(message)
	}
	return form, false, command
}

func (form *credentialForm) focus() tea.Cmd {
	form.email.Blur()
	form.password.Blur()
	form.totp.Blur()
	form.accountPassword.Blur()
	form.confirmation.Blur()
	switch form.focused {
	case 0:
		return form.email.Focus()
	case 1:
		return form.password.Focus()
	case 2:
		return form.totp.Focus()
	case 3:
		return form.accountPassword.Focus()
	default:
		return form.confirmation.Focus()
	}
}

func (form *credentialForm) clearSecrets() {
	form.password.Clear()
	form.totp.Clear()
	form.accountPassword.Clear()
	form.confirmation.Clear()
}

func (form credentialForm) GoString() string {
	return fmt.Sprintf(
		"tui.credentialForm{email:<redacted>, password:%#v, totp:%#v, accountPassword:%#v, confirmation:%#v, focused:%d, status:%q}",
		form.password, form.totp, form.accountPassword,
		form.confirmation, form.focused, form.status,
	)
}
