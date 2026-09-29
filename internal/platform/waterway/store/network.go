package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
)

// ---------------------------------------------------------------------------
// Job dựng

const jobCols = `j.id, j.tenant_id, j.status, j.params::text, j.requested_by, j.requested_at, j.idem_key, j.network_version_id,
  v.version_no, j.reused, j.error_code, j.completed_at`

func scanJob(row pgx.Row) (domain.BuildJob, error) {
	var j domain.BuildJob
	var params string
	err := row.Scan(&j.ID, &j.TenantID, &j.Status, &params, &j.RequestedBy, &j.RequestedAt, &j.IdemKey, &j.NetworkVersionID,
		&j.VersionNo, &j.Reused, &j.ErrorCode, &j.CompletedAt)
	if err != nil {
		return j, err
	}
	err = json.Unmarshal([]byte(params), &j.Params)
	return j, err
}

func oneJob(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.BuildJob, error) {
	j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobCols+` FROM pf_waterway.build_job j
	  LEFT JOIN pf_waterway.network_version v ON v.id = j.network_version_id WHERE `+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

// JobByIdemKey tìm job đã tạo với idem_key.
func JobByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.BuildJob, error) {
	return oneJob(ctx, tx, "j.idem_key = $1::text", key)
}

// JobByID đọc một job; forUpdate khóa dòng (worker).
func JobByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.BuildJob, error) {
	q := "j.id = $1"
	if forUpdate {
		q += " FOR UPDATE OF j"
	}
	return oneJob(ctx, tx, q, id)
}

// InsertJob ghi job; requested_at (mốc as_of) do DB đặt.
func InsertJob(ctx context.Context, tx pgx.Tx, j *domain.BuildJob) error {
	p, err := json.Marshal(j.Params)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_waterway.build_job (id, tenant_id, status, params, requested_by, idem_key)
	  VALUES ($1, $2, 'queued', $3::jsonb, $4, $5) RETURNING requested_at`,
		j.ID, j.TenantID, string(p), j.RequestedBy, j.IdemKey).Scan(&j.RequestedAt)
}

// CompleteJob đánh dấu job xong với phiên bản kết quả.
func CompleteJob(ctx context.Context, tx pgx.Tx, id, versionID uuid.UUID, reused bool, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_waterway.build_job SET status = 'completed', network_version_id = $2, reused = $3,
	    completed_at = $4, attempts = attempts + 1 WHERE id = $1`, id, versionID, reused, at.UTC())
	return err
}

// FailJob đánh dấu job hỏng vĩnh viễn.
func FailJob(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string) error {
	_, err := tx.Exec(ctx, "UPDATE pf_waterway.build_job SET status = 'failed', error_code = $2, attempts = attempts + 1 WHERE id = $1", id, code)
	return err
}

// LockTenantBuilds tuần tự hóa việc cấp số phiên bản của tenant trong giao dịch.
func LockTenantBuilds(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_waterway.version:' || $1::text, 0))", tid)
	return err
}

// LockRef tuần tự hóa ghi một chuỗi sửa (đoạn/công trình) trong giao dịch.
func LockRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_waterway.ref:' || $1::text, 0))", ref)
	return err
}

// ---------------------------------------------------------------------------
// Phiên bản mạng

const versionCols = `id, tenant_id, version_no, status, segment_set_sha256, graph_sha256, params::text, rule_version, as_of,
  segment_count, structure_count, vertex_count, edge_count, component_count, finding_count, build_job_id, built_by, built_at,
  published_by, published_at, effective_from, superseded_at, superseded_by`

func scanVersion(row pgx.Row) (domain.NetworkVersion, error) {
	var v domain.NetworkVersion
	var params string
	err := row.Scan(&v.ID, &v.TenantID, &v.VersionNo, &v.Status, &v.SegmentSetSHA256, &v.GraphSHA256, &params, &v.RuleVersion,
		&v.AsOf, &v.SegmentCount, &v.StructureCount, &v.VertexCount, &v.EdgeCount, &v.ComponentCount, &v.FindingCount,
		&v.BuildJobID, &v.BuiltBy, &v.BuiltAt, &v.PublishedBy, &v.PublishedAt, &v.EffectiveFrom, &v.SupersededAt, &v.SupersededBy)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(params), &v.Params)
	return v, err
}

func oneVersion(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.NetworkVersion, error) {
	v, err := scanVersion(tx.QueryRow(ctx, "SELECT "+versionCols+" FROM pf_waterway.network_version WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VersionByNo đọc phiên bản theo số; forUpdate khóa dòng (phát hành).
func VersionByNo(ctx context.Context, tx pgx.Tx, no int, forUpdate bool) (*domain.NetworkVersion, error) {
	q := "version_no = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneVersion(ctx, tx, q, no)
}

// VersionByID đọc phiên bản theo id.
func VersionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.NetworkVersion, error) {
	return oneVersion(ctx, tx, "id = $1", id)
}

// PublishedVersion đọc phiên bản đang phát hành; forUpdate khóa dòng.
func PublishedVersion(ctx context.Context, tx pgx.Tx, forUpdate bool) (*domain.NetworkVersion, error) {
	q := "status = 'published'"
	if forUpdate {
		q += " FOR UPDATE"
	}
	return oneVersion(ctx, tx, q)
}

// ReusableVersion tìm phiên bản built/published có cùng băm tập đoạn.
func ReusableVersion(ctx context.Context, tx pgx.Tx, hash string) (*domain.NetworkVersion, error) {
	return oneVersion(ctx, tx, "segment_set_sha256 = $1 AND status IN ('built', 'published') ORDER BY version_no DESC LIMIT 1", hash)
}

// NextVersionNo trả số phiên bản kế tiếp (gọi sau LockTenantBuilds).
func NextVersionNo(ctx context.Context, tx pgx.Tx) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT coalesce(max(version_no), 0) + 1 FROM pf_waterway.network_version").Scan(&n)
	return n, err
}

// ListVersions trả phiên bản mới → cũ (cursor theo version_no).
func ListVersions(ctx context.Context, tx pgx.Tx, before, limit int) ([]domain.NetworkVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+versionCols+` FROM pf_waterway.network_version
	  WHERE ($1::int = 0 OR version_no < $1::int) ORDER BY version_no DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanVersion)
}

// InsertVersion ghi phiên bản đã dựng.
func InsertVersion(ctx context.Context, tx pgx.Tx, v *domain.NetworkVersion) error {
	p, err := json.Marshal(v.Params)
	if err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_waterway.network_version
	    (id, tenant_id, version_no, status, segment_set_sha256, graph_sha256, params, rule_version, as_of, segment_count,
	     structure_count, vertex_count, edge_count, component_count, finding_count, build_job_id, built_by)
	  VALUES ($1, $2, $3, 'built', $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING built_at`,
		v.ID, v.TenantID, v.VersionNo, v.SegmentSetSHA256, v.GraphSHA256, string(p), v.RuleVersion, v.AsOf.UTC(), v.SegmentCount,
		v.StructureCount, v.VertexCount, v.EdgeCount, v.ComponentCount, v.FindingCount, v.BuildJobID, v.BuiltBy).Scan(&v.BuiltAt)
}

// Supersede chuyển phiên bản đang phát hành sang superseded.
func Supersede(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_waterway.network_version SET status = 'superseded', superseded_at = $2, superseded_by = $3
	  WHERE id = $1 AND status = 'published'`, id, at.UTC(), by)
	return err
}

// Publish chuyển phiên bản built sang published.
func Publish(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_waterway.network_version SET status = 'published', published_by = $2, published_at = $3,
	    effective_from = $3 WHERE id = $1 AND status = 'built'`, id, by, at.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Đồ thị

// ptrUUID: mã tham chiếu dạng chuỗi (rỗng → NULL) cho mảng uuid[] — truyền mảng
// dạng text[] rồi ép kiểu phía CSDL (an toàn với chế độ thực thi của PgBouncer).
func ptrUUID(s string) *string {
	if s == "" {
		return nil
	}
	if _, err := uuid.Parse(s); err != nil {
		return nil
	}
	return &s
}

// InsertGraph ghi đỉnh, cạnh, đoạn của phiên bản và phát hiện (một lệnh mỗi bảng).
// cost_m do CSDL tính (geodesic trên geography) từ hình học cạnh.
func InsertGraph(ctx context.Context, tx pgx.Tx, tid, vid uuid.UUID, g domain.Graph) error {
	if len(g.Vertices) > 0 {
		var idsv []int64
		var lng, lat []float64
		var deg []int32
		for _, v := range g.Vertices {
			idsv = append(idsv, v.ID)
			lng = append(lng, v.At.Lng)
			lat = append(lat, v.At.Lat)
			deg = append(deg, int32(v.Degree))
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_waterway.vertex (network_version_id, tenant_id, id, geom, degree)
		  SELECT $1, $2, t.id, ST_SetSRID(ST_MakePoint(t.lng, t.lat), 4326), t.deg
		  FROM unnest($3::bigint[], $4::float8[], $5::float8[], $6::int[]) AS t(id, lng, lat, deg)`,
			vid, tid, idsv, lng, lat, deg); err != nil {
			return err
		}
	}
	if len(g.Edges) > 0 {
		n := len(g.Edges)
		idsv, src, dst := make([]int64, n), make([]int64, n), make([]int64, n)
		geo, segRef, segRev, source, limiting, vessel := make([]string, n), make([]string, n), make([]string, n),
			make([]string, n), make([]string, n), make([]*string, n)
		maxLoad, derived, width, depth, clear := make([]*string, n), make([]*string, n), make([]*string, n), make([]*string, n), make([]*string, n)
		certain := make([]bool, n)
		structRef := make([]*string, n)
		for i, e := range g.Edges {
			idsv[i], src[i], dst[i] = e.ID, e.Source, e.Target
			geo[i], segRef[i], segRev[i], source[i] = LineJSON(e.Points), e.SegmentRef.String(), e.SegmentRevisionID.String(), e.DataSource
			limiting[i], certain[i] = e.Capacity.Limiting, e.Capacity.Certain
			if e.Capacity.VesselCode != "" {
				vc := e.Capacity.VesselCode
				vessel[i] = &vc
			}
			maxLoad[i], derived[i] = decText(e.Capacity.MaxLoadTons), decText(e.Capacity.Derived)
			width[i], depth[i], clear[i] = decText(e.Constraints.WidthM), decText(e.Constraints.DepthM), decText(e.Constraints.ClearanceM)
			structRef[i] = ptrUUID(e.Capacity.StructureRef)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_waterway.edge (network_version_id, tenant_id, id, source, target, cost_m,
		    reverse_cost, max_load_tons, derived_max_load_tons, certain, vessel_code, limiting, limiting_structure_ref, width_m,
		    depth_m, clearance_m, segment_ref, segment_revision_id, data_source, geom)
		  SELECT $1, $2, t.id, t.src, t.dst, ST_Length(t.g::geography), ST_Length(t.g::geography), t.ml::numeric, t.dr::numeric,
		         t.certain, t.vessel, t.limiting, t.sref::uuid, t.w::numeric, t.d::numeric, t.c::numeric, t.segref::uuid, t.segrev::uuid, t.source, t.g
		  FROM (SELECT u.*, ST_SetSRID(ST_GeomFromGeoJSON(u.geo), 4326) AS g
		        FROM unnest($3::bigint[], $4::bigint[], $5::bigint[], $6::text[], $7::text[], $8::text[], $9::bool[], $10::text[],
		                    $11::text[], $12::text[], $13::text[], $14::text[], $15::text[], $16::text[], $17::text[], $18::text[])
		             AS u(id, src, dst, geo, ml, dr, certain, vessel, limiting, sref, w, d, c, segref, segrev, source)) t`,
			vid, tid, idsv, src, dst, geo, maxLoad, derived, certain, vessel, limiting, structRef, width, depth, clear, segRef, segRev, source); err != nil {
			return err
		}
	}
	if len(g.Segments) > 0 {
		n := len(g.Segments)
		ref, rev := make([]string, n), make([]string, n)
		revNo, edgeCount := make([]int32, n), make([]int32, n)
		name, class, source, limiting, geo := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
		length := make([]string, n)
		width, depth, declared, maxLoad := make([]*string, n), make([]*string, n), make([]*string, n), make([]*string, n)
		certain := make([]bool, n)
		structRef := make([]*string, n)
		for i, s := range g.Segments {
			in := s.Input
			ref[i], rev[i], revNo[i], edgeCount[i] = in.Ref.String(), in.RevisionID.String(), int32(in.Revision), int32(s.EdgeCount)
			name[i], class[i], source[i], limiting[i] = in.Name, in.Class, in.DataSource, s.Capacity.Limiting
			geo[i], length[i] = LineJSON(in.Points), in.LengthM.String()
			width[i], depth[i], declared[i], maxLoad[i] = decText(in.WidthM), decText(in.DepthM), decText(in.DeclaredTons), decText(s.Capacity.MaxLoadTons)
			certain[i], structRef[i] = s.Capacity.Certain, ptrUUID(s.Capacity.StructureRef)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_waterway.version_segment (network_version_id, tenant_id, segment_ref,
		    segment_revision_id, revision, name, waterway_class, data_source, length_m, width_m, depth_m, declared_max_load_tons,
		    max_load_tons, certain, limiting, limiting_structure_ref, edge_count, geom)
		  SELECT $1, $2, u.ref::uuid, u.rev::uuid, u.revno, u.name, u.class, u.source, u.len::numeric, u.w::numeric, u.d::numeric, u.decl::numeric,
		         u.ml::numeric, u.certain, u.limiting, u.sref::uuid, u.ec, ST_SetSRID(ST_GeomFromGeoJSON(u.geo), 4326)
		  FROM unnest($3::text[], $4::text[], $5::int[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::text[],
		              $12::text[], $13::text[], $14::bool[], $15::text[], $16::text[], $17::int[], $18::text[])
		       AS u(ref, rev, revno, name, class, source, len, w, d, decl, ml, certain, limiting, sref, ec, geo)`,
			vid, tid, ref, rev, revNo, name, class, source, length, width, depth, declared, maxLoad, certain, limiting, structRef,
			edgeCount, geo); err != nil {
			return err
		}
	}
	if len(g.Findings) > 0 {
		n := len(g.Findings)
		idsv := make([]int64, n)
		kind, msg, refs := make([]string, n), make([]string, n), make([]string, n)
		lng, lat := make([]float64, n), make([]float64, n)
		gap := make([]*string, n)
		fixed := make([]bool, n)
		for i, f := range g.Findings {
			idsv[i], kind[i], msg[i], fixed[i] = f.ID, f.Kind, f.Message, f.AutoFixed
			lng[i], lat[i], gap[i] = f.At.Lng, f.At.Lat, decText(f.GapM)
			b, _ := json.Marshal(f.SegmentRefs)
			refs[i] = string(b)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_waterway.build_finding (network_version_id, tenant_id, id, kind, segment_refs,
		    geom, gap_m, auto_fixed, message)
		  SELECT $1, $2, u.id, u.kind, ARRAY(SELECT jsonb_array_elements_text(u.refs::jsonb)::uuid),
		         ST_SetSRID(ST_MakePoint(u.lng, u.lat), 4326), u.gap::numeric, u.fixed, u.msg
		  FROM unnest($3::bigint[], $4::text[], $5::text[], $6::float8[], $7::float8[], $8::text[], $9::bool[], $10::text[])
		       AS u(id, kind, refs, lng, lat, gap, fixed, msg)`,
			vid, tid, idsv, kind, refs, lng, lat, gap, fixed, msg); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Đọc

// FindingRow là một phát hiện đã lưu.
type FindingRow struct {
	ID          int64
	Kind        string
	SegmentRefs []uuid.UUID
	At          domain.Point
	GapM        *string
	AutoFixed   bool
	Message     string
}

// ListFindings trả phát hiện của phiên bản (cursor theo id).
func ListFindings(ctx context.Context, tx pgx.Tx, vid uuid.UUID, kind string, after int64, limit int) ([]FindingRow, error) {
	rows, err := tx.Query(ctx, `SELECT id, kind, segment_refs::text[], ST_X(geom), ST_Y(geom), gap_m::text, auto_fixed, message
	  FROM pf_waterway.build_finding WHERE network_version_id = $1 AND ($2::text = '' OR kind = $2::text) AND id > $3
	  ORDER BY id LIMIT $4`, vid, kind, after, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Row) (FindingRow, error) {
		var f FindingRow
		var refs []string
		if err := r.Scan(&f.ID, &f.Kind, &refs, &f.At.Lng, &f.At.Lat, &f.GapM, &f.AutoFixed, &f.Message); err != nil {
			return f, err
		}
		for _, s := range refs {
			u, err := uuid.Parse(s)
			if err != nil {
				return f, err
			}
			f.SegmentRefs = append(f.SegmentRefs, u)
		}
		return f, nil
	})
}

// BBox là khung lọc (kinh độ/vĩ độ).
type BBox struct{ MinLng, MinLat, MaxLng, MaxLat float64 }

// ListVersionSegments trả đoạn của phiên bản giao khung bbox (cursor theo segment_ref).
func ListVersionSegments(ctx context.Context, tx pgx.Tx, vid uuid.UUID, box *BBox, after uuid.UUID, limit int) ([]domain.VersionSegment, error) {
	var b [4]*float64
	if box != nil {
		b = [4]*float64{&box.MinLng, &box.MinLat, &box.MaxLng, &box.MaxLat}
	}
	rows, err := tx.Query(ctx, `SELECT segment_ref, segment_revision_id, revision, name, waterway_class, data_source, length_m::text,
	    width_m::text, depth_m::text, declared_max_load_tons::text, max_load_tons::text, certain, limiting, limiting_structure_ref,
	    edge_count, ST_AsGeoJSON(geom, 7)
	  FROM pf_waterway.version_segment
	  WHERE network_version_id = $1 AND segment_ref > $2
	    AND ($3::float8 IS NULL OR geom && ST_MakeEnvelope($3, $4, $5, $6, 4326))
	  ORDER BY segment_ref LIMIT $7`, vid, after, b[0], b[1], b[2], b[3], limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Row) (domain.VersionSegment, error) {
		var s domain.VersionSegment
		var length, geo string
		var w, dep, decl, ml *string
		err := r.Scan(&s.SegmentRef, &s.RevisionID, &s.Revision, &s.Name, &s.Class, &s.DataSource, &length, &w, &dep, &decl, &ml,
			&s.Certain, &s.Limiting, &s.StructureRef, &s.EdgeCount, &geo)
		if err != nil {
			return s, err
		}
		if s.Points, err = ParseLine(geo); err != nil {
			return s, err
		}
		if s.WidthM, err = textDec(w); err != nil {
			return s, err
		}
		if s.DepthM, err = textDec(dep); err != nil {
			return s, err
		}
		if s.DeclaredTons, err = textDec(decl); err != nil {
			return s, err
		}
		if s.MaxLoadTons, err = textDec(ml); err != nil {
			return s, err
		}
		l, err := textDec(&length)
		if err == nil {
			s.LengthM = *l
		}
		return s, err
	})
}

// CountVersionSegments đếm đoạn trong khung và số đoạn phù hợp tải trọng (GIS US-WATER-01 AC-2).
func CountVersionSegments(ctx context.Context, tx pgx.Tx, vid uuid.UUID, box *BBox, minLoad decimal.Decimal) (total, passable int, err error) {
	var b [4]*float64
	if box != nil {
		b = [4]*float64{&box.MinLng, &box.MinLat, &box.MaxLng, &box.MaxLat}
	}
	err = tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE $6::numeric IS NULL OR max_load_tons >= $6::numeric)
	  FROM pf_waterway.version_segment
	  WHERE network_version_id = $1 AND ($2::float8 IS NULL OR geom && ST_MakeEnvelope($2, $3, $4, $5, 4326))`,
		vid, b[0], b[1], b[2], b[3], loadFilter(minLoad)).Scan(&total, &passable)
	return total, passable, err
}
