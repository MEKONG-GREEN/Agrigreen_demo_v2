package store

// SQL KN v7.7 gói P1 (migrations/pf/000060–000061). Chạy trong giao dịch tenant của accounts_app (R-05).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

func nilRef(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func deref(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---------------------------------------------------------------------------
// Gán vai trò
// ---------------------------------------------------------------------------

// AsgEvent là một dòng sổ role_assignment_event; A là trạng thái SAU sự việc.
type AsgEvent struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Action     string
	A          port.Assignment
	Before     []byte
	After      []byte
	Reason     *string
	EventTime  time.Time
	RecordedAt time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
}

// InsertAsgEvent ghi một dòng sổ; seq, recorded_at do DB tính.
func InsertAsgEvent(ctx context.Context, tx pgx.Tx, e *AsgEvent) error {
	a := e.A
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.role_assignment_event
	    (id, tenant_id, stream_key, assignment_id, user_ref, action, role, org_ref, unit_ref, department_codes, mgmt_level,
	     commune_codes, valid_from, valid_to, before, after, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16::jsonb, $17, $18, $19, $20, $21)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, "asg:"+a.ID.String(), a.ID, a.UserRef, e.Action, a.Role, nilRef(a.OrgRef), nilRef(a.UnitRef),
		nonNilStrings(a.Departments), strPtr(a.MgmtLevel), nonNilStrings(a.Communes), a.ValidFrom, a.ValidTo,
		jsonOrNil(e.Before), string(e.After), e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.KNRuleVersion).
		Scan(&e.Seq, &e.RecordedAt)
}

func jsonOrNil(b []byte) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	return &s
}

const asgEventCols = `id, tenant_id, action, assignment_id, user_ref, role, org_ref, unit_ref, department_codes,
  coalesce(mgmt_level, ''), commune_codes, valid_from, valid_to, before, after, reason, event_time, recorded_at, actor_ref, idem_key, seq`

func scanAsgEvent(row pgx.Row) (AsgEvent, error) {
	var e AsgEvent
	var org, unit *uuid.UUID
	err := row.Scan(&e.ID, &e.TenantID, &e.Action, &e.A.ID, &e.A.UserRef, &e.A.Role, &org, &unit, &e.A.Departments,
		&e.A.MgmtLevel, &e.A.Communes, &e.A.ValidFrom, &e.A.ValidTo, &e.Before, &e.After, &e.Reason, &e.EventTime,
		&e.RecordedAt, &e.ActorRef, &e.IdemKey, &e.Seq)
	e.A.OrgRef, e.A.UnitRef, e.A.Active = deref(org), deref(unit), e.Action != domain.AsgRevoked
	return e, err
}

// AsgEventByIdem tìm dòng sổ đã ghi với idem_key.
func AsgEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*AsgEvent, error) {
	e, err := scanAsgEvent(tx.QueryRow(ctx, "SELECT "+asgEventCols+" FROM pf_accounts.role_assignment_event WHERE idem_key = $1 ORDER BY seq LIMIT 1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// AsgEventsOfUser liệt kê sổ gán vai trò của một tài khoản theo thời điểm ghi.
func AsgEventsOfUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]AsgEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+asgEventCols+" FROM pf_accounts.role_assignment_event WHERE user_ref = $1 ORDER BY recorded_at, seq", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AsgEvent
	for rows.Next() {
		e, err := scanAsgEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PutAssignment ghi bảng chiếu theo dòng sổ vừa ghi (R-21).
func PutAssignment(ctx context.Context, tx pgx.Tx, e AsgEvent) error {
	a := e.A
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.role_assignment_current
	    (tenant_id, assignment_id, user_ref, role, org_ref, unit_ref, department_codes, mgmt_level, commune_codes,
	     valid_from, valid_to, active, last_event_id, last_seq, created_by, created_at, updated_by, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $15, $16)
	  ON CONFLICT (tenant_id, assignment_id) DO UPDATE SET role = excluded.role, org_ref = excluded.org_ref,
	    unit_ref = excluded.unit_ref, department_codes = excluded.department_codes, mgmt_level = excluded.mgmt_level,
	    commune_codes = excluded.commune_codes, valid_from = excluded.valid_from, valid_to = excluded.valid_to,
	    active = excluded.active, last_event_id = excluded.last_event_id, last_seq = excluded.last_seq,
	    updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		e.TenantID, a.ID, a.UserRef, a.Role, nilRef(a.OrgRef), nilRef(a.UnitRef), nonNilStrings(a.Departments), strPtr(a.MgmtLevel),
		nonNilStrings(a.Communes), a.ValidFrom, a.ValidTo, a.Active, e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}

const asgCols = `assignment_id, user_ref, role, org_ref, unit_ref, department_codes, coalesce(mgmt_level, ''), commune_codes,
  valid_from, valid_to, active, created_by, created_at, updated_by, updated_at`

func scanAssignment(row pgx.Row) (port.Assignment, error) {
	var a port.Assignment
	var org, unit *uuid.UUID
	err := row.Scan(&a.ID, &a.UserRef, &a.Role, &org, &unit, &a.Departments, &a.MgmtLevel, &a.Communes, &a.ValidFrom, &a.ValidTo,
		&a.Active, &a.CreatedBy, &a.CreatedAt, &a.UpdatedBy, &a.UpdatedAt)
	a.OrgRef, a.UnitRef = deref(org), deref(unit)
	return a, err
}

func listAssignments(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]port.Assignment, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AssignmentByID đọc một lần gán; nil nếu không có. forUpdate khóa dòng.
func AssignmentByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*port.Assignment, error) {
	q := "SELECT " + asgCols + " FROM pf_accounts.role_assignment_current WHERE assignment_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanAssignment(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// AssignmentsOfUser liệt kê lần gán của tài khoản (kể cả đã thu hồi nếu all).
func AssignmentsOfUser(ctx context.Context, tx pgx.Tx, user uuid.UUID, all bool) ([]port.Assignment, error) {
	q := "SELECT " + asgCols + " FROM pf_accounts.role_assignment_current WHERE user_ref = $1"
	if !all {
		q += " AND active"
	}
	return listAssignments(ctx, tx, q+" ORDER BY created_at, assignment_id", user)
}

// ActiveAssignmentsAt liệt kê lần gán còn hiệu lực tại now, lọc theo vai/phòng ban (rỗng = mọi).
func ActiveAssignmentsAt(ctx context.Context, tx pgx.Tx, role, dept string, now time.Time) ([]port.Assignment, error) {
	return listAssignments(ctx, tx, "SELECT "+asgCols+` FROM pf_accounts.role_assignment_current
	  WHERE active AND valid_from <= $3 AND (valid_to IS NULL OR valid_to > $3)
	    AND ($1 = '' OR role = $1) AND ($2 = '' OR $2 = ANY (department_codes))
	  ORDER BY created_at, assignment_id`, role, dept, now)
}

// CountUsersByDepartment đếm tài khoản có lần gán đang hoạt động (chưa thu hồi) thuộc phòng ban code của đơn vị.
func CountUsersByDepartment(ctx context.Context, tx pgx.Tx, unit uuid.UUID, code string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(DISTINCT user_ref) FROM pf_accounts.role_assignment_current
	  WHERE active AND unit_ref = $1 AND $2 = ANY (department_codes)`, unit, code).Scan(&n)
	return n, err
}

// CountUsersByOrg đếm tài khoản có lần gán đang hoạt động trên Nút org hoặc phụ trách một xã trong communes.
func CountUsersByOrg(ctx context.Context, tx pgx.Tx, org uuid.UUID, communes []string) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(DISTINCT user_ref) FROM pf_accounts.role_assignment_current
	  WHERE active AND (org_ref = $1 OR unit_ref = $1 OR commune_codes && $2::text[])`, org, nonNilStrings(communes)).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Ngữ cảnh phiên
// ---------------------------------------------------------------------------

// Context là một dòng session_context.
type Context struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	UserRef      uuid.UUID
	AssignmentID uuid.UUID
	Role         string
	Department   string
	OrgRef       uuid.UUID
	UnitRef      uuid.UUID
	Communes     []string
	SupportID    uuid.UUID
	OnBehalfOf   uuid.UUID
	IdemKey      string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

// InsertContext ghi ngữ cảnh mới (bất biến).
func InsertContext(ctx context.Context, tx pgx.Tx, c Context) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.session_context
	    (tenant_id, context_id, user_ref, assignment_id, role, department_code, org_ref, unit_ref, commune_codes,
	     support_session_id, on_behalf_of, idem_key, created_at, expires_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		c.TenantID, c.ID, c.UserRef, nilRef(c.AssignmentID), c.Role, strPtr(c.Department), nilRef(c.OrgRef), nilRef(c.UnitRef),
		nonNilStrings(c.Communes), nilRef(c.SupportID), nilRef(c.OnBehalfOf), c.IdemKey, c.CreatedAt.UTC(), c.ExpiresAt.UTC())
	return err
}

const ctxCols = `context_id, tenant_id, user_ref, assignment_id, role, coalesce(department_code, ''), org_ref, unit_ref, commune_codes,
  support_session_id, on_behalf_of, idem_key, created_at, expires_at`

func scanContext(row pgx.Row) (*Context, error) {
	var c Context
	var asg, org, unit, sup, obo *uuid.UUID
	err := row.Scan(&c.ID, &c.TenantID, &c.UserRef, &asg, &c.Role, &c.Department, &org, &unit, &c.Communes, &sup, &obo, &c.IdemKey,
		&c.CreatedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.AssignmentID, c.OrgRef, c.UnitRef, c.SupportID, c.OnBehalfOf = deref(asg), deref(org), deref(unit), deref(sup), deref(obo)
	return &c, nil
}

// ContextByID đọc ngữ cảnh; nil nếu không có (RLS: chỉ tenant hiện tại).
func ContextByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*Context, error) {
	return scanContext(tx.QueryRow(ctx, "SELECT "+ctxCols+" FROM pf_accounts.session_context WHERE context_id = $1", id))
}

// ContextByIdem tìm ngữ cảnh đã tạo bởi cùng người với cùng khóa (gọi lặp POST /v1/me/context).
func ContextByIdem(ctx context.Context, tx pgx.Tx, user uuid.UUID, key string) (*Context, error) {
	return scanContext(tx.QueryRow(ctx, "SELECT "+ctxCols+" FROM pf_accounts.session_context WHERE user_ref = $1 AND idem_key = $2 LIMIT 1", user, key))
}

// ---------------------------------------------------------------------------
// Phiên hỗ trợ (FN-AD-04.07)
// ---------------------------------------------------------------------------

// Support là một phiên hỗ trợ (bảng chiếu support_session).
type Support struct {
	ID               uuid.UUID
	AdminRef         uuid.UUID
	ActedRole        string
	Department       string
	OrgRef           uuid.UUID
	Communes         []string
	OnBehalfOf       uuid.UUID
	TargetAssignment uuid.UUID
	ContextID        uuid.UUID
	StartedAt        time.Time
	ExpiresAt        time.Time
	EndedAt          *time.Time
}

// SupportEvent là một dòng sổ support_session_event.
type SupportEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Action    string // started | ended
	S         Support
	Reason    *string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
}

// InsertSupportEvent ghi sổ phiên hỗ trợ và bảng chiếu.
func InsertSupportEvent(ctx context.Context, tx pgx.Tx, e *SupportEvent) error {
	s := e.S
	var rec time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO pf_accounts.support_session_event
	    (id, tenant_id, stream_key, support_session_id, action, acted_role, department_code, org_ref, commune_codes,
	     on_behalf_of, target_assignment, reason, expires_at, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, "support:"+s.ID.String(), s.ID, e.Action, s.ActedRole, s.Department, s.OrgRef, nonNilStrings(s.Communes),
		nilRef(s.OnBehalfOf), nilRef(s.TargetAssignment), e.Reason, s.ExpiresAt.UTC(), e.EventTime.UTC(), e.ActorRef, e.IdemKey,
		domain.KNRuleVersion).Scan(&e.Seq, &rec); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.support_session
	    (tenant_id, support_session_id, admin_ref, acted_role, department_code, org_ref, commune_codes, on_behalf_of,
	     target_assignment, context_id, started_at, expires_at, ended_at, last_event_id, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	  ON CONFLICT (tenant_id, support_session_id) DO UPDATE SET ended_at = excluded.ended_at,
	    last_event_id = excluded.last_event_id, last_seq = excluded.last_seq`,
		e.TenantID, s.ID, s.AdminRef, s.ActedRole, s.Department, s.OrgRef, nonNilStrings(s.Communes), nilRef(s.OnBehalfOf),
		nilRef(s.TargetAssignment), s.ContextID, s.StartedAt.UTC(), s.ExpiresAt.UTC(), s.EndedAt, e.ID, e.Seq)
	return err
}

const supCols = `support_session_id, admin_ref, acted_role, department_code, org_ref, commune_codes, on_behalf_of, target_assignment,
  context_id, started_at, expires_at, ended_at`

// SupportByID đọc phiên hỗ trợ; nil nếu không có.
func SupportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Support, error) {
	q := "SELECT " + supCols + " FROM pf_accounts.support_session WHERE support_session_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	var s Support
	var obo, ta *uuid.UUID
	err := tx.QueryRow(ctx, q, id).Scan(&s.ID, &s.AdminRef, &s.ActedRole, &s.Department, &s.OrgRef, &s.Communes, &obo, &ta,
		&s.ContextID, &s.StartedAt, &s.ExpiresAt, &s.EndedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.OnBehalfOf, s.TargetAssignment = deref(obo), deref(ta)
	return &s, nil
}

// SupportEventByIdem trả phiên của dòng sổ đã ghi với idem_key (gọi lặp).
func SupportEventByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT support_session_id FROM pf_accounts.support_session_event WHERE idem_key = $1 LIMIT 1", key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// SupportAction là một dòng nhật ký thao tác hỗ trợ.
type SupportAction struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	SessionID  uuid.UUID
	ContextID  uuid.UUID
	RequestID  uuid.UUID
	Phase      string
	OnBehalfOf uuid.UUID
	ActedRole  string
	Department string
	Function   string
	Content    []byte
	Status     *int
	Reason     string
	TraceID    string
	EventTime  time.Time
	RecordedAt time.Time
	ActorRef   uuid.UUID
	Seq        int64
}

// InsertSupportAction ghi một dòng nhật ký hỗ trợ.
func InsertSupportAction(ctx context.Context, tx pgx.Tx, a *SupportAction) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.support_action_log
	    (id, tenant_id, stream_key, support_session_id, context_id, request_id, phase, on_behalf_of, acted_role, department_code,
	     function, content, status_code, reason, trace_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16, $17, $18, $19)
	  RETURNING seq, recorded_at`,
		a.ID, a.TenantID, "support:"+a.SessionID.String(), a.SessionID, a.ContextID, a.RequestID, a.Phase, nilRef(a.OnBehalfOf),
		a.ActedRole, a.Department, a.Function, string(a.Content), a.Status, a.Reason, strPtr(a.TraceID), a.EventTime.UTC(), a.ActorRef,
		"support-action:"+a.RequestID.String()+":"+a.Phase, domain.KNRuleVersion).Scan(&a.Seq, &a.RecordedAt)
}

// SupportActions liệt kê nhật ký hỗ trợ mới nhất trước, con trỏ = (recorded_at, id) của dòng cuối trang trước.
func SupportActions(ctx context.Context, tx pgx.Tx, session uuid.UUID, beforeAt *time.Time, beforeID uuid.UUID, limit int) ([]SupportAction, error) {
	rows, err := tx.Query(ctx, `SELECT id, support_session_id, context_id, request_id, phase, on_behalf_of, acted_role, department_code,
	    function, content, status_code, reason, coalesce(trace_id, ''), event_time, recorded_at, actor_ref, seq
	  FROM pf_accounts.support_action_log
	  WHERE ($1::uuid IS NULL OR support_session_id = $1)
	    AND ($2::timestamptz IS NULL OR (recorded_at, id) < ($2, $3))
	  ORDER BY recorded_at DESC, id DESC LIMIT $4`, nilRef(session), beforeAt, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SupportAction
	for rows.Next() {
		var a SupportAction
		var obo *uuid.UUID
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ContextID, &a.RequestID, &a.Phase, &obo, &a.ActedRole, &a.Department, &a.Function,
			&a.Content, &a.Status, &a.Reason, &a.TraceID, &a.EventTime, &a.RecordedAt, &a.ActorRef, &a.Seq); err != nil {
			return nil, err
		}
		a.OnBehalfOf = deref(obo)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Sổ chỉnh sửa thông tin tài khoản
// ---------------------------------------------------------------------------

// ProfileChange là một dòng account_profile_change.
type ProfileChange struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserRef    uuid.UUID
	Fields     []string
	Reason     *string
	EventTime  time.Time
	RecordedAt time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
}

// InsertProfileChange ghi một dòng sổ chỉnh sửa.
func InsertProfileChange(ctx context.Context, tx pgx.Tx, p *ProfileChange) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.account_profile_change
	    (id, tenant_id, stream_key, user_ref, fields, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING seq, recorded_at`,
		p.ID, p.TenantID, "profile:"+p.UserRef.String(), p.UserRef, p.Fields, p.Reason, p.EventTime.UTC(), p.ActorRef, p.IdemKey,
		domain.KNRuleVersion).Scan(&p.Seq, &p.RecordedAt)
}

// ProfileChangeByIdem tìm dòng đã ghi với idem_key.
func ProfileChangeByIdem(ctx context.Context, tx pgx.Tx, key string) (*ProfileChange, error) {
	var p ProfileChange
	err := tx.QueryRow(ctx, `SELECT id, user_ref, fields, reason, event_time, recorded_at, actor_ref, idem_key, seq
	  FROM pf_accounts.account_profile_change WHERE idem_key = $1 LIMIT 1`, key).
		Scan(&p.ID, &p.UserRef, &p.Fields, &p.Reason, &p.EventTime, &p.RecordedAt, &p.ActorRef, &p.IdemKey, &p.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProfileChanges liệt kê sổ chỉnh sửa của tài khoản.
func ProfileChanges(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]ProfileChange, error) {
	rows, err := tx.Query(ctx, `SELECT id, user_ref, fields, reason, event_time, recorded_at, actor_ref, idem_key, seq
	  FROM pf_accounts.account_profile_change WHERE user_ref = $1 ORDER BY recorded_at, seq`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProfileChange
	for rows.Next() {
		var p ProfileChange
		if err := rows.Scan(&p.ID, &p.UserRef, &p.Fields, &p.Reason, &p.EventTime, &p.RecordedAt, &p.ActorRef, &p.IdemKey, &p.Seq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AccountEvents đọc sổ account_event của tài khoản (cho khung Log hợp nhất).
func AccountEvents(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]domain.Event, error) {
	return Events(ctx, tx, user)
}
