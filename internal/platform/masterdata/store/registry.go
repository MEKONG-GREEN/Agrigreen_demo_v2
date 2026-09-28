package store

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// CatalogCounts đếm mục theo danh mục: [tổng, ngừng dùng].
func CatalogCounts(ctx context.Context, tx pgx.Tx) (map[string][2]int, error) {
	rows, err := tx.Query(ctx, `SELECT catalog, count(*), count(*) FILTER (WHERE NOT active)
	  FROM pf_masterdata.catalog_item GROUP BY catalog`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][2]int{}
	for rows.Next() {
		var c string
		var n, off int
		if err := rows.Scan(&c, &n, &off); err != nil {
			return nil, err
		}
		out[c] = [2]int{n, off}
	}
	return out, rows.Err()
}

// KeyStat là thống kê phiên bản của một khóa tham số.
type KeyStat struct {
	Versions  int
	Submitted int
	HasText   bool
}

// ParamKeyStats trả mọi khóa đã có phiên bản (lọc theo tiền tố) kèm số phiên bản chờ duyệt.
func ParamKeyStats(ctx context.Context, tx pgx.Tx, prefix string) (map[string]KeyStat, error) {
	rows, err := tx.Query(ctx, `SELECT param_key, count(*), count(*) FILTER (WHERE status = 'submitted'),
	    bool_or(value_text IS NOT NULL)
	  FROM pf_masterdata.param_version WHERE starts_with(param_key, $1) GROUP BY param_key`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]KeyStat{}
	for rows.Next() {
		var k string
		var s KeyStat
		if err := rows.Scan(&k, &s.Versions, &s.Submitted, &s.HasText); err != nil {
			return nil, err
		}
		out[k] = s
	}
	return out, rows.Err()
}
