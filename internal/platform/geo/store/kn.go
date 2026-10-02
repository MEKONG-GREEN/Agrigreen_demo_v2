package store

// SQL của màn "Vẽ thửa ruộng" KN v7.7 (FN-XA-03.05, migration 000079): danh sách thửa theo xã/HTX, thuộc tính v7.7,
// lịch sử (Log) từ hai sổ, phần chồng lấn để hiển thị, xuất GeoJSON/KML cho một xã.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
)

const plotEventCols = `id, tenant_id, plot_ref, action, code, name, commune_code, cooperative_ref, household_ref,
  status, reason, event_time, actor_ref, idem_key, seq, recorded_at,
  farmer_ref, coalesce(lot_name, ''), coalesce(note, ''), declared_area_ha::text, coalesce(shape, ''), coalesce(department_code, ''),
  on_behalf_of, support_reason`

func scanPlotEvent(row pgx.Row) (PlotEvent, error) {
	var e PlotEvent
	var name, reason, declared *string
	var coop, hh, farmer *uuid.UUID
	err := row.Scan(&e.ID, &e.TenantID, &e.PlotRef, &e.Action, &e.Code, &name, &e.CommuneCode, &coop, &hh, &e.Status, &reason,
		&e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt,
		&farmer, &e.LotName, &e.Note, &declared, &e.Shape, &e.Department, &e.OnBehalfOf, &e.SupportReason)
	if err != nil {
		return e, err
	}
	if name != nil {
		e.Name = *name
	}
	if reason != nil {
		e.Reason = *reason
	}
	e.CooperativeRef, e.HouseholdRef, e.FarmerRef = deref(coop), deref(hh), deref(farmer)
	e.DeclaredAreaHa, err = parseDec(declared)
	return e, err
}

// PlotEvents trả mọi dòng sổ biến động của thửa theo thứ tự ghi (Log).
func PlotEvents(ctx context.Context, tx pgx.Tx, plotRef uuid.UUID) ([]PlotEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotEventCols+` FROM pf_geo.plot_event
	  WHERE stream_key = $1::text ORDER BY seq`, domain.StreamKey(plotRef))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlotEvent
	for rows.Next() {
		e, err := scanPlotEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// KNFilter là điều kiện danh sách thửa của ngữ cảnh phiên KN.
type KNFilter struct {
	All      bool     // phạm vi toàn quốc (Cấp Quốc gia, Admin)
	Units    []string // xã/tỉnh trong phạm vi
	Commune  string   // lọc một xã (rỗng = mọi xã trong phạm vi)
	Coop     uuid.UUID
	Dept     string // rỗng = mọi phòng ban (Admin); khác rỗng: phòng ban này hoặc thửa không mang phòng ban
	After    uuid.UUID
	Limit    int
	WithGeom bool
}

// ListKN trả thửa chưa ngừng dùng theo phạm vi KN, phân trang theo ref.
func ListKN(ctx context.Context, tx pgx.Tx, f KNFilter) ([]Plot, error) {
	geom := "''"
	if f.WithGeom {
		geom = "ST_AsGeoJSON(p.geom, 7)"
	}
	rows, err := tx.Query(ctx, "SELECT "+plotCols+", "+geom+`
	  FROM pf_geo.plot p
	  WHERE ($1::boolean OR p.commune_code = ANY($2::text[]))
	    AND ($3::text = '' OR p.commune_code = $3::text)
	    AND ($4::uuid IS NULL OR p.cooperative_ref = $4::uuid)
	    AND ($5::text = '' OR p.department_code IS NULL OR p.department_code = $5::text)
	    AND p.status <> 'retired' AND p.ref > $6::uuid
	  ORDER BY p.ref LIMIT $7::int`, f.All, units(f.Units), f.Commune, nilIfZero(f.Coop), f.Dept, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	return collectPlots(rows)
}

// SetKNAttrs ghi thuộc tính v7.7 và HTX sau sự kiện 'attributes_changed' (R-21: cùng giao dịch với sổ).
func SetKNAttrs(ctx context.Context, tx pgx.Tx, p Plot) error {
	if err := lockSync(ctx, tx); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pf_geo.plot SET name = $2::text, cooperative_ref = $3::uuid, household_ref = $4::uuid,
	    status = $5::text, farmer_ref = $6::uuid, lot_name = $7::text, note = $8::text, declared_area_ha = $9::numeric,
	    shape = $10::text, department_code = $11::text,
	    revision = revision + 1, sync_seq = nextval('pf_geo.plot_sync_seq'), updated_at = now()
	  WHERE ref = $1::uuid`, p.Ref, nilIfEmpty(p.Name), nilIfZero(p.CooperativeRef), nilIfZero(p.HouseholdRef), p.Status,
		nilIfZero(p.FarmerRef), nilIfEmpty(p.LotName), nilIfEmpty(p.Note), decPtr(p.DeclaredAreaHa), nilIfEmpty(p.Shape),
		nilIfEmpty(p.Department))
	return err
}

// SetShape ghi cách vẽ và diện tích khai báo của phiên bản ranh giới hiện hành mới (KN sửa ranh giới).
func SetShape(ctx context.Context, tx pgx.Tx, ref uuid.UUID, shape string, declared *decimal.Decimal) error {
	_, err := tx.Exec(ctx, `UPDATE pf_geo.plot SET shape = coalesce($2::text, shape), declared_area_ha = coalesce($3::numeric, declared_area_ha)
	  WHERE ref = $1::uuid`, ref, nilIfEmpty(shape), decPtr(declared))
	return err
}

// Intersection là phần chồng lấn của hình mới với một thửa đã có (hiển thị cho người vẽ).
type Intersection struct {
	PlotRef uuid.UUID
	AreaHa  decimal.Decimal
	GeoJSON string
}

// Intersections trả hình phần giao (GeoJSON) và diện tích trắc địa với các thửa refs.
func Intersections(ctx context.Context, tx pgx.Tx, geojson string, refs []uuid.UUID) ([]Intersection, error) {
	rs := make([]string, 0, len(refs))
	for _, r := range refs {
		rs = append(rs, r.String())
	}
	rows, err := tx.Query(ctx, `WITH n AS (SELECT `+geomSQL("$1")+` AS g)
	  SELECT p.ref, (ST_Area(ST_Intersection(p.geom, n.g)::geography) / 10000)::numeric(14,4)::text,
	         ST_AsGeoJSON(ST_CollectionExtract(ST_Intersection(p.geom, n.g), 3), 7)
	  FROM pf_geo.plot p, n WHERE p.ref = ANY($2::uuid[]) ORDER BY p.ref`, geojson, rs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Intersection
	for rows.Next() {
		var it Intersection
		var area string
		if err := rows.Scan(&it.PlotRef, &area, &it.GeoJSON); err != nil {
			return nil, err
		}
		if it.AreaHa, err = decimal.NewFromString(area); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ExportRow là một thửa của tệp xuất.
type ExportRow struct {
	Plot
	KML string
}

// ExportCommune trả thửa chưa ngừng dùng của một xã kèm hình GeoJSON và KML (ST_AsKML), tối đa limit thửa.
func ExportCommune(ctx context.Context, tx pgx.Tx, commune, dept string, limit int) ([]ExportRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+plotCols+`, ST_AsGeoJSON(p.geom, 7), ST_AsKML(p.geom, 7)
	  FROM pf_geo.plot p
	  WHERE p.commune_code = $1::text AND p.status <> 'retired'
	    AND ($2::text = '' OR p.department_code IS NULL OR p.department_code = $2::text)
	  ORDER BY p.code LIMIT $3::int`, commune, dept, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRow
	for rows.Next() {
		var r ExportRow
		var kml string
		p, err := scanPlotKML(rows, &kml)
		if err != nil {
			return nil, err
		}
		r.Plot, r.KML = p, kml
		out = append(out, r)
	}
	return out, rows.Err()
}

// scanPlotKML đọc một dòng plotCols + geojson + kml.
func scanPlotKML(row pgx.Row, kml *string) (Plot, error) {
	w := &kmlRow{kml: kml}
	p, err := scanPlot(w.wrap(row), true)
	return p, err
}

// kmlRow thêm đích quét KML vào cuối danh sách đích của scanPlot.
type kmlRow struct {
	row pgx.Row
	kml *string
}

func (k *kmlRow) wrap(row pgx.Row) pgx.Row { k.row = row; return k }

func (k *kmlRow) Scan(dest ...any) error { return k.row.Scan(append(dest, k.kml)...) }
