package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// PlotFact là dữ kiện tối thiểu của thửa cho sản phẩm tổng hợp (không hình học, không tên).
type PlotFact struct {
	TenantID       uuid.UUID // đơn vị chủ (W4.2)
	Ref            uuid.UUID
	Code           string
	CommuneCode    string
	CooperativeRef uuid.UUID
	HouseholdRef   uuid.UUID
	Status         string
	Version        int64
	AreaHa         decimal.Decimal
}

// PlotFacts đọc dữ kiện của các thửa (kể cả đã ngừng dùng) theo ref.
func PlotFacts(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) ([]PlotFact, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	ss := make([]string, len(refs))
	for i, r := range refs {
		ss[i] = r.String()
	}
	rows, err := tx.Query(ctx, `SELECT p.ref, p.code, p.commune_code, p.cooperative_ref, p.household_ref, p.status, p.current_version,
	    p.area_ha::text, p.tenant_id
	  FROM pf_coop.plot p WHERE p.ref = ANY($1::text[]::uuid[])`, ss)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlotFact
	for rows.Next() {
		var p PlotFact
		var coop, hh *uuid.UUID
		var area string
		if err := rows.Scan(&p.Ref, &p.Code, &p.CommuneCode, &coop, &hh, &p.Status, &p.Version, &area, &p.TenantID); err != nil {
			return nil, err
		}
		if coop != nil {
			p.CooperativeRef = *coop
		}
		if hh != nil {
			p.HouseholdRef = *hh
		}
		if p.AreaHa, err = decOf(area); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
