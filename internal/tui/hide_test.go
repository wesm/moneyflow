package tui

import (
	"fmt"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/fixture"
	"github.com/wesm/moneyflow/internal/home"
	"github.com/wesm/moneyflow/internal/store/sqlite"
)

func TestMixedGroupHideChangesOnlyVisibleRowsAndUndoRestoresMixedState(t *testing.T) {
	t.Parallel()
	model := newMixedHideModel(t).model
	before := hideStates(t, model)
	model = press(t, model, keyRune('h'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, 1, model.pending.AffectedTransactions)
	assert.Equal(t, map[string]bool{
		"hide_1": true, "hide_2": true, "hide_3": true, "hide_4": true, "hide_5": true,
		"hide_6": true, "hide_7": false,
	}, hideStates(t, model))
	model = press(t, model, keyRune('u'))
	assert.Equal(t, before, hideStates(t, model))
}

func TestMixedGroupHideRespectsSelectedGroupAndCurrentDateFilter(t *testing.T) {
	t.Parallel()
	model := newMixedHideModel(t).model
	start, err := domain.ParseDate("2026-01-01")
	require.NoError(t, err)
	end, err := domain.ParseDate("2026-12-31")
	require.NoError(t, err)
	// Leave a visible row outside the selected period to catch edits escaping the date filter.
	detail := app.NewSession()
	detail.ShowAllDetail()
	_, err = model.service.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: model.service.Revision(),
		State: detail.ViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "hide_1"},
	})
	require.NoError(t, err)
	model.session.DateRange = &domain.DateRange{Start: start, End: end}
	model.refresh()
	for index, row := range model.result.AggregateRows {
		if row.Label == "Mixed Merchant" {
			model.cursor = index
		}
	}
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	for index, row := range model.result.AggregateRows {
		if row.Label == "Other Merchant" {
			model.cursor = index
		}
	}
	model = press(t, model, keyRune('h'))
	assert.Empty(t, model.session.SelectedAggregateKeys)
	assert.Equal(t, map[string]bool{
		"hide_1": false, "hide_2": true, "hide_3": true, "hide_4": true, "hide_5": true,
		"hide_6": true, "hide_7": false,
	}, hideStates(t, model))
}

func TestAllHiddenGroupHideUnhidesEveryMember(t *testing.T) {
	t.Parallel()
	model := newMixedHideModel(t).model
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('h'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.Equal(t, map[string]bool{
		"hide_1": false, "hide_2": false, "hide_3": false, "hide_4": false, "hide_5": false,
		"hide_6": true, "hide_7": false,
	}, hideStates(t, model))
}

func TestAllHiddenGroupHideUnhidesRowsWithEvenPendingToggleCounts(t *testing.T) {
	t.Parallel()
	model := newMixedHideModel(t).model
	detail := app.NewSession()
	detail.ShowAllDetail()
	for _, ids := range [][]domain.EntityID{
		{"hide_5"},
		{"hide_1", "hide_2", "hide_3", "hide_4"},
	} {
		selection, err := app.NewExplicitTransactionSelection(ids, model.service.Revision())
		require.NoError(t, err)
		_, err = model.service.Mutate(t.Context(), app.MutationRequest{
			Action: app.ActionToggleHidden, ExpectedRevision: model.service.Revision(),
			State: detail.ViewState(), Selection: selection,
		})
		require.NoError(t, err)
	}
	model.refresh()
	for index, row := range model.result.AggregateRows {
		if row.Label == "Mixed Merchant" {
			model.cursor = index
		}
	}
	model = press(t, model, keyRune('h'))
	model = press(t, model, keyRune('h'))
	assert.Equal(t, map[string]bool{
		"hide_1": false, "hide_2": false, "hide_3": false, "hide_4": false, "hide_5": false,
		"hide_6": true, "hide_7": false,
	}, hideStates(t, model))
}

func TestMixedGroupHideRejectsStaleRevision(t *testing.T) {
	t.Parallel()
	fixture := newMixedHideModel(t)
	model := fixture.model
	externalStore, err := sqlite.Open(t.Context(), fixture.paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = externalStore.Close() })
	externalService, err := app.NewProfileService(t.Context(), externalStore)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowAllDetail()
	_, err = externalService.Mutate(t.Context(), app.MutationRequest{
		Action: app.ActionToggleHidden, ExpectedRevision: externalService.Revision(),
		State: session.ViewState(), Selection: app.EmptySelection(),
		Target: &app.RowTarget{Kind: app.IdentityTransaction, Identity: "hide_7"},
	})
	require.NoError(t, err)
	model = press(t, model, keyRune('h'))
	assert.Equal(t, overlayNone, model.overlay)
	assert.Contains(t, model.status, "profile changed")
	assert.Equal(t, 1, model.pending.ActiveOperations)
	assert.Equal(t, map[string]bool{
		"hide_1": true, "hide_2": true, "hide_3": true, "hide_4": true, "hide_5": false,
		"hide_6": true, "hide_7": true,
	}, hideStates(t, model))
}

func newMixedHideModel(t *testing.T) persistentModelFixture {
	t.Helper()
	ctx := t.Context()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = profile.Close() })
	base := fixture.Generate(42, 1)[0]
	base.Category = domain.CategoryRef{
		ID: "category_test", Name: "Test Category", GroupID: "group_test", Group: "Test Group",
	}
	base.Date, err = domain.ParseDate("2026-10-02")
	require.NoError(t, err)
	transactions := make([]domain.Transaction, 7)
	for index := range transactions {
		transaction := base.Clone()
		transaction.ID = fmt.Sprintf("hide_%d", index+1)
		transaction.ProviderID = transaction.ID
		transaction.Merchant = domain.EntityRef{ID: "merchant_mixed", Name: "Mixed Merchant"}
		transaction.Hidden = index < 4 || index == 5
		if index >= 5 {
			transaction.Merchant = domain.EntityRef{ID: "merchant_other", Name: "Other Merchant"}
		}
		transactions[index] = transaction
	}
	transactions[0].Date, err = domain.ParseDate("2025-10-02")
	require.NoError(t, err)
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	model, err := NewModel(ctx, service, app.NewSession(), Options{Theme: ThemeDefault, ColorMode: ColorModeNone})
	require.NoError(t, err)
	require.Len(t, model.result.AggregateRows, 2)
	for index, row := range model.result.AggregateRows {
		if row.Label == "Mixed Merchant" {
			model.cursor = index
		}
	}
	return persistentModelFixture{model: model, profile: profile, paths: paths, ctx: ctx}
}

func hideStates(t *testing.T, model Model) map[string]bool {
	t.Helper()
	session := app.NewSession()
	session.ShowAllDetail()
	result, err := model.service.QueryContext(t.Context(), session)
	require.NoError(t, err)
	states := make(map[string]bool, len(result.DetailRows))
	for _, row := range result.DetailRows {
		states[row.Transaction.ID] = row.Flags.Hidden
	}
	return states
}
