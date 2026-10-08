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
