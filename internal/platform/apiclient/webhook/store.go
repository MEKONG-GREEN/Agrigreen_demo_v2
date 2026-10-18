package webhook

// SQL của webhook (pf 000130). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant đơn vị vận hành (RLS).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "w57.webhook.v1"

// event là một dòng sổ webhook_event.
type event struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	WebhookID      uuid.UUID
	ClientID       uuid.UUID
	Action         string
	URL            *string
	Feeds          []string
	Threshold      *int
	SecretVersion  *int
	KeyVersion     *int
	Nonce          *string
	PrevValidUntil *time.Time
	Reason         *string
	At             time.Time
	Actor          uuid.UUID
	IdemKey        string
	Seq            int64
}

func insertEvent(ctx context.Context, tx pgx.Tx, e *event) error {
	var feeds any
	if e.Feeds != nil {
		feeds = e.Feeds
	}
	err := tx.QueryRow(ctx, `INSERT INTO pf_apiclient.webhook_event (id, tenant_id, stream_key, webhook_id, client_id, action, url, feeds,
	    failure_threshold, secret_version, key_version, secret_nonce, prev_valid_until, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text, $7::text, $8::text[], $9::int, $10::int, $11::int, $12::text,
	    $13::timestamptz, $14::text, $15::timestamptz, $16::uuid, $17::text, $18::text)
	  RETURNING seq`,
		e.ID, e.TenantID, "webhook:"+e.WebhookID.String(), e.WebhookID, e.ClientID, e.Action, e.URL, feeds, e.Threshold, e.SecretVersion,
		e.KeyVersion, e.Nonce, utcp(e.PrevValidUntil), e.Reason, e.At.UTC(), e.Actor, e.IdemKey, RuleVersion).Scan(&e.Seq)
	return ledger.Classify(err)
}

// webhookIDByIdem trả webhook_id của dòng sổ đã ghi với idem_key (gọi lặp); Nil nếu chưa có.
func webhookIDByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT webhook_id FROM pf_apiclient.webhook_event WHERE idem_key = $1::text`+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

func utcp(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// Registration là bảng chiếu webhook_current (+ ngắt mạch).
type Registration struct {
	TenantID       uuid.UUID
	ID             uuid.UUID
	ClientID       uuid.UUID
	URL            string
	Host           string
	Feeds          []string
	Status         string
	Threshold      int
	SecretVersion  int
	KeyVersion     int
	Nonce          string
	PrevVersion    *int
	PrevKeyVersion *int
	PrevNonce      *string
	PrevValidUntil *time.Time
	StatusReason   *string
	CreatedBy      uuid.UUID
	CreatedAt      time.Time
	LastEventID    uuid.UUID
	LastSeq        int64
	Breaker        Breaker
}

const regCols = `w.tenant_id, w.webhook_id, w.client_id, w.url, w.host, w.feeds, w.status, w.failure_threshold, w.secret_version,
  w.key_version, w.secret_nonce, w.prev_secret_version, w.prev_key_version, w.prev_secret_nonce, w.prev_valid_until, w.status_reason,
  w.created_by, w.created_at, w.last_event_id, w.last_seq,
  b.state, b.consecutive_failures, b.opened_count, b.opened_at, b.next_probe_at, b.last_success_at, b.last_failure_at, coalesce(b.last_error, '')`

const regFrom = ` FROM pf_apiclient.webhook_current w JOIN pf_apiclient.webhook_breaker b ON b.tenant_id = w.tenant_id AND b.webhook_id = w.webhook_id`

func scanReg(row pgx.Row) (Registration, error) {
	var r Registration
	b := &r.Breaker
	err := row.Scan(&r.TenantID, &r.ID, &r.ClientID, &r.URL, &r.Host, &r.Feeds, &r.Status, &r.Threshold, &r.SecretVersion, &r.KeyVersion,
		&r.Nonce, &r.PrevVersion, &r.PrevKeyVersion, &r.PrevNonce, &r.PrevValidUntil, &r.StatusReason, &r.CreatedBy, &r.CreatedAt,
		&r.LastEventID, &r.LastSeq, &b.State, &b.Failures, &b.OpenedCount, &b.OpenedAt, &b.NextProbeAt, &b.LastSuccess, &b.LastFailure,
		&b.LastError)
	return r, err
}

// regByID đọc (và khóa nếu lock) một webhook của khách hàng; nil nếu không có.
func regByID(ctx context.Context, tx pgx.Tx, clientID, id uuid.UUID, lock bool) (*Registration, error) {
	q := "SELECT " + regCols + regFrom + " WHERE w.webhook_id = $1::uuid AND w.client_id = $2::uuid"
	if lock {
		q += " FOR UPDATE OF w, b"
	}
	r, err := scanReg(tx.QueryRow(ctx, q, id, clientID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// regsOf liệt kê webhook của khách hàng (mới nhất trước).
func regsOf(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) ([]Registration, error) {
	rows, err := tx.Query(ctx, "SELECT "+regCols+regFrom+" WHERE w.client_id = $1::uuid ORDER BY w.webhook_id DESC", clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Registration{}
	for rows.Next() {
		r, err := scanReg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func countActive(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_apiclient.webhook_current WHERE client_id = $1::uuid AND status = 'active'`, clientID).Scan(&n)
	return n, err
}

// putReg ghi bảng chiếu (chèn hoặc cập nhật).
func putReg(ctx context.Context, tx pgx.Tx, r Registration) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.webhook_current (tenant_id, webhook_id, client_id, url, host, feeds, status,
	    failure_threshold, secret_version, key_version, secret_nonce, prev_secret_version, prev_key_version, prev_secret_nonce,
	    prev_valid_until, status_reason, created_by, created_at, last_event_id, last_seq)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::text, $6::text[], $7::text, $8::int, $9::int, $10::int, $11::text, $12::int,
	    $13::int, $14::text, $15::timestamptz, $16::text, $17::uuid, $18::timestamptz, $19::uuid, $20::bigint)
	  ON CONFLICT (tenant_id, webhook_id) DO UPDATE SET status = EXCLUDED.status, secret_version = EXCLUDED.secret_version,
	    key_version = EXCLUDED.key_version, secret_nonce = EXCLUDED.secret_nonce, prev_secret_version = EXCLUDED.prev_secret_version,
	    prev_key_version = EXCLUDED.prev_key_version, prev_secret_nonce = EXCLUDED.prev_secret_nonce,
	    prev_valid_until = EXCLUDED.prev_valid_until, status_reason = EXCLUDED.status_reason, last_event_id = EXCLUDED.last_event_id,
	    last_seq = EXCLUDED.last_seq, updated_at = now()`,
		r.TenantID, r.ID, r.ClientID, r.URL, r.Host, r.Feeds, r.Status, r.Threshold, r.SecretVersion, r.KeyVersion, r.Nonce, r.PrevVersion,
		r.PrevKeyVersion, r.PrevNonce, utcp(r.PrevValidUntil), r.StatusReason, r.CreatedBy, r.CreatedAt.UTC(), r.LastEventID, r.LastSeq)
	return err
}

// putBreaker ghi trạng thái ngắt mạch.
func putBreaker(ctx context.Context, tx pgx.Tx, tid, webhookID uuid.UUID, b Breaker) error {
	var last *string
	if b.LastError != "" {
		last = &b.LastError
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.webhook_breaker (tenant_id, webhook_id, state, consecutive_failures, opened_count,
	    opened_at, next_probe_at, last_success_at, last_failure_at, last_error)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::int, $5::int, $6::timestamptz, $7::timestamptz, $8::timestamptz, $9::timestamptz, $10::text)
	  ON CONFLICT (tenant_id, webhook_id) DO UPDATE SET state = EXCLUDED.state, consecutive_failures = EXCLUDED.consecutive_failures,
	    opened_count = EXCLUDED.opened_count, opened_at = EXCLUDED.opened_at, next_probe_at = EXCLUDED.next_probe_at,
	    last_success_at = EXCLUDED.last_success_at, last_failure_at = EXCLUDED.last_failure_at, last_error = EXCLUDED.last_error,
	    updated_at = now()`,
		tid, webhookID, b.State, b.Failures, b.OpenedCount, utcp(b.OpenedAt), utcp(b.NextProbeAt), utcp(b.LastSuccess), utcp(b.LastFailure), last)
	return err
}

// Cursor là trạng thái chỉ báo của một webhook × feed.
type Cursor struct {
	Feed          string
	NotifiedSeq   int64
	Attempts      int
	NextAttemptAt *time.Time
	LastAttemptAt *time.Time
}

func cursorsOf(ctx context.Context, tx pgx.Tx, webhookID uuid.UUID) ([]Cursor, error) {
	rows, err := tx.Query(ctx, `SELECT feed, notified_seq, attempts, next_attempt_at, last_attempt_at FROM pf_apiclient.webhook_cursor
	  WHERE webhook_id = $1::uuid ORDER BY feed`, webhookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Cursor{}
	for rows.Next() {
		var c Cursor
		if err := rows.Scan(&c.Feed, &c.NotifiedSeq, &c.Attempts, &c.NextAttemptAt, &c.LastAttemptAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func putCursor(ctx context.Context, tx pgx.Tx, tid, webhookID uuid.UUID, c Cursor) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.webhook_cursor (tenant_id, webhook_id, feed, notified_seq, attempts, next_attempt_at,
	    last_attempt_at)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::bigint, $5::int, $6::timestamptz, $7::timestamptz)
	  ON CONFLICT (tenant_id, webhook_id, feed) DO UPDATE SET notified_seq = GREATEST(pf_apiclient.webhook_cursor.notified_seq,
	    EXCLUDED.notified_seq), attempts = EXCLUDED.attempts, next_attempt_at = EXCLUDED.next_attempt_at,
	    last_attempt_at = EXCLUDED.last_attempt_at, updated_at = now()`,
		tid, webhookID, c.Feed, c.NotifiedSeq, c.Attempts, utcp(c.NextAttemptAt), utcp(c.LastAttemptAt))
	return err
}

// pending là một chỉ báo cần giao: feed có seq mới hơn seq đã báo.
type pending struct {
	WebhookID     uuid.UUID
	Feed          string
	HeadSeq       int64
	HeadAt        *time.Time
	NotifiedSeq   int64
	Attempts      int
	NextAttemptAt *time.Time
}

// pendingAll liệt kê chỉ báo chờ của mọi webhook active của khách hàng active (feed chung, partition ”), theo
// webhook rồi feed.
func pendingAll(ctx context.Context, tx pgx.Tx) ([]pending, error) {
	rows, err := tx.Query(ctx, `SELECT w.webhook_id, f.feed, h.last_seq, h.last_recorded_at, coalesce(c.notified_seq, 0),
	    coalesce(c.attempts, 0), c.next_attempt_at
	  FROM pf_apiclient.webhook_current w
	  JOIN pf_apiclient.client_current cl ON cl.tenant_id = w.tenant_id AND cl.client_id = w.client_id AND cl.status = 'active'
	  CROSS JOIN LATERAL unnest(w.feeds) AS f(feed)
	  JOIN pf_apiclient.feed_head h ON h.tenant_id = w.tenant_id AND h.feed = f.feed AND h.partition_key = ''
	  LEFT JOIN pf_apiclient.webhook_cursor c ON c.tenant_id = w.tenant_id AND c.webhook_id = w.webhook_id AND c.feed = f.feed
	  WHERE w.status = 'active' AND h.last_seq > coalesce(c.notified_seq, 0)
	  ORDER BY w.webhook_id, f.feed`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.WebhookID, &p.Feed, &p.HeadSeq, &p.HeadAt, &p.NotifiedSeq, &p.Attempts, &p.NextAttemptAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// regByIDAny đọc một webhook theo mã (job giao — không lọc khách hàng).
func regByIDAny(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Registration, error) {
	q := "SELECT " + regCols + regFrom + " WHERE w.webhook_id = $1::uuid"
	if lock {
		q += " FOR UPDATE OF w, b"
	}
	r, err := scanReg(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// delivery là một dòng sổ webhook_delivery.
type delivery struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	WebhookID    uuid.UUID
	ClientID     uuid.UUID
	Feed         string
	CursorSeq    int64
	Attempt      int
	Probe        bool
	Outcome      string
	HTTPStatus   *int
	ErrorClass   string
	LatencyMS    int
	BreakerState string
	At           time.Time
}

func insertDelivery(ctx context.Context, tx pgx.Tx, d delivery) error {
	var class *string
	if d.ErrorClass != "" {
		class = &d.ErrorClass
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.webhook_delivery (id, tenant_id, stream_key, webhook_id, client_id, feed, cursor_seq,
	    attempt, probe, outcome, http_status, error_class, latency_ms, breaker_state, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text, $7::bigint, $8::int, $9::boolean, $10::text, $11::int, $12::text,
	    $13::int, $14::text, $15::timestamptz, $5::uuid, $16::text, $17::text)`,
		d.ID, d.TenantID, "webhook:"+d.WebhookID.String(), d.WebhookID, d.ClientID, d.Feed, d.CursorSeq, d.Attempt, d.Probe, d.Outcome,
		d.HTTPStatus, class, d.LatencyMS, d.BreakerState, d.At.UTC(), "delivery:"+d.ID.String(), RuleVersion)
	return ledger.Classify(err)
}

// Delivery là một lần giao (đọc lại cho quản trị và kiểm thử).
type Delivery struct {
	ID           uuid.UUID
	Feed         string
	CursorSeq    int64
	Attempt      int
	Probe        bool
	Outcome      string
	HTTPStatus   *int
	ErrorClass   string
	BreakerState string
	At           time.Time
}

func deliveriesOf(ctx context.Context, tx pgx.Tx, webhookID uuid.UUID, limit int) ([]Delivery, error) {
	rows, err := tx.Query(ctx, `SELECT id, feed, cursor_seq, attempt, probe, outcome, http_status, coalesce(error_class, ''), breaker_state,
	    event_time FROM pf_apiclient.webhook_delivery WHERE webhook_id = $1::uuid ORDER BY event_time DESC, seq DESC LIMIT $2::int`,
		webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		if err := rows.Scan(&d.ID, &d.Feed, &d.CursorSeq, &d.Attempt, &d.Probe, &d.Outcome, &d.HTTPStatus, &d.ErrorClass, &d.BreakerState,
			&d.At); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
