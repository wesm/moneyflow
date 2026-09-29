package amazon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/moneyflow/internal/domain"
)

func TestASINLessKeyIsStableNormalizedSHA256(t *testing.T) {
	t.Parallel()
	left, err := ASINLessKey("  Example\u00a0 Product  ")
	require.NoError(t, err)
	right, err := ASINLessKey("Example Product")
	require.NoError(t, err)
	assert.Equal(t, left, right)
	assert.Regexp(t, `^amazon:asinless:[0-9a-f]{64}$`, left)
}

func TestIdentityFingerprintIgnoresStatusAndUnitPrice(t *testing.T) {
	t.Parallel()
	date, err := domain.ParseDate("2026-08-19")
	require.NoError(t, err)
	unit := int64(500)
	base := Row{
		OrderID: "order-1", ASIN: "ASIN1", OrderDate: date, ProductName: "Product",
		Quantity: 1, AmountMinor: -1000, Currency: "USD", Scale: 2,
		OrderStatus: "Closed", ShipmentStatus: "Shipped", UnitPriceMinor: &unit,
	}
	first, err := Fingerprints(base)
	require.NoError(t, err)
	changed := base
	changed.OrderStatus = "Delivered"
	changed.ShipmentStatus = "Delivered"
	changedUnit := int64(999)
	changed.UnitPriceMinor = &changedUnit
	second, err := Fingerprints(changed)
	require.NoError(t, err)

	assert.Equal(t, first.Identity, second.Identity)
	assert.NotEqual(t, first.Full, second.Full)
}
