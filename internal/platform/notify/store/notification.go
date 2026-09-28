// Package store: truy cập DB của pf/notify (schema pf_notify), chỉ qua pool của vai
// notify_app (R-05). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orgArg(v domain.Viewer) *uuid.UUID {
	if v.OrgRef == uuid.Nil {
		return nil
	}
	o := v.OrgRef
	return &o
}

// viewerArgs là $1..$7 của điều kiện visible.
func viewerArgs(v domain.Viewer, since time.Time) []any {
	return []any{v.ActorRef, orgArg(v), strs(v.Roles), v.Scope.All, strs(v.Scope.Units), v.Scope.Empty(), since.UTC()}
}

// visible là điều kiện "người xem thấy thông báo n" (bản SQL của domain.Audience.Matches):
// thông báo cá nhân của chính người xem, hoặc thông báo chủ đề trong cửa sổ $7 khớp
// vai ($3), tổ chức ($2), toàn quốc (phạm vi khác rỗng, $6) hoặc đơn vị ($5, khi
// phạm vi không phải toàn quốc $4).
const visible = `(n.recipient_ref = $1 OR (n.recipient_ref IS NULL AND n.created_at >= $7
  AND (cardinality(n.audience_roles) = 0 OR n.audience_roles && $3::text[])
  AND ((n.audience_org_ref IS NOT NULL AND n.audience_org_ref = $2::uuid)
    OR (n.audience_org_ref IS NULL AND n.audience_national AND NOT $6::boolean)
    OR (n.audience_org_ref IS NULL AND NOT $4::boolean AND n.audience_units && $5::text[]))))`

const fromViewer = ` FROM pf_notify.notification n
  LEFT JOIN pf_notify.receipt r ON r.tenant_id = n.tenant_id AND r.notification_id = n.id AND r.actor_ref = $1
  WHERE ` + visible

const notifCols = `n.id, n.recipient_ref, n.audience_national, n.audience_units, n.audience_org_ref, n.audience_roles,
  n.kind, n.severity, n.title, n.body, n.deep_link, n.subject_ref, n.source_event_id, n.source_event_type, n.created_at,
  coalesce(n.read_at, r.read_at), greatest(n.sync_seq, coalesce(r.sync_seq, 0))`

func scanNotification(row pgx.Row) (domain.Notification, error) {
	var n domain.Notification
	err := row.Scan(&n.ID, &n.RecipientRef, &n.Audience.National, &n.Audience.Units, &n.Audience.OrgRef, &n.Audience.Roles,
		&n.Kind, &n.Severity, &n.Title, &n.Body, &n.DeepLink, &n.SubjectRef, &n.SourceEventID, &n.SourceEventType, &n.CreatedAt,
		&n.ReadAt, &n.SyncSeq)
	return n, err
}

func collect(rows pgx.Rows, err error) ([]domain.Notification, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// InsertNotification ghi thông báo; trùng (sự kiện, người nhận) → bỏ qua, trả false.
func InsertNotification(ctx context.Context, tx pgx.Tx, tid uuid.UUID, n domain.Notification) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO pf_notify.notification
	    (tenant_id, id, recipient_ref, audience_national, audience_units, audience_org_ref, audience_roles, kind, severity,
	     title, body, deep_link, subject_ref, source_event_id, source_event_type, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) ON CONFLICT DO NOTHING`,
		tid, n.ID, n.RecipientRef, n.Audience.National, strs(n.Audience.Units), n.Audience.OrgRef, strs(n.Audience.Roles),
		n.Kind, n.Severity, n.Title, n.Body, n.DeepLink, n.SubjectRef, n.SourceEventID, n.SourceEventType, n.CreatedAt.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ListForViewer trả hộp thư của người xem, mới nhất trước; before = id của mục
// cuối trang trước (uuid.Nil = từ đầu).
func ListForViewer(ctx context.Context, tx pgx.Tx, v domain.Viewer, since time.Time, before uuid.UUID, unreadOnly bool, limit int) ([]domain.Notification, error) {
	args := append(viewerArgs(v, since), before, unreadOnly, limit)
	return collect(tx.Query(ctx, "SELECT "+notifCols+fromViewer+`
	  AND ($8::uuid = '00000000-0000-0000-0000-000000000000' OR n.id < $8)
	  AND (NOT $9::boolean OR (n.read_at IS NULL AND r.read_at IS NULL))
	  ORDER BY n.id DESC LIMIT $10`, args...))
}

// GetForViewer đọc một thông báo người xem thấy được; không thấy → nil.
func GetForViewer(ctx context.Context, tx pgx.Tx, v domain.Viewer, since time.Time, id uuid.UUID) (*domain.Notification, error) {
	args := append(viewerArgs(v, since), id)
	n, err := scanNotification(tx.QueryRow(ctx, "SELECT "+notifCols+fromViewer+" AND n.id = $8", args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// UnreadCount đếm thông báo chưa đọc người xem thấy được.
func UnreadCount(ctx context.Context, tx pgx.Tx, v domain.Viewer, since time.Time) (int64, error) {
	var c int64
	err := tx.QueryRow(ctx, "SELECT count(*)"+fromViewer+" AND n.read_at IS NULL AND r.read_at IS NULL",
		viewerArgs(v, since)...).Scan(&c)
	return c, err
}

// MarkPersonalRead đánh dấu đã đọc thông báo cá nhân (chưa đọc) và cấp số đồng bộ mới.
func MarkPersonalRead(ctx context.Context, tx pgx.Tx, id, actor uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_notify.notification SET read_at = $3, sync_seq = nextval('pf_notify.sync_seq')
	  WHERE id = $1 AND recipient_ref = $2 AND read_at IS NULL`, id, actor, now.UTC())
	return err
}

// InsertReceipt ghi đã đọc thông báo chủ đề (đọc lại → giữ lần đầu).
func InsertReceipt(ctx context.Context, tx pgx.Tx, tid, id, actor uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_notify.receipt (tenant_id, notification_id, actor_ref, read_at)
	  VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, tid, id, actor, now.UTC())
	return err
}

// ReadAll đánh dấu đã đọc mọi thông báo người xem thấy được; trả số đã đánh dấu.
func ReadAll(ctx context.Context, tx pgx.Tx, v domain.Viewer, since, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_notify.notification SET read_at = $2, sync_seq = nextval('pf_notify.sync_seq')
	  WHERE recipient_ref = $1 AND read_at IS NULL`, v.ActorRef, now.UTC())
	if err != nil {
		return 0, err
	}
	n := tag.RowsAffected()
	args := append(viewerArgs(v, since), now.UTC())
	tag, err = tx.Exec(ctx, `INSERT INTO pf_notify.receipt (tenant_id, notification_id, actor_ref, read_at)
	  SELECT n.tenant_id, n.id, $1, $8`+fromViewer+` AND n.recipient_ref IS NULL AND r.read_at IS NULL
	  ON CONFLICT DO NOTHING`, args...)
	if err != nil {
		return 0, err
	}
	return n + tag.RowsAffected(), nil
}

// SyncChanges trả thông báo người xem thấy được có số đồng bộ (của thông báo
// hoặc biên nhận đã đọc) > after, tăng dần.
func SyncChanges(ctx context.Context, tx pgx.Tx, v domain.Viewer, since time.Time, after int64, limit int) ([]domain.Notification, error) {
	args := append(viewerArgs(v, since), after, limit)
	return collect(tx.Query(ctx, "SELECT "+notifCols+fromViewer+` AND n.created_at >= $7
	  AND greatest(n.sync_seq, coalesce(r.sync_seq, 0)) > $8
	  ORDER BY greatest(n.sync_seq, coalesce(r.sync_seq, 0)) LIMIT $9`, args...))
}
