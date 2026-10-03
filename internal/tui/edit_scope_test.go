package tui

import (
	"context"
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

func TestEditorsRespectCurrentViewScope(t *testing.T) {
	t.Parallel()
	for _, editor := range []struct {
		name        string
		key         rune
		destination string
		operation   domain.OperationType
	}{
		{"merchant", 'm', "Destination Merchant", domain.OperationMerchantReassign},
		{"category", 'c', "Destination Category", domain.OperationCategoryAssign},
	} {
		for _, view := range []string{"merchant row", "selected merchant row", "category row", "merchant detail", "time drilldown"} {
			t.Run(editor.name+"/"+view, func(t *testing.T) {
				fixture := newFilteredEditModel(t)
				model := fixture.model
				if view == "time drilldown" {
					model = press(t, model, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
					for index, row := range model.result.AggregateRows {
						if row.Label == "2026" {
							model.cursor = index
						}
					}
					model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
					model = press(t, model, keyRune('g'))
				} else {
					model = press(t, model, keyRune('t'))
					model = typeText(t, model, "2026")
					model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
				}
				require.Equal(t, 2, model.result.FilteredCount)
				want := []domain.EntityID{"transaction_visible_a", "transaction_visible_b"}
				switch view {
				case "selected merchant row":
					model = press(t, model, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
				case "category row":
					model = press(t, model, keyRune('g'))
					model = press(t, model, keyRune('g'))
					require.Equal(t, domain.DimensionCategory, model.result.AggregateRows[0].Dimension)
				case "merchant detail":
					model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
					require.Len(t, model.result.DetailRows, 3)
					for index, row := range model.result.DetailRows {
						if row.Transaction.ID == "transaction_visible_a" {
							model.cursor = index
						}
					}
					want = []domain.EntityID{"transaction_visible_a"}
				}

				model = press(t, model, keyRune(editor.key))
				if editor.key == 'm' {
					require.NotNil(t, model.merchant.preview)
					assert.Equal(t, len(want), model.merchant.preview.AffectedTransactions)
				}
				model = typeText(t, model, editor.destination)
				model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
				require.Equal(t, overlayNone, model.overlay)
				stored, err := fixture.profile.Load(fixture.ctx)
				require.NoError(t, err)
				require.Len(t, stored.Journal, 1)
				assert.Equal(t, editor.operation, stored.Journal[0].Type)
				assert.ElementsMatch(t, want, stored.Journal[0].Targets)
				assert.Equal(t, len(want), model.pending.AffectedTransactions)
			})
		}
	}
}

func TestMerchantWholeEntityScopeRequiresExplicitChoice(t *testing.T) {
	t.Parallel()
	fixture := newFilteredEditModel(t)
	model := press(t, fixture.model, keyRune('t'))
	model = typeText(t, model, "2026")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	model = press(t, model, keyRune('m'))
	require.NotNil(t, model.merchant.preview)
	assert.Equal(t, 2, model.merchant.preview.AffectedTransactions)
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyTab})
	require.NotNil(t, model.merchant.preview)
	assert.Equal(t, 6, model.merchant.preview.AffectedTransactions)
	model = typeText(t, model, "Destination Merchant")
	model = press(t, model, tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, overlayNone, model.overlay)
	stored, err := fixture.profile.Load(fixture.ctx)
	require.NoError(t, err)
	require.Len(t, stored.Journal, 1)
	assert.Equal(t, domain.OperationMerchantMerge, stored.Journal[0].Type)
	assert.Equal(t, 6, model.pending.AffectedTransactions)
}

func newFilteredEditModel(t testing.TB) persistentModelFixture {
	t.Helper()
	transactions := make([]domain.Transaction, 0, 7)
	for _, spec := range []struct {
		id   string
		date string
	}{
		{"visible_a", "2026-05-02"}, {"visible_b", "2026-05-01"},
		{"older", "2025-12-31"}, {"other_search", "2026-05-01"},
		{"hidden", "2026-05-01"}, {"other_account", "2026-05-01"},
		{"destination", "2026-05-01"},
	} {
		date, err := domain.ParseDate(spec.date)
		require.NoError(t, err)
		transaction := domain.Transaction{
			ID: "transaction_" + spec.id, ProviderID: spec.id, Provider: "synthetic",
			Date: date, Amount: domain.Money{Minor: -100, Currency: "USD", Scale: 2},
			Account:  domain.EntityRef{ID: "account_source", Name: "Source Account"},
			Merchant: domain.EntityRef{ID: "merchant_source", Name: "Source Merchant"},
			Category: domain.CategoryRef{ID: "category_source", Name: "Matching Category", GroupID: "group_example", Group: "Example Group"},
		}
		switch spec.id {
		case "other_search":
			transaction.Category.ID, transaction.Category.Name = "category_other", "Other Category"
		case "hidden":
			transaction.Hidden = true
		case "other_account":
			transaction.Account = domain.EntityRef{ID: "account_other", Name: "Other Account"}
		case "destination":
			transaction.Merchant = domain.EntityRef{ID: "merchant_destination", Name: "Destination Merchant"}
			transaction.Category.ID, transaction.Category.Name = "category_destination", "Destination Category"
		}
		transactions = append(transactions, transaction)
	}
	ctx := context.Background()
	paths, err := home.ResolveRoot(filepath.Join(t.TempDir(), "profile"), nil, "")
	require.NoError(t, err)
	profile, err := sqlite.Open(ctx, paths, sqlite.DefaultOptions)
	require.NoError(t, err)
	t.Cleanup(func() { _ = profile.Close() })
	committed, err := fixture.CommittedProfile(transactions)
	require.NoError(t, err)
	_, err = profile.CreateSeededProfile(ctx, committed)
	require.NoError(t, err)
	service, err := app.NewProfileService(ctx, profile)
	require.NoError(t, err)
	session := app.NewSession()
	session.ShowHidden = false
	session.SetSearch("Matching")
	session.Drilldowns = []domain.Drilldown{{Dimension: domain.DimensionAccount, Key: "account_source", Label: "Source Account", Currency: "USD", Scale: 2}}
	model, err := NewModel(ctx, service, session, Options{ColorMode: ColorModeNone})
	require.NoError(t, err)
	return persistentModelFixture{model: model, profile: profile, paths: paths, ctx: ctx}
}
