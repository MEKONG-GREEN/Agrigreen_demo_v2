// Package store: SQL thửa ruộng KN (pf_extension.kn_plot_version — sổ ảnh chụp chỉ ghi thêm; kn_plot — bảng chiếu hiện
// hành, migration 20261110013754). Phép hình học chính xác (ST_IsValid, diện tích trắc địa, chồng lấn) chạy trong PostGIS.
// Chỉ đọc/ghi bảng của KN (pf_extension); hồ sơ HTX là hồ sơ KN (kn_producer).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/domain"
)

// Snapshot là thửa đầy đủ thuộc tính (một dòng sổ hoặc bảng chiếu).
type Snapshot struct {
	Ref            uuid.UUID
	Code           string
	CommuneCode    string
	BoundaryID     uuid.UUID
	AreaHa         decimal.Decimal
	CooperativeRef *uuid.UUID
	FarmerRef      *uuid.UUID
	LotName        string
	Note           string
	DeclaredAreaHa *decimal.Decimal
	Shape          string
	Department     string
}

// Plot là dòng bảng chiếu.
type Plot struct {
	Snapshot
	Version   int64
	SyncSeq   int64
	UpdatedAt time.Time
	GeoJSON   string // chỉ khi đọc kèm hình
	KML       string // chỉ khi xuất KML
}

// Version là một dòng sổ kn_plot_version.
type Version struct {
	Snapshot
	ID            uuid.UUID
	Event         string
	Source        string
	Reason        *string
	EventTime     time.Time
	ActorRef      uuid.UUID
	OnBehalfOf    *uuid.UUID
	SupportReason *string
	DeviceID      *string
	IdemKey       string
	GeoJSON       string // rỗng = giữ hình hiện hành của thửa (sự việc thuộc tính)
	Seq           int64
	RecordedAt    time.Time
}

func geomSQL(n string) string {
	return "ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(" + n + "::text), 4326))"
}

func nilStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func decArg(d *decimal.Decimal) *string {
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
	return &d, err
}

// Analysis là kết quả kiểm hình trong PostGIS.
type Analysis struct {
	Valid  bool
	Reason string
	AreaHa decimal.Decimal
}

// Analyze kiểm hợp lệ (tự cắt, vòng sai…) và tính diện tích trắc địa (ha).
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

// Overlap là phần giao của hình mới với một thửa đã có.
type Overlap struct {
	Ref         uuid.UUID
	Code        string
	AreaHa      decimal.Decimal
	IntersectHa decimal.Decimal
	GeoJSON     string
}

// Overlaps tìm thửa của tenant giao với hình (trừ thửa exclude), kèm phần giao (trắc địa, GeoJSON). Chung cạnh = 0.
func Overlaps(ctx context.Context, tx pgx.Tx, geojson string, exclude uuid.UUID) ([]Overlap, error) {
	rows, err := tx.Query(ctx, `WITH n AS (SELECT `+geomSQL("$1")+` AS g)
	  SELECT p.ref, p.code, p.area_ha::text, (ST_Area(ST_Intersection(p.geom, n.g)::geography) / 10000)::numeric(14,4)::text,
	         coalesce(ST_AsGeoJSON(ST_CollectionExtract(ST_Intersection(p.geom, n.g), 3), 7), '')
	  FROM pf_extension.kn_plot p, n
	  WHERE p.ref <> $2::uuid AND p.geom && n.g AND ST_Intersects(p.geom, n.g)
	  ORDER BY p.ref LIMIT 100`, geojson, exclude)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Overlap
	for rows.Next() {
		var o Overlap
		var area, inter string
		if err := rows.Scan(&o.Ref, &o.Code, &area, &inter, &o.GeoJSON); err != nil {
			return nil, err
		}
		if o.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		if o.IntersectHa, err = decimal.NewFromString(inter); err != nil {
			return nil, err
		}
		if o.IntersectHa.IsPositive() {
			out = append(out, o)
		}
	}
	return out, rows.Err()
}

// NextCode sinh mã thửa mới của KN (KT0000001…), khác dải mã TH… của thửa đã chép.
func NextCode(ctx context.Context, tx pgx.Tx) (string, error) {
	var code string
	err := tx.QueryRow(ctx, "SELECT 'KT' || lpad(nextval('pf_extension.kn_plot_code_seq')::text, 7, '0')").Scan(&code)
	return code, err
}

// Lock giữ khóa giao dịch theo thửa (ghi tuần tự trên một thửa).
func Lock(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('kn_plot:' || $1::text, 0))`, ref)
	return err
}

// LockTenant tuần tự hóa lần ghi hình trong tenant để kiểm chồng lấn không bị hai giao dịch cùng lọt.
func LockTenant(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('kn_plot_geom:' || $1::text, 0))`, tid)
	return err
}

// InsertVersion ghi một ảnh chụp vào sổ (DB tính seq, chuỗi băm) rồi chiếu sang kn_plot cùng giao dịch (R-21).
func InsertVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *Version) error {
	var g *string
	if v.GeoJSON != "" {
		g = &v.GeoJSON
	}
	err := tx.QueryRow(ctx, `INSERT INTO pf_extension.kn_plot_version (id, tenant_id, stream_key, plot_ref, event, code, commune_code,
	    boundary_id, geom, area_ha, cooperative_ref, farmer_ref, lot_name, note, declared_area_ha, shape, department_code, source,
	    reason, event_time, actor_ref, on_behalf_of, support_reason, device_id, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::text, $7::text, $8::uuid,
	    coalesce(ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($9::text), 4326)),
	             (SELECT p.geom FROM pf_extension.kn_plot p WHERE p.tenant_id = $2::uuid AND p.ref = $4::uuid)),
	    $10::numeric, $11::uuid, $12::uuid, $13::text, $14::text, $15::numeric, $16::text, $17::text, $18::text, $19::text,
	    $20::timestamptz, $21::uuid, $22::uuid, $23::text, $24::text, $25::text, $26::text)
	  RETURNING seq, recorded_at`,
		v.ID, tid, domain.StreamKey(v.Ref.String()), v.Ref, v.Event, v.Code, v.CommuneCode, v.BoundaryID, g, v.AreaHa.String(),
		v.CooperativeRef, v.FarmerRef, nilStr(v.LotName), nilStr(v.Note), decArg(v.DeclaredAreaHa), nilStr(v.Shape),
		nilStr(v.Department), v.Source, v.Reason, v.EventTime.UTC(), v.ActorRef, v.OnBehalfOf, v.SupportReason, v.DeviceID,
		v.IdemKey, domain.RuleVersion).Scan(&v.Seq, &v.RecordedAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.kn_plot (tenant_id, ref, code, commune_code, cooperative_ref, farmer_ref, lot_name,
	    note, declared_area_ha, shape, department_code, version, boundary_id, geom, area_ha, created_by)
	  SELECT tenant_id, plot_ref, code, commune_code, cooperative_ref, farmer_ref, lot_name, note, declared_area_ha, shape,
	    department_code, seq, boundary_id, geom, area_ha, actor_ref
	  FROM pf_extension.kn_plot_version WHERE id = $1::uuid AND recorded_at = $2::timestamptz
	  ON CONFLICT (tenant_id, ref) DO UPDATE SET commune_code = EXCLUDED.commune_code, cooperative_ref = EXCLUDED.cooperative_ref,
	    farmer_ref = EXCLUDED.farmer_ref, lot_name = EXCLUDED.lot_name, note = EXCLUDED.note,
	    declared_area_ha = EXCLUDED.declared_area_ha, shape = EXCLUDED.shape, department_code = EXCLUDED.department_code,
	    version = EXCLUDED.version, boundary_id = EXCLUDED.boundary_id, geom = EXCLUDED.geom, area_ha = EXCLUDED.area_ha,
	    sync_seq = nextval('pf_extension.kn_plot_sync_seq'), updated_at = now()`, v.ID, v.RecordedAt)
	return err
}

const versionCols = `id, plot_ref, event, code, commune_code, boundary_id, area_ha::text, cooperative_ref, farmer_ref,
  coalesce(lot_name, ''), coalesce(note, ''), declared_area_ha::text, coalesce(shape, ''), coalesce(department_code, ''), source,
  reason, event_time, actor_ref, on_behalf_of, support_reason, device_id, idem_key, seq, recorded_at`

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	var area string
	var declared *string
	err := row.Scan(&v.ID, &v.Ref, &v.Event, &v.Code, &v.CommuneCode, &v.BoundaryID, &area, &v.CooperativeRef, &v.FarmerRef,
		&v.LotName, &v.Note, &declared, &v.Shape, &v.Department, &v.Source, &v.Reason, &v.EventTime, &v.ActorRef, &v.OnBehalfOf,
		&v.SupportReason, &v.DeviceID, &v.IdemKey, &v.Seq, &v.RecordedAt)
	if err != nil {
		return v, err
	}
	if v.AreaHa, err = decimal.NewFromString(area); err != nil {
		return v, err
	}
	v.DeclaredAreaHa, err = parseDec(declared)
	return v, err
}

// Versions trả mọi ảnh chụp của thửa theo thứ tự ghi.
func Versions(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Version, error) {
	rows, err := tx.Query(ctx, "SELECT "+versionCols+` FROM pf_extension.kn_plot_version
	  WHERE stream_key = $1::text ORDER BY seq`, domain.StreamKey(ref.String()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ByIdem trả ảnh chụp đã ghi với khóa idem (lặp lệnh), nil nếu chưa có.
func ByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Version, error) {
	v, err := scanVersion(tx.QueryRow(ctx, "SELECT "+versionCols+` FROM pf_extension.kn_plot_version
	  WHERE idem_key = $1::text ORDER BY seq DESC LIMIT 1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

const plotCols = `p.ref, p.code, p.commune_code, p.boundary_id, p.area_ha::text, p.cooperative_ref, p.farmer_ref,
  coalesce(p.lot_name, ''), coalesce(p.note, ''), p.declared_area_ha::text, coalesce(p.shape, ''), coalesce(p.department_code, ''),
  p.version, p.sync_seq, p.updated_at`

func scanPlot(row pgx.Row, extra ...any) (Plot, error) {
	var p Plot
	var area string
	var declared *string
	dest := append([]any{&p.Ref, &p.Code, &p.CommuneCode, &p.BoundaryID, &area, &p.CooperativeRef, &p.FarmerRef, &p.LotName, &p.Note,
		&declared, &p.Shape, &p.Department, &p.Version, &p.SyncSeq, &p.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return p, err
	}
	var err error
	if p.AreaHa, err = decimal.NewFromString(area); err != nil {
		return p, err
	}
	p.DeclaredAreaHa, err = parseDec(declared)
	return p, err
}

// Get đọc một thửa kèm hình; nil nếu không có.
func Get(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Plot, error) {
	var g string
	p, err := scanPlot(tx.QueryRow(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7) FROM pf_extension.kn_plot p
	  WHERE p.ref = $1::uuid`, ref), &g)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.GeoJSON = g
	return &p, nil
}

// Filter là điều kiện danh sách; phạm vi lọc trong SQL.
type Filter struct {
	All     bool
	Units   []string
	Commune string
	Coop    *uuid.UUID
	Dept    string // rỗng = mọi phòng ban; thửa không mang phòng ban tính là domain.LegacyDepartment
	After   uuid.UUID
	Limit   int
}

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// List trả thửa theo phạm vi, phân trang theo ref (không hình).
func List(ctx context.Context, tx pgx.Tx, f Filter) ([]Plot, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotCols+` FROM pf_extension.kn_plot p
	  WHERE ($1::boolean OR p.commune_code = ANY($2::text[]))
	    AND ($3::text = '' OR p.commune_code = $3::text)
	    AND ($4::uuid IS NULL OR p.cooperative_ref = $4::uuid)
	    AND ($5::text = '' OR coalesce(p.department_code, $8::text) = $5::text)
	    AND p.ref > $6::uuid
	  ORDER BY p.ref LIMIT $7::int`, f.All, units(f.Units), f.Commune, f.Coop, f.Dept, f.After, f.Limit, domain.LegacyDepartment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plot{}
	for rows.Next() {
		p, err := scanPlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Export trả thửa của một xã kèm GeoJSON và KML, tối đa limit.
func Export(ctx context.Context, tx pgx.Tx, commune, dept string, limit int) ([]Plot, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7), ST_AsKML(p.geom, 7) FROM pf_extension.kn_plot p
	  WHERE p.commune_code = $1::text AND ($2::text = '' OR coalesce(p.department_code, $4::text) = $2::text)
	  ORDER BY p.code LIMIT $3::int`, commune, dept, limit, domain.LegacyDepartment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plot
	for rows.Next() {
		var g, k string
		p, err := scanPlot(rows, &g, &k)
		if err != nil {
			return nil, err
		}
		p.GeoJSON, p.KML = g, k
		out = append(out, p)
	}
	return out, rows.Err()
}

// ChangesAfter trả thửa có sync_seq > after trong phạm vi (đồng bộ chiều xuống), kèm hình.
func ChangesAfter(ctx context.Context, tx pgx.Tx, all bool, us []string, dept string, after int64, limit int) ([]Plot, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7) FROM pf_extension.kn_plot p
	  WHERE ($1::boolean OR p.commune_code = ANY($2::text[])) AND p.sync_seq > $3::bigint
	    AND ($5::text = '' OR coalesce(p.department_code, $6::text) = $5::text)
	  ORDER BY p.sync_seq LIMIT $4::int`, all, units(us), after, limit, dept, domain.LegacyDepartment)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plot
	for rows.Next() {
		var g string
		p, err := scanPlot(rows, &g)
		if err != nil {
			return nil, err
		}
		p.GeoJSON = g
		out = append(out, p)
	}
	return out, rows.Err()
}

// Cooperative là hồ sơ KN loại HTX (pf_extension.kn_producer, phiên bản mới nhất).
type Cooperative struct {
	CommuneCode string
	Active      bool
}

// CooperativeRecord đọc hồ sơ KN loại HTX; nil nếu không có hoặc không phải HTX.
func CooperativeRecord(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Cooperative, error) {
	var c Cooperative
	var kind, event string
	err := tx.QueryRow(ctx, `SELECT kind, event, commune_code FROM pf_extension.kn_producer
	  WHERE producer_ref = $1::uuid ORDER BY seq DESC LIMIT 1`, ref).Scan(&kind, &event, &c.CommuneCode)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && kind != "cooperative") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Active = event != "deactivated"
	return &c, nil
}
