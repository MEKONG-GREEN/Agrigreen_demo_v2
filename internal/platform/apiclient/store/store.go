// Package store: SQL của pf/apiclient (migration pf 000081). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh
// tenant đơn vị vận hành (tenant.WithTenantTx). Sổ: DB tính seq, chuỗi băm, chống trùng idem_key và
// kiểm R-56 bằng CHECK. Tham số luôn ép kiểu tường minh (PgBouncer — QueryExecModeExec).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// ---------------------------------------------------------------------------
// Khách hàng
// ---------------------------------------------------------------------------

// ClientEvent là một dòng sổ client_event.
type ClientEvent struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	ClientID       uuid.UUID
	Action         string
	ClientTenantID uuid.UUID
	DisplayName    string
	Purpose        string
	ContactRef     uuid.UUID
	AuthMethods    []string
	OAuthClientID  *string
	ProposedBy     uuid.UUID
	Reason         *string
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	Seq            int64
}

// InsertClientEvent ghi sổ khách hàng.
func InsertClientEvent(ctx context.Context, tx pgx.Tx, e *ClientEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_apiclient.client_event
	    (id, tenant_id, stream_key, client_id, action, client_tenant_id, display_name, purpose, contact_ref, auth_methods,
	     oauth_client_id, proposed_by, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::text, $6::uuid, $7::text, $8::text, $9::uuid, $10::text[],
	     $11::text, $12::uuid, $13::text, $14::timestamptz, $15::uuid, $16::text, $17::text)
	  RETURNING seq`,
		e.ID, e.TenantID, domain.ClientStream(e.ClientID.String()), e.ClientID, e.Action, e.ClientTenantID, e.DisplayName, e.Purpose,
		e.ContactRef, e.AuthMethods, e.OAuthClientID, e.ProposedBy, e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleVersion).Scan(&e.Seq)
}

// ClientIDByIdem trả client_id của dòng sổ đã ghi với idem_key (gọi lặp); Nil nếu chưa có.
func ClientIDByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT client_id FROM pf_apiclient.client_event WHERE idem_key = $1::text`+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// Client là bảng chiếu client_current.
type Client struct {
	TenantID       uuid.UUID
	ClientID       uuid.UUID
	ClientTenantID uuid.UUID
	DisplayName    string
	Purpose        string
	ContactRef     uuid.UUID
	AuthMethods    []string
	OAuthClientID  *string
	Status         string
	ProposedBy     uuid.UUID
	ProposedAt     time.Time
	ApprovedBy     *uuid.UUID
	ApprovedAt     *time.Time
	StatusReason   *string
	LastEventID    uuid.UUID
	LastSeq        int64
	UpdatedAt      time.Time
}

const clientCols = `tenant_id, client_id, client_tenant_id, display_name, purpose, contact_ref, auth_methods, oauth_client_id, status,
  proposed_by, proposed_at, approved_by, approved_at, status_reason, last_event_id, last_seq, updated_at`

func scanClient(row pgx.Row) (Client, error) {
	var c Client
	err := row.Scan(&c.TenantID, &c.ClientID, &c.ClientTenantID, &c.DisplayName, &c.Purpose, &c.ContactRef, &c.AuthMethods, &c.OAuthClientID,
		&c.Status, &c.ProposedBy, &c.ProposedAt, &c.ApprovedBy, &c.ApprovedAt, &c.StatusReason, &c.LastEventID, &c.LastSeq, &c.UpdatedAt)
	return c, err
}

// ClientByID đọc (và khóa nếu lock) một khách hàng; nil nếu không có.
func ClientByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Client, error) {
	q := "SELECT " + clientCols + " FROM pf_apiclient.client_current WHERE client_id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	c, err := scanClient(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// PutClient ghi bảng chiếu khách hàng.
func PutClient(ctx context.Context, tx pgx.Tx, c Client) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.client_current (`+clientCols+`)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::text, $6::uuid, $7::text[], $8::text, $9::text, $10::uuid, $11::timestamptz,
	    $12::uuid, $13::timestamptz, $14::text, $15::uuid, $16::bigint, now())
	  ON CONFLICT (tenant_id, client_id) DO UPDATE SET status = EXCLUDED.status, approved_by = EXCLUDED.approved_by,
	    approved_at = EXCLUDED.approved_at, status_reason = EXCLUDED.status_reason, last_event_id = EXCLUDED.last_event_id,
	    last_seq = EXCLUDED.last_seq, updated_at = now()`,
		c.TenantID, c.ClientID, c.ClientTenantID, c.DisplayName, c.Purpose, c.ContactRef, c.AuthMethods, c.OAuthClientID, c.Status,
		c.ProposedBy, c.ProposedAt.UTC(), c.ApprovedBy, utc(c.ApprovedAt), c.StatusReason, c.LastEventID, c.LastSeq)
	return err
}

// ListClients phân trang khách hàng theo client_id (UUIDv7).
func ListClients(ctx context.Context, tx pgx.Tx, status string, after uuid.UUID, limit int) ([]Client, error) {
	rows, err := tx.Query(ctx, "SELECT "+clientCols+` FROM pf_apiclient.client_current
	    WHERE client_id > $1::uuid AND ($2::text = '' OR status = $2::text) ORDER BY client_id LIMIT $3::int`, after, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Khóa API
// ---------------------------------------------------------------------------

// KeyEvent là một dòng sổ api_key_event. KHÔNG có bí mật — chỉ prefix + HMAC.
type KeyEvent struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	KeyID         uuid.UUID
	ClientID      uuid.UUID
	Action        string
	KeyPrefix     string
	KeyHMAC       []byte // chỉ với issued
	PepperVersion int
	ExpiresAt     time.Time
	GraceUntil    *time.Time
	ReplacedBy    *uuid.UUID
	RotatedFrom   *uuid.UUID
	Reason        *string
	EventTime     time.Time
	ActorRef      uuid.UUID
	IdemKey       string
	Seq           int64
}

// InsertKeyEvent ghi sổ khóa.
func InsertKeyEvent(ctx context.Context, tx pgx.Tx, e *KeyEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_apiclient.api_key_event
	    (id, tenant_id, stream_key, key_id, client_id, action, key_prefix, key_hmac, pepper_version, expires_at, grace_until,
	     replaced_by, rotated_from, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text, $7::text, $8::bytea, $9::int, $10::timestamptz, $11::timestamptz,
	     $12::uuid, $13::uuid, $14::text, $15::timestamptz, $16::uuid, $17::text, $18::text)
	  RETURNING seq`,
		e.ID, e.TenantID, domain.ClientStream(e.ClientID.String()), e.KeyID, e.ClientID, e.Action, e.KeyPrefix, e.KeyHMAC, e.PepperVersion,
		e.ExpiresAt.UTC(), utc(e.GraceUntil), e.ReplacedBy, e.RotatedFrom, e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey,
		domain.RuleVersion).Scan(&e.Seq)
}

// KeyIDByIdem trả key_id của dòng sổ khóa đã ghi với idem_key; Nil nếu chưa có.
func KeyIDByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT key_id FROM pf_apiclient.api_key_event WHERE idem_key = $1::text`+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// Key là bảng chiếu api_key_current.
type Key struct {
	TenantID      uuid.UUID
	KeyID         uuid.UUID
	ClientID      uuid.UUID
	KeyPrefix     string
	KeyHMAC       []byte
	PepperVersion int
	Status        string
	IssuedAt      time.Time
	IssuedBy      uuid.UUID
	ExpiresAt     time.Time
	GraceUntil    *time.Time
	RevokedAt     *time.Time
	RevokeReason  *string
	LastEventID   uuid.UUID
	UpdatedAt     time.Time
}

const keyCols = `tenant_id, key_id, client_id, key_prefix, key_hmac, pepper_version, status, issued_at, issued_by, expires_at, grace_until,
  revoked_at, revoke_reason, last_event_id, updated_at`

func scanKey(row pgx.Row) (Key, error) {
	var k Key
	err := row.Scan(&k.TenantID, &k.KeyID, &k.ClientID, &k.KeyPrefix, &k.KeyHMAC, &k.PepperVersion, &k.Status, &k.IssuedAt, &k.IssuedBy,
		&k.ExpiresAt, &k.GraceUntil, &k.RevokedAt, &k.RevokeReason, &k.LastEventID, &k.UpdatedAt)
	return k, err
}

func oneKey(ctx context.Context, tx pgx.Tx, q string, arg any) (*Key, error) {
	k, err := scanKey(tx.QueryRow(ctx, q, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// KeyByPrefix đọc khóa theo prefix (xác thực); nil nếu không có.
func KeyByPrefix(ctx context.Context, tx pgx.Tx, prefix string) (*Key, error) {
	return oneKey(ctx, tx, "SELECT "+keyCols+" FROM pf_apiclient.api_key_current WHERE key_prefix = $1::text", prefix)
}

// KeyByID đọc và khóa một khóa; nil nếu không có.
func KeyByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Key, error) {
	return oneKey(ctx, tx, "SELECT "+keyCols+" FROM pf_apiclient.api_key_current WHERE key_id = $1::uuid FOR UPDATE", id)
}

// KeysOf trả mọi khóa của một khách hàng (mới nhất trước).
func KeysOf(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) ([]Key, error) {
	rows, err := tx.Query(ctx, "SELECT "+keyCols+" FROM pf_apiclient.api_key_current WHERE client_id = $1::uuid ORDER BY key_id DESC", clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Key{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// PutKey ghi bảng chiếu khóa.
func PutKey(ctx context.Context, tx pgx.Tx, k Key) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.api_key_current (`+keyCols+`)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::bytea, $6::int, $7::text, $8::timestamptz, $9::uuid, $10::timestamptz,
	    $11::timestamptz, $12::timestamptz, $13::text, $14::uuid, now())
	  ON CONFLICT (tenant_id, key_id) DO UPDATE SET status = EXCLUDED.status, grace_until = EXCLUDED.grace_until,
	    revoked_at = EXCLUDED.revoked_at, revoke_reason = EXCLUDED.revoke_reason, last_event_id = EXCLUDED.last_event_id, updated_at = now()`,
		k.TenantID, k.KeyID, k.ClientID, k.KeyPrefix, k.KeyHMAC, k.PepperVersion, k.Status, k.IssuedAt.UTC(), k.IssuedBy, k.ExpiresAt.UTC(),
		utc(k.GraceUntil), utc(k.RevokedAt), k.RevokeReason, k.LastEventID)
	return err
}

// ---------------------------------------------------------------------------
// Đăng ký sản phẩm
// ---------------------------------------------------------------------------

// SubscriptionEvent là một dòng sổ subscription_event.
type SubscriptionEvent struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	SubscriptionID uuid.UUID
	ClientID       uuid.UUID
	Action         string
	Product        string
	Scopes         []string
	ProposedBy     uuid.UUID
	Reason         *string
	EventTime      time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	Seq            int64
}

// InsertSubscriptionEvent ghi sổ đăng ký.
func InsertSubscriptionEvent(ctx context.Context, tx pgx.Tx, e *SubscriptionEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_apiclient.subscription_event
	    (id, tenant_id, stream_key, subscription_id, client_id, action, product, scopes, proposed_by, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::uuid, $6::text, $7::text, $8::text[], $9::uuid, $10::text, $11::timestamptz,
	     $12::uuid, $13::text, $14::text)
	  RETURNING seq`,
		e.ID, e.TenantID, domain.ClientStream(e.ClientID.String()), e.SubscriptionID, e.ClientID, e.Action, e.Product, e.Scopes, e.ProposedBy,
		e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RuleVersion).Scan(&e.Seq)
}

// SubscriptionIDByIdem trả subscription_id đã ghi với idem_key; Nil nếu chưa có.
func SubscriptionIDByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT subscription_id FROM pf_apiclient.subscription_event WHERE idem_key = $1::text`+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

// Subscription là bảng chiếu subscription_current.
type Subscription struct {
	TenantID       uuid.UUID
	SubscriptionID uuid.UUID
	ClientID       uuid.UUID
	Product        string
	Scopes         []string
	Status         string
	ProposedBy     uuid.UUID
	ProposedAt     time.Time
	ApprovedBy     *uuid.UUID
	ApprovedAt     *time.Time
	RevokedAt      *time.Time
	RevokeReason   *string
	LastEventID    uuid.UUID
	UpdatedAt      time.Time
}

const subCols = `tenant_id, subscription_id, client_id, product, scopes, status, proposed_by, proposed_at, approved_by, approved_at,
  revoked_at, revoke_reason, last_event_id, updated_at`

func scanSub(row pgx.Row) (Subscription, error) {
	var s Subscription
	err := row.Scan(&s.TenantID, &s.SubscriptionID, &s.ClientID, &s.Product, &s.Scopes, &s.Status, &s.ProposedBy, &s.ProposedAt,
		&s.ApprovedBy, &s.ApprovedAt, &s.RevokedAt, &s.RevokeReason, &s.LastEventID, &s.UpdatedAt)
	return s, err
}

// SubscriptionByID đọc và khóa một đăng ký; nil nếu không có.
func SubscriptionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Subscription, error) {
	s, err := scanSub(tx.QueryRow(ctx, "SELECT "+subCols+" FROM pf_apiclient.subscription_current WHERE subscription_id = $1::uuid FOR UPDATE", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SubscriptionsOf trả mọi đăng ký của một khách hàng.
func SubscriptionsOf(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) ([]Subscription, error) {
	rows, err := tx.Query(ctx, "SELECT "+subCols+" FROM pf_apiclient.subscription_current WHERE client_id = $1::uuid ORDER BY subscription_id", clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		s, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ActiveScopes trả scope của mọi đăng ký active của khách hàng (sắp, không trùng).
func ActiveScopes(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) ([]string, error) {
	var out []string
	err := tx.QueryRow(ctx, `SELECT coalesce(array_agg(DISTINCT s ORDER BY s), '{}'::text[])
	    FROM pf_apiclient.subscription_current, unnest(scopes) s WHERE client_id = $1::uuid AND status = 'active'`, clientID).Scan(&out)
	return out, err
}

// PutSubscription ghi bảng chiếu đăng ký.
func PutSubscription(ctx context.Context, tx pgx.Tx, s Subscription) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.subscription_current (`+subCols+`)
	  VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::text[], $6::text, $7::uuid, $8::timestamptz, $9::uuid, $10::timestamptz,
	    $11::timestamptz, $12::text, $13::uuid, now())
	  ON CONFLICT (tenant_id, subscription_id) DO UPDATE SET status = EXCLUDED.status, approved_by = EXCLUDED.approved_by,
	    approved_at = EXCLUDED.approved_at, revoked_at = EXCLUDED.revoked_at, revoke_reason = EXCLUDED.revoke_reason,
	    last_event_id = EXCLUDED.last_event_id, updated_at = now()`,
		s.TenantID, s.SubscriptionID, s.ClientID, s.Product, s.Scopes, s.Status, s.ProposedBy, s.ProposedAt.UTC(), s.ApprovedBy,
		utc(s.ApprovedAt), utc(s.RevokedAt), s.RevokeReason, s.LastEventID)
	return err
}

// ---------------------------------------------------------------------------
// Gói hạn mức
// ---------------------------------------------------------------------------

// PlanRow là một phiên bản gói hạn mức (sổ quota_plan + bảng chiếu quota_plan_current).
type PlanRow struct {
	TenantID          uuid.UUID
	ClientID          uuid.UUID
	Version           int
	RequestsPerMinute int64
	DailyUnits        int64
	MonthlyUnits      int64
	MaxExportRows     int64
	EffectiveFrom     time.Time
	Status            string
	ProposedBy        uuid.UUID
	ProposedAt        time.Time
	ApprovedBy        *uuid.UUID
	ApprovedAt        *time.Time
}

// InsertPlanEvent ghi sổ gói hạn mức (action proposed | approved).
func InsertPlanEvent(ctx context.Context, tx pgx.Tx, id uuid.UUID, p PlanRow, action string, actor uuid.UUID, at time.Time, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.quota_plan
	    (id, tenant_id, stream_key, client_id, version, action, requests_per_minute, daily_units, monthly_units, max_export_rows,
	     effective_from, proposed_by, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1::uuid, $2::uuid, $3::text, $4::uuid, $5::int, $6::text, $7::bigint, $8::bigint, $9::bigint, $10::bigint,
	     $11::timestamptz, $12::uuid, $13::timestamptz, $14::uuid, $15::text, $16::text)`,
		id, p.TenantID, domain.PlanStream(p.ClientID.String()), p.ClientID, p.Version, action, p.RequestsPerMinute, p.DailyUnits,
		p.MonthlyUnits, p.MaxExportRows, p.EffectiveFrom.UTC(), p.ProposedBy, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// PlanVersionByIdem trả phiên bản đã ghi với idem_key; 0 nếu chưa có.
func PlanVersionByIdem(ctx context.Context, tx pgx.Tx, key string) (int, error) {
	var v int
	err := tx.QueryRow(ctx, `SELECT version FROM pf_apiclient.quota_plan WHERE idem_key = $1::text`+ledger.NewestByIdem(""), key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

const planCols = `tenant_id, client_id, version, requests_per_minute, daily_units, monthly_units, max_export_rows, effective_from, status,
  proposed_by, proposed_at, approved_by, approved_at`

func scanPlan(row pgx.Row) (PlanRow, error) {
	var p PlanRow
	err := row.Scan(&p.TenantID, &p.ClientID, &p.Version, &p.RequestsPerMinute, &p.DailyUnits, &p.MonthlyUnits, &p.MaxExportRows,
		&p.EffectiveFrom, &p.Status, &p.ProposedBy, &p.ProposedAt, &p.ApprovedBy, &p.ApprovedAt)
	return p, err
}

func onePlan(ctx context.Context, tx pgx.Tx, q string, args ...any) (*PlanRow, error) {
	p, err := scanPlan(tx.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// NextPlanVersion trả số phiên bản kế tiếp của khách hàng (khóa dòng khách hàng trước khi gọi).
func NextPlanVersion(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) (int, error) {
	var v int
	err := tx.QueryRow(ctx, `SELECT coalesce(max(version), 0) + 1 FROM pf_apiclient.quota_plan_current WHERE client_id = $1::uuid`, clientID).Scan(&v)
	return v, err
}

// PlanVersion đọc và khóa một phiên bản; nil nếu không có.
func PlanVersion(ctx context.Context, tx pgx.Tx, clientID uuid.UUID, version int) (*PlanRow, error) {
	return onePlan(ctx, tx, "SELECT "+planCols+` FROM pf_apiclient.quota_plan_current WHERE client_id = $1::uuid AND version = $2::int FOR UPDATE`,
		clientID, version)
}

// PlanAt trả phiên bản ĐÃ DUYỆT có hiệu lực tại at (effective_from lớn nhất ≤ at); nil nếu không có.
func PlanAt(ctx context.Context, tx pgx.Tx, clientID uuid.UUID, at time.Time) (*PlanRow, error) {
	return onePlan(ctx, tx, "SELECT "+planCols+` FROM pf_apiclient.quota_plan_current
	    WHERE client_id = $1::uuid AND status = 'approved' AND effective_from <= $2::timestamptz
	    ORDER BY effective_from DESC, version DESC LIMIT 1`, clientID, at.UTC())
}

// PlansOf trả mọi phiên bản gói của khách hàng.
func PlansOf(ctx context.Context, tx pgx.Tx, clientID uuid.UUID) ([]PlanRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+planCols+" FROM pf_apiclient.quota_plan_current WHERE client_id = $1::uuid ORDER BY version", clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlanRow{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PutPlan ghi bảng chiếu phiên bản gói.
func PutPlan(ctx context.Context, tx pgx.Tx, p PlanRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_apiclient.quota_plan_current (`+planCols+`)
	  VALUES ($1::uuid, $2::uuid, $3::int, $4::bigint, $5::bigint, $6::bigint, $7::bigint, $8::timestamptz, $9::text, $10::uuid,
	    $11::timestamptz, $12::uuid, $13::timestamptz)
	  ON CONFLICT (tenant_id, client_id, version) DO UPDATE SET status = EXCLUDED.status, approved_by = EXCLUDED.approved_by,
	    approved_at = EXCLUDED.approved_at, updated_at = now()`,
		p.TenantID, p.ClientID, p.Version, p.RequestsPerMinute, p.DailyUnits, p.MonthlyUnits, p.MaxExportRows, p.EffectiveFrom.UTC(),
		p.Status, p.ProposedBy, p.ProposedAt.UTC(), p.ApprovedBy, utc(p.ApprovedAt))
	return err
}

// ClientsWithDueKeys trả mã khách hàng (không trùng, tăng dần) có khóa active/rotated đã quá hạn
// (expires_at ≤ now) hoặc khóa rotated đã hết hạn chồng lấn (grace_until ≤ now) — đầu vào job
// port.JobKeyExpiry. tx ở ngữ cảnh tenant đơn vị vận hành.
func ClientsWithDueKeys(ctx context.Context, tx pgx.Tx, now time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT client_id FROM pf_apiclient.api_key_current
	   WHERE (status = 'active' AND expires_at <= $1::timestamptz)
	      OR (status = 'rotated' AND (expires_at <= $1::timestamptz OR grace_until IS NULL OR grace_until <= $1::timestamptz))
	   ORDER BY client_id`, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
