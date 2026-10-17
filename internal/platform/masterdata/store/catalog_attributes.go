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

// AttributeTokenOwners reads current reservations, including inactive items. Selectors are all bound
// values, not SQL identifiers. The caller supplies only registered attribute names and owns the Tx/RLS.
func AttributeTokenOwners(ctx context.Context, tx pgx.Tx, catalog, exceptCode string, attributes []string, separator string, tokens []string) (map[string]string, error) {
	out := map[string]string{}
	if len(tokens) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `
	  SELECT token, i.code
	    FROM pf_masterdata.catalog_item i
	    CROSS JOIN unnest($3::text[]) AS a
	    CROSS JOIN LATERAL unnest(string_to_array(coalesce(i.attrs->>a, ''), $4)) AS token
	   WHERE i.catalog = $1 AND i.code <> $2 AND token = ANY($5::text[])`,
		catalog, exceptCode, attributes, separator, tokens)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var token, code string
		if err := rows.Scan(&token, &code); err != nil {
			return nil, err
		}
		out[token] = code
	}
	return out, rows.Err()
}
