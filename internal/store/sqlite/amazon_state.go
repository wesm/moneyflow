package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/wesm/moneyflow/internal/domain"
	"github.com/wesm/moneyflow/internal/store"
)

// LoadAmazonState returns a detached committed snapshot and Amazon source ledger.
func (profile *profile) LoadAmazonState(ctx context.Context) (store.AmazonImportState, error) {
	transaction, err := profile.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return store.AmazonImportState{}, mapDriverError(err, store.CodeStoreError)
	}
	finished := false
	defer func() {
		if !finished {
			_ = transaction.Rollback()
		}
	}()
	state, err := loadAmazonState(ctx, transaction)
	if err != nil {
		return store.AmazonImportState{}, err
	}
	if err = transaction.Commit(); err != nil {
		return store.AmazonImportState{}, mapDriverError(err, store.CodeStoreError)
	}
	finished = true
	return state, nil
}

func loadAmazonState(ctx context.Context, queryer snapshotQueryer) (store.AmazonImportState, error) {
	snapshot, err := loadSnapshot(ctx, queryer)
	if err != nil {
		return store.AmazonImportState{}, err
	}
	settings, err := loadAmazonSettings(ctx, queryer)
	if err != nil {
		return store.AmazonImportState{}, err
	}
	items, err := loadAmazonItems(ctx, queryer)
	if err != nil {
		return store.AmazonImportState{}, err
	}
	allocations, err := loadLabelAllocations(ctx, queryer)
	if err != nil {
		return store.AmazonImportState{}, err
	}
	return store.AmazonImportState{
		Snapshot: snapshot, Settings: settings, Items: items, Allocations: allocations,
	}, nil
}

func loadAmazonSettings(ctx context.Context, queryer snapshotQueryer) (*store.AmazonSettings, error) {
	var settings store.AmazonSettings
	var taxonomy sql.NullString
	var created int64
	err := queryer.QueryRowContext(ctx, `
		SELECT currency, scale, taxonomy_source_profile_id, created_at_unix_ms
		FROM amazon_profile_settings WHERE singleton = 1`).
		Scan(&settings.Currency, &settings.Scale, &taxonomy, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, loadFailure(err)
	}
	settings.TaxonomySourceProfileID = taxonomy.String
	settings.CreatedAt = unixMilliTime(created)
	return &settings, nil
}

func loadAmazonItems(ctx context.Context, queryer snapshotQueryer) ([]store.AmazonOrderItem, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT local_transaction_id, source_identity, order_id, asin, asinless_key,
			product_name, order_date, quantity, amount_minor, unit_price_minor, currency,
			scale, order_status, shipment_status, identity_fingerprint, full_fingerprint, retired
		FROM amazon_order_items ORDER BY source_identity`)
	if err != nil {
		return nil, loadFailure(err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]store.AmazonOrderItem, 0)
	for rows.Next() {
		var item store.AmazonOrderItem
		var asin sql.NullString
		var unitPrice sql.NullInt64
		var date string
		var retired int
		if err = rows.Scan(
			&item.LocalTransactionID, &item.SourceIdentity, &item.OrderID, &asin,
			&item.ASINLessKey, &item.ProductName, &date, &item.Quantity, &item.AmountMinor,
			&unitPrice, &item.Currency, &item.Scale, &item.OrderStatus, &item.ShipmentStatus,
			&item.IdentityFingerprint, &item.FullFingerprint, &retired,
		); err != nil {
			return nil, loadFailure(err)
		}
		item.ASIN = asin.String
		if unitPrice.Valid {
			value := unitPrice.Int64
			item.UnitPriceMinor = &value
		}
		item.OrderDate, err = domain.ParseDate(date)
		if err != nil {
			return nil, store.NewError(store.CodeStoreCorrupt, err)
		}
		item.Retired = retired == 1
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, loadFailure(err)
	}
	return items, nil
}

func unixMilliTime(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}
