package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
)

// ---------------------------------------------------------------------------
// Token đẩy
// ---------------------------------------------------------------------------

const tokenCols = `device_id, actor_ref, org_ref, provider, token, platform, scope_all, scope_units, roles,
  registered_at, updated_at, disabled_at, disabled_reason`

func scanToken(row pgx.Row) (domain.PushToken, error) {
	var t domain.PushToken
	err := row.Scan(&t.DeviceID, &t.ActorRef, &t.OrgRef, &t.Provider, &t.Token, &t.Platform, &t.ScopeAll, &t.ScopeUnits,
		&t.Roles, &t.RegisteredAt, &t.UpdatedAt, &t.DisabledAt, &t.DisabledReason)
	return t, err
}

func collectTokens(rows pgx.Rows, err error) ([]domain.PushToken, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PushToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpsertToken gắn (hoặc thay) token của thiết bị và bật lại nếu đã ngừng.
func UpsertToken(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t domain.PushToken) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_notify.push_token
	    (tenant_id, device_id, actor_ref, org_ref, provider, token, platform, scope_all, scope_units, roles, registered_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
	  ON CONFLICT (tenant_id, device_id) DO UPDATE SET
	    actor_ref = EXCLUDED.actor_ref, org_ref = EXCLUDED.org_ref, provider = EXCLUDED.provider, token = EXCLUDED.token,
	    platform = EXCLUDED.platform, scope_all = EXCLUDED.scope_all, scope_units = EXCLUDED.scope_units,
	    roles = EXCLUDED.roles, updated_at = EXCLUDED.updated_at, disabled_at = NULL, disabled_reason = NULL`,
		tid, t.DeviceID, t.ActorRef, t.OrgRef, t.Provider, t.Token, t.Platform, t.ScopeAll, strs(t.ScopeUnits), strs(t.Roles),
		t.UpdatedAt.UTC())
	return err
}

// TokenByDevice đọc token của thiết bị (kể cả đã ngừng); không có → nil.
func TokenByDevice(ctx context.Context, tx pgx.Tx, device uuid.UUID) (*domain.PushToken, error) {
	t, err := scanToken(tx.QueryRow(ctx, "SELECT "+tokenCols+" FROM pf_notify.push_token WHERE device_id = $1", device))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DisableTokenValue ngừng dùng cùng giá trị token ở thiết bị khác (cài lại app → mã
// thiết bị mới nhưng FCM giữ token).
func DisableTokenValue(ctx context.Context, tx pgx.Tx, token string, except uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_notify.push_token SET disabled_at = $3, disabled_reason = 'replaced'
	  WHERE token = $1 AND device_id <> $2 AND disabled_at IS NULL`, token, except, now.UTC())
	return err
}

// DisableDevice ngừng dùng token của một thiết bị.
func DisableDevice(ctx context.Context, tx pgx.Tx, device uuid.UUID, reason string, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_notify.push_token SET disabled_at = $3, disabled_reason = $2
	  WHERE device_id = $1 AND disabled_at IS NULL`, device, reason, now.UTC())
	return tag.RowsAffected(), err
}

// DisableOrg ngừng dùng token của mọi thiết bị thuộc tổ chức.
func DisableOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_notify.push_token SET disabled_at = $2, disabled_reason = 'org_deactivated'
	  WHERE org_ref = $1 AND disabled_at IS NULL`, org, now.UTC())
	return tag.RowsAffected(), err
}

// ActiveTokensForActors trả token đang dùng của các người nhận.
func ActiveTokensForActors(ctx context.Context, tx pgx.Tx, actors []uuid.UUID) ([]domain.PushToken, error) {
	if len(actors) == 0 {
		return nil, nil
	}
	return collectTokens(tx.Query(ctx, "SELECT "+tokenCols+` FROM pf_notify.push_token
	  WHERE disabled_at IS NULL AND actor_ref = ANY($1::uuid[]) ORDER BY device_id`, uuidStrings(actors)))
}

// ActiveTokensForAudience trả token đang dùng có ảnh chụp phạm vi/vai khớp chủ đề
// (cùng quy tắc với domain.Audience.Matches).
func ActiveTokensForAudience(ctx context.Context, tx pgx.Tx, a domain.Audience) ([]domain.PushToken, error) {
	return collectTokens(tx.Query(ctx, "SELECT "+tokenCols+` FROM pf_notify.push_token t
	  WHERE disabled_at IS NULL
	    AND (cardinality($4::text[]) = 0 OR t.roles && $4::text[])
	    AND (($3::uuid IS NOT NULL AND t.org_ref = $3::uuid)
	      OR ($3::uuid IS NULL AND $1::boolean AND (t.scope_all OR cardinality(t.scope_units) > 0))
	      OR ($3::uuid IS NULL AND NOT t.scope_all AND t.scope_units && $2::text[]))
	  ORDER BY device_id`, a.National, strs(a.Units), a.OrgRef, strs(a.Roles)))
}

// ActiveTokensInUnit trả token đang dùng của thiết bị có phạm vi chứa đơn vị (nudge).
func ActiveTokensInUnit(ctx context.Context, tx pgx.Tx, unit string) ([]domain.PushToken, error) {
	return collectTokens(tx.Query(ctx, "SELECT "+tokenCols+` FROM pf_notify.push_token
	  WHERE disabled_at IS NULL AND $1 = ANY(scope_units) ORDER BY device_id`, unit))
}

// ---------------------------------------------------------------------------
// Lần gửi đẩy
// ---------------------------------------------------------------------------

// InsertDelivery xếp một lần gửi.
func InsertDelivery(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d domain.Delivery) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_notify.push_delivery
	    (tenant_id, id, batch_id, device_id, notification_id, kind, urgent, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
		tid, d.ID, d.BatchID, d.DeviceID, d.NotificationID, d.Kind, d.Urgent, d.CreatedAt.UTC())
	return err
}

// OpenDeliveries trả lần gửi chưa xong của lô (pending/failed còn lượt), kèm token
// hiện tại của thiết bị.
func OpenDeliveries(ctx context.Context, tx pgx.Tx, batch uuid.UUID) ([]domain.DeliveryJob, error) {
	rows, err := tx.Query(ctx, `SELECT d.id, d.batch_id, d.device_id, d.notification_id, d.kind, d.urgent, d.status, d.attempts,
	    d.created_at, d.sent_at, t.device_id IS NOT NULL AND t.disabled_at IS NULL, coalesce(t.token, ''), coalesce(t.platform, ''),
	    coalesce(n.entities, '{}')
	  FROM pf_notify.push_delivery d
	  LEFT JOIN pf_notify.push_token t ON t.tenant_id = d.tenant_id AND t.device_id = d.device_id
	  LEFT JOIN pf_notify.nudge n ON n.tenant_id = d.tenant_id AND n.id = d.batch_id AND d.kind = 'sync_now'
	  WHERE d.batch_id = $1 AND d.status IN ('pending', 'failed') AND d.attempts < $2
	  ORDER BY d.id`, batch, domain.MaxDeliveryAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DeliveryJob
	for rows.Next() {
		var j domain.DeliveryJob
		var active bool
		var tok domain.PushToken
		if err := rows.Scan(&j.ID, &j.BatchID, &j.DeviceID, &j.NotificationID, &j.Kind, &j.Urgent, &j.Status, &j.Attempts,
			&j.CreatedAt, &j.SentAt, &active, &tok.Token, &tok.Platform, &j.Entities); err != nil {
			return nil, err
		}
		if active {
			tok.DeviceID = j.DeviceID
			j.Token = &tok
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// FinishDelivery ghi kết quả một lần thử (finished_at = now, cùng UPDATE với status — mốc của delivery-health).
// lastErr chỉ là mã lỗi ngắn, không token.
func FinishDelivery(ctx context.Context, tx pgx.Tx, id uuid.UUID, status string, lastErr *string, now time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE pf_notify.push_delivery SET status = $2, attempts = attempts + 1, last_error = $3,
	    finished_at = $4::timestamptz, sent_at = CASE WHEN $2 = 'sent' THEN $4::timestamptz ELSE sent_at END
	  WHERE id = $1`, id, status, lastErr, now.UTC())
	return err
}

// DeliveryStatuses đếm lần gửi của lô theo trạng thái (kiểm thử, vận hành).
func DeliveryStatuses(ctx context.Context, tx pgx.Tx, batch uuid.UUID) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT status, count(*) FROM pf_notify.push_delivery WHERE batch_id = $1 GROUP BY status`, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Nudge (US-SYNC-02)
// ---------------------------------------------------------------------------

// LockNudgeTarget tuần tự hóa kiểm giới hạn tần suất của một đích trong giao dịch.
func LockNudgeTarget(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind, target string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 7270012))", tid.String()+"|"+kind+"|"+target)
	return err
}

// LastNudgeAt trả lần nudge gần nhất của đích; chưa có → nil.
func LastNudgeAt(ctx context.Context, tx pgx.Tx, kind, target string) (*time.Time, error) {
	var t *time.Time
	err := tx.QueryRow(ctx, `SELECT max(created_at) FROM pf_notify.nudge WHERE target_kind = $1 AND target = $2`, kind, target).Scan(&t)
	return t, err
}

// CountNudgesSince đếm nudge của một người từ since.
func CountNudgesSince(ctx context.Context, tx pgx.Tx, actor uuid.UUID, since time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_notify.nudge WHERE actor_ref = $1 AND created_at >= $2`, actor, since.UTC()).Scan(&n)
	return n, err
}

// InsertNudge ghi một yêu cầu đồng bộ ngay.
func InsertNudge(ctx context.Context, tx pgx.Tx, tid, id, actor uuid.UUID, kind, target string, devices int, entities []string, now time.Time) error {
	if entities == nil {
		entities = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_notify.nudge (tenant_id, id, actor_ref, target_kind, target, devices, entities, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, tid, id, actor, kind, target, devices, entities, now.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Email tóm tắt chỉ đạo khẩn
// ---------------------------------------------------------------------------

// InsertDigestItem xếp một tham chiếu nguồn vào email tóm tắt (trùng → bỏ qua).
func InsertDigestItem(ctx context.Context, tx pgx.Tx, tid uuid.UUID, it domain.DigestItem, eventID uuid.UUID, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_notify.email_digest_item
	    (tenant_id, directive_id, source_event_id, audience_units, issued_at, queued_at)
	  VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
		tid, it.Reference, eventID, strs(it.AudienceUnits), it.IssuedAt.UTC(), now.UTC())
	return err
}

// ClaimDigest NHẬN tối đa limit mục chưa gửi và chưa có người nhận còn hạn (P0-POOL-SHARED, #126): khóa dòng (hai
// worker không nhận trùng), ghi claim_id + claimed_until rồi trả mục. Gửi email chạy SAU khi giao dịch này commit;
// kết quả ghi bằng MarkDigestSent / ReleaseDigestClaim trong giao dịch ngắn khác. Hết claimed_until mà chưa kết
// (tiến trình chết) → mục được nhận lại.
func ClaimDigest(ctx context.Context, tx pgx.Tx, claim uuid.UUID, now, until time.Time, limit int) ([]domain.DigestItem, error) {
	rows, err := tx.Query(ctx, `WITH picked AS (
	    SELECT tenant_id, directive_id FROM pf_notify.email_digest_item
	     WHERE sent_at IS NULL AND (claimed_until IS NULL OR claimed_until <= $2::timestamptz)
	     ORDER BY issued_at, directive_id LIMIT $4 FOR UPDATE SKIP LOCKED)
	  UPDATE pf_notify.email_digest_item d SET claim_id = $1::uuid, claimed_until = $3::timestamptz
	    FROM picked p WHERE d.tenant_id = p.tenant_id AND d.directive_id = p.directive_id
	  RETURNING d.directive_id, d.audience_units, d.issued_at`, claim, now.UTC(), until.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DigestItem
	for rows.Next() {
		var it domain.DigestItem
		if err := rows.Scan(&it.Reference, &it.AudienceUnits, &it.IssuedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b domain.DigestItem) int {
		if c := a.IssuedAt.Compare(b.IssuedAt); c != 0 {
			return c
		}
		return strings.Compare(a.Reference.String(), b.Reference.String())
	})
	return out, nil
}

// MarkDigestSent kết lần nhận claim: các mục còn giữ claim và chưa gửi → digest_id = claim, sent_at. Trả số mục.
func MarkDigestSent(ctx context.Context, tx pgx.Tx, claim uuid.UUID, now time.Time) (int64, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_notify.email_digest_item SET digest_id = $1::uuid, sent_at = $2::timestamptz,
	    claim_id = NULL, claimed_until = NULL
	  WHERE claim_id = $1::uuid AND sent_at IS NULL`, claim, now.UTC())
	return tag.RowsAffected(), err
}

// ReleaseDigestClaim trả các mục của lần nhận claim về hàng chờ (gửi lỗi) — lần chạy sau gửi lại.
func ReleaseDigestClaim(ctx context.Context, tx pgx.Tx, claim uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE pf_notify.email_digest_item SET claim_id = NULL, claimed_until = NULL
	  WHERE claim_id = $1::uuid AND sent_at IS NULL`, claim)
	return err
}

// PendingDigestTenants tìm tenant có mục email chờ gửi. pool phải là vai
// ag_ops_jobs (BYPASSRLS, chỉ có quyền đọc hai cột).
func PendingDigestTenants(ctx context.Context, pool *pgxpool.Pool) ([]uuid.UUID, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_notify.email_digest_item WHERE sent_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var t uuid.UUID
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// uuidStrings: mảng uuid gửi dạng text[] (pgx không có kế hoạch mã hóa []uuid.UUID ở chế độ PgBouncer).
func uuidStrings(in []uuid.UUID) []string {
	out := make([]string, len(in))
	for i, u := range in {
		out[i] = u.String()
	}
	return out
}
