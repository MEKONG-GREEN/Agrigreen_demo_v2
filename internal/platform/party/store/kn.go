package store

// KN v7.7 gói P2 (migration pf 000062): sổ + bảng chiếu phòng ban, khu vực, gán khu vực cho Nút xã.
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// KNEvent là các cột chung của ba sổ KN (department_event, region_event, org_region_event).
type KNEvent struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	EntityID   uuid.UUID // department_id | region_id | org_ref
	Action     string
	Before     []byte // jsonb; nil khi tạo mới (org_region_event không dùng)
	After      []byte
	Reason     *string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
	// Cột riêng.
	OrgRef         uuid.UUID // department_event: đơn vị
	Code           string    // department_event, region_event
	Status         string    // department_event
	ProvinceCode   string    // region_event, org_region_event
	CommuneCode    string    // org_region_event
	RegionCode     *string   // org_region_event
	PrevRegionCode *string   // org_region_event
}

// jsonArg truyền ảnh chụp JSON vào cột jsonb (nil → NULL).
func jsonArg(b []byte) any {
	if b == nil {
		return nil
	}
	return string(b)
}

func noRows[T any](v T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// ---------------------------------------------------------------------------
// Phòng ban
// ---------------------------------------------------------------------------

// DeptStream là stream_key của một phòng ban.
func DeptStream(id uuid.UUID) string { return "dept:" + id.String() }

// InsertDeptEvent ghi sổ phòng ban; DB tính seq, chuỗi băm.
func InsertDeptEvent(ctx context.Context, tx pgx.Tx, e *KNEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.department_event
	    (id, tenant_id, stream_key, department_id, org_ref, code, action, status, before, after, reason, event_time, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15) RETURNING seq, recorded_at`,
		e.ID, e.TenantID, DeptStream(e.EntityID), e.EntityID, e.OrgRef, e.Code, e.Action, e.Status, jsonArg(e.Before), jsonArg(e.After), e.Reason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.DepartmentRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const deptEventCols = `id, tenant_id, department_id, org_ref, code, action, status, before, after, reason, event_time, actor_ref,
  idem_key, seq, recorded_at`

func scanDeptEvent(row pgx.Row) (KNEvent, error) {
	var e KNEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.EntityID, &e.OrgRef, &e.Code, &e.Action, &e.Status, &e.Before, &e.After, &e.Reason,
		&e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	return e, err
}

// DeptEventByIdem đọc dòng sổ đã ghi với idem_key.
func DeptEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*KNEvent, error) {
	return noRows(scanDeptEvent(tx.QueryRow(ctx, "SELECT "+deptEventCols+" FROM pf_party.department_event WHERE idem_key = $1::text", key)))
}

// DeptEvents trả sổ của một phòng ban, seq > after, tăng dần.
func DeptEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID, after int64, limit int) ([]KNEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+deptEventCols+` FROM pf_party.department_event
	    WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`, DeptStream(id), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KNEvent{}
	for rows.Next() {
		e, err := scanDeptEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const deptCols = `id, org_ref, code, name, coalesce(description, ''), sort_order, status, menu_profile, retired, updated_by, updated_at`

func scanDept(row pgx.Row) (port.Department, error) {
	var d port.Department
	err := row.Scan(&d.ID, &d.OrgRef, &d.Code, &d.Name, &d.Description, &d.SortOrder, &d.Status, &d.MenuProfile, &d.Retired,
		&d.UpdatedBy, &d.UpdatedAt)
	return d, err
}

// DepartmentByID đọc (và khóa nếu lock) một phòng ban.
func DepartmentByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*port.Department, error) {
	q := "SELECT " + deptCols + " FROM pf_party.department WHERE id = $1"
	if lock {
		q += " FOR UPDATE"
	}
	return noRows(scanDept(tx.QueryRow(ctx, q, id)))
}

// DepartmentByCode đọc phòng ban của đơn vị theo mã (kể cả đã "xóa").
func DepartmentByCode(ctx context.Context, tx pgx.Tx, orgRef uuid.UUID, code string) (*port.Department, error) {
	return noRows(scanDept(tx.QueryRow(ctx, "SELECT "+deptCols+" FROM pf_party.department WHERE org_ref = $1 AND code = $2", orgRef, code)))
}

// ListDepartments trả phòng ban của đơn vị theo thứ tự hiển thị.
func ListDepartments(ctx context.Context, tx pgx.Tx, orgRef uuid.UUID, includeRetired bool) ([]port.Department, error) {
	rows, err := tx.Query(ctx, "SELECT "+deptCols+` FROM pf_party.department
	    WHERE org_ref = $1 AND ($2::boolean OR NOT retired) ORDER BY sort_order, code`, orgRef, includeRetired)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.Department{}
	for rows.Next() {
		d, err := scanDept(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListAllDepartments trả phòng ban (chưa "xóa") của mọi đơn vị trong tenant.
func ListAllDepartments(ctx context.Context, tx pgx.Tx) ([]port.Department, error) {
	rows, err := tx.Query(ctx, "SELECT "+deptCols+" FROM pf_party.department WHERE NOT retired ORDER BY sort_order, code, org_ref")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.Department{}
	for rows.Next() {
		d, err := scanDept(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PutDepartment ghi bảng chiếu phòng ban (cùng giao dịch với sổ, R-21).
func PutDepartment(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d port.Department, e KNEvent) error {
	var desc *string
	if d.Description != "" {
		desc = &d.Description
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.department
	    (tenant_id, id, org_ref, code, name, description, sort_order, status, menu_profile, retired, last_event_id, last_seq, updated_by,
	     created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
	  ON CONFLICT (tenant_id, id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
	    sort_order = EXCLUDED.sort_order, status = EXCLUDED.status, menu_profile = EXCLUDED.menu_profile, retired = EXCLUDED.retired,
	    last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		tid, d.ID, d.OrgRef, d.Code, d.Name, desc, d.SortOrder, d.Status, d.MenuProfile, d.Retired, e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}

// ---------------------------------------------------------------------------
// Khu vực
// ---------------------------------------------------------------------------

// RegionStream là stream_key của một khu vực.
func RegionStream(id uuid.UUID) string { return "region:" + id.String() }

// InsertRegionEvent ghi sổ khu vực.
func InsertRegionEvent(ctx context.Context, tx pgx.Tx, e *KNEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.region_event
	    (id, tenant_id, stream_key, region_id, province_code, code, action, before, after, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
		e.ID, e.TenantID, RegionStream(e.EntityID), e.EntityID, e.ProvinceCode, e.Code, e.Action, jsonArg(e.Before), jsonArg(e.After), e.Reason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.RegionRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const regionEventCols = `id, tenant_id, region_id, province_code, code, action, before, after, reason, event_time, actor_ref,
  idem_key, seq, recorded_at`

func scanRegionEvent(row pgx.Row) (KNEvent, error) {
	var e KNEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.EntityID, &e.ProvinceCode, &e.Code, &e.Action, &e.Before, &e.After, &e.Reason, &e.EventTime,
		&e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	return e, err
}

// RegionEventByIdem đọc dòng sổ khu vực đã ghi với idem_key.
func RegionEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*KNEvent, error) {
	return noRows(scanRegionEvent(tx.QueryRow(ctx, "SELECT "+regionEventCols+" FROM pf_party.region_event WHERE idem_key = $1::text", key)))
}

// RegionEvents trả sổ của một khu vực.
func RegionEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID, after int64, limit int) ([]KNEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+regionEventCols+` FROM pf_party.region_event
	    WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`, RegionStream(id), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KNEvent{}
	for rows.Next() {
		e, err := scanRegionEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RegionRow là bảng chiếu khu vực kèm lý do ngừng dùng.
type RegionRow struct {
	port.Region
	DeactivationReason string
	UpdatedBy          uuid.UUID
	UpdatedAt          time.Time
}

const regionCols = `id, province_code, code, name, sort_order, active, coalesce(deactivation_reason, ''), updated_by, updated_at`

func scanRegion(row pgx.Row) (RegionRow, error) {
	var r RegionRow
	err := row.Scan(&r.ID, &r.ProvinceCode, &r.Code, &r.Name, &r.SortOrder, &r.Active, &r.DeactivationReason, &r.UpdatedBy, &r.UpdatedAt)
	return r, err
}

// RegionByID đọc (và khóa nếu lock) một khu vực.
func RegionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*RegionRow, error) {
	q := "SELECT " + regionCols + " FROM pf_party.region WHERE id = $1"
	if lock {
		q += " FOR UPDATE"
	}
	return noRows(scanRegion(tx.QueryRow(ctx, q, id)))
}

// RegionByCode đọc khu vực của tỉnh theo mã.
func RegionByCode(ctx context.Context, tx pgx.Tx, province, code string) (*RegionRow, error) {
	return noRows(scanRegion(tx.QueryRow(ctx, "SELECT "+regionCols+" FROM pf_party.region WHERE province_code = $1 AND code = $2", province, code)))
}

// ListRegions trả khu vực của tỉnh theo thứ tự hiển thị.
func ListRegions(ctx context.Context, tx pgx.Tx, province string, includeInactive bool) ([]RegionRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+regionCols+` FROM pf_party.region
	    WHERE province_code = $1 AND ($2::boolean OR active) ORDER BY sort_order, code`, province, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RegionRow{}
	for rows.Next() {
		r, err := scanRegion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PutRegion ghi bảng chiếu khu vực.
func PutRegion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r RegionRow, e KNEvent) error {
	var reason *string
	if !r.Active {
		reason = &r.DeactivationReason
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.region
	    (tenant_id, id, province_code, code, name, sort_order, active, deactivation_reason, last_event_id, last_seq, updated_by, created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
	  ON CONFLICT (tenant_id, id) DO UPDATE SET name = EXCLUDED.name, sort_order = EXCLUDED.sort_order, active = EXCLUDED.active,
	    deactivation_reason = EXCLUDED.deactivation_reason, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq,
	    updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		tid, r.ID, r.ProvinceCode, r.Code, r.Name, r.SortOrder, r.Active, reason, e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}

// RegionCommuneCounts đếm Nút xã đang hoạt động theo khu vực của tỉnh (khóa "" = chưa gán không có ở đây).
func RegionCommuneCounts(ctx context.Context, tx pgx.Tx, province string) (map[string]int, error) {
	rows, err := tx.Query(ctx, `SELECT r.region_code, count(*) FROM pf_party.org_region r
	    JOIN pf_party.org o ON o.ref = r.org_ref AND o.active AND o.kind = 'extension_team'
	    WHERE r.province_code = $1 AND r.region_code IS NOT NULL GROUP BY r.region_code`, province)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out[code] = n
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Gán khu vực cho Nút xã
// ---------------------------------------------------------------------------

// OrgRegionStream là stream_key gán khu vực của một Nút.
func OrgRegionStream(ref uuid.UUID) string { return "org-region:" + ref.String() }

// InsertOrgRegionEvent ghi sổ gán khu vực.
func InsertOrgRegionEvent(ctx context.Context, tx pgx.Tx, e *KNEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_party.org_region_event
	    (id, tenant_id, stream_key, org_ref, commune_code, province_code, action, region_code, prev_region_code, reason, event_time,
	     actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING seq, recorded_at`,
		e.ID, e.TenantID, OrgRegionStream(e.EntityID), e.EntityID, e.CommuneCode, e.ProvinceCode, e.Action, e.RegionCode, e.PrevRegionCode,
		e.Reason, e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.OrgRegionRuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

const orgRegionEventCols = `id, tenant_id, org_ref, commune_code, province_code, action, region_code, prev_region_code, reason,
  event_time, actor_ref, idem_key, seq, recorded_at`

func scanOrgRegionEvent(row pgx.Row) (KNEvent, error) {
	var e KNEvent
	err := row.Scan(&e.ID, &e.TenantID, &e.EntityID, &e.CommuneCode, &e.ProvinceCode, &e.Action, &e.RegionCode, &e.PrevRegionCode,
		&e.Reason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.RecordedAt)
	return e, err
}

// OrgRegionEventByIdem đọc dòng sổ gán khu vực đã ghi với idem_key.
func OrgRegionEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*KNEvent, error) {
	return noRows(scanOrgRegionEvent(tx.QueryRow(ctx, "SELECT "+orgRegionEventCols+" FROM pf_party.org_region_event WHERE idem_key = $1::text", key)))
}

// OrgRegionEvents trả sổ gán khu vực của một Nút.
func OrgRegionEvents(ctx context.Context, tx pgx.Tx, ref uuid.UUID, after int64, limit int) ([]KNEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+orgRegionEventCols+` FROM pf_party.org_region_event
	    WHERE stream_key = $1 AND seq > $2 ORDER BY seq LIMIT $3`, OrgRegionStream(ref), after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KNEvent{}
	for rows.Next() {
		e, err := scanOrgRegionEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// OrgRegion là bảng chiếu gán khu vực của một Nút xã.
type OrgRegion struct {
	OrgRef       uuid.UUID
	CommuneCode  string
	ProvinceCode string
	RegionCode   *string
}

// OrgRegionByOrg đọc (và khóa nếu lock) gán khu vực của một Nút.
func OrgRegionByOrg(ctx context.Context, tx pgx.Tx, ref uuid.UUID, lock bool) (*OrgRegion, error) {
	q := "SELECT org_ref, commune_code, province_code, region_code FROM pf_party.org_region WHERE org_ref = $1"
	if lock {
		q += " FOR UPDATE"
	}
	var o OrgRegion
	err := tx.QueryRow(ctx, q, ref).Scan(&o.OrgRef, &o.CommuneCode, &o.ProvinceCode, &o.RegionCode)
	return noRows(o, err)
}

// PutOrgRegion ghi bảng chiếu gán khu vực.
func PutOrgRegion(ctx context.Context, tx pgx.Tx, tid uuid.UUID, o OrgRegion, e KNEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_party.org_region (tenant_id, org_ref, commune_code, province_code, region_code, last_event_id, last_seq,
	    updated_by, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	  ON CONFLICT (tenant_id, org_ref) DO UPDATE SET commune_code = EXCLUDED.commune_code, province_code = EXCLUDED.province_code,
	    region_code = EXCLUDED.region_code, last_event_id = EXCLUDED.last_event_id, last_seq = EXCLUDED.last_seq,
	    updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		tid, o.OrgRef, o.CommuneCode, o.ProvinceCode, o.RegionCode, e.ID, e.Seq, e.ActorRef, e.EventTime.UTC())
	return err
}

// CommuneRegions trả khu vực hiện hành của mọi Nút xã (extension_team) đang hoạt động gắn các mã xã,
// khóa theo mã xã. Xã chưa gán có RegionCode rỗng.
func CommuneRegions(ctx context.Context, tx pgx.Tx, communes []string) (map[string]port.CommuneRegion, error) {
	rows, err := tx.Query(ctx, `SELECT o.ref, o.admin_unit_code, coalesce(g.code, ''), coalesce(g.name, ''), coalesce(g.sort_order, 0)
	    FROM pf_party.org o
	    LEFT JOIN pf_party.org_region a ON a.org_ref = o.ref
	    LEFT JOIN pf_party.region g ON g.province_code = a.province_code AND g.code = a.region_code
	    WHERE o.kind = 'extension_team' AND o.active AND o.admin_unit_code = ANY($1::text[])
	    ORDER BY o.ref`, units(communes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]port.CommuneRegion{}
	for rows.Next() {
		var c port.CommuneRegion
		if err := rows.Scan(&c.OrgRef, &c.CommuneCode, &c.RegionCode, &c.RegionName, &c.SortOrder); err != nil {
			return nil, err
		}
		if _, dup := out[c.CommuneCode]; !dup {
			out[c.CommuneCode] = c
		}
	}
	return out, rows.Err()
}

// RegionsOfOrgs trả khu vực (mã, tên) của các Nút xã đã gán, khóa theo org_ref.
func RegionsOfOrgs(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) (map[uuid.UUID]port.CommuneRegion, error) {
	if len(refs) == 0 {
		return map[uuid.UUID]port.CommuneRegion{}, nil
	}
	rows, err := tx.Query(ctx, `SELECT a.org_ref, a.commune_code, a.region_code, coalesce(g.name, ''), coalesce(g.sort_order, 0)
	    FROM pf_party.org_region a
	    LEFT JOIN pf_party.region g ON g.province_code = a.province_code AND g.code = a.region_code
	    WHERE a.org_ref = ANY($1::uuid[]) AND a.region_code IS NOT NULL`, uuidStrings(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]port.CommuneRegion{}
	for rows.Next() {
		var c port.CommuneRegion
		if err := rows.Scan(&c.OrgRef, &c.CommuneCode, &c.RegionCode, &c.RegionName, &c.SortOrder); err != nil {
			return nil, err
		}
		out[c.OrgRef] = c
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Cây 3 cấp
// ---------------------------------------------------------------------------

// TreeOrgs trả mọi Nút của cây khuyến nông 3 cấp (ttkn_national, ttkn_province, extension_team).
func TreeOrgs(ctx context.Context, tx pgx.Tx, includeInactive bool) ([]port.Org, error) {
	rows, err := tx.Query(ctx, "SELECT "+orgCols+` FROM pf_party.org
	    WHERE kind IN ('ttkn_national', 'ttkn_province', 'extension_team') AND ($1::boolean OR active)
	    ORDER BY code, ref`, includeInactive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []port.Org{}
	for rows.Next() {
		o, err := scanOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// SubtreeUnits trả mã đơn vị hành chính (không rỗng) của mọi Nút đang hoạt động trong nhánh root (kể cả root).
func SubtreeUnits(ctx context.Context, tx pgx.Tx, root uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE sub(ref, unit, active) AS (
	    SELECT ref, admin_unit_code, active FROM pf_party.org WHERE ref = $1
	    UNION
	    SELECT o.ref, o.admin_unit_code, o.active FROM pf_party.org o JOIN sub ON o.parent_ref = sub.ref)
	  SELECT DISTINCT unit FROM sub WHERE unit IS NOT NULL AND active ORDER BY unit`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountSubtreeCooperatives đếm HTX đang hoạt động trong nhánh root.
func CountSubtreeCooperatives(ctx context.Context, tx pgx.Tx, root uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `WITH RECURSIVE sub(ref) AS (
	    SELECT ref FROM pf_party.org WHERE ref = $1
	    UNION
	    SELECT o.ref FROM pf_party.org o JOIN sub ON o.parent_ref = sub.ref)
	  SELECT count(*) FROM pf_party.org o JOIN sub ON sub.ref = o.ref WHERE o.kind = 'cooperative' AND o.active`, root).Scan(&n)
	return n, err
}

// ExtensionTeamUnits trả mã xã đã có Nút extension_team (mọi trạng thái) trong các mã cho trước.
func ExtensionTeamUnits(ctx context.Context, tx pgx.Tx, communes []string) (map[string]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT admin_unit_code, ref FROM pf_party.org
	    WHERE kind = 'extension_team' AND admin_unit_code = ANY($1::text[]) ORDER BY active DESC, ref`, units(communes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var u string
		var ref uuid.UUID
		if err := rows.Scan(&u, &ref); err != nil {
			return nil, err
		}
		if _, ok := out[u]; !ok {
			out[u] = ref
		}
	}
	return out, rows.Err()
}
