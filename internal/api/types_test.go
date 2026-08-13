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
				Category: domain.CategoryRef{ID: "category-id", Name: "Example Category", Group: "Example Group"},
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
	assert.NotContains(t, text, "provider")
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

	projection := app.WebProjection{State: app.DefaultViewState(), Selection: app.EmptySelection()}
	wire := projectionToWire(projection, "v=1", nil)
	assert.Equal(t, string(projection.State.Current.Mode), wire.View.Mode)
	assert.Equal(t, string(projection.State.Current.Dimension), wire.View.Grouping)
	assert.Equal(t, string(projection.State.Current.Sort.Field), wire.View.SortField)
	assert.Equal(t, string(projection.State.Current.Sort.Direction), wire.View.SortDirection)
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
