package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
)

// CapacitySnapshot là một dòng sổ sức chứa (ảnh chụp hoặc lệnh nhả số Admin).
type CapacitySnapshot struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	SubjectKind      string
	SubjectRef       uuid.UUID
	Seq              int64
	Action           string
	Source           string
	AsOf             time.Time
	Capacity         *decimal.Decimal
	Used             *decimal.Decimal
	Unit             string
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
}

// InsertCapacity ghi một dòng sổ sức chứa; DB tính seq và băm.
func InsertCapacity(ctx context.Context, tx pgx.Tx, s *CapacitySnapshot) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_geo.facility_capacity
	    (id, tenant_id, stream_key, subject_kind, subject_ref, action, source, as_of, capacity, used, unit, event_time,
	     supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::text, $5::uuid, $6::text, $7::text, $8::timestamptz, $9::numeric, $10::numeric,
	     $11::text, $12::timestamptz, $13::uuid, $14::text, $15::uuid, $16::text, $17::text, $18::text)
	  RETURNING seq, recorded_at`,
		s.ID, s.TenantID, domain.CapacityStreamKey(s.SubjectKind, s.SubjectRef), s.SubjectKind, s.SubjectRef, s.Action, s.Source,
		s.AsOf.UTC(), decPtr(s.Capacity), decPtr(s.Used), nilIfEmpty(s.Unit), s.EventTime.UTC(), s.SupersedesID,
		s.CorrectionReason, s.ActorRef, s.DeviceID, s.IdemKey, domain.RuleCapacity).Scan(&s.Seq, &s.RecordedAt)
}

const capacityCols = `id, tenant_id, subject_kind, subject_ref, seq, action, source, as_of, capacity::text, used::text,
  coalesce(unit, ''), event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key`

func scanCapacity(row pgx.Row) (CapacitySnapshot, error) {
	var s CapacitySnapshot
	var capacity, used *string
	if err := row.Scan(&s.ID, &s.TenantID, &s.SubjectKind, &s.SubjectRef, &s.Seq, &s.Action, &s.Source, &s.AsOf, &capacity,
		&used, &s.Unit, &s.EventTime, &s.RecordedAt, &s.SupersedesID, &s.CorrectionReason, &s.ActorRef, &s.DeviceID,
		&s.IdemKey); err != nil {
		return s, err
	}
	var err error
	if s.Capacity, err = parseDec(capacity); err != nil {
		return s, err
	}
	s.Used, err = parseDec(used)
	return s, err
}

// CapacityByIdemKey tìm dòng sổ sức chứa đã ghi với idem_key.
func CapacityByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*CapacitySnapshot, error) {
	s, err := scanCapacity(tx.QueryRow(ctx, "SELECT "+capacityCols+" FROM pf_geo.facility_capacity WHERE idem_key = $1::text", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CapacityByID đọc một dòng sổ sức chứa.
func CapacityByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*CapacitySnapshot, error) {
	s, err := scanCapacity(tx.QueryRow(ctx, "SELECT "+capacityCols+" FROM pf_geo.facility_capacity WHERE id = $1::uuid", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// CapacityHistory trả các dòng sổ sức chứa mới nhất trước (Event Log / Snapshot).
func CapacityHistory(ctx context.Context, tx pgx.Tx, kind string, ref uuid.UUID, limit int) ([]CapacitySnapshot, error) {
	rows, err := tx.Query(ctx, "SELECT "+capacityCols+` FROM pf_geo.facility_capacity
	  WHERE subject_kind = $1::text AND subject_ref = $2::uuid ORDER BY seq DESC LIMIT $3::int`, kind, ref, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CapacitySnapshot
	for rows.Next() {
		s, err := scanCapacity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CapacityLatest là ảnh chụp mới nhất của một nguồn cho một đối tượng.
type CapacityLatest struct {
	SubjectKind string
	SubjectRef  uuid.UUID
	Source      string
	SnapshotID  uuid.UUID
	AsOf        time.Time
	Capacity    decimal.Decimal
	Used        decimal.Decimal
	Unit        string
	Released    bool
	ReleasedAt  *time.Time
}

// ApplyLatest cập nhật bảng chiếu theo ảnh chụp: chỉ thay khi as_of không cũ hơn.
// Ảnh chụp mới của nguồn Admin bỏ cờ đã nhả. Không đụng dòng của nguồn khác
// — số của van không bao giờ ghi đè số Admin nhập (BR-52).
func ApplyLatest(ctx context.Context, tx pgx.Tx, s CapacitySnapshot) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_geo.capacity_latest
	    (tenant_id, subject_kind, subject_ref, source, snapshot_id, as_of, capacity, used, unit)
	  VALUES ($1::uuid, $2::text, $3::uuid, $4::text, $5::uuid, $6::timestamptz, $7::numeric, $8::numeric, $9::text)
	  ON CONFLICT (tenant_id, subject_kind, subject_ref, source) DO UPDATE SET snapshot_id = EXCLUDED.snapshot_id,
	    as_of = EXCLUDED.as_of, capacity = EXCLUDED.capacity, used = EXCLUDED.used, unit = EXCLUDED.unit,
	    released = false, released_at = NULL, updated_at = now()
	  WHERE pf_geo.capacity_latest.as_of <= EXCLUDED.as_of`,
		s.TenantID, s.SubjectKind, s.SubjectRef, s.Source, s.ID, s.AsOf.UTC(), decPtr(s.Capacity), decPtr(s.Used), s.Unit)
	return err
}

// ReleaseAdmin đánh dấu các số Admin (manual, import) của đối tượng là đã nhả;
// trả số dòng bị đổi.
func ReleaseAdmin(ctx context.Context, tx pgx.Tx, kind string, ref uuid.UUID, at time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_geo.capacity_latest SET released = true, released_at = $3::timestamptz, updated_at = now()
	  WHERE subject_kind = $1::text AND subject_ref = $2::uuid AND source IN ('manual', 'import') AND NOT released`,
		kind, ref, at.UTC())
	return tag.RowsAffected(), err
}

// SubjectKey là khóa (loại, ref) của đối tượng có sức chứa.
type SubjectKey struct {
	Kind string
	Ref  uuid.UUID
}

// LatestFor trả ảnh chụp mới nhất theo nguồn của các đối tượng.
func LatestFor(ctx context.Context, tx pgx.Tx, kind string, refs []uuid.UUID) (map[SubjectKey][]CapacityLatest, error) {
	out := map[SubjectKey][]CapacityLatest{}
	if len(refs) == 0 {
		return out, nil
	}
	ss := make([]string, 0, len(refs))
	for _, r := range refs {
		ss = append(ss, r.String())
	}
	rows, err := tx.Query(ctx, `SELECT subject_kind, subject_ref, source, snapshot_id, as_of, capacity::text, used::text, unit,
	    released, released_at
	  FROM pf_geo.capacity_latest WHERE subject_kind = $1::text AND subject_ref = ANY($2::text[]::uuid[])`, kind, ss)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var l CapacityLatest
		var capacity, used string
		if err := rows.Scan(&l.SubjectKind, &l.SubjectRef, &l.Source, &l.SnapshotID, &l.AsOf, &capacity, &used, &l.Unit,
			&l.Released, &l.ReleasedAt); err != nil {
			return nil, err
		}
		if l.Capacity, err = decOf(capacity); err != nil {
			return nil, err
		}
		if l.Used, err = decOf(used); err != nil {
			return nil, err
		}
		k := SubjectKey{l.SubjectKind, l.SubjectRef}
		out[k] = append(out[k], l)
	}
	return out, rows.Err()
}
