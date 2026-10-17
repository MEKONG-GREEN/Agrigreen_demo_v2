package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// CodeByAttribute reads the current identity of an item, including inactive rows.
// All selectors are SQL values; the caller checks the catalog and attribute schema.
func CodeByAttribute(ctx context.Context, tx pgx.Tx, catalog, attribute, value string) (string, error) {
	var code string
	err := tx.QueryRow(ctx, `SELECT code FROM pf_masterdata.catalog_item
        WHERE catalog = $1 AND attrs->>($2::text) = $3 ORDER BY code LIMIT 1`,
		catalog, attribute, value).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return code, err
}
