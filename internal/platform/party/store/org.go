package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// nilIfZero đổi uuid.Nil thành NULL.
func nilIfZero(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

const orgCols = `ref, kind, code, name, coalesce(admin_unit_code, ''),
  coalesce(parent_ref, '00000000-0000-0000-0000-000000000000'::uuid),
  coalesce(representative_ref, '00000000-0000-0000-0000-000000000000'::uuid), active`

func scanOrg(row pgx.Row) (port.Org, error) {
	var o port.Org
	var kind string
	err := row.Scan(&o.Ref, &kind, &o.Code, &o.Name, &o.AdminUnitCode, &o.ParentRef, &o.RepresentativeRef, &o.Active)
	o.Kind = port.OrgKind(kind)
	return o, err
}

// OrgState là trạng thái hiện tại kèm vị trí trong sổ.
type OrgState struct {
	port.Org
	StatusReason string
	LastSeq      int64
}

// OrgByRef đọc trạng thái hiện tại; forUpdate khóa dòng để ghi trong cùng giao dịch.
func OrgByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*OrgState, error) {
	q := "SELECT " + orgCols + ", coalesce(status_reason, ''), last_seq FROM pf_party.org WHERE ref = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	var s OrgState
	var kind string
	err := tx.QueryRow(ctx, q, ref).Scan(&s.Ref, &kind, &s.Code, &s.Name, &s.AdminUnitCode, &s.ParentRef,
		&s.RepresentativeRef, &s.Active, &s.StatusReason, &s.LastSeq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Kind = port.OrgKind(kind)
	return &s, nil
}

// OrgByCode đọc tổ chức theo mã nghiệp vụ (duy nhất trong tenant); nil nếu không có.
func OrgByCode(ctx context.Context, tx pgx.Tx, code string) (*port.Org, error) {
	o, err := scanOrg(tx.QueryRow(ctx, "SELECT "+orgCols+" FROM pf_party.org WHERE code = $1", code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// OrgCodeTaken cho biết mã tổ chức đã dùng trong tenant.
func OrgCodeTaken(ctx context.Context, tx pgx.Tx, code string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pf_party.org WHERE code = $1)", code).Scan(&ok)
	return ok, err
}

// NextCooperativeCode sinh Mã HTX (CGH BR-01): HTX000001…
func NextCooperativeCode(ctx context.Context, tx pgx.Tx) (string, error) {
	var code string
	err := tx.QueryRow(ctx, "SELECT 'HTX' || lpad(nextval('pf_party.cooperative_code_seq')::text, 6, '0')").Scan(&code)
	return code, err
}

// IsInSubtree cho biết candidate là root hoặc nằm dưới root trong cây (chống tạo vòng khi di chuyển).
func IsInSubtree(ctx context.Context, tx pgx.Tx, root, candidate uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `WITH RECURSIVE sub(ref) AS (
	    SELECT ref FROM pf_party.org WHERE ref = $1
	    UNION
	    SELECT o.ref FROM pf_party.org o JOIN sub ON o.parent_ref = sub.ref)
	  SELECT EXISTS (SELECT 1 FROM sub WHERE ref = $2)`, root, candidate).Scan(&ok)
	return ok, err
}

// CountActiveChildren đếm tổ chức con đang hoạt động.
func CountActiveChildren(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, "SELECT count(*) FROM pf_party.org WHERE parent_ref = $1 AND active", ref).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Sổ org_event
// ---------------------------------------------------------------------------

const orgEventCols = `id, tenant_id, org_ref, action, kind, code, name, coalesce(admin_unit_code, ''),
  coalesce(parent_ref, '00000000-0000-0000-0000-000000000000'::uuid),
  coalesce(prev_parent_ref, '00000000-0000-0000-0000-000000000000'::uuid), coalesce(prev_admin_unit, ''),
  coalesce(representative_ref, '00000000-0000-0000-0000-000000000000'::uuid), active, coalesce(reason, ''),
  coalesce(details::text, ''), event_time, actor_ref, idem_key, seq, recorded_at`

func scanOrgEvent(row pgx.Row) (domain.OrgEvent, error) {
	var e domain.OrgEvent
	var action, kind, details string
	err := row.Scan(&e.ID, &e.TenantID, &e.Org.Ref, &action, &kind, &e.Org.Code, &e.Org.Name, &e.Org.AdminUnitCode,
		&e.Org.ParentRef, &e.PrevParentRef, &e.PrevAdminUnit, &e.Org.RepresentativeRef, &e.Org.Active, &e.Reason,
		&details, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	if err != nil {
		return e, err
	}
	e.Action, e.Org.Kind = domain.OrgAction(action), port.OrgKind(kind)
	if details != "" {
		if err := json.Unmarshal([]byte(details), &e.Details); err != nil {
			return e, err
		}
	}
	return e, nil
}

// InsertOrgEvent ghi sổ; DB tính seq, prev_hash, row_hash (shared.fn_chain).
func InsertOrgEvent(ctx context.Context, tx pgx.Tx, e *domain.OrgEvent) error {
	var details *string
	if len(e.Details) > 0 {
		b, err := json.Marshal(e.Details)
		if err != nil {
			return err
		}
		s := string(b)
		details = &s
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_party.org_event
	    (id, tenant_id, stream_key, org_ref, action, kind, code, name, admin_unit_code, parent_ref, prev_parent_ref,
	     prev_admin_unit, representative_ref, active, reason, details, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::jsonb, $17, $18, $19, $20)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.OrgStreamKey(e.Org.Ref), e.Org.Ref, string(e.Action), string(e.Org.Kind), e.Org.Code,
		e.Org.Name, nilIfEmpty(e.Org.AdminUnitCode), nilIfZero(e.Org.ParentRef), nilIfZero(e.PrevParentRef),
		nilIfEmpty(e.PrevAdminUnit), nilIfZero(e.Org.RepresentativeRef), e.Org.Active, nilIfEmpty(e.Reason), details,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.OrgRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

// OrgEventByIdemKey tìm bản ghi sổ đã ghi với idem_key (gọi lặp).
func OrgEventByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.OrgEvent, error) {
	e, err := scanOrgEvent(tx.QueryRow(ctx, "SELECT "+orgEventCols+" FROM pf_party.org_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// OrgEvents liệt kê lịch sử biến động của một tổ chức theo seq (nhật ký chỉ đọc, US-HTXSTATUS-03).
func OrgEvents(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]domain.OrgEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+orgEventCols+" FROM pf_party.org_event WHERE org_ref = $1 ORDER BY seq", ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OrgEvent
	for rows.Next() {
		e, err := scanOrgEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Bảng hiện tại
// ---------------------------------------------------------------------------

// InsertOrg ghi trạng thái hiện tại của tổ chức mới.
func InsertOrg(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, o port.Org, e domain.OrgEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.org
	    (tenant_id, ref, kind, code, name, admin_unit_code, parent_ref, representative_ref, active, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		tenantID, o.Ref, string(o.Kind), o.Code, o.Name, nilIfEmpty(o.AdminUnitCode), nilIfZero(o.ParentRef),
		nilIfZero(o.RepresentativeRef), o.Active, e.ID, e.Seq)
	return err
}

// UpdateOrg ghi trạng thái sau một biến động (R-21: cùng giao dịch với sổ).
func UpdateOrg(ctx context.Context, tx pgx.Tx, o port.Org, statusReason string, e domain.OrgEvent) error {
	_, err := tx.Exec(ctx, `UPDATE pf_party.org SET name = $2, admin_unit_code = $3, parent_ref = $4, active = $5,
	    status_reason = $6, last_event_id = $7, last_seq = $8, updated_at = now()
	  WHERE ref = $1`,
		o.Ref, o.Name, nilIfEmpty(o.AdminUnitCode), nilIfZero(o.ParentRef), o.Active, nilIfEmpty(statusReason), e.ID, e.Seq)
	return err
}

// OrgFilter là điều kiện liệt kê tổ chức theo phạm vi.
type OrgFilter struct {
	All             bool
	Units           []string
	Kind            string // rỗng = mọi loại
	Q               string // tìm theo tên (không phân biệt hoa thường), mã hoặc mã số thuế
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
}

// ListOrgs liệt kê tổ chức trong phạm vi, phân trang bằng cursor (R-53).
func ListOrgs(ctx context.Context, tx pgx.Tx, f OrgFilter) ([]port.Org, error) {
	rows, err := tx.Query(ctx, "SELECT "+orgCols+` FROM pf_party.org
	    WHERE ($1::boolean OR admin_unit_code = ANY($2::text[]))
	      AND ($3 = '' OR kind = $3)
	      AND ($4::boolean OR active)
	      AND ref > $5
	    ORDER BY ref LIMIT $6`, f.All, units(f.Units), f.Kind, f.IncludeInactive, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []port.Org
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// LockOrgIdempotency serializes concurrent retries before reading the ledger.
// Tenant and operation namespace are part of the lock; it ends with the tx.
func LockOrgIdempotency(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_party.org.idem:' || shared.current_tenant()::text || ':' || $1, 0))", key)
	return err
}

// LockCooperativeName tuần tự hóa việc tạo HTX cùng tên (không phân biệt hoa thường)
// trong giao dịch hiện tại — khóa tư vấn tự nhả khi giao dịch kết thúc.
func LockCooperativeName(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_party.cooperative_name:' || lower($1), 0))", strings.TrimSpace(name))
	return err
}

// CooperativeNameOwner trả HTX đang mang tên này (không phân biệt hoa thường, bỏ
// khoảng trắng hai đầu), Nil nếu chưa có — US-HTX-03 AC-3.
func CooperativeNameOwner(ctx context.Context, tx pgx.Tx, name string) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, `SELECT ref FROM pf_party.org WHERE kind = 'cooperative' AND lower(name) = lower($1) LIMIT 1`,
		strings.TrimSpace(name)).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return ref, err
}

// CoopOrgFilter là điều kiện liệt kê Nút HTX (kind = cooperative) của màn HTX. Hồ sơ HTX ở pf/cooperative (W3.3):
// điều kiện trên hồ sơ (mã số thuế, diện tích) được bên gọi quy thành danh sách Nút qua partyport.CoopProfiles.
type CoopOrgFilter struct {
	All             bool
	Units           []string
	Commune         string // rỗng = mọi xã trong phạm vi
	Status          string // active | inactive | all (rỗng = theo IncludeInactive)
	IncludeInactive bool
	// Q tìm theo tên (không phân biệt hoa thường) hoặc tiền tố mã; QRefs là Nút khớp Q theo hồ sơ (mã số thuế) —
	// cũng được nhận.
	Q     string
	QRefs []uuid.UUID
	// Only khác nil: chỉ các Nút này (lọc theo hồ sơ, vd diện tích).
	Only  []uuid.UUID
	After uuid.UUID
	Limit int
}

// CoopOrg là Nút HTX kèm lý do trạng thái, seq cuối và thời điểm cập nhật Nút.
type CoopOrg struct {
	OrgState
	UpdatedAt time.Time
}

const coopOrgCols = orgCols + ", coalesce(status_reason, ''), last_seq, updated_at"

func scanCoopOrg(row pgx.Row) (CoopOrg, error) {
	var c CoopOrg
	var kind string
	err := row.Scan(&c.Ref, &kind, &c.Code, &c.Name, &c.AdminUnitCode, &c.ParentRef, &c.RepresentativeRef, &c.Active,
		&c.StatusReason, &c.LastSeq, &c.UpdatedAt)
	c.Kind = port.OrgKind(kind)
	return c, err
}

// ListCooperativeOrgs liệt kê Nút HTX trong phạm vi theo bộ lọc, phân trang cursor theo ref (R-53).
func ListCooperativeOrgs(ctx context.Context, tx pgx.Tx, f CoopOrgFilter) ([]CoopOrg, error) {
	status := f.Status
	if status == "" {
		status = "active"
		if f.IncludeInactive {
			status = "all"
		}
	}
	var only []string
	if f.Only != nil {
		only = uuidStrings(f.Only)
	}
	rows, err := tx.Query(ctx, "SELECT "+coopOrgCols+` FROM pf_party.org
	    WHERE kind = 'cooperative'
	      AND ($1::boolean OR admin_unit_code = ANY($2::text[]))
	      AND ($3 = '' OR admin_unit_code = $3)
	      AND ($4 = 'all' OR ($4 = 'active' AND active) OR ($4 = 'inactive' AND NOT active))
	      AND ($5 = '' OR name ILIKE '%' || $5 || '%' OR code ILIKE $5 || '%' OR ref = ANY($6::text[]::uuid[]))
	      AND ($7::text[] IS NULL OR ref = ANY($7::text[]::uuid[]))
	      AND ref > $8
	    ORDER BY ref LIMIT $9`,
		f.All, units(f.Units), f.Commune, status, escapeLike(strings.TrimSpace(f.Q)), uuidStrings(f.QRefs), only, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoopOrg{}
	for rows.Next() {
		c, err := scanCoopOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CoopOrgByRef đọc Nút HTX; nil nếu không có hoặc không phải HTX.
func CoopOrgByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*CoopOrg, error) {
	c, err := scanCoopOrg(tx.QueryRow(ctx, "SELECT "+coopOrgCols+" FROM pf_party.org WHERE ref = $1 AND kind = 'cooperative'", ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// OrgsByRefs đọc tổ chức theo lô (partyport.OrgDirectory).
func OrgsByRefs(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) ([]port.Org, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+orgCols+" FROM pf_party.org WHERE ref = ANY($1::text[]::uuid[])", uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (port.Org, error) { return scanOrg(r) })
}

// OrgsOfKind liệt kê tổ chức theo loại, sắp theo ref (partyport.OrgDirectory). unitCodes nil = mọi đơn vị.
func OrgsOfKind(ctx context.Context, tx pgx.Tx, kind string, unitCodes []string, activeOnly bool) ([]port.Org, error) {
	rows, err := tx.Query(ctx, "SELECT "+orgCols+` FROM pf_party.org
	    WHERE kind = $1 AND ($2::boolean OR admin_unit_code = ANY($3::text[])) AND (NOT $4::boolean OR active)
	    ORDER BY ref`, kind, unitCodes == nil, units(unitCodes), activeOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (port.Org, error) { return scanOrg(r) })
}

// escapeLike vô hiệu ký tự đại diện của LIKE trong từ khóa người dùng.
func escapeLike(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '%' || r == '_' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}
