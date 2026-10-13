package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

// ---------------------------------------------------------------------------
// Email theo người nhận (gói NOTIF, pf 20261013070025). Không lưu địa chỉ.
// ---------------------------------------------------------------------------

// InsertEmailDelivery xếp một lần gửi email của thông báo (một thông báo tối đa một lần — UNIQUE). Trả false nếu đã có.
func InsertEmailDelivery(ctx context.Context, tx pgx.Tx, tid, id, batch, notification, recipient uuid.UUID, kind string,
	now time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_notify.email_delivery
	    (tenant_id, id, batch_id, notification_id, recipient_ref, kind, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`, tid, id, batch, notification, recipient, kind, now.UTC())
	return tag.RowsAffected() == 1, err
}

// OpenEmailDeliveries trả lần gửi email chưa xong của lô (pending/failed còn lượt) kèm tiêu đề/nội dung thông báo.
func OpenEmailDeliveries(ctx context.Context, tx pgx.Tx, batch uuid.UUID) ([]domain.EmailJob, error) {
	rows, err := tx.Query(ctx, `SELECT d.id, d.batch_id, d.notification_id, d.recipient_ref, d.kind, d.status, d.attempts,
	    n.title, n.body, n.deep_link, d.created_at
	  FROM pf_notify.email_delivery d
	  JOIN pf_notify.notification n ON n.tenant_id = d.tenant_id AND n.id = d.notification_id
	  WHERE d.batch_id = $1 AND d.status IN ('pending', 'failed') AND d.attempts < $2
	  ORDER BY d.id`, batch, domain.MaxDeliveryAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EmailJob
	for rows.Next() {
		var j domain.EmailJob
		if err := rows.Scan(&j.ID, &j.BatchID, &j.NotificationID, &j.RecipientRef, &j.Kind, &j.Status, &j.Attempts,
			&j.Title, &j.Body, &j.DeepLink, &j.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// FinishEmailDelivery ghi kết quả một lần thử. lastErr chỉ là mã lỗi, không địa chỉ.
func FinishEmailDelivery(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, lastErr *string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_notify.email_delivery SET status = $2, attempts = attempts + 1, last_error = $3,
	    sent_at = CASE WHEN $2 = 'sent' THEN $4::timestamptz ELSE sent_at END
	  WHERE id = $1`, id, status, lastErr, now.UTC())
	return err
}

// EmailDeliveries liệt kê lần gửi email của thông báo kind (kiểm thử, vận hành): người nhận → trạng thái, số lần thử.
func EmailDeliveries(ctx context.Context, tx pgx.Tx, kind string) (map[uuid.UUID]domain.EmailJob, error) {
	rows, err := tx.Query(ctx, `SELECT id, batch_id, notification_id, recipient_ref, kind, status, attempts, created_at
	  FROM pf_notify.email_delivery WHERE kind = $1`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]domain.EmailJob{}
	for rows.Next() {
		var j domain.EmailJob
		if err := rows.Scan(&j.ID, &j.BatchID, &j.NotificationID, &j.RecipientRef, &j.Kind, &j.Status, &j.Attempts, &j.CreatedAt); err != nil {
			return nil, err
		}
		out[j.RecipientRef] = j
	}
	return out, rows.Err()
}
