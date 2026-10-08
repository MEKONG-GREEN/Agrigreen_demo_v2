package store

// Cấp quyền chia sẻ giữa hai tenant (ADR-0020, migration pf 000042). Mọi hàm nhận pgx.Tx đã ở
// ngữ cảnh tenant đúng bảng: share_grant_event / share_grant / partner_read_log ở tenant BÊN CẤP,
// share_grant_inbound ở tenant BÊN NHẬN (app/ đổi ngữ cảnh bằng tenant.Within).

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
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
	// Điều khoản v2 (W4.1, pf 000120; nil = cấp phép trước v2 — partner kế thừa).
	Terms *Terms
	// Lần rà soát định kỳ cuối (R-SEC-04); nil = chưa rà soát.
	LastReviewedAt *time.Time
	LastReviewedBy *uuid.UUID
}

// Class trả lớp cấp phép: theo điều khoản v2, hoặc partner với cấp phép trước v2.
func (g Grant) Class() string {
	if g.Terms == nil {
		return ClassPartner
	}
	return g.Terms.Class
}

// Lớp cấp phép (ADR-0030 mục 3).
const (
	ClassIntraPlatform = "intra_platform" // đọc tại chỗ qua share_scope (W4.2)
	ClassPartner       = "partner"        // MKG, bên thứ ba — đường ADR-0020 (/v1/partner/*, Public API)
)

// Terms là điều khoản v2 của một cấp phép (sổ phụ share_grant_terms, chiếu lên share_grant).
type Terms struct {
	Class        string     // intra_platform | partner
	ModuleTable  *string    // '<module>:<share>' (intra_platform)
	ScopeNodeRef *uuid.UUID // Nút phạm vi của đơn vị chủ (intra_platform)
	PurposeCode  *string
	BasisRef     *string // mã thỏa thuận chia sẻ
}

const grantCols = `tenant_id, grant_id, grantee_tenant_id, kinds, purpose, valid_from, valid_to, status, proposed_by, proposed_at,
  approved_by, approved_at, revoked_by, revoked_at, revoke_reason, last_event_id, last_seq, updated_at,
  grant_class, module_table, scope_node_ref, purpose_code, basis_ref, last_reviewed_at, last_reviewed_by`

func scanGrant(row pgx.Row) (Grant, error) {
	var g Grant
	var t Terms
	var class *string
	err := row.Scan(&g.TenantID, &g.GrantID, &g.Grantee, &g.Kinds, &g.Purpose, &g.ValidFrom, &g.ValidTo, &g.Status, &g.ProposedBy,
		&g.ProposedAt, &g.ApprovedBy, &g.ApprovedAt, &g.RevokedBy, &g.RevokedAt, &g.RevokeReason, &g.LastEventID, &g.LastSeq, &g.UpdatedAt,
		&class, &t.ModuleTable, &t.ScopeNodeRef, &t.PurposeCode, &t.BasisRef, &g.LastReviewedAt, &g.LastReviewedBy)
	if class != nil {
		t.Class = *class
		g.Terms = &t
	}
	return g, err
}

func (g Grant) termCols() (class, table *string, node *uuid.UUID, purpose, basis *string) {
	if g.Terms == nil {
		return nil, nil, nil, nil, nil
	}
	c := g.Terms.Class
	return &c, g.Terms.ModuleTable, g.Terms.ScopeNodeRef, g.Terms.PurposeCode, g.Terms.BasisRef
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

// PutGrant ghi bảng chiếu hiện hành. Điều khoản v2 chỉ ghi khi tạo dòng (không đổi sau đề xuất).
func PutGrant(ctx context.Context, tx pgx.Tx, g Grant) error {
	class, table, node, purpose, basis := g.termCols()
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.share_grant (`+grantCols+`)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, now(), $18, $19, $20, $21, $22, $23, $24)
	  ON CONFLICT (tenant_id, grant_id) DO UPDATE SET status = EXCLUDED.status, approved_by = EXCLUDED.approved_by,
	    approved_at = EXCLUDED.approved_at, revoked_by = EXCLUDED.revoked_by, revoked_at = EXCLUDED.revoked_at,
	    revoke_reason = EXCLUDED.revoke_reason, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq,
	    last_reviewed_at = EXCLUDED.last_reviewed_at, last_reviewed_by = EXCLUDED.last_reviewed_by, updated_at = now()`,
		g.TenantID, g.GrantID, g.Grantee, g.Kinds, g.Purpose, g.ValidFrom.UTC(), g.ValidTo.UTC(), g.Status, g.ProposedBy, g.ProposedAt.UTC(),
		g.ApprovedBy, g.ApprovedAt, g.RevokedBy, g.RevokedAt, g.RevokeReason, g.LastEventID, g.LastSeq,
		class, table, node, purpose, basis, g.LastReviewedAt, g.LastReviewedBy)
	return err
}

// InsertGrantTerms ghi sổ phụ điều khoản v2 (một dòng mỗi cấp phép, CHECK seq = 1 ở DB) cùng giao dịch với
// dòng 'proposed'. Không thêm cột vào share_grant_event: chuỗi băm tính trên toàn dòng (pf 000120).
func InsertGrantTerms(ctx context.Context, tx pgx.Tx, tenantID, grantID, actor uuid.UUID, t Terms, at time.Time, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.share_grant_terms
	    (id, tenant_id, stream_key, grant_id, grant_class, module_table, scope_node_ref, purpose_code, basis_ref, event_time, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		ids.New(), tenantID, "terms:"+grantID.String(), grantID, t.Class, t.ModuleTable, t.ScopeNodeRef, t.PurposeCode, t.BasisRef, at.UTC(),
		actor, idem, ShareRuleVersion)
	return err
}

// Review là một dòng sổ rà soát cấp phép định kỳ share_grant_review (R-SEC-04).
type Review struct {
	ID         uuid.UUID
	GrantID    uuid.UUID
	Decision   string // retain | revoke
	Note       *string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// InsertReview ghi sổ rà soát; DB tính seq và chuỗi băm.
func InsertReview(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, r *Review) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.share_grant_review
	    (id, tenant_id, stream_key, grant_id, decision, note, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	  RETURNING seq, recorded_at`,
		r.ID, tenantID, "review:"+r.GrantID.String(), r.GrantID, r.Decision, r.Note, r.EventTime.UTC(), r.ActorRef, r.IdemKey, ShareRuleVersion).
		Scan(&r.Seq, &r.RecordedAt)
}

const reviewCols = `id, grant_id, decision, note, event_time, actor_ref, idem_key, seq, recorded_at`

func scanReview(row pgx.Row) (Review, error) {
	var r Review
	err := row.Scan(&r.ID, &r.GrantID, &r.Decision, &r.Note, &r.EventTime, &r.ActorRef, &r.IdemKey, &r.Seq, &r.RecordedAt)
	return r, err
}

// ReviewByIdem đọc dòng rà soát đã ghi với idem_key (gọi lặp); nil nếu chưa có.
func ReviewByIdem(ctx context.Context, tx pgx.Tx, key string) (*Review, error) {
	r, err := scanReview(tx.QueryRow(ctx, "SELECT "+reviewCols+" FROM pf_party.share_grant_review WHERE idem_key = $1::text", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Reviews trả sổ rà soát của một cấp phép theo seq.
func Reviews(ctx context.Context, tx pgx.Tx, grantID uuid.UUID) ([]Review, error) {
	rows, err := tx.Query(ctx, "SELECT "+reviewCols+" FROM pf_party.share_grant_review WHERE grant_id = $1::uuid ORDER BY seq", grantID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Review, error) { return scanReview(r) })
}

// GrantsDueForReview liệt kê cấp phép đang hiệu lực của tenant hiện tại (bên cấp) mà lần rà soát cuối (hoặc lúc
// duyệt, nếu chưa rà soát) ≤ before — đến hạn rà soát định kỳ. Phân trang theo grant_id.
func GrantsDueForReview(ctx context.Context, tx pgx.Tx, before time.Time, after uuid.UUID, limit int) ([]Grant, error) {
	rows, err := tx.Query(ctx, "SELECT "+grantCols+` FROM pf_party.share_grant
	    WHERE status = 'active' AND coalesce(last_reviewed_at, approved_at) <= $1::timestamptz AND grant_id > $2::uuid
	    ORDER BY grant_id LIMIT $3::int`, before.UTC(), after, limit)
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

// BumpContextEpoch tăng epoch ngữ cảnh của tenant viewer (đơn vị xem — tx PHẢI ở ngữ cảnh tenant đó, RLS
// WITH CHECK); trả epoch mới (lần đầu = 1).
func BumpContextEpoch(ctx context.Context, tx pgx.Tx, viewer uuid.UUID, cause string, causeRef, causeTenant uuid.UUID) (int64, error) {
	var epoch int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_party.context_epoch (tenant_id, epoch, cause, cause_ref, cause_tenant)
	  VALUES ($1, 1, $2, $3, $4)
	  ON CONFLICT (tenant_id) DO UPDATE SET epoch = pf_party.context_epoch.epoch + 1, cause = EXCLUDED.cause,
	    cause_ref = EXCLUDED.cause_ref, cause_tenant = EXCLUDED.cause_tenant, updated_at = now()
	  RETURNING epoch`, viewer, cause, causeRef, causeTenant).Scan(&epoch)
	return epoch, err
}

// ContextEpoch đọc epoch ngữ cảnh của tenant hiện tại; 0 khi chưa từng tăng.
func ContextEpoch(ctx context.Context, tx pgx.Tx) (int64, error) {
	var epoch int64
	err := tx.QueryRow(ctx, `SELECT coalesce(max(epoch), 0) FROM pf_party.context_epoch`).Scan(&epoch)
	return epoch, err
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

// VerifiedActive trả cấp quyền đang hiệu lực tại at của bên nhận grantee (tx PHẢI đang ở ngữ cảnh
// tenant bên nhận) đã kiểm HAI phía: bảng chiếu share_grant_inbound của bên nhận + bảng hiện hành
// share_grant của bên cấp (đổi ngữ cảnh bằng tenant.Within với bằng chứng grantee → bên cấp của dòng
// chiếu — chỉ để đọc dòng hiện hành kiểm phía bên cấp; RLS vẫn áp, không BYPASSRLS): active,
// đúng bên nhận, có loại kind (rỗng = mọi loại), at ∈ [valid_from, valid_to). Dùng chung cho
// Authorize/ActiveGrant của app/ và bộ điều hợp chỉ đọc party/port/grantlookup (cmd/api-public).
func VerifiedActive(ctx context.Context, tx pgx.Tx, grantee uuid.UUID, kind string, grantor uuid.UUID, at time.Time) ([]Inbound, error) {
	rows, err := ActiveInbound(ctx, tx, kind, grantor, at)
	if err != nil {
		return nil, err
	}
	out := make([]Inbound, 0, len(rows))
	for _, in := range rows {
		ok := false
		err := tenant.Within(ctx, tx, tenant.MintGrant(grantee, in.Grantor), func() error {
			g, err := GrantByID(ctx, tx, in.GrantID, false)
			if err != nil || g == nil {
				return err
			}
			ok = g.Status == "active" && g.Grantee == grantee && (kind == "" || slices.Contains(g.Kinds, kind)) &&
				!at.Before(g.ValidFrom) && at.Before(g.ValidTo)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, in)
		}
	}
	return out, nil
}
