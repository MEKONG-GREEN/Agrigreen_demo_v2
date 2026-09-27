package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
)

// ---------------------------------------------------------------------------
// Chốt kỳ
// ---------------------------------------------------------------------------

// PeriodClosed cho biết kỳ đã chốt ở tỉnh.
func PeriodClosed(ctx context.Context, tx pgx.Tx, period, province string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_extension.period_close WHERE period = $1 AND province_code = $2)",
		period, province).Scan(&ok)
	return ok, err
}

// ClosePeriod chốt kỳ; đã chốt → trả false (giữ lần chốt đầu).
func ClosePeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, period, province string, by uuid.UUID, at time.Time, note string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_extension.period_close (tenant_id, period, province_code, closed_by, closed_at, note)
	    VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, tid, period, province, by, at.UTC(), nilIfEmpty(note))
	return tag.RowsAffected() == 1, err
}

// ClosedProvinces trả các tỉnh đã chốt kỳ.
func ClosedProvinces(ctx context.Context, tx pgx.Tx, period string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, "SELECT province_code FROM pf_extension.period_close WHERE period = $1", period)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Danh bạ trực
// ---------------------------------------------------------------------------

const staffCols = `actor_ref, unit_code, province_code, role_title, display_name, active, updated_at`

func scanStaff(row pgx.Row) (domain.Staff, error) {
	var s domain.Staff
	err := row.Scan(&s.ActorRef, &s.UnitCode, &s.ProvinceCode, &s.RoleTitle, &s.DisplayName, &s.Active, &s.UpdatedAt)
	return s, err
}

// StaffByActor đọc một cán bộ.
func StaffByActor(ctx context.Context, tx pgx.Tx, actor uuid.UUID) (*domain.Staff, error) {
	s, err := scanStaff(tx.QueryRow(ctx, "SELECT "+staffCols+" FROM pf_extension.staff WHERE actor_ref = $1", actor))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// UpsertStaff ghi danh bạ (bảng hiện tại, không phải sổ).
func UpsertStaff(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s domain.Staff, by uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.staff
	    (tenant_id, actor_ref, unit_code, province_code, role_title, display_name, active, updated_by)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	  ON CONFLICT (tenant_id, actor_ref) DO UPDATE SET
	    unit_code = EXCLUDED.unit_code, province_code = EXCLUDED.province_code, role_title = EXCLUDED.role_title,
	    display_name = EXCLUDED.display_name, active = EXCLUDED.active, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		tid, s.ActorRef, s.UnitCode, s.ProvinceCode, s.RoleTitle, s.DisplayName, s.Active, by)
	return err
}

// StaffInUnits liệt kê cán bộ đang hoạt động có đơn vị (xã) hoặc tỉnh thuộc codes.
// all = true: mọi cán bộ.
func StaffInUnits(ctx context.Context, tx pgx.Tx, all bool, codes []string) ([]domain.Staff, error) {
	rows, err := tx.Query(ctx, "SELECT "+staffCols+` FROM pf_extension.staff
	  WHERE active AND ($1::boolean OR unit_code = ANY($2::text[]) OR province_code = ANY($2::text[]))
	  ORDER BY province_code, unit_code, actor_ref`, all, units(codes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Staff
	for rows.Next() {
		s, err := scanStaff(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertDutyStatus ghi trạng thái tự khai.
func InsertDutyStatus(ctx context.Context, tx pgx.Tx, tid, id, actor uuid.UUID, status string, at, until time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.duty_status (tenant_id, id, actor_ref, status, set_at, valid_until)
	    VALUES ($1, $2, $3, $4, $5, $6)`, tid, id, actor, status, at.UTC(), until.UTC())
	return err
}

// LatestDutyStatus trả trạng thái tự khai mới nhất (không muộn hơn now) của từng actor.
func LatestDutyStatus(ctx context.Context, tx pgx.Tx, actors []uuid.UUID, now time.Time) (map[uuid.UUID]domain.ManualStatus, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (actor_ref) actor_ref, status, set_at, valid_until
	  FROM pf_extension.duty_status WHERE actor_ref = ANY($1::uuid[]) AND set_at <= $2
	  ORDER BY actor_ref, set_at DESC, recorded_at DESC`, uuidStrings(actors), now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]domain.ManualStatus{}
	for rows.Next() {
		var a uuid.UUID
		var m domain.ManualStatus
		if err := rows.Scan(&a, &m.Status, &m.SetAt, &m.ValidUntil); err != nil {
			return nil, err
		}
		out[a] = m
	}
	return out, rows.Err()
}

// InsertShift ghi một ca trực.
func InsertShift(ctx context.Context, tx pgx.Tx, tid uuid.UUID, s domain.Shift, by uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.duty_shift (tenant_id, id, actor_ref, starts_at, ends_at, created_by)
	    VALUES ($1, $2, $3, $4, $5, $6)`, tid, s.ID, s.ActorRef, s.StartsAt.UTC(), s.EndsAt.UTC(), by)
	return err
}

// ShiftsCovering trả ca trực của các actor có hiệu lực tại now.
func ShiftsCovering(ctx context.Context, tx pgx.Tx, actors []uuid.UUID, now time.Time) (map[uuid.UUID][]domain.Shift, error) {
	rows, err := tx.Query(ctx, `SELECT id, actor_ref, starts_at, ends_at FROM pf_extension.duty_shift
	  WHERE actor_ref = ANY($1::uuid[]) AND starts_at <= $2 AND ends_at > $2 ORDER BY starts_at`, uuidStrings(actors), now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]domain.Shift{}
	for rows.Next() {
		var s domain.Shift
		if err := rows.Scan(&s.ID, &s.ActorRef, &s.StartsAt, &s.EndsAt); err != nil {
			return nil, err
		}
		out[s.ActorRef] = append(out[s.ActorRef], s)
	}
	return out, rows.Err()
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
