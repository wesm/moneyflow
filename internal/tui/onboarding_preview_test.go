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

// OnboardingPreviewSemanticsForTest returns the Python-compatible field contract.
func OnboardingPreviewSemanticsForTest(screen string) OnboardingPreviewSemantics {
	switch screen {
	case "account_selector":
		return OnboardingPreviewSemantics{Focus: "select-monarch-example", Fields: []string{}}
	case "provider_selector":
		return OnboardingPreviewSemantics{Focus: "monarch-button", Fields: []string{}}
	case "credential_setup":
		return OnboardingPreviewSemantics{
			Focus: "email-input",
			Fields: []string{
				"email-input", "password-input", "mfa-input",
				"encrypt-pass-input", "confirm-pass-input",
			},
		}
	case "credential_unlock":
		return OnboardingPreviewSemantics{Focus: "unlock-input", Fields: []string{"unlock-input"}}
	default:
		return OnboardingPreviewSemantics{}
	}
}

// RenderOnboardingPreviewForTest exposes actual shell rendering to external golden tests.
func RenderOnboardingPreviewForTest(
	screen string,
	width int,
	height int,
	allStatuses bool,
) (RenderedScreen, error) {
	palette, err := PaletteFor(ThemeDefault, ColorModeTrueColor)
	if err != nil {
		return RenderedScreen{}, err
	}
	shell := Shell{
		options: Options{Theme: ThemeDefault, ColorMode: ColorModeTrueColor},
		palette: palette, width: width, height: height,
	}
	switch screen {
	case "account_selector":
		shell.screen = shellSelector
		entries := []profilecatalog.Entry{{
			ID: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa", Key: "profile_aaaaaaaaaaaaaaaaaaaaaaaaaa",
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
	case "credential_unlock":
		shell.screen = shellOnboarding
		shell.haveSnapshot = true
		shell.snapshot = onboarding.Snapshot{
			ProtocolVersion: onboarding.ProtocolVersion,
			State:           onboarding.StateUnlockRequired, ProviderKind: "monarch",
		}
		shell.unlock, _ = newUnlockForm()
	default:
		return RenderedScreen{}, errors.New("unknown onboarding preview")
	}
	if err != nil {
		return RenderedScreen{}, err
	}
	return shell.RenderScreen(), nil
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
