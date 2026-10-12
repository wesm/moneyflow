package api

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/app"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestWireMoneyUsesExactStringsAtInt64Boundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		minor   int64
		decimal string
		display string
	}{
		{minor: math.MaxInt64, decimal: "92233720368547758.07", display: "+92,233,720,368,547,758.07"},
		{minor: math.MinInt64, decimal: "-92233720368547758.08", display: "-92,233,720,368,547,758.08"},
	}
	for _, test := range tests {
		wire := moneyToWire(domain.Money{Minor: test.minor, Currency: "USD", Scale: 2})
		assert.Equal(t, test.decimal, wire.Decimal)
		assert.Equal(t, test.display, wire.Display)
		assert.Equal(t, test.decimal[:strings.Index(test.decimal, ".")]+test.decimal[strings.Index(test.decimal, ".")+1:], wire.Minor)
	}
}

func TestWireProjectionOmitsProviderAndPrivateDomainData(t *testing.T) {
	t.Parallel()

	date, err := domain.ParseDate("2024-01-02")
	require.NoError(t, err)
	projection := app.WebProjection{
		State:     app.DefaultViewState(),
		Selection: app.EmptySelection(),
		DetailRows: []app.WebDetailRow{{
			Index:    0,
			Identity: "txn-safe",
			Row: domain.DetailRow{Transaction: domain.Transaction{
				ID: "txn-safe", ProviderID: "private-provider-id", Provider: "private-provider",
				Date: date, Account: domain.EntityRef{ID: "account-id", Name: "Account Name"},
				Merchant: domain.EntityRef{ID: "merchant-id", Name: "Example Merchant"},
				Category: domain.CategoryRef{ID: "category-id", Name: "Example Category", GroupID: "group-id", Group: "Example Group"},
				Amount:   domain.Money{Minor: -1234, Currency: "USD", Scale: 2},
				Notes:    "private note", Metadata: map[string]string{"private": "value"},
			}, Flags: domain.RowFlags{Selected: true}},
		}},
	}
	wire := projectionToWire(projection, "v=1", nil)
	data, err := json.Marshal(wire)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, `"minor":"-1234"`)
	assert.Contains(t, text, `"decimal":"-12.34"`)
	assert.NotContains(t, text, "private-provider")
	assert.NotContains(t, text, "private note")
	assert.NotContains(t, text, "metadata")
	assert.NotContains(t, text, `"provider":`)
	assert.NotContains(t, text, `"provider_id":`)
	assert.NotContains(t, text, "returns")
}

func TestWireRequestShapesUseOpaqueSelectionAndBoundedWindow(t *testing.T) {
	t.Parallel()

	body := TransitionBody{
		Query: "v=1", Selection: string(app.EmptySelection()), Action: app.ActionCycleGrouping,
		Window: Window{Offset: 10, Limit: 20},
	}
	data, err := json.Marshal(body)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"selection":"mfsel1.`)
	assert.NotContains(t, string(data), `"ids"`)
}

func TestWireProjectionIncludesServerDerivedViewMetadata(t *testing.T) {
	t.Parallel()

	projection := app.WebProjection{
		State: app.DefaultViewState(), Selection: app.EmptySelection(), Revision: 42,
		Pending: app.PendingSummary{ActiveOperations: 3, InactiveOperations: 1, AffectedTransactions: 7},
	}
	wire := projectionToWire(projection, "v=1", nil)
	assert.Equal(t, "42", wire.Revision)
	assert.Equal(t, PendingSummary{ActiveOperations: 3, InactiveOperations: 1, AffectedTransactions: 7}, wire.Pending)
	assert.Equal(t, string(projection.State.Current.Mode), wire.View.Mode)
	assert.Equal(t, string(projection.State.Current.Dimension), wire.View.Grouping)
	assert.Equal(t, string(projection.State.Current.Sort.Field), wire.View.SortField)
	assert.Equal(t, string(projection.State.Current.Sort.Direction), wire.View.SortDirection)
}

func TestWireProjectionCarriesAmazonSettingsWithoutSourceFacts(t *testing.T) {
	projection := app.WebProjection{
		State: app.DefaultViewState(), Selection: app.EmptySelection(), ProfileKind: "amazon",
		AmazonSettings: &app.AmazonProjectionSettings{Currency: "EUR", Scale: 2},
	}

	wire := projectionToWire(projection, "v=1", nil)

	require.NotNil(t, wire.AmazonSettings)
	assert.Equal(t, "EUR", wire.AmazonSettings.Currency)
	assert.Equal(t, uint8(2), wire.AmazonSettings.Scale)
}

func TestWireCapabilitiesIncludeCategoriesAndUnavailableWebActions(t *testing.T) {
	t.Parallel()

	projection := app.WebProjection{Actions: []app.ActionID{app.ActionOpenSearch}}
	wire := projectionToWire(projection, "v=1", nil)
	byID := make(map[app.ActionID]Capability, len(wire.Capabilities))
	for _, capability := range wire.Capabilities {
		byID[capability.ID] = capability
	}
	assert.True(t, byID[app.ActionOpenSearch].Available)
	assert.Equal(t, "Filters", byID[app.ActionOpenSearch].Category)
	assert.False(t, byID[app.ActionEditMerchant].Available)
	assert.Equal(t, "Actions", byID[app.ActionEditMerchant].Category)
	_, lifecycleVisible := byID[app.ActionQuit]
	assert.False(t, lifecycleVisible)
}

func TestWireCapabilitiesListKeylessWebActionsWithoutKeys(t *testing.T) {
	t.Parallel()

	projection := app.WebProjection{
		Actions: []app.ActionID{app.ActionSetSort},
		Capabilities: []app.Capability{{
			Action: app.ActionSetCurrency, Available: false, Reason: "The profile has one currency.",
		}},
	}
	wire := projectionToWire(projection, "v=1", nil)
	byID := make(map[app.ActionID]Capability, len(wire.Capabilities))
	for _, capability := range wire.Capabilities {
		byID[capability.ID] = capability
	}
	for _, id := range []app.ActionID{
		app.ActionSetSubGrouping, app.ActionSetCurrency, app.ActionSetSort,
		app.ActionApplySearch, app.ActionApplyFilters,
	} {
		capability, listed := byID[id]
		require.True(t, listed, id)
		assert.Empty(t, capability.KeyDisplay, id)
	}
	assert.True(t, byID[app.ActionSetSort].Available)
	assert.False(t, byID[app.ActionSetCurrency].Available)
	assert.Equal(t, "The profile has one currency.", byID[app.ActionSetCurrency].Reason)
	_, tuiOnly := byID[app.ActionToggleTime]
	assert.False(t, tuiOnly, "actions the web does not offer stay out")
}

func TestWireProjectionCarriesCurrenciesAndSelectionSummary(t *testing.T) {
	t.Parallel()

	first, err := domain.ParseDate("2024-01-02")
	require.NoError(t, err)
	latest, err := domain.ParseDate("2026-03-04")
	require.NoError(t, err)
	projection := app.WebProjection{
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
		Currencies: []domain.CurrencyCount{
			{Currency: "USD", Scale: 2, Count: 9, First: first, Latest: latest},
			{Currency: "KWD", Scale: 3, Count: 1, First: latest, Latest: latest},
		},
		SelectionSummary: []domain.CurrencyStats{{
			Currency: "USD", Scale: 2, Count: 2,
			In:  domain.Money{Minor: 500, Currency: "USD", Scale: 2},
			Out: domain.Money{Minor: -300, Currency: "USD", Scale: 2},
			Net: domain.Money{Minor: 200, Currency: "USD", Scale: 2},
		}},
	}

	wire := projectionToWire(projection, "v=1", nil)

	assert.Equal(t, []CurrencyCount{
		{Currency: "USD", Scale: 2, Count: 9, First: "2024-01-02", Latest: "2026-03-04"},
		{Currency: "KWD", Scale: 3, Count: 1, First: "2026-03-04", Latest: "2026-03-04"},
	}, wire.Currencies)
	require.Len(t, wire.SelectionSummary, 1)
	assert.Equal(t, 2, wire.SelectionSummary[0].Count)
	assert.Equal(t, "+2.00", wire.SelectionSummary[0].Net.Display)
}

func TestWireProjectionOmitsSelectionSummaryWhenNothingIsSelected(t *testing.T) {
	t.Parallel()

	wire := projectionToWire(app.WebProjection{
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	}, "v=1", nil)
	data, err := json.Marshal(wire)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "selection_summary")
	assert.Contains(t, string(data), `"currencies":[]`)
}

func TestWireViewMetadataCarriesCurrencyScopeAndSubGrouping(t *testing.T) {
	t.Parallel()

	state := app.DefaultViewState()
	subGrouping := domain.DimensionCategory
	state.Current.SubGrouping = &subGrouping
	state.Current.Currency = &domain.CurrencyScope{Currency: "EUR", Scale: 2}
	wire := projectionToWire(
		app.WebProjection{State: state, Selection: app.EmptySelection()}, "v=1", nil,
	)

	assert.Equal(t, "category", wire.View.SubGrouping)
	assert.Equal(t, &CurrencyScope{Currency: "EUR", Scale: 2}, wire.View.Currency)

	wire = projectionToWire(app.WebProjection{
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
	}, "v=1", nil)
	assert.Empty(t, wire.View.SubGrouping)
	assert.Nil(t, wire.View.Currency)
}

func TestWireRowsCarryStagedValuesAndSavingsRate(t *testing.T) {
	t.Parallel()

	hidden := true
	rate := -125
	projection := app.WebProjection{
		State: app.DefaultViewState(), Selection: app.EmptySelection(),
		DetailRows: []app.WebDetailRow{
			{Identity: "renamed", Staged: &app.StagedValues{
				Merchant: &domain.EntityRef{ID: "m2", Name: "New Merchant"},
				Category: &domain.CategoryRef{ID: "c2", Name: "New Category", Group: "New Group"},
				Hidden:   &hidden,
			}},
			{Identity: "removed", Staged: &app.StagedValues{Deleted: true}},
			{Identity: "plain"},
		},
		AggregateRows: []app.WebAggregateRow{
			{Identity: "time", SavingsRateTenths: &rate},
			{Identity: "other"},
		},
	}

	wire := projectionToWire(projection, "v=1", nil)

	staged := wire.DetailRows[0].Staged
	require.NotNil(t, staged)
	assert.Equal(t, "New Merchant", *staged.Merchant)
	assert.Equal(t, "New Category", *staged.Category)
	assert.Equal(t, "New Group", *staged.Group)
	assert.True(t, *staged.Hidden)
	assert.False(t, staged.Deleted)
	assert.Equal(t, &StagedValues{Deleted: true}, wire.DetailRows[1].Staged)
	assert.Nil(t, wire.DetailRows[2].Staged)
	require.NotNil(t, wire.AggregateRows[0].SavingsRateTenths)
	assert.Equal(t, -125, *wire.AggregateRows[0].SavingsRateTenths)
	assert.Nil(t, wire.AggregateRows[1].SavingsRateTenths)
}

func TestWireCapabilitiesCarryDynamicUnavailableReason(t *testing.T) {
	t.Parallel()

	projection := app.WebProjection{Capabilities: []app.Capability{{
		Action: app.ActionRefreshProvider, Available: false,
		Reason: "Connect a provider before refreshing.",
	}}}
	wire := projectionToWire(projection, "v=1", nil)
	for _, capability := range wire.Capabilities {
		if capability.ID == app.ActionRefreshProvider {
			assert.False(t, capability.Available)
			assert.Equal(t, "Connect a provider before refreshing.", capability.Reason)
			return
		}
	}
	t.Fatal("provider refresh capability is missing")
}
