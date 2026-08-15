package tui

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store"
	"github.com/wesm/moneyflow/internal/store/sqlite"
	paritydata "github.com/wesm/moneyflow/testdata/parity"
)

type persistentModelFixture struct {
	model   Model
	profile store.Profile
	paths   home.Paths
	ctx     context.Context
}

func typeText(t testing.TB, model Model, value string) Model {
	t.Helper()
	for _, character := range value {
		model = press(t, model, tea.KeyPressMsg{Code: character, Text: string(character)})
	}
	return model
}

func newPersistentModel(t testing.TB, session app.Session) persistentModelFixture {
	t.Helper()
	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	transactions, err := fixture.Decode(bytes.NewReader(paritydata.Transactions))
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	model, err := NewModel(ctx, service, session, Options{
		Theme: ThemeDefault, ColorMode: ColorModeNone,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = profile.Close() })
	return persistentModelFixture{model: model, profile: profile, paths: paths, ctx: ctx}
}
