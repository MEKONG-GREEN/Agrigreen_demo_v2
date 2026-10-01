package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ProductHub là một hub MKG (pf_geo.hub_view) cho sản phẩm dữ liệu hub_capacity (G5): tọa độ dạng
// chuỗi thập phân (không float), không hình học vùng.
type ProductHub struct {
	HubRef       uuid.UUID
	Code         string
	Name         string
	Kind         string
	Lat, Lng     string
	ProvinceCode string
	CommuneCode  string
	Capacity     *decimal.Decimal
	Unit         string
	Status       string
	ReceivedAt   time.Time
}

// ProductHubs trả hub có tỉnh, loại kho/hub (không nhà máy), theo hub_ref sau after, tối đa limit.
// province rỗng = mọi tỉnh.
func ProductHubs(ctx context.Context, tx pgx.Tx, province string, after uuid.UUID, limit int) ([]ProductHub, error) {
	rows, err := tx.Query(ctx, `SELECT h.hub_ref, h.code, h.name, h.kind, round(ST_Y(h.point)::numeric, 6)::text,
	    round(ST_X(h.point)::numeric, 6)::text, h.province_code, coalesce(h.commune_code, ''), h.capacity::text, h.unit, h.status,
	    h.received_at
	  FROM pf_geo.hub_view h
	  WHERE h.province_code IS NOT NULL AND h.kind IN ('kho', 'hub') AND ($1::text = '' OR h.province_code = $1::text)
	    AND h.hub_ref > $2::uuid
	  ORDER BY h.hub_ref LIMIT $3::int`, province, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProductHub
	for rows.Next() {
		var h ProductHub
		var capacity *string
		if err := rows.Scan(&h.HubRef, &h.Code, &h.Name, &h.Kind, &h.Lat, &h.Lng, &h.ProvinceCode, &h.CommuneCode, &capacity, &h.Unit,
			&h.Status, &h.ReceivedAt); err != nil {
			return nil, err
		}
		if h.Capacity, err = parseDec(capacity); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// PlotFact là dữ kiện tối thiểu của thửa cho sản phẩm tổng hợp (không hình học, không tên).
type PlotFact struct {
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
	    p.area_ha::text
	  FROM pf_geo.plot p WHERE p.ref = ANY($1::text[]::uuid[])`, ss)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlotFact
	for rows.Next() {
		var p PlotFact
		var coop, hh *uuid.UUID
		var area string
		if err := rows.Scan(&p.Ref, &p.Code, &p.CommuneCode, &coop, &hh, &p.Status, &p.Version, &area); err != nil {
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
