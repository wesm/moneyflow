package domain

import "io"

// AmazonItemFacts is the retained, privacy-reviewed source metadata for one imported order item.
// File names, row coordinates, addresses, payment data, and tracking data never enter this value.
type AmazonItemFacts struct {
	OrderID        string
	ASIN           string
	ASINLessKey    string
	ProductName    string
	Quantity       int64
	UnitPriceMinor *int64
	OrderStatus    string
	ShipmentStatus string
}

// NewAmazonSourceIdentity creates one opaque source-ledger identity.
func NewAmazonSourceIdentity(random io.Reader) (string, error) {
	return randomID("amazon_item_", random)
}
