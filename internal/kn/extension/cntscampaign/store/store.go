// Package store: SQL của Chiến dịch điều tra CNTS (pf 20261024091058 — pf_extension.cnts_campaign, cnts_campaign_event) trên
// pool vai của pf/extension, luôn trong giao dịch có ngữ cảnh tenant (RLS). Hai sổ chỉ ghi thêm (shared.apply_ledger).
package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscampaign/domain"
)

// Version là một phiên bản nội dung đợt.
type Version struct {
	RowID             uuid.UUID
	CampaignID        uuid.UUID
	Seq               int64
	OrgRef            uuid.UUID
	Province          *string
	Name              string
	Goal              *string
	Field             string
	TemplateCode      string
	ScopeMode         string
	Regions           []string
	Units             []string
	From, To          time.Time
	DueAt             time.Time
	OwnerRef          *uuid.UUID
	RemindBeforeHours int
	ActorRef          uuid.UUID
	EventTime         time.Time
	RecordedAt        time.Time
}

const versionCols = `id, campaign_id, seq, org_ref, province_code, name, goal, field, template_code, scope_mode, regions, units,
  period_from, period_to, due_at, owner_ref, remind_before_hours, actor_ref, event_time, recorded_at`

func scanVersion(row pgx.Row) (Version, error) {
	var v Version
	err := row.Scan(&v.RowID, &v.CampaignID, &v.Seq, &v.OrgRef, &v.Province, &v.Name, &v.Goal, &v.Field, &v.TemplateCode, &v.ScopeMode,
		&v.Regions, &v.Units, &v.From, &v.To, &v.DueAt, &v.OwnerRef, &v.RemindBeforeHours, &v.ActorRef, &v.EventTime, &v.RecordedAt)
	return v, err
}

func dateOnly(t time.Time) string { return t.Format(time.DateOnly) }

// InsertVersion ghi phiên bản nội dung (trigger chặn khi đợt đã phát hành — cnts_campaign_not_draft).
func InsertVersion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, v *Version, idem string) error {
	if v.Regions == nil {
		v.Regions = []string{}
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_campaign (id, tenant_id, stream_key, campaign_id, org_ref, province_code, name,
	    goal, field, template_code, scope_mode, regions, units, period_from, period_to, due_at, owner_ref, remind_before_hours,
	    event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_campaign:' || $3::uuid::text, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::date, $14::date, $15, $16, $17,
	    $18, $19, $20, $21)
	  RETURNING seq, recorded_at`,
		v.RowID, tid, v.CampaignID, v.OrgRef, v.Province, v.Name, v.Goal, v.Field, v.TemplateCode, v.ScopeMode, v.Regions, v.Units,
		dateOnly(v.From), dateOnly(v.To), v.DueAt.UTC(), v.OwnerRef, v.RemindBeforeHours, v.EventTime.UTC(), v.ActorRef, idem,
		domain.RuleVersion).Scan(&v.Seq, &v.RecordedAt)
}

// VersionByIdem đọc phiên bản đã ghi với khóa idempotent (nil nếu chưa có).
func VersionByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Version, error) {
	v, err := scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_extension.cnts_campaign WHERE idem_key = $1 LIMIT 1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

// Latest đọc phiên bản nội dung mới nhất của đợt (nil nếu không có).
func Latest(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Version, error) {
	v, err := scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_extension.cnts_campaign WHERE campaign_id = $1
	  ORDER BY seq DESC LIMIT 1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

// VersionAt đọc phiên bản seq của đợt (nil nếu không có).
func VersionAt(ctx context.Context, tx pgx.Tx, id uuid.UUID, seq int64) (*Version, error) {
	v, err := scanVersion(tx.QueryRow(ctx, `SELECT `+versionCols+` FROM pf_extension.cnts_campaign WHERE campaign_id = $1 AND seq = $2`, id, seq))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

// List liệt kê phiên bản mới nhất của các đợt (org = Nút; Nil = mọi Nút), mới trước.
func List(ctx context.Context, tx pgx.Tx, org uuid.UUID, limit int) ([]Version, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (campaign_id) `+versionCols+` FROM pf_extension.cnts_campaign
	  WHERE ($1 = '00000000-0000-0000-0000-000000000000'::uuid OR org_ref = $1)
	  ORDER BY campaign_id DESC, seq DESC LIMIT $2`, org, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Version, error) { return scanVersion(r) })
}

// Event là một sự kiện của đợt.
type Event struct {
	ID              uuid.UUID
	CampaignID      uuid.UUID
	Seq             int64
	Action          string
	Version         *int64
	FormsPeriodRef  *uuid.UUID
	PeriodKey       *string
	TemplateVersion *int64
	DueAt           *time.Time
	Reason          *string
	Reminded        *int
	ActorRef        uuid.UUID
	EventTime       time.Time
	RecordedAt      time.Time
}

const eventCols = `id, campaign_id, seq, action, version, forms_period_ref, period_key, template_version, due_at, reason, reminded,
  actor_ref, event_time, recorded_at`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.CampaignID, &e.Seq, &e.Action, &e.Version, &e.FormsPeriodRef, &e.PeriodKey, &e.TemplateVersion, &e.DueAt,
		&e.Reason, &e.Reminded, &e.ActorRef, &e.EventTime, &e.RecordedAt)
	return e, err
}

// InsertEvent ghi sự kiện (trigger kiểm máy trạng thái — cnts_campaign_event_state).
func InsertEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *Event, idem string) error {
	var due *time.Time
	if e.DueAt != nil {
		u := e.DueAt.UTC()
		due = &u
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_campaign_event (id, tenant_id, stream_key, campaign_id, action, version,
	    forms_period_ref, period_key, template_version, due_at, reason, reminded, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'cnts_campaign_event:' || $3::uuid::text, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	  RETURNING seq, recorded_at`,
		e.ID, tid, e.CampaignID, e.Action, e.Version, e.FormsPeriodRef, e.PeriodKey, e.TemplateVersion, due, e.Reason, e.Reminded,
		e.EventTime.UTC(), e.ActorRef, idem, domain.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// EventByIdem đọc sự kiện đã ghi với khóa idempotent (nil nếu chưa có).
func EventByIdem(ctx context.Context, tx pgx.Tx, idem string) (*Event, error) {
	e, err := scanEvent(tx.QueryRow(ctx, `SELECT `+eventCols+` FROM pf_extension.cnts_campaign_event WHERE idem_key = $1 LIMIT 1`, idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// Events đọc sự kiện của các đợt, theo thứ tự ghi.
func Events(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID][]Event, error) {
	out := map[uuid.UUID][]Event{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT `+eventCols+` FROM pf_extension.cnts_campaign_event WHERE campaign_id = ANY($1::uuid[]) ORDER BY campaign_id, seq`, strIDs(ids))
	if err != nil {
		return nil, err
	}
	es, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) { return scanEvent(r) })
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		out[e.CampaignID] = append(out[e.CampaignID], e)
	}
	return out, nil
}

// Lock giữ khóa tư vấn của đợt trong giao dịch (cùng khóa của hai trigger) — tuần tự hóa thao tác ghi đồng thời.
func Lock(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('cnts_campaign:' || $1::text || ':' || $2::text, 0))`, tid.String(), id.String())
	return err
}

// Claim ghi ý định thao tác của đợt (BF-CNTS-CAMPAIGN-POOL, #156; pf 20261028081435) hạn lease tính bằng giờ DB; ý định khác
// còn hạn → false. Gọi trong giao dịch đã giữ Lock.
func Claim(ctx context.Context, tx pgx.Tx, tid, id, token uuid.UUID, action string, lease time.Duration) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `INSERT INTO pf_extension.cnts_campaign_intent (tenant_id, campaign_id, claim_id, action, claim_until, updated_at)
	  VALUES ($1, $2, $3, $4, now() + $5::interval, now())
	  ON CONFLICT (tenant_id, campaign_id) DO UPDATE SET claim_id = EXCLUDED.claim_id, action = EXCLUDED.action,
	    claim_until = EXCLUDED.claim_until, updated_at = now()
	  WHERE cnts_campaign_intent.claim_until IS NULL OR cnts_campaign_intent.claim_until <= now()
	  RETURNING true`, tid, id, token, action, strconv.FormatInt(int64(lease/time.Second), 10)+" seconds").Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return ok, err
}

// Release bỏ ý định của token. live → chỉ khi ý định CÒN HẠN (giờ DB). released=false: token không còn giữ ý định (hết hạn /
// người khác đã nhận) — bên gọi quyết định đó có phải lỗi (ghi kết quả: có; bỏ khi lỗi: không, ý định đã tự hết).
func Release(ctx context.Context, tx pgx.Tx, tid, id, token uuid.UUID, live bool) (released bool, err error) {
	q := `UPDATE pf_extension.cnts_campaign_intent SET claim_id = NULL, action = NULL, claim_until = NULL, updated_at = now()
	  WHERE tenant_id = $1 AND campaign_id = $2 AND claim_id = $3`
	if live {
		q += ` AND claim_until > now()`
	}
	tag, err := tx.Exec(ctx, q, tid, id, token)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Renew gia hạn ý định CÒN HẠN của token thêm lease (giờ DB). false: token không còn giữ ý định.
func Renew(ctx context.Context, tx pgx.Tx, tid, id, token uuid.UUID, lease time.Duration) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_extension.cnts_campaign_intent SET claim_until = now() + $4::interval, updated_at = now()
	  WHERE tenant_id = $1 AND campaign_id = $2 AND claim_id = $3 AND claim_until > now()`,
		tid, id, token, strconv.FormatInt(int64(lease/time.Second), 10)+" seconds")
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// MarkStale ghi nhận kỳ native của đợt có thể lệch sổ đợt (review #156 [P2]): một lời gọi cap/forms ghi kỳ native đã trả về
// mà yêu cầu gọi nó không còn giữ ý định (hoặc không rõ kết quả). Gọi trong giao dịch đã giữ Lock, SAU khi lời gọi đã trả về.
func MarkStale(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.cnts_campaign_intent (tenant_id, campaign_id, stale_gen, updated_at)
	  VALUES ($1, $2, 1, now())
	  ON CONFLICT (tenant_id, campaign_id) DO UPDATE SET stale_gen = cnts_campaign_intent.stale_gen + 1, updated_at = now()`, tid, id)
	return err
}

// StaleGen đọc thế hệ lệch hiện tại; dirty = còn lần lệch chưa đối soát.
func StaleGen(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID) (gen int64, dirty bool, err error) {
	var synced int64
	err = tx.QueryRow(ctx, `SELECT stale_gen, synced_gen FROM pf_extension.cnts_campaign_intent WHERE tenant_id = $1 AND campaign_id = $2`,
		tid, id).Scan(&gen, &synced)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return gen, gen > synced, err
}

// MarkSynced ghi nhận kỳ native đã khớp sổ đợt tới thế hệ gen (đọc TRƯỚC khi đối soát).
func MarkSynced(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, gen int64) error {
	_, err := tx.Exec(ctx, `UPDATE pf_extension.cnts_campaign_intent SET synced_gen = GREATEST(synced_gen, $3), updated_at = now()
	  WHERE tenant_id = $1 AND campaign_id = $2`, tid, id, gen)
	return err
}

func strIDs(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
