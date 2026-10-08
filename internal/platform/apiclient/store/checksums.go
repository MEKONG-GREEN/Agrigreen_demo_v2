package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// DatasetVersionsFrom liệt kê tối đa limit phiên bản của tập từ phiên bản from (tăng dần) — đối soát R-INT-10 (W5.7).
func DatasetVersionsFrom(ctx context.Context, tx pgx.Tx, dataset string, from int64, limit int) ([]DatasetVersion, error) {
	rows, err := tx.Query(ctx, `SELECT `+versionCols+` FROM pf_apiclient.dataset_version WHERE dataset = $1::text AND version >= $2::bigint
	  ORDER BY version LIMIT $3::int`, dataset, from, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DatasetVersion{}
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
