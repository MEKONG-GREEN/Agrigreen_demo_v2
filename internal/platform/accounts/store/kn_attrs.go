package store

// SQL W11.07 (migrations/pf/000142): sổ phụ thuộc tính vai, sổ ủy quyền có hạn. Chạy trong giao dịch tenant
// của module (r_accounts, R-05).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// refsArg chuyển mã tham chiếu thành text[] (SQL ép ::uuid[]) — pgx không tự mã hóa []uuid.UUID.
func refsArg(r []uuid.UUID) []string {
	out := make([]string, 0, len(r))
	for _, x := range r {
		out = append(out, x.String())
	}
	return out
}

func parseRefs(in []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(in))
	for _, s := range in {
		r, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Thuộc tính vai
// ---------------------------------------------------------------------------

// AttrEvent là một dòng sổ assignment_attr_event; A là trạng thái SAU sự việc.
type AttrEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	UserRef   uuid.UUID
	Role      string
	A         port.AssignmentAttrs
	Before    []byte
	After     []byte
	Reason    *string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
}

// InsertAttrEvent ghi một dòng sổ thuộc tính vai; seq do DB tính.
func InsertAttrEvent(ctx context.Context, tx pgx.Tx, e *AttrEvent) error {
	a := e.A
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.assignment_attr_event
	    (id, tenant_id, stream_key, assignment_id, user_ref, role, province_group, duties, internal_unit_code, facility_refs,
	     household_refs, before, after, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::uuid[], $11::uuid[], $12::jsonb, $13::jsonb, $14, $15, $16, $17, $18) RETURNING seq`,
		e.ID, e.TenantID, "asgattr:"+a.AssignmentID.String(), a.AssignmentID, e.UserRef, e.Role, strPtr(a.ProvinceGroup),
		nonNilStrings(a.Duties), strPtr(a.InternalUnit), refsArg(a.Facilities), refsArg(a.Households), jsonOrNil(e.Before),
		string(e.After), e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.AttrRuleVersion).Scan(&e.Seq)
}

// AttrEventByIdem trả assignment_id và seq của dòng sổ đã ghi với idem_key (ok = false nếu chưa có).
func AttrEventByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT assignment_id FROM pf_accounts.assignment_attr_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// PutAttrs ghi bảng chiếu thuộc tính vai theo dòng sổ vừa ghi (R-21).
func PutAttrs(ctx context.Context, tx pgx.Tx, e AttrEvent) error {
	a := e.A
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.assignment_attr_current
	    (tenant_id, assignment_id, user_ref, role, province_group, duties, internal_unit_code, facility_refs, household_refs,
	     last_event_id, last_seq, updated_by, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::uuid[], $9::uuid[], $10, $11, $12, $13)
	  ON CONFLICT (tenant_id, assignment_id) DO UPDATE SET role = excluded.role, province_group = excluded.province_group,
	    duties = excluded.duties, internal_unit_code = excluded.internal_unit_code, facility_refs = excluded.facility_refs,
	    household_refs = excluded.household_refs, last_event_id = excluded.last_event_id, last_seq = excluded.last_seq,
	    updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		e.TenantID, a.AssignmentID, e.UserRef, e.Role, strPtr(a.ProvinceGroup), nonNilStrings(a.Duties), strPtr(a.InternalUnit),
		refsArg(a.Facilities), refsArg(a.Households), e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}

const attrCols = `assignment_id, coalesce(province_group, ''), duties, coalesce(internal_unit_code, ''), facility_refs::text[],
  household_refs::text[], last_seq, updated_by, updated_at`

func scanAttrs(row pgx.Row) (port.AssignmentAttrs, error) {
	var a port.AssignmentAttrs
	var fac, hh []string
	if err := row.Scan(&a.AssignmentID, &a.ProvinceGroup, &a.Duties, &a.InternalUnit, &fac, &hh, &a.Seq, &a.UpdatedBy, &a.UpdatedAt); err != nil {
		return a, err
	}
	var err error
	if a.Facilities, err = parseRefs(fac); err != nil {
		return a, err
	}
	a.Households, err = parseRefs(hh)
	return a, err
}

// AttrsOf đọc thuộc tính của một lần gán; chưa có → giá trị rỗng với AssignmentID (Seq = 0).
func AttrsOf(ctx context.Context, tx pgx.Tx, assignment uuid.UUID, forUpdate bool) (port.AssignmentAttrs, error) {
	q := "SELECT " + attrCols + " FROM pf_accounts.assignment_attr_current WHERE assignment_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	a, err := scanAttrs(tx.QueryRow(ctx, q, assignment))
	if errors.Is(err, pgx.ErrNoRows) {
		return port.AssignmentAttrs{AssignmentID: assignment, Duties: []string{}, Facilities: []uuid.UUID{}, Households: []uuid.UUID{}}, nil
	}
	return a, err
}

// AttrsOfUser trả thuộc tính các lần gán của một tài khoản, khóa theo assignment_id.
func AttrsOfUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) (map[uuid.UUID]port.AssignmentAttrs, error) {
	rows, err := tx.Query(ctx, "SELECT "+attrCols+" FROM pf_accounts.assignment_attr_current WHERE user_ref = $1", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]port.AssignmentAttrs{}
	for rows.Next() {
		a, err := scanAttrs(rows)
		if err != nil {
			return nil, err
		}
		out[a.AssignmentID] = a
	}
	return out, rows.Err()
}

// UsersOfInternalUnit trả người dùng có lần gán vai role CÒN HIỆU LỰC tại now thuộc đơn vị nội bộ code
// (Cấp Xã: đơn vị quản lý HS-37), không trùng, theo thứ tự user_ref.
func UsersOfInternalUnit(ctx context.Context, tx pgx.Tx, unit uuid.UUID, code, role string, now time.Time) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT a.user_ref FROM pf_accounts.assignment_attr_current x
	    JOIN pf_accounts.role_assignment_current a ON a.tenant_id = x.tenant_id AND a.assignment_id = x.assignment_id
	  WHERE x.internal_unit_code = $1 AND a.role = $2 AND a.unit_ref = $3 AND a.active
	    AND a.valid_from <= $4 AND (a.valid_to IS NULL OR a.valid_to > $4)
	  ORDER BY a.user_ref`, code, role, unit, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var u uuid.UUID
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Ủy quyền có hạn
// ---------------------------------------------------------------------------

// DelegEvent là một dòng sổ delegation_event; D là trạng thái SAU sự việc.
type DelegEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Action    string // granted | revoked
	D         port.Delegation
	Reason    string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
}

// InsertDelegEvent ghi sổ ủy quyền.
func InsertDelegEvent(ctx context.Context, tx pgx.Tx, e *DelegEvent) error {
	d := e.D
	return tx.QueryRow(ctx, `INSERT INTO pf_accounts.delegation_event
	    (id, tenant_id, stream_key, delegation_id, action, grantor_ref, grantee_ref, unit_ref, department_code, power, valid_from,
	     valid_to, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING seq`,
		e.ID, e.TenantID, "deleg:"+d.ID.String(), d.ID, e.Action, d.GrantorRef, d.GranteeRef, d.UnitRef, d.Department, d.Power,
		d.ValidFrom, d.ValidTo, e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.AttrRuleVersion).Scan(&e.Seq)
}

// DelegEventByIdem trả delegation_id của dòng sổ đã ghi với idem_key.
func DelegEventByIdem(ctx context.Context, tx pgx.Tx, key string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, "SELECT delegation_id FROM pf_accounts.delegation_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return id, err == nil, err
}

// PutDelegation ghi bảng chiếu ủy quyền.
func PutDelegation(ctx context.Context, tx pgx.Tx, e DelegEvent) error {
	d := e.D
	_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.delegation_current
	    (tenant_id, delegation_id, grantor_ref, grantee_ref, unit_ref, department_code, power, valid_from, valid_to, revoked, reason,
	     last_event_id, last_seq, created_by, created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)
	  ON CONFLICT (tenant_id, delegation_id) DO UPDATE SET revoked = excluded.revoked, last_event_id = excluded.last_event_id,
	    last_seq = excluded.last_seq, updated_at = excluded.updated_at`,
		e.TenantID, d.ID, d.GrantorRef, d.GranteeRef, d.UnitRef, d.Department, d.Power, d.ValidFrom, d.ValidTo, d.Revoked, d.Reason,
		e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}

const delegCols = `delegation_id, grantor_ref, grantee_ref, unit_ref, department_code, power, valid_from, valid_to, revoked, reason,
  created_by, created_at, updated_at`

func scanDeleg(row pgx.Row) (port.Delegation, error) {
	var d port.Delegation
	err := row.Scan(&d.ID, &d.GrantorRef, &d.GranteeRef, &d.UnitRef, &d.Department, &d.Power, &d.ValidFrom, &d.ValidTo, &d.Revoked,
		&d.Reason, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func listDelegs(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]port.Delegation, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.Delegation{}
	for rows.Next() {
		d, err := scanDeleg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DelegationByID đọc một ủy quyền; nil nếu không có.
func DelegationByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*port.Delegation, error) {
	q := "SELECT " + delegCols + " FROM pf_accounts.delegation_current WHERE delegation_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	d, err := scanDeleg(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DelegationsOfUser liệt kê ủy quyền mà user là người giao hoặc người nhận, mới nhất trước.
func DelegationsOfUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]port.Delegation, error) {
	return listDelegs(ctx, tx, "SELECT "+delegCols+` FROM pf_accounts.delegation_current
	  WHERE grantor_ref = $1 OR grantee_ref = $1 ORDER BY created_at DESC, delegation_id`, user)
}

// ActivePowers trả quyền còn hiệu lực tại now mà grantee nhận trong phòng ban dept của đơn vị unit.
func ActivePowers(ctx context.Context, tx pgx.Tx, grantee, unit uuid.UUID, dept string, now time.Time) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT power FROM pf_accounts.delegation_current
	  WHERE grantee_ref = $1 AND unit_ref = $2 AND department_code = $3 AND NOT revoked AND valid_from <= $4 AND valid_to > $4
	  ORDER BY power`, grantee, unit, dept, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AttrLogRow là một dòng sổ thuộc tính vai cho khung Log tài khoản.
type AttrLogRow struct {
	AssignmentID uuid.UUID
	Before       []byte
	After        []byte
	Reason       *string
	EventTime    time.Time
	RecordedAt   time.Time
	ActorRef     uuid.UUID
}

// AttrEventsOfUser liệt kê sổ thuộc tính vai của một tài khoản theo thời điểm ghi.
func AttrEventsOfUser(ctx context.Context, tx pgx.Tx, user uuid.UUID) ([]AttrLogRow, error) {
	rows, err := tx.Query(ctx, `SELECT assignment_id, before, after, reason, event_time, recorded_at, actor_ref
	  FROM pf_accounts.assignment_attr_event WHERE user_ref = $1 ORDER BY recorded_at, seq`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AttrLogRow{}
	for rows.Next() {
		var r AttrLogRow
		if err := rows.Scan(&r.AssignmentID, &r.Before, &r.After, &r.Reason, &r.EventTime, &r.RecordedAt, &r.ActorRef); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
