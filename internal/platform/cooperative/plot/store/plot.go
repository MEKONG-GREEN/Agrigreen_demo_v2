// Package store: SQL thửa của pf/cooperative trên schema pf_coop (W3.3, chuyển từ pf/geo: plot, plot_boundary,
// plot_event, boundary_conflict, cooperative_boundary, cooperative_area), chạy dưới vai r_cooperative (db.ForModule,
// R-05) và luôn trong giao dịch có ngữ cảnh tenant (tenant.WithTenantTx) — RLS áp dụng. Tên sổ (TG_ARGV, vd
// 'pf_geo.plot_boundary') giữ nguyên (D-3). Ranh giới hành chính ở pf/geo (geoport.AdminGeometry).
//
// Mọi phép hình học chính xác (ST_IsValid, diện tích trắc địa ST_Area(geography),
// chồng lấn) chạy trong PostGIS. Hình đi vào dưới dạng GeoJSON MultiPolygon do
// domain dựng; tham số luôn ép kiểu tường minh vì pool dùng chế độ thực thi của
// PgBouncer (QueryExecModeExec).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/plot/domain"
)

// geomSQL dựng hình từ tham số GeoJSON thứ n.
func geomSQL(n string) string {
	return "ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(" + n + "::text), 4326))"
}

func nilIfZero(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func parseDec(s *string) (*decimal.Decimal, error) {
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
// Phân tích hình
// ---------------------------------------------------------------------------

// Analysis là kết quả kiểm hình trong PostGIS.
type Analysis struct {
	Valid  bool
	Reason string          // lý do không hợp lệ (ST_IsValidReason)
	AreaHa decimal.Decimal // trắc địa trên ellipsoid WGS84
}

// Analyze kiểm tính hợp lệ (tự cắt, vòng sai...) và tính diện tích trắc địa.
func Analyze(ctx context.Context, tx pgx.Tx, geojson string) (Analysis, error) {
	var a Analysis
	var area string
	err := tx.QueryRow(ctx, `SELECT ST_IsValid(g), ST_IsValidReason(g), (ST_Area(g::geography) / 10000)::numeric(14,4)::text
	  FROM (SELECT `+geomSQL("$1")+` AS g) s`, geojson).Scan(&a.Valid, &a.Reason, &area)
	if err != nil {
		return a, err
	}
	a.AreaHa, err = decimal.NewFromString(area)
	return a, err
}

// OverlapHit là một thửa hiện hành giao với hình mới.
type OverlapHit struct {
	domain.Overlap
	AreaHa decimal.Decimal // diện tích thửa đã có
}

// FindOverlaps tìm thửa chưa ngừng dùng giao với hình (trừ thửa exclude), kèm
// diện tích phần giao (trắc địa). Thửa chỉ chung cạnh có phần giao bằng 0.
func FindOverlaps(ctx context.Context, tx pgx.Tx, geojson string, exclude uuid.UUID) ([]OverlapHit, error) {
	rows, err := tx.Query(ctx, `WITH n AS (SELECT `+geomSQL("$1")+` AS g)
	  SELECT p.ref, p.code, coalesce(p.cooperative_ref, '00000000-0000-0000-0000-000000000000'::uuid), p.area_ha::text,
	         (ST_Area(ST_Intersection(p.geom, n.g)::geography) / 10000)::numeric(14,4)::text
	  FROM pf_coop.plot p, n
	  WHERE p.status <> 'retired' AND p.ref <> $2::uuid AND p.geom && n.g AND ST_Intersects(p.geom, n.g)
	  ORDER BY p.ref LIMIT 100`, geojson, exclude)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OverlapHit
	for rows.Next() {
		var h OverlapHit
		var area, inter string
		if err := rows.Scan(&h.PlotRef, &h.Code, &h.CooperativeRef, &area, &inter); err != nil {
			return nil, err
		}
		if h.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		if h.IntersectHa, err = decimal.NewFromString(inter); err != nil {
			return nil, err
		}
		if h.IntersectHa.IsPositive() {
			out = append(out, h)
		}
	}
	return out, rows.Err()
}

// NextPlotCode sinh mã thửa hiển thị TH0000001…
func NextPlotCode(ctx context.Context, tx pgx.Tx) (string, error) {
	var code string
	err := tx.QueryRow(ctx, "SELECT 'TH' || lpad(nextval('pf_coop.plot_code_seq')::text, 7, '0')").Scan(&code)
	return code, err
}

// ---------------------------------------------------------------------------
// Sổ plot_boundary
// ---------------------------------------------------------------------------

// Boundary là một phiên bản ranh giới (một dòng sổ).
type Boundary struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	PlotRef          uuid.UUID
	Version          int64 // = seq trong chuỗi của thửa
	AreaHa           decimal.Decimal
	PointCount       int
	GPSAccuracyM     *decimal.Decimal
	Source           string
	CommuneCode      string
	CooperativeRef   uuid.UUID
	BecameCurrent    bool
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	GeoJSON          string // chỉ có khi đọc kèm hình
	// KN v7.7 (FN-XA-03.05, 000079): cách vẽ, diện tích khai báo, chế độ hỗ trợ Admin.
	Shape          string
	DeclaredAreaHa *decimal.Decimal
	OnBehalfOf     *uuid.UUID
	SupportReason  *string
}

// InsertBoundary ghi một phiên bản; DB tính seq, băm và diện tích trắc địa.
func InsertBoundary(ctx context.Context, tx pgx.Tx, b *Boundary, geojson string) error {
	var area string
	err := tx.QueryRow(ctx, `INSERT INTO pf_coop.plot_boundary
	    (id, tenant_id, stream_key, plot_ref, geom, area_ha, point_count, gps_accuracy_m, source, commune_code,
	     cooperative_ref, became_current, event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version,
	     shape, declared_area_ha, on_behalf_of, support_reason)
	  SELECT $1::uuid, $2::uuid, $3::text, $4::uuid, s.g, (ST_Area(s.g::geography) / 10000)::numeric(14,4), $6::int,
	         $7::numeric, $8::text, $9::text, $10::uuid, $11::boolean, $12::timestamptz, $13::uuid, $14::text, $15::uuid,
	         $16::text, $17::text, $18::text, $19::text, $20::numeric, $21::uuid, $22::text
	  FROM (SELECT `+geomSQL("$5")+` AS g) s
	  RETURNING seq, recorded_at, area_ha::text`,
		b.ID, b.TenantID, domain.StreamKey(b.PlotRef), b.PlotRef, geojson, b.PointCount, decPtr(b.GPSAccuracyM), b.Source,
		b.CommuneCode, nilIfZero(b.CooperativeRef), b.BecameCurrent, b.EventTime.UTC(), b.SupersedesID, b.CorrectionReason,
		b.ActorRef, b.DeviceID, b.IdemKey, domain.RuleVersion, nilIfEmpty(b.Shape), decPtr(b.DeclaredAreaHa), b.OnBehalfOf,
		b.SupportReason).Scan(&b.Version, &b.RecordedAt, &area)
	if err != nil {
		return err
	}
	b.AreaHa, err = decimal.NewFromString(area)
	return err
}

const boundaryCols = `id, tenant_id, plot_ref, seq, area_ha::text, point_count, gps_accuracy_m::text, source, commune_code,
  cooperative_ref, became_current, event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key,
  coalesce(shape, ''), declared_area_ha::text, on_behalf_of, support_reason`

func scanBoundary(row pgx.Row, withGeom bool) (Boundary, error) {
	var b Boundary
	var area string
	var acc *string
	var coop *uuid.UUID
	var declared *string
	dest := []any{&b.ID, &b.TenantID, &b.PlotRef, &b.Version, &area, &b.PointCount, &acc, &b.Source, &b.CommuneCode,
		&coop, &b.BecameCurrent, &b.EventTime, &b.RecordedAt, &b.SupersedesID, &b.CorrectionReason, &b.ActorRef,
		&b.DeviceID, &b.IdemKey, &b.Shape, &declared, &b.OnBehalfOf, &b.SupportReason}
	if withGeom {
		dest = append(dest, &b.GeoJSON)
	}
	if err := row.Scan(dest...); err != nil {
		return b, err
	}
	b.CooperativeRef = deref(coop)
	var err error
	if b.AreaHa, err = decimal.NewFromString(area); err != nil {
		return b, err
	}
	if b.GPSAccuracyM, err = parseDec(acc); err != nil {
		return b, err
	}
	b.DeclaredAreaHa, err = parseDec(declared)
	return b, err
}

func oneBoundary(ctx context.Context, tx pgx.Tx, where string, arg any) (*Boundary, error) {
	b, err := scanBoundary(tx.QueryRow(ctx, "SELECT "+boundaryCols+" FROM pf_coop.plot_boundary WHERE "+where, arg), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// BoundaryByIdemKey tìm phiên bản đã ghi với idem_key (gọi lặp, đồng bộ lặp).
func BoundaryByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*Boundary, error) {
	return oneBoundary(ctx, tx, "idem_key = $1::text", key)
}

// BoundaryByID đọc một phiên bản theo id.
func BoundaryByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Boundary, error) {
	return oneBoundary(ctx, tx, "id = $1::uuid", id)
}

// Boundaries trả mọi phiên bản của thửa theo thứ tự, kèm hình (lịch sử chỉ đọc).
func Boundaries(ctx context.Context, tx pgx.Tx, plotRef uuid.UUID) ([]Boundary, error) {
	rows, err := tx.Query(ctx, "SELECT "+boundaryCols+`, ST_AsGeoJSON(geom, 7)
	  FROM pf_coop.plot_boundary WHERE plot_ref = $1::uuid ORDER BY seq`, plotRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Boundary
	for rows.Next() {
		b, err := scanBoundary(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Sổ plot_event
// ---------------------------------------------------------------------------

// PlotEvent là một dòng sổ biến động thuộc tính thửa (ảnh chụp sau sự việc).
type PlotEvent struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	PlotRef        uuid.UUID
	Action         string
	Code           string
	Name           string
	CommuneCode    string
	CooperativeRef uuid.UUID
	HouseholdRef   uuid.UUID
	Status         string
	Reason         string
	EventTime      time.Time
	ActorRef       uuid.UUID
	DeviceID       *string
	IdemKey        string
	Seq            int64
	RecordedAt     time.Time
	// KNAttrs: ảnh chụp thuộc tính v7.7 sau sự việc (000079) — Log là hiệu giữa hai ảnh chụp liên tiếp.
	KNAttrs
	OnBehalfOf    *uuid.UUID
	SupportReason *string
}

// KNAttrs là thuộc tính thửa KN v7.7 (FN-XA-03.05): không có PII — tên nông dân nằm trong kho định danh (FarmerRef).
type KNAttrs struct {
	FarmerRef      uuid.UUID
	LotName        string
	Note           string
	DeclaredAreaHa *decimal.Decimal
	Shape          string
	Department     string
}

// InsertPlotEvent ghi sổ biến động; DB tính seq và băm.
func InsertPlotEvent(ctx context.Context, tx pgx.Tx, e *PlotEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_coop.plot_event
	    (id, tenant_id, stream_key, plot_ref, action, code, name, commune_code, cooperative_ref, household_ref, status,
	     reason, event_time, actor_ref, device_id, idem_key, rule_version,
	     farmer_ref, lot_name, note, declared_area_ha, shape, department_code, on_behalf_of, support_reason)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::text, $9::uuid, $10::uuid, $11::text,
	     $12::text, $13::timestamptz, $14::uuid, $15::text, $16::text, $17::text,
	     $18::uuid, $19::text, $20::text, $21::numeric, $22::text, $23::text, $24::uuid, $25::text)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.StreamKey(e.PlotRef), e.PlotRef, e.Action, e.Code, nilIfEmpty(e.Name), e.CommuneCode,
		nilIfZero(e.CooperativeRef), nilIfZero(e.HouseholdRef), e.Status, nilIfEmpty(e.Reason), e.EventTime.UTC(), e.ActorRef,
		e.DeviceID, e.IdemKey, domain.RuleVersion,
		nilIfZero(e.FarmerRef), nilIfEmpty(e.LotName), nilIfEmpty(e.Note), decPtr(e.DeclaredAreaHa), nilIfEmpty(e.Shape),
		nilIfEmpty(e.Department), e.OnBehalfOf, e.SupportReason).Scan(&e.Seq, &e.RecordedAt)
}

// PlotEventByIdemKey tìm biến động đã ghi với idem_key.
func PlotEventByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*PlotEvent, error) {
	e, err := scanPlotEvent(tx.QueryRow(ctx, "SELECT "+plotEventCols+" FROM pf_coop.plot_event WHERE idem_key = $1::text", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// ---------------------------------------------------------------------------
// Bảng hiện tại pf_coop.plot
// ---------------------------------------------------------------------------

// Plot là trạng thái hiện tại của thửa.
type Plot struct {
	TenantID          uuid.UUID // đơn vị chủ (W4.2: khác tenant phiên = đọc qua cấp phép)
	Ref               uuid.UUID
	Code              string
	Name              string
	CommuneCode       string
	CooperativeRef    uuid.UUID
	HouseholdRef      uuid.UUID
	Status            string
	StatusReason      string
	CurrentVersion    int64
	CurrentBoundaryID uuid.UUID
	AreaHa            decimal.Decimal
	Source            string
	NeedsReview       bool
	Revision          int64
	SyncSeq           int64
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
	GeoJSON           string // có khi đọc kèm hình
	KNAttrs                  // KN v7.7 (000079)
}

const plotCols = `p.ref, p.code, coalesce(p.name, ''), p.commune_code, p.cooperative_ref, p.household_ref, p.status,
  coalesce(p.status_reason, ''), p.current_version, p.current_boundary_id, p.area_ha::text, p.source, p.needs_review,
  p.revision, p.sync_seq, p.created_by, p.created_at, p.updated_at,
  p.farmer_ref, coalesce(p.lot_name, ''), coalesce(p.note, ''), p.declared_area_ha::text, coalesce(p.shape, ''),
  coalesce(p.department_code, ''), p.tenant_id`

func scanPlot(row pgx.Row, withGeom bool) (Plot, error) {
	var p Plot
	var area string
	var declared *string
	var coop, hh, farmer *uuid.UUID
	dest := []any{&p.Ref, &p.Code, &p.Name, &p.CommuneCode, &coop, &hh, &p.Status, &p.StatusReason, &p.CurrentVersion,
		&p.CurrentBoundaryID, &area, &p.Source, &p.NeedsReview, &p.Revision, &p.SyncSeq, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		&farmer, &p.LotName, &p.Note, &declared, &p.Shape, &p.Department, &p.TenantID}
	if withGeom {
		dest = append(dest, &p.GeoJSON)
	}
	if err := row.Scan(dest...); err != nil {
		return p, err
	}
	p.CooperativeRef, p.HouseholdRef, p.FarmerRef = deref(coop), deref(hh), deref(farmer)
	var err error
	if p.AreaHa, err = decimal.NewFromString(area); err != nil {
		return p, err
	}
	p.DeclaredAreaHa, err = parseDec(declared)
	return p, err
}

// PlotByRef đọc thửa kèm hình hiện hành; forUpdate khóa dòng trong giao dịch.
func PlotByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*Plot, error) {
	q := "SELECT " + plotCols + ", ST_AsGeoJSON(p.geom, 7) FROM pf_coop.plot p WHERE p.ref = $1::uuid"
	if forUpdate {
		q += " FOR UPDATE"
	}
	p, err := scanPlot(tx.QueryRow(ctx, q, ref), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// lockSync tuần tự hóa việc cấp sync_seq trong tenant cho tới khi giao dịch kết
// thúc: giá trị sequence lấy sau khóa nên thứ tự commit trùng thứ tự sync_seq,
// con trỏ chiều xuống không bỏ sót dòng commit muộn. Gọi NGAY TRƯỚC khi ghi plot.
func lockSync(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_geo.plot_sync:' || current_setting('app.tenant_id', true), 0))")
	return err
}

// InsertPlot ghi thửa mới với phiên bản ranh giới đầu tiên.
func InsertPlot(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, p Plot, geojson string) error {
	if err := lockSync(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.plot
	    (tenant_id, ref, code, name, commune_code, cooperative_ref, household_ref, status, current_version,
	     current_boundary_id, geom, area_ha, source, needs_review, created_by,
	     farmer_ref, lot_name, note, declared_area_ha, shape, department_code)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::text, $6::uuid, $7::uuid, $8::text, $9::bigint, $10::uuid,
	     `+geomSQL("$11")+`, $12::numeric, $13::text, $14::boolean, $15::uuid,
	     $16::uuid, $17::text, $18::text, $19::numeric, $20::text, $21::text)`,
		tenantID, p.Ref, p.Code, nilIfEmpty(p.Name), p.CommuneCode, nilIfZero(p.CooperativeRef), nilIfZero(p.HouseholdRef),
		p.Status, p.CurrentVersion, p.CurrentBoundaryID, geojson, p.AreaHa.String(), p.Source, p.NeedsReview, p.CreatedBy,
		nilIfZero(p.FarmerRef), nilIfEmpty(p.LotName), nilIfEmpty(p.Note), decPtr(p.DeclaredAreaHa), nilIfEmpty(p.Shape),
		nilIfEmpty(p.Department))
	return err
}

// SetCurrentBoundary đặt phiên bản hiện hành mới (R-21: cùng giao dịch với sổ).
func SetCurrentBoundary(ctx context.Context, tx pgx.Tx, ref uuid.UUID, b Boundary, geojson string, needsReview bool) error {
	if err := lockSync(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_coop.plot SET current_version = $2::bigint, current_boundary_id = $3::uuid,
	    geom = `+geomSQL("$4")+`, area_ha = $5::numeric, source = $6::text, needs_review = needs_review OR $7::boolean,
	    revision = revision + 1, sync_seq = nextval('pf_coop.plot_sync_seq'), updated_at = now()
	  WHERE ref = $1::uuid`, ref, b.Version, b.ID, geojson, b.AreaHa.String(), b.Source, needsReview)
	return err
}

// SetAttrs ghi thuộc tính sau gán HTX/nông hộ hoặc ngừng dùng.
func SetAttrs(ctx context.Context, tx pgx.Tx, p Plot) error {
	if err := lockSync(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_coop.plot SET cooperative_ref = $2::uuid, household_ref = $3::uuid, status = $4::text,
	    status_reason = $5::text, revision = revision + 1, sync_seq = nextval('pf_coop.plot_sync_seq'), updated_at = now()
	  WHERE ref = $1::uuid`, p.Ref, nilIfZero(p.CooperativeRef), nilIfZero(p.HouseholdRef), p.Status, nilIfEmpty(p.StatusReason))
	return err
}

// SetNeedsReview đổi cờ chờ xem xét (khi mở/đóng xung đột).
func SetNeedsReview(ctx context.Context, tx pgx.Tx, ref uuid.UUID, v bool) error {
	if err := lockSync(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_coop.plot SET needs_review = $2::boolean, revision = revision + 1,
	    sync_seq = nextval('pf_coop.plot_sync_seq'), updated_at = now()
	  WHERE ref = $1::uuid AND needs_review IS DISTINCT FROM $2::boolean`, ref, v)
	return err
}

// Filter là điều kiện đọc theo phạm vi.
type Filter struct {
	All   bool
	Units []string
	// CoopOnly: người gọi là quản lý HTX — chỉ thửa của HTX Coop và thửa nháp chưa gán.
	CoopOnly bool
	Coop     uuid.UUID
}

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// scopeSQL: điều kiện phạm vi dùng tham số $1..$4.
const scopeSQL = `($1::boolean OR p.commune_code = ANY($2::text[]))
  AND (NOT $3::boolean OR p.cooperative_ref = $4::uuid OR p.cooperative_ref IS NULL)`

func (f Filter) args() []any { return []any{f.All, units(f.Units), f.CoopOnly, f.Coop} }

// BBoxQuery là truy vấn theo khung nhìn (GIS FN-07 BR-25).
type BBoxQuery struct {
	Filter
	MinLon, MinLat, MaxLon, MaxLat float64
	SimplifyDeg                    float64 // 0 = không đơn giản hóa
	Limit                          int
}

// ListBBox trả thửa chưa ngừng dùng giao khung nhìn, kèm hình đã đơn giản hóa theo zoom.
func ListBBox(ctx context.Context, tx pgx.Tx, q BBoxQuery) ([]Plot, error) {
	args := append(q.args(), q.MinLon, q.MinLat, q.MaxLon, q.MaxLat, q.SimplifyDeg, q.Limit)
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`,
	    ST_AsGeoJSON(CASE WHEN $9::float8 > 0 THEN ST_SimplifyPreserveTopology(p.geom, $9::float8) ELSE p.geom END, 7)
	  FROM pf_coop.plot p
	  WHERE `+scopeSQL+` AND p.status <> 'retired'
	    AND p.geom && ST_MakeEnvelope($5::float8, $6::float8, $7::float8, $8::float8, 4326)
	  ORDER BY p.ref LIMIT $10::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectPlots(rows)
}

// ListByCooperative trả thửa của một HTX, phân trang cursor theo ref (R-53).
func ListByCooperative(ctx context.Context, tx pgx.Tx, f Filter, coop, after uuid.UUID, limit int) ([]Plot, error) {
	args := append(f.args(), coop, after, limit)
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7)
	  FROM pf_coop.plot p
	  WHERE `+scopeSQL+` AND p.status <> 'retired' AND p.cooperative_ref = $5::uuid AND p.ref > $6::uuid
	  ORDER BY p.ref LIMIT $7::int`, args...)
	if err != nil {
		return nil, err
	}
	return collectPlots(rows)
}

// ChangesAfter trả thửa có sync_seq > after trong các xã được xét, theo thứ tự tăng.
func ChangesAfter(ctx context.Context, tx pgx.Tx, all bool, communes []string, after int64, limit int) ([]Plot, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7)
	  FROM pf_coop.plot p
	  WHERE ($1::boolean OR p.commune_code = ANY($2::text[])) AND p.sync_seq > $3::bigint
	  ORDER BY p.sync_seq LIMIT $4::int`, all, units(communes), after, limit)
	if err != nil {
		return nil, err
	}
	return collectPlots(rows)
}

// ChangesAfterForHouseholds là ChangesAfter của thiết bị nông dân: thửa hiện thuộc một nông
// hộ trong households, hoặc từng được gán cho một nông hộ đó (sự kiện plot_event) — để thửa
// đã chuyển sang hộ khác vẫn đi xuống (bên gọi trả tombstone).
func ChangesAfterForHouseholds(ctx context.Context, tx pgx.Tx, households []uuid.UUID, after int64, limit int) ([]Plot, error) {
	hh := make([]string, 0, len(households))
	for _, h := range households {
		hh = append(hh, h.String())
	}
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7)
	  FROM pf_coop.plot p
	  WHERE p.sync_seq > $2::bigint
	    AND (p.household_ref = ANY($1::uuid[])
	      OR EXISTS (SELECT 1 FROM pf_coop.plot_event e WHERE e.plot_ref = p.ref AND e.household_ref = ANY($1::uuid[])))
	  ORDER BY p.sync_seq LIMIT $3::int`, hh, after, limit)
	if err != nil {
		return nil, err
	}
	return collectPlots(rows)
}

func collectPlots(rows pgx.Rows) ([]Plot, error) {
	defer rows.Close()
	var out []Plot
	for rows.Next() {
		p, err := scanPlot(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DistanceM trả khoảng cách trắc địa (m) từ điểm tới thửa; nil nếu không có thửa.
func DistanceM(ctx context.Context, tx pgx.Tx, ref uuid.UUID, lon, lat string) (*decimal.Decimal, error) {
	var s *string
	err := tx.QueryRow(ctx, `SELECT round(ST_Distance(p.geom::geography,
	    ST_SetSRID(ST_MakePoint($2::text::float8, $3::text::float8), 4326)::geography)::numeric, 1)::text
	  FROM pf_coop.plot p WHERE p.ref = $1::uuid`, ref, lon, lat).Scan(&s)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseDec(s)
}
