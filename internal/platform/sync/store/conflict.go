package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
)

const conflictCols = `tenant_id, id, entity, first_record_id, first_idem_key, device_id, reported_by, owner_org_ref,
  opened_at, due_at, status, resolved_at, resolved_by, resolution_note, resolved_record_id`

func scanConflict(row pgx.Row) (domain.Conflict, error) {
	var c domain.Conflict
	err := row.Scan(&c.TenantID, &c.ID, &c.Entity, &c.FirstRecordID, &c.FirstIdemKey, &c.DeviceID, &c.ReportedBy, &c.OwnerOrgRef,
		&c.OpenedAt, &c.DueAt, &c.Status, &c.ResolvedAt, &c.ResolvedBy, &c.ResolutionNote, &c.ResolvedRecordID)
	return c, err
}

// InsertConflict đăng ký xung đột; đã có (bản ghi thứ hai của cùng xung đột) →
// inserted = false, giữ mục cũ.
func InsertConflict(ctx context.Context, tx pgx.Tx, c domain.Conflict) (inserted bool, err error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_sync.conflict
	    (tenant_id, id, entity, first_record_id, first_idem_key, device_id, reported_by, owner_org_ref, opened_at, due_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT DO NOTHING`,
		c.TenantID, c.ID, c.Entity, c.FirstRecordID, c.FirstIdemKey, c.DeviceID, c.ReportedBy, c.OwnerOrgRef,
		c.OpenedAt.UTC(), c.DueAt.UTC())
	return tag.RowsAffected() == 1, err
}

// ConflictByID đọc một xung đột; forUpdate khóa dòng.
func ConflictByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Conflict, error) {
	q := "SELECT " + conflictCols + " FROM pf_sync.conflict WHERE id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	c, err := scanConflict(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ConflictFilter lọc danh sách xung đột.
type ConflictFilter struct {
	Status      string     // rỗng = mọi trạng thái
	OverdueAt   *time.Time // khác nil: chỉ mục đang mở có due_at < OverdueAt
	OwnerOrgRef *uuid.UUID
	Entity      string // rỗng = mọi entity
	// DueWindow khác nil: chỉ mục đang mở CHƯA quá hạn tại Now và hết hạn trước Now + DueWindow
	// ("sắp quá hạn", US-SYNC-03). OnTrackAt khác nil: mục đang mở còn hạn sau thời điểm này.
	Now       time.Time
	DueWindow *time.Duration
	OnTrackAt *time.Time
}

// ListConflicts phân trang theo id (UUIDv7).
func ListConflicts(ctx context.Context, tx pgx.Tx, f ConflictFilter, after uuid.UUID, limit int) ([]domain.Conflict, error) {
	var status *string
	if f.Status != "" {
		status = &f.Status
	}
	var overdue *time.Time
	if f.OverdueAt != nil {
		t := f.OverdueAt.UTC()
		overdue = &t
	}
	var soonFrom, soonTo, onTrack *time.Time
	if f.DueWindow != nil {
		a, b := f.Now.UTC(), f.Now.Add(*f.DueWindow).UTC()
		soonFrom, soonTo = &a, &b
	}
	if f.OnTrackAt != nil {
		t := f.OnTrackAt.UTC()
		onTrack = &t
	}
	rows, err := tx.Query(ctx, "SELECT "+conflictCols+` FROM pf_sync.conflict
	  WHERE id > $1 AND ($2::text IS NULL OR status = $2)
	    AND ($3::timestamptz IS NULL OR (status = 'open' AND due_at < $3))
	    AND ($4::uuid IS NULL OR owner_org_ref = $4)
	    AND ($6::text = '' OR entity = $6)
	    AND ($7::timestamptz IS NULL OR (status = 'open' AND due_at >= $7 AND due_at < $8::timestamptz))
	    AND ($9::timestamptz IS NULL OR (status = 'open' AND due_at >= $9))
	  ORDER BY id LIMIT $5`, after, status, overdue, f.OwnerOrgRef, limit, f.Entity, soonFrom, soonTo, onTrack)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conflict
	for rows.Next() {
		c, err := scanConflict(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ResolveConflict đóng xung đột đang mở.
func ResolveConflict(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, note string, record *uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_sync.conflict
	  SET status = 'resolved', resolved_at = $2, resolved_by = $3, resolution_note = $4, resolved_record_id = $5
	  WHERE id = $1 AND status = 'open'`, id, now.UTC(), by, note, record)
	return err
}

// ConflictSLA đếm xung đột đang mở và quá hạn tại now trong tenant hiện tại.
func ConflictSLA(ctx context.Context, tx pgx.Tx, now time.Time) (port.ConflictSLA, error) {
	var s port.ConflictSLA
	err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE due_at < $1), min(due_at) FILTER (WHERE due_at < $1)
	  FROM pf_sync.conflict WHERE status = 'open'`, now.UTC()).Scan(&s.Open, &s.Overdue, &s.OldestDueAt)
	return s, err
}
