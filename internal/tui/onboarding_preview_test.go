package tui

import (
	"errors"

	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/profilecatalog"
)

// OnboardingPreviewSemantics describes only stable focus and field identities.
type OnboardingPreviewSemantics struct {
	Focus  string
	Fields []string
}

// OnboardingPreview carries one real rendered form plus values used to prove
// visible and secret-field behavior without duplicating the renderer contract.
type OnboardingPreview struct {
	Screen        RenderedScreen
	Semantics     OnboardingPreviewSemantics
	VisibleValues []string
	SecretValues  []string
}

// RenderOnboardingPreviewForTest exposes actual shell rendering to external golden tests.
func RenderOnboardingPreviewForTest(
	screen string,
	width int,
	height int,
	allStatuses bool,
) (RenderedScreen, error) {
	preview, err := onboardingPreviewForTest(screen, width, height, allStatuses, false)
	return preview.Screen, err
}

// PopulatedOnboardingPreviewForTest drives the actual forms with key events
// before exposing their render and derived semantic state to parity tests.
func PopulatedOnboardingPreviewForTest(
	screen string,
	width int,
	height int,
	allStatuses bool,
) (OnboardingPreview, error) {
	return onboardingPreviewForTest(screen, width, height, allStatuses, true)
}

func onboardingPreviewForTest(
	screen string,
	width int,
	height int,
	allStatuses bool,
	populate bool,
) (OnboardingPreview, error) {
	palette, err := PaletteFor(ThemeDefault, ColorModeTrueColor)
	if err != nil {
		return OnboardingPreview{}, err
	}
	shell := Shell{
		options: Options{Theme: ThemeDefault, ColorMode: ColorModeTrueColor},
		palette: palette, width: width, height: height,
	}
	switch screen {
	case "account_selector":
		shell.screen = shellSelector
		entries := []profilecatalog.Entry{{
			ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", Key: "monarch-example",
			DisplayName: "Example Profile", ProviderKind: "monarch", Status: profilecatalog.StatusReady,
		}}
		if allStatuses {
			entries = onboardingStatusEntries()
		}
		shell.entries = entries
		shell.selector = newProfileSelector(entries)
	case "provider_selector":
		shell.screen = shellProvider
		shell.providers = newProviderSelector()
	case "credential_setup":
		shell.screen = shellOnboarding
		shell.haveSnapshot = true
		shell.snapshot = onboarding.Snapshot{
			ProtocolVersion: onboarding.ProtocolVersion,
			State:           onboarding.StateCredentialsRequired, ProviderKind: "monarch",
		}
		shell.credentials, _ = newCredentialForm()
		if populate {
			values := []string{
				"person@example.test", "fake-monarch-password", "JBSWY3DPEHPK3PXP",
				"fake-account-password", "fake-account-password",
			}
			for index, value := range values {
				shell.credentials = typeCredentialPreviewValue(shell.credentials, value)
				if index < len(values)-1 {
					shell.credentials, _, _ = shell.credentials.update(keyMessage("tab"))
				}
			}
			for range len(values) - 1 {
				shell.credentials, _, _ = shell.credentials.update(keyMessage("shift+tab"))
			}
		}
	case "credential_unlock":
		shell.screen = shellOnboarding
		shell.haveSnapshot = true
		shell.snapshot = onboarding.Snapshot{
			ProtocolVersion: onboarding.ProtocolVersion,
			State:           onboarding.StateUnlockRequired, ProviderKind: "monarch",
		}
		shell.unlock, _ = newUnlockForm()
		if populate {
			shell.unlock = typeUnlockPreviewValue(shell.unlock, "fake-account-password")
		}
	default:
		return OnboardingPreview{}, errors.New("unknown onboarding preview")
	}
	if err != nil {
		return OnboardingPreview{}, err
	}
	preview := OnboardingPreview{
		Screen: shell.RenderScreen(), Semantics: onboardingPreviewSemantics(shell),
	}
	if populate {
		switch screen {
		case "credential_setup":
			preview.VisibleValues = []string{"person@example.test"}
			preview.SecretValues = []string{
				"fake-monarch-password", "JBSWY3DPEHPK3PXP", "fake-account-password",
			}
		case "credential_unlock":
			preview.SecretValues = []string{"fake-account-password"}
		}
	}
	return preview, nil
}

func onboardingPreviewSemantics(shell Shell) OnboardingPreviewSemantics {
	switch shell.screen {
	case shellSelector:
		rows := shell.selector.rows()
		if shell.selector.cursor < 0 || shell.selector.cursor >= len(rows) {
			return OnboardingPreviewSemantics{}
		}
		return OnboardingPreviewSemantics{
			Focus: "select-" + rows[shell.selector.cursor].entry.Key, Fields: []string{},
		}
	case shellProvider:
		focus := map[providerChoice]string{
			providerMonarch: "monarch-button", providerYNAB: "ynab-button",
			providerSimpleFIN: "simplefin-button",
		}[shell.providers.focused()]
		return OnboardingPreviewSemantics{Focus: focus, Fields: []string{}}
	case shellOnboarding:
		switch shell.snapshot.State {
		case onboarding.StateCredentialsRequired:
			fields := []string{
				"email-input", "password-input", "mfa-input",
				"encrypt-pass-input", "confirm-pass-input",
			}
			if shell.credentials.focused < 0 || shell.credentials.focused >= len(fields) {
				return OnboardingPreviewSemantics{Fields: fields}
			}
			return OnboardingPreviewSemantics{
				Focus: fields[shell.credentials.focused], Fields: fields,
			}
		case onboarding.StateUnlockRequired:
			return OnboardingPreviewSemantics{
				Focus: "unlock-input", Fields: []string{"unlock-input"},
			}
		}
	}
	return OnboardingPreviewSemantics{}
}

func typeCredentialPreviewValue(form credentialForm, value string) credentialForm {
	for _, character := range value {
		form, _, _ = form.update(keyMessage(string(character)))
	}
	return form
}

func typeUnlockPreviewValue(form unlockForm, value string) unlockForm {
	for _, character := range value {
		form, _, _ = form.update(keyMessage(string(character)))
	}
	return form
}

func onboardingStatusEntries() []profilecatalog.Entry {
	definitions := []struct {
		name   string
		status profilecatalog.Status
	}{
		{"Ready Profile", profilecatalog.StatusReady},
		{"Reconnect Profile", profilecatalog.StatusReconnect},
		{"Setup Profile", profilecatalog.StatusSetupIncomplete},
		{"Local Profile", profilecatalog.StatusLocalOnly},
		{"Recovery Profile", profilecatalog.StatusNeedsRecovery},
		{"Newer Profile", profilecatalog.StatusRequiresNewer},
		{"Manifest Profile", profilecatalog.StatusManifestUnsupported},
	}
	entries := make([]profilecatalog.Entry, 0, len(definitions))
	for index, definition := range definitions {
		id := "profile_aaaaaaaaaaaaaaaaaaaaaaaaa" + string(rune('a'+index))
		entries = append(entries, profilecatalog.Entry{
			ID: id, Key: id, DisplayName: definition.name,
			ProviderKind: "monarch", Status: definition.status,
		})
	}
	return entries
}
