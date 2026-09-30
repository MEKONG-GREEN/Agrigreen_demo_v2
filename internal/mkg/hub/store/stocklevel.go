package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// StockLevel là tồn của một dạng hàng tại một cơ sở (bảng chiếu lot_balance).
type StockLevel struct {
	FacilityID  uuid.UUID
	Product     string
	AvailableKg string
	Lots        int
}

// StockByProduct tổng tồn của các lô còn tồn theo cơ sở cho một dạng hàng (facility nil = mọi cơ sở).
func StockByProduct(ctx context.Context, tx pgx.Tx, product string, facility *uuid.UUID) ([]StockLevel, error) {
	rows, err := tx.Query(ctx, `SELECT facility_id, sum(remaining_kg)::numeric(14,3)::text, count(*)::int FROM mkg_hub.lot_balance
	    WHERE product = $1 AND status = 'in_stock' AND ($2::uuid IS NULL OR facility_id = $2)
	    GROUP BY facility_id ORDER BY facility_id`, product, facility)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StockLevel
	for rows.Next() {
		var l StockLevel
		if err := rows.Scan(&l.FacilityID, &l.AvailableKg, &l.Lots); err != nil {
			return nil, err
		}
		l.Product = product
		out = append(out, l)
	}
	return out, rows.Err()
}
