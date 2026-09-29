package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
)

// EdgeRow là một cạnh đọc cho định tuyến.
type EdgeRow struct {
	ID, Source, Target int64
	CostM              float64
	MaxLoadTons        *decimal.Decimal
	Certain            bool
	Limiting           string
	StructureRef       *uuid.UUID
	SegmentRef         uuid.UUID
	DataSource         string
	Points             []domain.Point
}

// Snap là điểm bám gần nhất trên một cạnh đi được.
type Snap struct {
	Edge     EdgeRow
	Fraction float64 // vị trí trên cạnh (ST_LineLocatePoint)
	AccessM  float64 // khoảng cách geodesic từ điểm tới cạnh
}

func loadFilter(minLoad decimal.Decimal) *string {
	if !minLoad.IsPositive() {
		return nil
	}
	s := minLoad.String()
	return &s
}

const edgeCols = `id, source, target, cost_m, max_load_tons::text, certain, limiting, limiting_structure_ref, segment_ref, data_source,
  ST_AsGeoJSON(geom, 7)`

func scanEdge(r pgx.Row, extra ...any) (EdgeRow, error) {
	var e EdgeRow
	var ml *string
	var geo string
	dst := append([]any{&e.ID, &e.Source, &e.Target, &e.CostM, &ml, &e.Certain, &e.Limiting, &e.StructureRef, &e.SegmentRef,
		&e.DataSource, &geo}, extra...)
	if err := r.Scan(dst...); err != nil {
		return e, err
	}
	var err error
	if e.MaxLoadTons, err = textDec(ml); err != nil {
		return e, err
	}
	e.Points, err = ParseLine(geo)
	return e, err
}

// NearestEdge tìm cạnh đi được (lọc tải trọng) gần điểm nhất trong phiên bản.
// Cạnh chưa kết luận tải trọng bị loại khi có lọc (không đoán là đi được).
func NearestEdge(ctx context.Context, tx pgx.Tx, vid uuid.UUID, at domain.Point, minLoad decimal.Decimal) (*Snap, error) {
	var s Snap
	var err error
	s.Edge, err = scanEdge(tx.QueryRow(ctx, `WITH p AS (SELECT ST_SetSRID(ST_MakePoint($2, $3), 4326) AS g)
	  SELECT `+edgeCols+`, ST_LineLocatePoint(e.geom, p.g), ST_Distance(e.geom::geography, p.g::geography)
	  FROM pf_waterway.edge e, p
	  WHERE e.network_version_id = $1 AND ($4::numeric IS NULL OR e.max_load_tons >= $4::numeric)
	  ORDER BY e.geom <-> p.g, e.id LIMIT 1`, vid, at.Lng, at.Lat, loadFilter(minLoad)), &s.Fraction, &s.AccessM)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// PathStep là một bước của đường đi pgr_dijkstra.
type PathStep struct {
	Start, End int64
	Node, Edge int64
	AggCost    float64
}

// Dijkstra chạy pgr_dijkstra nhiều-nhiều trên đồ thị của phiên bản, lọc tải trọng.
// Chỉ dùng pgr_dijkstra (có ở pgRouting 3.x và 4.x). Câu lệnh cạnh dựng bằng
// format(%L) phía CSDL — không nối chuỗi ở Go; RLS vẫn áp trong câu lệnh con.
func Dijkstra(ctx context.Context, tx pgx.Tx, vid uuid.UUID, minLoad decimal.Decimal, starts, ends []int64) ([]PathStep, error) {
	rows, err := tx.Query(ctx, `SELECT start_vid, end_vid, node, edge, agg_cost FROM pgr_dijkstra(
	    format('SELECT id, source, target, cost_m AS cost, reverse_cost FROM pf_waterway.edge
	            WHERE network_version_id = %L AND (%L::numeric IS NULL OR max_load_tons >= %L::numeric)', $1::uuid, $2::text, $2::text),
	    $3::bigint[], $4::bigint[], directed := true)
	  ORDER BY start_vid, end_vid, path_seq`, vid, loadFilter(minLoad), starts, ends)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Row) (PathStep, error) {
		var p PathStep
		err := r.Scan(&p.Start, &p.End, &p.Node, &p.Edge, &p.AggCost)
		return p, err
	})
}

// EdgesByID đọc các cạnh theo id trong phiên bản.
func EdgesByID(ctx context.Context, tx pgx.Tx, vid uuid.UUID, idsv []int64) (map[int64]EdgeRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+edgeCols+" FROM pf_waterway.edge WHERE network_version_id = $1 AND id = ANY($2::bigint[])", vid, idsv)
	if err != nil {
		return nil, err
	}
	list, err := collect(rows, func(r pgx.Row) (EdgeRow, error) { return scanEdge(r) })
	if err != nil {
		return nil, err
	}
	out := make(map[int64]EdgeRow, len(list))
	for _, e := range list {
		out[e.ID] = e
	}
	return out, nil
}
