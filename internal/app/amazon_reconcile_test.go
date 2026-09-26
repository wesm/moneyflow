package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/importer/amazon"
	"github.com/wesm/moneyflow/internal/store"
)

func TestAmazonReconcileExactRetiredEvidencePrecedesUnequalActiveSingleton(t *testing.T) {
	t.Parallel()
	existing := []store.AmazonOrderItem{
		amazonStoredItem("transaction-active", "source-active", "order-a", "ASIN-A", "active-fingerprint", false),
		amazonStoredItem("transaction-retired", "source-retired", "order-a", "ASIN-A", "returning-fingerprint", true),
	}
	rows := []amazon.Row{
		amazonIncomingRow("order-a", "ASIN-A", "returning-fingerprint", "a.csv", 2),
		amazonIncomingRow("order-a", "ASIN-A", "changed-active", "a.csv", 3),
	}
	result, err := reconcileAmazonRows(existing, rows, []string{"order-a"}, store.ProposedAmazonIDs{})
	require.NoError(t, err)

	byFingerprint := make(map[string]store.AmazonOrderItem)
	for _, item := range result.Items {
		if !item.Retired {
			byFingerprint[item.IdentityFingerprint] = item
		}
	}
	assert.Equal(t, domain.EntityID("transaction-retired"), byFingerprint["returning-fingerprint"].LocalTransactionID)
	assert.Equal(t, domain.EntityID("transaction-active"), byFingerprint["changed-active"].LocalTransactionID)
	assert.Equal(t, 1, result.Restored)
	assert.Equal(t, 1, result.Updated)
}

func TestAmazonReconcileObservedShrinkAndAbsentOrder(t *testing.T) {
	t.Parallel()
	existing := []store.AmazonOrderItem{
		amazonStoredItem("transaction-keep", "source-keep", "order-seen", "ASIN-A", "keep", false),
		amazonStoredItem("transaction-retire", "source-retire", "order-seen", "ASIN-B", "retire", false),
		amazonStoredItem("transaction-absent", "source-absent", "order-absent", "ASIN-C", "absent", false),
	}
	rows := []amazon.Row{amazonIncomingRow("order-seen", "ASIN-A", "keep", "a.csv", 2)}
	result, err := reconcileAmazonRows(existing, rows, []string{"order-seen"}, store.ProposedAmazonIDs{})
	require.NoError(t, err)

	items := make(map[domain.EntityID]store.AmazonOrderItem)
	for _, item := range result.Items {
		items[item.LocalTransactionID] = item
	}
	assert.False(t, items["transaction-keep"].Retired)
	assert.True(t, items["transaction-retire"].Retired)
	assert.False(t, items["transaction-absent"].Retired)
	assert.Equal(t, 1, result.Retired)
}

func TestAmazonReconcileOrderWideASINLessSingletonSurvivesLabelKeyChange(t *testing.T) {
	t.Parallel()
	existing := []store.AmazonOrderItem{
		amazonStoredItem("transaction-a", "source-a", "order-a", "", "old", false),
	}
	existing[0].ASINLessKey = "amazon:asinless:old"
	row := amazonIncomingRow("order-a", "", "new", "a.csv", 2)
	row.ASINLessKey = "amazon:asinless:new"
	result, err := reconcileAmazonRows(existing, []amazon.Row{row}, []string{"order-a"}, store.ProposedAmazonIDs{})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	assert.Equal(t, domain.EntityID("transaction-a"), result.Items[0].LocalTransactionID)
	assert.Equal(t, "amazon:asinless:new", result.Items[0].ASINLessKey)
}

func amazonStoredItem(transactionID, sourceID, orderID, asin, fingerprint string, retired bool) store.AmazonOrderItem {
	return store.AmazonOrderItem{
		LocalTransactionID: domain.EntityID(transactionID), SourceIdentity: sourceID,
		OrderID: orderID, ASIN: asin, ProductName: "Example Product", OrderDate: amazonTestDate(),
		Quantity: 1, AmountMinor: -1000, Currency: "USD", Scale: 2,
		OrderStatus: "Closed", ShipmentStatus: "Delivered",
		IdentityFingerprint: fingerprint, FullFingerprint: fingerprint + "-full", Retired: retired,
	}
}

func amazonIncomingRow(orderID, asin, fingerprint, filename string, record int) amazon.Row {
	return amazon.Row{
		OrderID: orderID, ASIN: asin, ProductName: "Example Product", OrderDate: amazonTestDate(),
		Quantity: 1, AmountMinor: -1000, Currency: "USD", Scale: 2,
		OrderStatus: "Closed", ShipmentStatus: "Delivered",
		IdentityFingerprint: fingerprint, FullFingerprint: fingerprint + "-full",
		RelativeFilename: filename, Record: record,
	}
}

func amazonTestDate() domain.Date {
	value, err := domain.ParseDate("2026-08-20")
	if err != nil {
		panic(err)
	}
	return value
}
