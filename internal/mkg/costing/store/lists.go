package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Danh sách cho web nội bộ (G4 đóng khoảng trống: trước đây web chỉ nhớ mã vừa xem). Mọi danh sách phân trang
// theo con trỏ = khóa UUIDv7 cuối trang, mới nhất trước.

// LotCostRow là một dòng lot_cost_current kèm lô.
type LotCostRow struct {
	LotRef uuid.UUID
	LotCost
}

// LotsWithCosts trả các lô có giá thành (lot_ref < before, mới nhất trước, tối đa limit lô) và mọi nhóm của chúng.
func LotsWithCosts(ctx context.Context, tx pgx.Tx, before *uuid.UUID, limit int) ([]LotCostRow, error) {
	rows, err := tx.Query(ctx, `SELECT c.lot_ref, c.category, c.amount::text, c.mass_kg::text, c.entries FROM mkg_costing.lot_cost_current c
	  WHERE c.lot_ref IN (SELECT DISTINCT lot_ref FROM mkg_costing.lot_cost_current WHERE ($1::uuid IS NULL OR lot_ref < $1)
	                      ORDER BY lot_ref DESC LIMIT $2)
	  ORDER BY c.lot_ref DESC, c.category`, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LotCostRow, error) {
		var c LotCostRow
		var amt string
		var kg *string
		if err := r.Scan(&c.LotRef, &c.Category, &amt, &kg, &c.Entries); err != nil {
			return c, err
		}
		var err error
		if c.Amount, err = money.ParseAmount(amt); err != nil {
			return c, err
		}
		c.MassKg, err = kgPtr(kg)
		return c, err
	})
}

// ListCarbon liệt kê khoản doanh thu carbon đã ghi (kỳ tùy chọn), mới nhất trước.
func ListCarbon(ctx context.Context, tx pgx.Tx, period *uuid.UUID, before *uuid.UUID, limit int) ([]Carbon, error) {
	rows, err := tx.Query(ctx, "SELECT "+carbonCols+` FROM mkg_costing.carbon_revenue_event
	  WHERE ($1::uuid IS NULL OR period_ref = $1) AND ($2::uuid IS NULL OR id < $2) ORDER BY id DESC LIMIT $3`, period, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Carbon, error) { return scanCarbon(r) })
}

// ListJobs liệt kê job đóng kỳ (kỳ / trạng thái tùy chọn), mới nhất trước.
func ListJobs(ctx context.Context, tx pgx.Tx, period *uuid.UUID, status string, before *uuid.UUID, limit int) ([]CloseJob, error) {
	rows, err := tx.Query(ctx, "SELECT "+jobCols+` FROM mkg_costing.close_job
	  WHERE ($1::uuid IS NULL OR period_id = $1) AND ($2 = '' OR status = $2) AND ($3::uuid IS NULL OR id < $3)
	  ORDER BY id DESC LIMIT $4`, period, status, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (CloseJob, error) { return scanJob(r) })
}
