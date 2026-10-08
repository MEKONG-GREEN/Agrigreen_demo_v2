package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// StatRow là số thửa chưa ngừng dùng và tổng diện tích của một nhóm.
type StatRow struct {
	Key    string
	Plots  int64
	AreaHa decimal.Decimal
}

func scanStats(rows pgx.Rows, err error) ([]StatRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatRow
	for rows.Next() {
		var r StatRow
		var area string
		if err := rows.Scan(&r.Key, &r.Plots, &area); err != nil {
			return nil, err
		}
		d, err := decimal.NewFromString(area)
		if err != nil {
			return nil, err
		}
		r.AreaHa = d
		out = append(out, r)
	}
	return out, rows.Err()
}

// StatsByCommune đếm thửa chưa ngừng dùng theo xã; all = mọi xã.
func StatsByCommune(ctx context.Context, tx pgx.Tx, all bool, communes []string) ([]StatRow, error) {
	return scanStats(tx.Query(ctx, `SELECT commune_code, count(*), coalesce(sum(area_ha), 0)::numeric(14,4)::text
	  FROM pf_coop.plot WHERE status <> 'retired' AND ($1::boolean OR commune_code = ANY($2::text[]))
	  GROUP BY commune_code ORDER BY commune_code`, all, units(communes)))
}

// StatsByCooperative đếm thửa chưa ngừng dùng theo HTX.
func StatsByCooperative(ctx context.Context, tx pgx.Tx, coops []uuid.UUID) ([]StatRow, error) {
	ss := make([]string, 0, len(coops))
	for _, c := range coops {
		ss = append(ss, c.String())
	}
	return scanStats(tx.Query(ctx, `SELECT cooperative_ref::text, count(*), coalesce(sum(area_ha), 0)::numeric(14,4)::text
	  FROM pf_coop.plot WHERE status <> 'retired' AND cooperative_ref = ANY($1::text[]::uuid[])
	  GROUP BY cooperative_ref`, ss))
}
