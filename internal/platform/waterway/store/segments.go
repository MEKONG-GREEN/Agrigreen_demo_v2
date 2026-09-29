// Package store: SQL của pf/waterway, chỉ dùng pool vai waterway_app (R-05).
// Mọi truy vấn chạy trong giao dịch tenant (RLS, ADR-0005).
package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// LineJSON mã hóa polyline thành GeoJSON LineString.
func LineJSON(pts []domain.Point) string {
	var sb strings.Builder
	sb.WriteString(`{"type":"LineString","coordinates":[`)
	for i, p := range pts {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "[%s,%s]", fmtCoord(p.Lng), fmtCoord(p.Lat))
	}
	sb.WriteString("]}")
	return sb.String()
}

func fmtCoord(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ParseLine đọc GeoJSON LineString (ST_AsGeoJSON).
func ParseLine(s string) ([]domain.Point, error) {
	var g struct {
		Coordinates [][]float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(s), &g); err != nil {
		return nil, err
	}
	out := make([]domain.Point, 0, len(g.Coordinates))
	for _, c := range g.Coordinates {
		if len(c) < 2 {
			return nil, errors.New("waterway: tọa độ hỏng")
		}
		out = append(out, domain.Point{Lng: c[0], Lat: c[1]})
	}
	return out, nil
}

// ParsePoint đọc GeoJSON Point.
func ParsePoint(s string) (domain.Point, error) {
	var g struct {
		Coordinates []float64 `json:"coordinates"`
	}
	if err := json.Unmarshal([]byte(s), &g); err != nil {
		return domain.Point{}, err
	}
	if len(g.Coordinates) < 2 {
		return domain.Point{}, errors.New("waterway: điểm hỏng")
	}
	return domain.Point{Lng: g.Coordinates[0], Lat: g.Coordinates[1]}, nil
}

const lineSQL = "ST_SetSRID(ST_GeomFromGeoJSON(%s::text), 4326)"

func decText(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func textDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ---------------------------------------------------------------------------
// Đoạn

const segmentCols = `id, segment_ref, revision, name, waterway_class, status, ST_AsGeoJSON(geom, 9), length_m::text,
  width_m::text, depth_m::text, max_load_tons::text, data_source, note, event_time, recorded_at, supersedes_id,
  correction_reason, actor_ref, seq, encode(row_hash, 'hex')`

func scanSegment(row pgx.Row) (domain.Segment, error) {
	var s domain.Segment
	var geo, length string
	var w, dep, tons *string
	err := row.Scan(&s.ID, &s.SegmentRef, &s.Revision, &s.Name, &s.Class, &s.Status, &geo, &length, &w, &dep, &tons,
		&s.DataSource, &s.Note, &s.EventTime, &s.RecordedAt, &s.SupersedesID, &s.CorrectionReason, &s.ActorRef, &s.Seq, &s.RowHash)
	if err != nil {
		return s, err
	}
	if s.Points, err = ParseLine(geo); err != nil {
		return s, err
	}
	if s.LengthM, err = decimal.NewFromString(length); err != nil {
		return s, err
	}
	if s.WidthM, err = textDec(w); err != nil {
		return s, err
	}
	if s.DepthM, err = textDec(dep); err != nil {
		return s, err
	}
	s.MaxLoadTons, err = textDec(tons)
	return s, err
}

func collect[T any](rows pgx.Rows, scan func(pgx.Row) (T, error)) ([]T, error) {
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// InsertSegment ghi một bản sửa đoạn; length_m do CSDL tính (geodesic). idem_key
// 'segment:<ref>:<revision>' bảo đảm không thể có hai bản cùng số sửa (chống rẽ nhánh).
func InsertSegment(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *domain.Segment) error {
	var length string
	err := tx.QueryRow(ctx, `WITH g AS (SELECT `+fmt.Sprintf(lineSQL, "$6")+` AS geom)
	  INSERT INTO pf_waterway.segment_revision
	    (id, tenant_id, stream_key, segment_ref, revision, geom, length_m, name, waterway_class, status, width_m, depth_m,
	     max_load_tons, data_source, note, event_time, supersedes_id, correction_reason, actor_ref, idem_key, rule_version)
	  SELECT $1, $2, $3, $4, $5, g.geom, round(ST_Length(g.geom::geography)::numeric, 2), $7, $8, $9, $10::numeric, $11::numeric,
	         $12::numeric, $13, $14, $15, $16, $17, $18, $19, $20
	  FROM g
	  RETURNING length_m::text, seq, recorded_at, encode(row_hash, 'hex')`,
		s.ID, tid, "segment:"+s.SegmentRef.String(), s.SegmentRef, s.Revision, LineJSON(s.Points), s.Name, s.Class, s.Status,
		decText(s.WidthM), decText(s.DepthM), decText(s.MaxLoadTons), s.DataSource, s.Note, s.EventTime.UTC(), s.SupersedesID,
		s.CorrectionReason, s.ActorRef, fmt.Sprintf("segment:%s:%d", s.SegmentRef, s.Revision), domain.RuleVersion).
		Scan(&length, &s.Seq, &s.RecordedAt, &s.RowHash)
	if err != nil {
		return ledger.Classify(err)
	}
	s.LengthM, err = decimal.NewFromString(length)
	return err
}

// SegmentHead đọc bản hiện hành của một đoạn.
func SegmentHead(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*domain.Segment, error) {
	s, err := scanSegment(tx.QueryRow(ctx, "SELECT "+segmentCols+
		" FROM pf_waterway.segment_revision WHERE segment_ref = $1 ORDER BY revision DESC LIMIT 1", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SegmentHistory đọc mọi bản sửa của một đoạn (cũ → mới).
func SegmentHistory(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]domain.Segment, error) {
	rows, err := tx.Query(ctx, "SELECT "+segmentCols+" FROM pf_waterway.segment_revision WHERE segment_ref = $1 ORDER BY revision", ref)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanSegment)
}

// SegmentFilter lọc danh sách đoạn (Admin API).
type SegmentFilter struct {
	Status string
	After  uuid.UUID
	Limit  int
}

// ListSegmentHeads trả bản hiện hành của các đoạn theo segment_ref (phân trang cursor).
func ListSegmentHeads(ctx context.Context, tx pgx.Tx, f SegmentFilter) ([]domain.Segment, error) {
	rows, err := tx.Query(ctx, `SELECT `+segmentCols+` FROM (
	    SELECT DISTINCT ON (segment_ref) * FROM pf_waterway.segment_revision
	    WHERE segment_ref > $1 ORDER BY segment_ref, revision DESC) h
	  WHERE ($2::text = '' OR status = $2::text)
	  ORDER BY segment_ref LIMIT $3`, f.After, f.Status, f.Limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanSegment)
}

// ConfirmedSegmentsAt trả bản hiện hành ĐÃ XÁC NHẬN của mọi đoạn tại as_of —
// đầu vào duy nhất của bộ dựng (tuyến nháp/ngừng dùng không bao giờ vào đồ thị).
func ConfirmedSegmentsAt(ctx context.Context, tx pgx.Tx, asOf time.Time) ([]domain.Segment, error) {
	rows, err := tx.Query(ctx, `SELECT `+segmentCols+` FROM (
	    SELECT DISTINCT ON (segment_ref) * FROM pf_waterway.segment_revision
	    WHERE recorded_at <= $1 ORDER BY segment_ref, revision DESC) h
	  WHERE status = 'confirmed' ORDER BY segment_ref`, asOf.UTC())
	if err != nil {
		return nil, err
	}
	return collect(rows, scanSegment)
}

// DistanceToSegmentM là khoảng cách geodesic từ điểm tới bản hiện hành của đoạn.
func DistanceToSegmentM(ctx context.Context, tx pgx.Tx, ref uuid.UUID, at domain.Point) (float64, error) {
	var dist float64
	err := tx.QueryRow(ctx, `SELECT ST_Distance(geom::geography, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography)
	  FROM pf_waterway.segment_revision WHERE segment_ref = $1 ORDER BY revision DESC LIMIT 1`, ref, at.Lng, at.Lat).Scan(&dist)
	return dist, err
}

// HexHash đổi băm sang hex.
func HexHash(b []byte) string { return hex.EncodeToString(b) }

// ---------------------------------------------------------------------------
// Công trình

const structureCols = `id, structure_ref, revision, kind, name, status, segment_ref, ST_AsGeoJSON(geom, 9),
  clearance_height_m::text, clearance_width_m::text, depth_m::text, survey_date, data_source, note, event_time, recorded_at,
  supersedes_id, correction_reason, actor_ref, seq, encode(row_hash, 'hex')`

func scanStructure(row pgx.Row) (domain.Structure, error) {
	var s domain.Structure
	var geo string
	var h, w, dep *string
	err := row.Scan(&s.ID, &s.StructureRef, &s.Revision, &s.Kind, &s.Name, &s.Status, &s.SegmentRef, &geo, &h, &w, &dep,
		&s.SurveyDate, &s.DataSource, &s.Note, &s.EventTime, &s.RecordedAt, &s.SupersedesID, &s.CorrectionReason, &s.ActorRef,
		&s.Seq, &s.RowHash)
	if err != nil {
		return s, err
	}
	if s.At, err = ParsePoint(geo); err != nil {
		return s, err
	}
	if s.ClearanceHeightM, err = textDec(h); err != nil {
		return s, err
	}
	if s.ClearanceWidthM, err = textDec(w); err != nil {
		return s, err
	}
	s.DepthM, err = textDec(dep)
	return s, err
}

// InsertStructure ghi một bản sửa công trình.
func InsertStructure(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s *domain.Structure) error {
	var survey *time.Time
	if s.SurveyDate != nil {
		t := s.SurveyDate.UTC()
		survey = &t
	}
	err := tx.QueryRow(ctx, `INSERT INTO pf_waterway.structure_revision
	    (id, tenant_id, stream_key, structure_ref, revision, kind, name, status, segment_ref, geom, clearance_height_m,
	     clearance_width_m, depth_m, survey_date, data_source, note, event_time, supersedes_id, correction_reason, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, ST_SetSRID(ST_MakePoint($10, $11), 4326), $12::numeric, $13::numeric,
	          $14::numeric, $15::date, $16, $17, $18, $19, $20, $21, $22, $23)
	  RETURNING seq, recorded_at, encode(row_hash, 'hex')`,
		s.ID, tid, "structure:"+s.StructureRef.String(), s.StructureRef, s.Revision, s.Kind, s.Name, s.Status, s.SegmentRef,
		s.At.Lng, s.At.Lat, decText(s.ClearanceHeightM), decText(s.ClearanceWidthM), decText(s.DepthM), survey, s.DataSource,
		s.Note, s.EventTime.UTC(), s.SupersedesID, s.CorrectionReason, s.ActorRef,
		fmt.Sprintf("structure:%s:%d", s.StructureRef, s.Revision), domain.RuleVersion).
		Scan(&s.Seq, &s.RecordedAt, &s.RowHash)
	return ledger.Classify(err)
}

// StructureHead đọc bản hiện hành của một công trình.
func StructureHead(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*domain.Structure, error) {
	s, err := scanStructure(tx.QueryRow(ctx, "SELECT "+structureCols+
		" FROM pf_waterway.structure_revision WHERE structure_ref = $1 ORDER BY revision DESC LIMIT 1", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// StructureFilter lọc danh sách công trình.
type StructureFilter struct {
	SegmentRef *uuid.UUID
	After      uuid.UUID
	Limit      int
}

// ListStructureHeads trả bản hiện hành của các công trình (phân trang cursor theo structure_ref).
func ListStructureHeads(ctx context.Context, tx pgx.Tx, f StructureFilter) ([]domain.Structure, error) {
	rows, err := tx.Query(ctx, `SELECT `+structureCols+` FROM (
	    SELECT DISTINCT ON (structure_ref) * FROM pf_waterway.structure_revision
	    WHERE structure_ref > $1 ORDER BY structure_ref, revision DESC) h
	  WHERE ($2::uuid IS NULL OR segment_ref = $2::uuid)
	  ORDER BY structure_ref LIMIT $3`, f.After, f.SegmentRef, f.Limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanStructure)
}

// ActiveStructuresAt trả bản hiện hành đang dùng của mọi công trình tại as_of.
func ActiveStructuresAt(ctx context.Context, tx pgx.Tx, asOf time.Time) ([]domain.Structure, error) {
	rows, err := tx.Query(ctx, `SELECT `+structureCols+` FROM (
	    SELECT DISTINCT ON (structure_ref) * FROM pf_waterway.structure_revision
	    WHERE recorded_at <= $1 ORDER BY structure_ref, revision DESC) h
	  WHERE status = 'active' ORDER BY structure_ref`, asOf.UTC())
	if err != nil {
		return nil, err
	}
	return collect(rows, scanStructure)
}
