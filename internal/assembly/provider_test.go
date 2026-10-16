package assembly_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/assembly"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/onboarding"
	"github.com/wesm/moneyflow/internal/provider/monarch"
	"github.com/wesm/moneyflow/internal/provider/simplefin"
	"github.com/wesm/moneyflow/internal/provider/ynab"
	"github.com/wesm/moneyflow/internal/simplefinonboarding"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestNewInstanceIDNamesTheRendererWithRandomHex(t *testing.T) {
	first, err := assembly.NewInstanceID("web")
	require.NoError(t, err)
	second, err := assembly.NewInstanceID("web")
	require.NoError(t, err)

	assert.Regexp(t, `^web-[0-9a-f]{32}$`, first)
	assert.NotEqual(t, first, second)
}

// saveSession saves each provider's session or credential file through the provider's own
// store, with synthetic values.
var saveSession = map[string]func(*testing.T, home.Paths){
	"monarch": func(t *testing.T, paths home.Paths) {
		sessions, err := monarch.NewSessionStore(paths)
		require.NoError(t, err)
		issued := time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC)
		require.NoError(t, sessions.Save(monarch.Session{
			Version: 2, Token: "synthetic-token", DeviceUUID: "synthetic-device",
			RemoteProfileID: "synthetic-profile", IssuedAt: issued, ValidatedAt: issued,
			Import: monarch.ImportConfig{Currency: "USD", Scale: 2},
		}))
	},
	"ynab": func(t *testing.T, paths home.Paths) {
		vault, err := ynab.NewCredentialVault(paths)
		require.NoError(t, err)
		require.NoError(t, vault.Save(ynab.StoredCredentials{
			AccessToken: "synthetic-token", PlanID: "synthetic-plan", Currency: "USD", Scale: 2,
		}, []byte("example password")))
	},
	"simplefin": func(t *testing.T, paths home.Paths) {
		sessions, err := simplefin.NewSessionStore(paths)
		require.NoError(t, err)
		session := simplefin.Session{ //nolint:gosec // synthetic test credential.
			Version: 1, AccessURL: "https://user:synthetic-secret@example.com/simplefin",
			Import: simplefin.ImportConfig{Currency: "USD", Scale: 2},
		}
		require.NoError(t, sessions.Save(session))
	},
}

func TestInspectSessionFindsTheProvidersSavedSession(t *testing.T) {
	for kind, save := range saveSession {
		root := t.TempDir()
		save(t, home.Paths{Root: root})

		present, err := assembly.InspectSession(root, kind)

		require.NoError(t, err, kind)
		assert.True(t, present, kind)
	}
}

func TestInspectSessionIgnoresEveryOtherProvidersSession(t *testing.T) {
	for _, kind := range []string{"monarch", "ynab", "simplefin", "local"} {
		root := t.TempDir()
		for other, save := range saveSession {
			if other != kind {
				save(t, home.Paths{Root: root})
			}
		}

		present, err := assembly.InspectSession(root, kind)

		require.NoError(t, err, kind)
		assert.False(t, present, kind)
	}
}

func newUnboundProfile(t *testing.T) (*app.Service, home.Paths) {
	t.Helper()
	paths, err := home.ResolveRoot(t.TempDir(), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(context.Background(), paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, profile.Close()) })
	service, err := app.NewProfileService(context.Background(), profile)
	require.NoError(t, err)
	return service, paths
}

func TestConfigureProviderLeavesAnUnboundProfileAlone(t *testing.T) {
	service, paths := newUnboundProfile(t)
	failing := assembly.Factories{
		Monarch: func(home.Paths, monarch.ImportConfig) (assembly.MonarchRuntime, error) {
			t.Fatal("an unbound profile must not build a Monarch runtime")
			return assembly.MonarchRuntime{}, nil
		},
		YNAB: func(home.Paths) (onboarding.Runtime, error) {
			t.Fatal("an unbound profile must not build a YNAB runtime")
			return onboarding.Runtime{}, nil
		},
		SimpleFIN: func(home.Paths) (simplefinonboarding.Runtime, error) {
			t.Fatal("an unbound profile must not build a SimpleFIN runtime")
			return simplefinonboarding.Runtime{}, nil
		},
	}

	err := assembly.ConfigureProvider(context.Background(), service, paths, failing, "web")

	require.NoError(t, err)
}

func TestUnlockYNABRejectsAProfileThatIsNotYNAB(t *testing.T) {
	service, paths := newUnboundProfile(t)

	err := assembly.UnlockYNAB(context.Background(), assembly.UnlockRequest{
		Service: service, Paths: paths, Renderer: "web",
		Password: func(context.Context) (string, error) {
			t.Fatal("an unbound profile must not ask for a password")
			return "", nil
		},
	})

	require.ErrorIs(t, err, assembly.ErrNotYNAB)
}
