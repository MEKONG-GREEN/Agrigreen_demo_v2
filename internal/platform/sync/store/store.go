// Package store: truy cập DB của pf/sync (schema pf_sync, pf_files), chỉ qua pool
// của vai sync_app (R-05). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
)

// ---------------------------------------------------------------------------
// Thiết bị
// ---------------------------------------------------------------------------

const deviceCols = `tenant_id, device_id, actor_ref, org_ref, platform, app_version, registered_at,
  last_seen_at, revoked_at, revoked_by, revoke_reason`

func scanDevice(row pgx.Row) (domain.Device, error) {
	var d domain.Device
	err := row.Scan(&d.TenantID, &d.ID, &d.ActorRef, &d.OrgRef, &d.Platform, &d.AppVersion, &d.RegisteredAt,
		&d.LastSeenAt, &d.RevokedAt, &d.RevokedBy, &d.RevokeReason)
	return d, err
}

// DeviceByID đọc thiết bị; forUpdate khóa dòng.
func DeviceByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Device, error) {
	q := "SELECT " + deviceCols + " FROM pf_sync.device WHERE device_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	d, err := scanDevice(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// InsertDevice ghi thiết bị mới.
func InsertDevice(ctx context.Context, tx pgx.Tx, d domain.Device) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_sync.device
	    (tenant_id, device_id, actor_ref, org_ref, platform, app_version, registered_at, last_seen_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		d.TenantID, d.ID, d.ActorRef, d.OrgRef, d.Platform, d.AppVersion, d.RegisteredAt.UTC())
	return err
}

// RefreshDevice cập nhật phiên bản app khi máy đăng ký lại (cài lại, nâng cấp).
func RefreshDevice(ctx context.Context, tx pgx.Tx, id uuid.UUID, platform, appVersion string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_sync.device SET platform = $2, app_version = $3, last_seen_at = $4
	  WHERE device_id = $1`, id, platform, appVersion, now.UTC())
	return err
}

// TouchDevice ghi lần thấy cuối, tối đa mỗi phút một lần để không ghi dồn.
func TouchDevice(ctx context.Context, tx pgx.Tx, id uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_sync.device SET last_seen_at = $2
	  WHERE device_id = $1 AND (last_seen_at IS NULL OR last_seen_at < $2::timestamptz - interval '1 minute')`, id, now.UTC())
	return err
}

// RevokeDevice đánh dấu thu hồi.
func RevokeDevice(ctx context.Context, tx pgx.Tx, id, by uuid.UUID, reason string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_sync.device SET revoked_at = $2, revoked_by = $3, revoke_reason = $4
	  WHERE device_id = $1 AND revoked_at IS NULL`, id, now.UTC(), by, reason)
	return err
}

// ListDevices phân trang theo device_id (UUIDv7), lọc theo người đăng ký nếu có.
func ListDevices(ctx context.Context, tx pgx.Tx, actor *uuid.UUID, after uuid.UUID, limit int) ([]domain.Device, error) {
	rows, err := tx.Query(ctx, "SELECT "+deviceCols+` FROM pf_sync.device
	  WHERE device_id > $1 AND ($2::uuid IS NULL OR actor_ref = $2) ORDER BY device_id LIMIT $3`, after, actor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
