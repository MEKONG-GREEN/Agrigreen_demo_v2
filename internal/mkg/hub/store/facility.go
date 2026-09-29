// Package store: SQL của mkg/hub, chỉ qua pool của vai hub_app ở cụm mkg (R-05).
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx) — RLS lọc tenant.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// FacilityStream là khóa chuỗi băm của mọi sổ theo cơ sở.
func FacilityStream(id uuid.UUID) string { return "facility:" + id.String() }

func nullJSON(raw json.RawMessage) *string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	s := string(raw)
	return &s
}

// BoundaryValid kiểm GeoJSON ranh giới bằng PostGIS (hình hợp lệ, không rỗng).
func BoundaryValid(ctx context.Context, tx pgx.Tx, raw json.RawMessage) (bool, error) {
	s := nullJSON(raw)
	if s == nil {
		return true, nil
	}
	// Savepoint: GeoJSON hỏng làm PostGIS báo lỗi, không được làm hỏng giao dịch ngoài.
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	var ok bool
	err = sp.QueryRow(ctx, `SELECT coalesce(ST_IsValid(g) AND NOT ST_IsEmpty(g)
	      AND GeometryType(ST_Multi(g)) = 'MULTIPOLYGON', false)
	    FROM (SELECT ST_SetSRID(ST_GeomFromGeoJSON($1::text), 4326) AS g) x`, *s).Scan(&ok)
	if err != nil {
		_ = sp.Rollback(ctx)
		return false, nil
	}
	return ok, sp.Commit(ctx)
}

const versionCols = `v.id, v.tenant_id, v.facility_id, v.seq, v.code, v.name, v.kind, v.parent_facility_id,
  ST_Y(v.location)::text, ST_X(v.location)::text, ST_AsGeoJSON(v.boundary, 7)::text, v.province_code, v.status,
  v.capacity_kg::text, v.effective_from, v.supersedes_id, v.correction_reason, v.actor_ref, v.event_time,
  v.recorded_at, v.idem_key, d.decision, d.actor_ref, d.event_time`

const versionFrom = ` FROM mkg_hub.facility_version v
  LEFT JOIN mkg_hub.facility_decision d ON d.version_id = v.id`

func scanVersion(row pgx.Row) (domain.FacilityVersion, error) {
	var v domain.FacilityVersion
	var lat, lng, kind, status, capKg string
	var boundary *string
	var decision *string
	err := row.Scan(&v.ID, &v.TenantID, &v.FacilityID, &v.Seq, &v.Spec.Code, &v.Spec.Name, &kind, &v.Spec.ParentFacilityID,
		&lat, &lng, &boundary, &v.Spec.ProvinceCode, &status, &capKg, &v.Spec.EffectiveFrom, &v.Supersedes, &v.ChangeReason,
		&v.ProposedBy, &v.EventTime, &v.RecordedAt, &v.IdemKey, &decision, &v.DecidedBy, &v.DecidedAt)
	if err != nil {
		return v, err
	}
	v.Spec.Kind, v.Spec.Status = domain.Kind(kind), domain.Status(status)
	if v.Spec.Location.Lat, err = decimal.NewFromString(lat); err != nil {
		return v, err
	}
	if v.Spec.Location.Lng, err = decimal.NewFromString(lng); err != nil {
		return v, err
	}
	if boundary != nil {
		v.Spec.Boundary = json.RawMessage(*boundary)
	}
	if v.Spec.Capacity, err = mass.ParseKg(capKg); err != nil {
		return v, err
	}
	if decision != nil {
		d := domain.Decision(*decision)
		v.Decision = &d
	}
	return v, nil
}

// InsertVersion ghi một phiên bản đề xuất; DB tính seq (= số phiên bản).
func InsertVersion(ctx context.Context, tx pgx.Tx, v *domain.FacilityVersion, deviceID *string) error {
	s := v.Spec
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.facility_version
	    (id, tenant_id, stream_key, facility_id, code, name, kind, parent_facility_id, location, boundary,
	     province_code, status, capacity_kg, effective_from, event_time, supersedes_id, correction_reason,
	     actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
	     ST_SetSRID(ST_MakePoint($9::text::float8, $10::text::float8), 4326),
	     CASE WHEN $11::text IS NULL THEN NULL ELSE ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON($11::text), 4326)) END,
	     $12, $13, $14::numeric, $15, $16, $17, $18, $19, $20, $21, $22)
	  RETURNING seq, recorded_at`,
		v.ID, v.TenantID, FacilityStream(v.FacilityID), v.FacilityID, s.Code, s.Name, string(s.Kind), s.ParentFacilityID,
		s.Location.Lng.String(), s.Location.Lat.String(), nullJSON(s.Boundary),
		s.ProvinceCode, string(s.Status), s.Capacity.String(), s.EffectiveFrom.UTC(), v.EventTime.UTC(), v.Supersedes, v.ChangeReason,
		v.ProposedBy, deviceID, v.IdemKey, domain.RuleFacility).Scan(&v.Seq, &v.RecordedAt)
}

func oneVersion(ctx context.Context, tx pgx.Tx, where string, args ...any) (*domain.FacilityVersion, error) {
	v, err := scanVersion(tx.QueryRow(ctx, "SELECT "+versionCols+versionFrom+" WHERE "+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// VersionByIdem tìm phiên bản đã ghi với idem_key (gọi lặp).
func VersionByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.FacilityVersion, error) {
	return oneVersion(ctx, tx, "v.idem_key = $1", key)
}

// VersionBySeq đọc một phiên bản của cơ sở.
func VersionBySeq(ctx context.Context, tx pgx.Tx, facility uuid.UUID, seq int64) (*domain.FacilityVersion, error) {
	return oneVersion(ctx, tx, "v.facility_id = $1 AND v.seq = $2", facility, seq)
}

// LatestVersion đọc phiên bản mới nhất (đã duyệt hay chưa) của cơ sở.
func LatestVersion(ctx context.Context, tx pgx.Tx, facility uuid.UUID) (*domain.FacilityVersion, error) {
	return oneVersion(ctx, tx, "v.facility_id = $1 ORDER BY v.seq DESC LIMIT 1", facility)
}

// LockFacilityStream tuần tự hóa đề xuất/quyết định của một cơ sở trong giao dịch.
func LockFacilityStream(ctx context.Context, tx pgx.Tx, facility uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 727101))", FacilityStream(facility))
	return err
}

// CodeTaken cho biết mã đã dùng bởi cơ sở KHÁC (hiện hành hoặc đang chờ duyệt).
func CodeTaken(ctx context.Context, tx pgx.Tx, code string, except uuid.UUID) (bool, error) {
	var taken bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM mkg_hub.facility_current WHERE code = $1 AND facility_id <> $2)
	    OR EXISTS (SELECT 1 FROM mkg_hub.facility_version v WHERE v.code = $1 AND v.facility_id <> $2
	               AND NOT EXISTS (SELECT 1 FROM mkg_hub.facility_decision d WHERE d.version_id = v.id))`, code, except).Scan(&taken)
	return taken, err
}

// ListVersions liệt kê phiên bản của cơ sở, mới nhất trước.
func ListVersions(ctx context.Context, tx pgx.Tx, facility uuid.UUID) ([]domain.FacilityVersion, error) {
	rows, err := tx.Query(ctx, "SELECT "+versionCols+versionFrom+" WHERE v.facility_id = $1 ORDER BY v.seq DESC", facility)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FacilityVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Decision là một dòng sổ quyết định.
type Decision struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	FacilityID  uuid.UUID
	VersionID   uuid.UUID
	VersionSeq  int64
	Decision    domain.Decision
	Reason      *string
	ProposerRef uuid.UUID
	ActorRef    uuid.UUID
	EventTime   time.Time
	IdemKey     string
	Seq         int64
}

// InsertDecision ghi quyết định; CHECK actor_ref <> proposer_ref là lớp chặn tự duyệt thứ hai.
func InsertDecision(ctx context.Context, tx pgx.Tx, d *Decision) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_hub.facility_decision
	    (id, tenant_id, stream_key, facility_id, version_id, version_seq, decision, reason, proposer_ref,
	     event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING seq`,
		d.ID, d.TenantID, FacilityStream(d.FacilityID), d.FacilityID, d.VersionID, d.VersionSeq, string(d.Decision), d.Reason,
		d.ProposerRef, d.EventTime.UTC(), d.ActorRef, d.IdemKey, domain.RuleFacility).Scan(&d.Seq)
}

// DecisionByIdem tìm quyết định đã ghi với idem_key.
func DecisionByIdem(ctx context.Context, tx pgx.Tx, key string) (*Decision, error) {
	var d Decision
	var dec string
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, facility_id, version_id, version_seq, decision, reason, proposer_ref,
	    actor_ref, event_time, idem_key, seq FROM mkg_hub.facility_decision WHERE idem_key = $1`, key).
		Scan(&d.ID, &d.TenantID, &d.FacilityID, &d.VersionID, &d.VersionSeq, &dec, &d.Reason, &d.ProposerRef,
			&d.ActorRef, &d.EventTime, &d.IdemKey, &d.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	d.Decision = domain.Decision(dec)
	return &d, err
}

// UpsertCurrent đặt phiên bản đã duyệt làm hiện hành (bảng chiếu, R-21).
func UpsertCurrent(ctx context.Context, tx pgx.Tx, v domain.FacilityVersion, approver uuid.UUID, approvedAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_hub.facility_current
	    (tenant_id, facility_id, code, name, kind, parent_facility_id, location, boundary, province_code, status,
	     capacity_kg, effective_from, version_id, version_seq, proposed_by, approved_by, approved_at)
	  SELECT tenant_id, facility_id, code, name, kind, parent_facility_id, location, boundary, province_code, status,
	     capacity_kg, effective_from, id, seq, actor_ref, $2, $3
	    FROM mkg_hub.facility_version WHERE id = $1
	  ON CONFLICT (tenant_id, facility_id) DO UPDATE SET
	    code = EXCLUDED.code, name = EXCLUDED.name, kind = EXCLUDED.kind, parent_facility_id = EXCLUDED.parent_facility_id,
	    location = EXCLUDED.location, boundary = EXCLUDED.boundary, province_code = EXCLUDED.province_code,
	    status = EXCLUDED.status, capacity_kg = EXCLUDED.capacity_kg, effective_from = EXCLUDED.effective_from,
	    version_id = EXCLUDED.version_id, version_seq = EXCLUDED.version_seq, proposed_by = EXCLUDED.proposed_by,
	    approved_by = EXCLUDED.approved_by, approved_at = EXCLUDED.approved_at, updated_at = now()`,
		v.ID, approver, approvedAt.UTC())
	return err
}

const currentCols = `facility_id, code, name, kind, parent_facility_id, ST_Y(location)::text, ST_X(location)::text,
  ST_AsGeoJSON(boundary, 7)::text, province_code, status, capacity_kg::text, effective_from, version_id, version_seq,
  proposed_by, approved_by, approved_at`

func scanCurrent(row pgx.Row) (domain.Facility, error) {
	var f domain.Facility
	var lat, lng, kind, status, capKg string
	var boundary *string
	err := row.Scan(&f.ID, &f.Spec.Code, &f.Spec.Name, &kind, &f.Spec.ParentFacilityID, &lat, &lng, &boundary,
		&f.Spec.ProvinceCode, &status, &capKg, &f.Spec.EffectiveFrom, &f.VersionID, &f.VersionSeq,
		&f.ProposedBy, &f.ApprovedBy, &f.ApprovedAt)
	if err != nil {
		return f, err
	}
	f.Spec.Kind, f.Spec.Status = domain.Kind(kind), domain.Status(status)
	if f.Spec.Location.Lat, err = decimal.NewFromString(lat); err != nil {
		return f, err
	}
	if f.Spec.Location.Lng, err = decimal.NewFromString(lng); err != nil {
		return f, err
	}
	if boundary != nil {
		f.Spec.Boundary = json.RawMessage(*boundary)
	}
	f.Spec.Capacity, err = mass.ParseKg(capKg)
	return f, err
}

// Current đọc dữ liệu chủ hiện hành của cơ sở; forUpdate khóa dòng.
func Current(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Facility, error) {
	q := "SELECT " + currentCols + " FROM mkg_hub.facility_current WHERE facility_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	f, err := scanCurrent(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// ListCurrent phân trang bằng cursor (R-53): facility_id > after.
func ListCurrent(ctx context.Context, tx pgx.Tx, kind string, after uuid.UUID, limit int) ([]domain.Facility, error) {
	rows, err := tx.Query(ctx, "SELECT "+currentCols+` FROM mkg_hub.facility_current
	    WHERE facility_id > $1 AND ($2 = '' OR kind = $2) ORDER BY facility_id LIMIT $3`, after, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Facility
	for rows.Next() {
		f, err := scanCurrent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
