package store

// Cấp quyền chia sẻ giữa hai tenant (ADR-0020, migration pf 000042). Mọi hàm nhận pgx.Tx đã ở
// ngữ cảnh tenant đúng bảng: share_grant_event / share_grant / partner_read_log ở tenant BÊN CẤP,
// share_grant_inbound ở tenant BÊN NHẬN (app/ đổi ngữ cảnh bằng tenant.Within).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ShareRuleVersion là rule_version của sổ cấp quyền và sổ đọc.
const ShareRuleVersion = "party.share_grant.v1"

// GrantStream là stream_key của một cấp quyền (cả sổ vòng đời và sổ đọc).
func GrantStream(id uuid.UUID) string { return "grant:" + id.String() }

// GrantEvent là một dòng sổ share_grant_event.
type GrantEvent struct {
	ID         uuid.UUID
	TenantID   uuid.UUID // bên cấp
	GrantID    uuid.UUID
	Action     string // proposed | approved | revoked
	Grantee    uuid.UUID
	Kinds      []string
	Purpose    string
	ValidFrom  time.Time
	ValidTo    time.Time
	ProposedBy uuid.UUID
	Reason     *string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// InsertGrantEvent ghi sổ; DB tính seq, chuỗi băm (R-20) và kiểm R-56 bằng CHECK.
func InsertGrantEvent(ctx context.Context, tx pgx.Tx, e *GrantEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.share_grant_event
	    (id, tenant_id, stream_key, grant_id, action, grantee_tenant_id, kinds, purpose, valid_from, valid_to, proposed_by,
	     reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, GrantStream(e.GrantID), e.GrantID, e.Action, e.Grantee, e.Kinds, e.Purpose, e.ValidFrom.UTC(), e.ValidTo.UTC(),
		e.ProposedBy, e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, ShareRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// GrantEventByIdem đọc dòng sổ đã ghi với idem_key (gọi lặp).
func GrantEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*GrantEvent, error) {
	var e GrantEvent
	err := tx.QueryRow(ctx, `SELECT id, tenant_id, grant_id, action, grantee_tenant_id, kinds, purpose, valid_from, valid_to, proposed_by,
	    reason, event_time, actor_ref, idem_key, seq, recorded_at FROM pf_party.share_grant_event WHERE idem_key = $1::text`, key).
		Scan(&e.ID, &e.TenantID, &e.GrantID, &e.Action, &e.Grantee, &e.Kinds, &e.Purpose, &e.ValidFrom, &e.ValidTo, &e.ProposedBy,
			&e.Reason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// Grant là bảng chiếu hiện hành share_grant (tenant bên cấp).
type Grant struct {
	TenantID     uuid.UUID
	GrantID      uuid.UUID
	Grantee      uuid.UUID
	Kinds        []string
	Purpose      string
	ValidFrom    time.Time
	ValidTo      time.Time
	Status       string // proposed | active | revoked
	ProposedBy   uuid.UUID
	ProposedAt   time.Time
	ApprovedBy   *uuid.UUID
	ApprovedAt   *time.Time
	RevokedBy    *uuid.UUID
	RevokedAt    *time.Time
	RevokeReason *string
	LastEventID  uuid.UUID
	LastSeq      int64
	UpdatedAt    time.Time
}

const grantCols = `tenant_id, grant_id, grantee_tenant_id, kinds, purpose, valid_from, valid_to, status, proposed_by, proposed_at,
  approved_by, approved_at, revoked_by, revoked_at, revoke_reason, last_event_id, last_seq, updated_at`

func scanGrant(row pgx.Row) (Grant, error) {
	var g Grant
	err := row.Scan(&g.TenantID, &g.GrantID, &g.Grantee, &g.Kinds, &g.Purpose, &g.ValidFrom, &g.ValidTo, &g.Status, &g.ProposedBy,
		&g.ProposedAt, &g.ApprovedBy, &g.ApprovedAt, &g.RevokedBy, &g.RevokedAt, &g.RevokeReason, &g.LastEventID, &g.LastSeq, &g.UpdatedAt)
	return g, err
}

// GrantByID đọc (và khóa nếu lock) một cấp quyền của tenant hiện tại.
func GrantByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Grant, error) {
	q := "SELECT " + grantCols + " FROM pf_party.share_grant WHERE grant_id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	g, err := scanGrant(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// PutGrant ghi bảng chiếu hiện hành.
func PutGrant(ctx context.Context, tx pgx.Tx, g Grant) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.share_grant (`+grantCols+`)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, now())
	  ON CONFLICT (tenant_id, grant_id) DO UPDATE SET status = EXCLUDED.status, approved_by = EXCLUDED.approved_by,
	    approved_at = EXCLUDED.approved_at, revoked_by = EXCLUDED.revoked_by, revoked_at = EXCLUDED.revoked_at,
	    revoke_reason = EXCLUDED.revoke_reason, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq, updated_at = now()`,
		g.TenantID, g.GrantID, g.Grantee, g.Kinds, g.Purpose, g.ValidFrom.UTC(), g.ValidTo.UTC(), g.Status, g.ProposedBy, g.ProposedAt.UTC(),
		g.ApprovedBy, g.ApprovedAt, g.RevokedBy, g.RevokedAt, g.RevokeReason, g.LastEventID, g.LastSeq)
	return err
}

// ListGrants phân trang theo grant_id (UUIDv7) các cấp quyền của tenant hiện tại (bên cấp).
func ListGrants(ctx context.Context, tx pgx.Tx, status string, after uuid.UUID, limit int) ([]Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+` FROM pf_party.share_grant
	    WHERE grant_id > $1::uuid AND ($2::text = '' OR status = $2::text) ORDER BY grant_id LIMIT $3::int`, after, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Inbound là bảng chiếu share_grant_inbound (tenant bên nhận).
type Inbound struct {
	TenantID  uuid.UUID // bên nhận
	GrantID   uuid.UUID
	Grantor   uuid.UUID
	Kinds     []string
	ValidFrom time.Time
	ValidTo   time.Time
	Status    string // active | revoked
	UpdatedAt time.Time
}

// PutInbound ghi bảng chiếu bên nhận — tx PHẢI đang ở ngữ cảnh tenant bên nhận (RLS WITH CHECK).
func PutInbound(ctx context.Context, tx pgx.Tx, in Inbound) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.share_grant_inbound
	    (tenant_id, grant_id, grantor_tenant_id, kinds, valid_from, valid_to, status)
	  VALUES ($1, $2, $3, $4, $5, $6, $7)
	  ON CONFLICT (tenant_id, grant_id) DO UPDATE SET status = EXCLUDED.status, updated_at = now()`,
		in.TenantID, in.GrantID, in.Grantor, in.Kinds, in.ValidFrom.UTC(), in.ValidTo.UTC(), in.Status)
	return err
}

// ActiveInbound trả cấp quyền đang hiệu lực tại at của tenant hiện tại (bên nhận), có loại kind
// (rỗng = mọi loại), của bên cấp grantor (Nil = mọi bên cấp).
func ActiveInbound(ctx context.Context, tx pgx.Tx, kind string, grantor uuid.UUID, at time.Time) ([]Inbound, error) {
	rows, err := tx.Query(ctx, `SELECT tenant_id, grant_id, grantor_tenant_id, kinds, valid_from, valid_to, status, updated_at
	    FROM pf_party.share_grant_inbound
	   WHERE status = 'active' AND valid_from <= $1::timestamptz AND valid_to > $1::timestamptz
	     AND ($2::text = '' OR $2::text = ANY (kinds)) AND ($3::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR grantor_tenant_id = $3::uuid)
	   ORDER BY grantor_tenant_id, grant_id`, at.UTC(), kind, grantor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Inbound{}
	for rows.Next() {
		var in Inbound
		if err := rows.Scan(&in.TenantID, &in.GrantID, &in.Grantor, &in.Kinds, &in.ValidFrom, &in.ValidTo, &in.Status, &in.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// ReadLog là một dòng sổ partner_read_log (tenant bên cấp).
type ReadLog struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	GrantID    uuid.UUID
	Grantee    uuid.UUID
	Kind       string
	Endpoint   string
	Query      *string
	Items      int
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// InsertReadLog ghi sổ đọc của bên nhận.
func InsertReadLog(ctx context.Context, tx pgx.Tx, r *ReadLog) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.partner_read_log
	    (id, tenant_id, stream_key, grant_id, grantee_tenant_id, kind, endpoint, query, item_count, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	  RETURNING seq, recorded_at`,
		r.ID, r.TenantID, GrantStream(r.GrantID), r.GrantID, r.Grantee, r.Kind, r.Endpoint, r.Query, r.Items, r.EventTime.UTC(), r.ActorRef,
		r.IdemKey, ShareRuleVersion).Scan(&r.Seq, &r.RecordedAt)
}

// ReadLogs trả sổ đọc của một cấp quyền theo seq > after.
func ReadLogs(ctx context.Context, tx pgx.Tx, grantID uuid.UUID, after int64, limit int) ([]ReadLog, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, grant_id, grantee_tenant_id, kind, endpoint, query, item_count, event_time, actor_ref,
	    idem_key, seq, recorded_at FROM pf_party.partner_read_log WHERE grant_id = $1::uuid AND seq > $2::bigint ORDER BY seq LIMIT $3::int`, grantID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReadLog{}
	for rows.Next() {
		var r ReadLog
		if err := rows.Scan(&r.ID, &r.TenantID, &r.GrantID, &r.Grantee, &r.Kind, &r.Endpoint, &r.Query, &r.Items, &r.EventTime, &r.ActorRef,
			&r.IdemKey, &r.Seq, &r.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CooperativeEntry là một dòng danh bạ HTX cho bên nhận (không người đại diện, không mã số thuế).
type CooperativeEntry struct {
	Ref         uuid.UUID
	Code        string
	Name        string
	CommuneCode *string
	Active      bool
	Lon, Lat    *string // độ, 7 chữ số thập phân; nil = hồ sơ chưa có toạ độ
	UpdatedAt   time.Time
}

const coopEntrySQL = `SELECT o.ref, o.code, o.name, o.admin_unit_code, o.active, round(ST_X(c.location)::numeric, 7)::text, round(ST_Y(c.location)::numeric, 7)::text, o.updated_at
  FROM pf_party.org o LEFT JOIN pf_party.cooperative c ON c.tenant_id = o.tenant_id AND c.org_ref = o.ref
 WHERE o.kind = 'cooperative'`

func scanCoopEntry(row pgx.Row) (CooperativeEntry, error) {
	var e CooperativeEntry
	err := row.Scan(&e.Ref, &e.Code, &e.Name, &e.CommuneCode, &e.Active, &e.Lon, &e.Lat, &e.UpdatedAt)
	return e, err
}

// CooperativeEntries phân trang danh bạ HTX của tenant hiện tại (bên cấp) theo ref.
func CooperativeEntries(ctx context.Context, tx pgx.Tx, after uuid.UUID, limit int) ([]CooperativeEntry, error) {
	rows, err := tx.Query(ctx, coopEntrySQL+" AND o.ref > $1::uuid ORDER BY o.ref LIMIT $2::int", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CooperativeEntry{}
	for rows.Next() {
		e, err := scanCoopEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CooperativeEntryByRef đọc một HTX; nil nếu không có.
func CooperativeEntryByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*CooperativeEntry, error) {
	e, err := scanCoopEntry(tx.QueryRow(ctx, coopEntrySQL+" AND o.ref = $1::uuid", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}
