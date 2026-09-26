package sqlite

import (
	"context"
	"database/sql"

	"github.com/wesm/moneyflow/internal/store"
)

func loadProviderWriteRestrictions(ctx context.Context, queryer providerRowsQueryer) ([]store.ProviderWriteRestriction, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT entity_type, entity_id, reason
		FROM provider_write_restrictions ORDER BY entity_type, entity_id`)
	if err != nil {
		return nil, mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = rows.Close() }()
	var result []store.ProviderWriteRestriction
	for rows.Next() {
		var restriction store.ProviderWriteRestriction
		if err = rows.Scan(&restriction.Kind, &restriction.EntityID, &restriction.Reason); err != nil {
			return nil, mapDriverError(err, store.CodeStoreError)
		}
		result = append(result, restriction)
	}
	return result, mapDriverError(rows.Err(), store.CodeStoreError)
}

func replaceProviderWriteRestrictions(ctx context.Context, connection *sql.Conn, restrictions []store.ProviderWriteRestriction) error {
	if _, err := connection.ExecContext(ctx, "DELETE FROM provider_write_restrictions"); err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	if len(restrictions) == 0 {
		return nil
	}
	statement, err := connection.PrepareContext(ctx, `INSERT INTO provider_write_restrictions
		(entity_type, entity_id, reason) VALUES (?, ?, ?)`)
	if err != nil {
		return mapDriverError(err, store.CodeStoreError)
	}
	defer func() { _ = statement.Close() }()
	for _, restriction := range restrictions {
		if _, err = statement.ExecContext(ctx, restriction.Kind, restriction.EntityID, restriction.Reason); err != nil {
			return mapDriverError(err, store.CodeStoreError)
		}
	}
	return nil
}
