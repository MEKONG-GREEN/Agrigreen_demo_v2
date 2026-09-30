package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PartnerPlot là một dòng danh bạ thửa cho bên nhận của cấp quyền plot_directory (ADR-0020):
// hình học, diện tích, xã, HTX, trạng thái — KHÔNG có nông hộ, tên thửa, người tạo.
type PartnerPlot struct {
	Ref            uuid.UUID
	Code           string
	CommuneCode    string
	CooperativeRef *uuid.UUID
	Status         string
	AreaHa         string // decimal dạng chuỗi
	Geometry       json.RawMessage
	Revision       int64
	UpdatedAt      time.Time
}

const partnerPlotSQL = `SELECT ref, code, commune_code, cooperative_ref, status, area_ha::text, ST_AsGeoJSON(geom, 7), revision, updated_at
  FROM pf_geo.plot`

func scanPartnerPlot(row pgx.Row) (PartnerPlot, error) {
	var p PartnerPlot
	var g string
	err := row.Scan(&p.Ref, &p.Code, &p.CommuneCode, &p.CooperativeRef, &p.Status, &p.AreaHa, &g, &p.Revision, &p.UpdatedAt)
	p.Geometry = json.RawMessage(g)
	return p, err
}

// PartnerPlots phân trang thửa của tenant hiện tại (bên cấp) theo ref; coop ≠ Nil lọc theo HTX.
func PartnerPlots(ctx context.Context, tx pgx.Tx, coop, after uuid.UUID, limit int) ([]PartnerPlot, error) {
	rows, err := tx.Query(ctx, partnerPlotSQL+` WHERE ref > $1::uuid
	    AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR cooperative_ref = $2::uuid) ORDER BY ref LIMIT $3::int`, after, coop, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PartnerPlot{}
	for rows.Next() {
		p, err := scanPartnerPlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PartnerPlotByRef đọc một thửa; nil nếu không có.
func PartnerPlotByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*PartnerPlot, error) {
	p, err := scanPartnerPlot(tx.QueryRow(ctx, partnerPlotSQL+" WHERE ref = $1::uuid", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
