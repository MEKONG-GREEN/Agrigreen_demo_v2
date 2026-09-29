package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
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

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func parseDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
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
	e, err := scanOrgEvent(tx.QueryRow(ctx, "SELECT "+orgEventCols+" FROM pf_party.org_event WHERE idem_key = $1", key))
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

// Cooperative là HTX kèm hồ sơ.
type Cooperative struct {
	OrgState
	Profile domain.CooperativeProfile
}

const coopCols = `o.ref, o.kind, o.code, o.name, coalesce(o.admin_unit_code, ''),
  coalesce(o.parent_ref, '00000000-0000-0000-0000-000000000000'::uuid),
  coalesce(o.representative_ref, '00000000-0000-0000-0000-000000000000'::uuid), o.active,
  coalesce(o.status_reason, ''), o.last_seq,
  coalesce(c.tax_code, ''), c.area_ha::text, c.established_on::text,
  ST_Y(c.location)::numeric(10,7)::text, ST_X(c.location)::numeric(10,7)::text, coalesce(c.head_office, '')`

func scanCoop(row pgx.Row) (Cooperative, error) {
	var c Cooperative
	var kind string
	var area, est, lat, lon *string
	err := row.Scan(&c.Ref, &kind, &c.Code, &c.Name, &c.AdminUnitCode, &c.ParentRef, &c.RepresentativeRef, &c.Active,
		&c.StatusReason, &c.LastSeq, &c.Profile.TaxCode, &area, &est, &lat, &lon, &c.Profile.Address)
	if err != nil {
		return c, err
	}
	c.Kind = port.OrgKind(kind)
	if c.Profile.AreaHa, err = parseDec(area); err != nil {
		return c, err
	}
	if est != nil {
		t, err := time.Parse(time.DateOnly, *est)
		if err != nil {
			return c, err
		}
		c.Profile.EstablishedOn = &t
	}
	if lat != nil && lon != nil {
		la, err := parseDec(lat)
		if err != nil {
			return c, err
		}
		lo, err := parseDec(lon)
		if err != nil {
			return c, err
		}
		c.Profile.Location = &domain.Point{Lat: *la, Lon: *lo}
	}
	return c, nil
}

// InsertCooperative ghi hồ sơ HTX.
func InsertCooperative(ctx context.Context, tx pgx.Tx, tenantID, ref uuid.UUID, p domain.CooperativeProfile) error {
	var est, lat, lon *string
	if p.EstablishedOn != nil {
		s := p.EstablishedOn.Format(time.DateOnly)
		est = &s
	}
	if p.Location != nil {
		a, b := p.Location.Lat.String(), p.Location.Lon.String()
		lat, lon = &a, &b
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.cooperative (tenant_id, org_ref, tax_code, area_ha, established_on, location, head_office)
	  VALUES ($1, $2, $3, $4::numeric, $5::date,
	    CASE WHEN $6::text IS NULL THEN NULL
	         ELSE ST_SetSRID(ST_MakePoint($7::text::float8, $6::text::float8), 4326) END, $8)`,
		tenantID, ref, nilIfEmpty(p.TaxCode), decPtr(p.AreaHa), est, lat, lon, nilIfEmpty(strings.TrimSpace(p.Address)))
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

// TaxCodeOwner trả HTX đang giữ mã số thuế trong tenant, Nil nếu chưa có.
func TaxCodeOwner(ctx context.Context, tx pgx.Tx, tax string) (uuid.UUID, error) {
	var ref uuid.UUID
	err := tx.QueryRow(ctx, "SELECT org_ref FROM pf_party.cooperative WHERE tax_code = $1", tax).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return ref, err
}

// CooperativeByRef đọc HTX kèm hồ sơ.
func CooperativeByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Cooperative, error) {
	c, err := scanCoop(tx.QueryRow(ctx, "SELECT "+coopCols+` FROM pf_party.org o
	    JOIN pf_party.cooperative c ON c.tenant_id = o.tenant_id AND c.org_ref = o.ref WHERE o.ref = $1`, ref))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CooperativeByCode đọc HTX theo Mã HTX.
func CooperativeByCode(ctx context.Context, tx pgx.Tx, code string) (*Cooperative, error) {
	c, err := scanCoop(tx.QueryRow(ctx, "SELECT "+coopCols+` FROM pf_party.org o
	    JOIN pf_party.cooperative c ON c.tenant_id = o.tenant_id AND c.org_ref = o.ref WHERE o.code = $1`, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListCooperatives liệt kê HTX trong phạm vi, tìm theo tên/mã/mã số thuế, phân trang cursor.
func ListCooperatives(ctx context.Context, tx pgx.Tx, f OrgFilter) ([]Cooperative, error) {
	rows, err := tx.Query(ctx, "SELECT "+coopCols+` FROM pf_party.org o
	    JOIN pf_party.cooperative c ON c.tenant_id = o.tenant_id AND c.org_ref = o.ref
	    WHERE ($1::boolean OR o.admin_unit_code = ANY($2::text[]))
	      AND ($3 = '' OR o.name ILIKE '%' || $3 || '%' OR o.code ILIKE $3 || '%' OR c.tax_code LIKE $3 || '%')
	      AND ($4::boolean OR o.active)
	      AND o.ref > $5
	    ORDER BY o.ref LIMIT $6`, f.All, units(f.Units), escapeLike(f.Q), f.IncludeInactive, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cooperative
	for rows.Next() {
		c, err := scanCoop(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
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
