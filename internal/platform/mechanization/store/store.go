// Package store là SQL của pf/mechanization, chỉ chạy trên pool của vai
// mechanization_app (R-05) và luôn trong giao dịch có ngữ cảnh tenant.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// Tên ràng buộc duy nhất để app quy về mã lỗi nghiệp vụ.
const (
	ConstraintSerial  = "machine_serial_unique"
	ConstraintChassis = "machine_chassis_unique"
	ConstraintPerson  = "owner_person_unique"
	ConstraintCoop    = "owner_coop_unique"
)

const nilUUID = "'00000000-0000-0000-0000-000000000000'::uuid"

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

// date chuẩn hóa ngày về 00:00 UTC để cột date nhận đúng ngày nghiệp vụ.
func date(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func datePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := date(*t)
	return &d
}

// uuids chuyển danh sách UUID thành []string (cast ::uuid[] trong SQL) — mã hóa được ở mọi chế độ thực thi của pgx.
func uuids(u []uuid.UUID) []string {
	out := make([]string, 0, len(u))
	for _, x := range u {
		out = append(out, x.String())
	}
	return out
}

func units(u []string) []string {
	if u == nil {
		return []string{}
	}
	return u
}

// ---------------------------------------------------------------------------
// Bộ đếm mã
// ---------------------------------------------------------------------------

// NextCode cấp mã hiển thị tiếp theo cho (tiền tố, nhãn tỉnh) — an toàn khi chạy song song.
func NextCode(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, prefix, province string) (string, error) {
	var n int
	err := tx.QueryRow(ctx, `INSERT INTO pf_mechanization.code_counter (tenant_id, prefix, province_label, last_value)
	  VALUES ($1, $2, $3, 1)
	  ON CONFLICT (tenant_id, prefix, province_label) DO UPDATE SET last_value = pf_mechanization.code_counter.last_value + 1
	  RETURNING last_value`, tenantID, prefix, province).Scan(&n)
	if err != nil {
		return "", err
	}
	return domain.FormatCode(prefix, province, n), nil
}

// ---------------------------------------------------------------------------
// Chủ sở hữu
// ---------------------------------------------------------------------------

const ownerCols = `id, code, owner_type, coalesce(cooperative_ref, ` + nilUUID + `), coalesce(person_ref, ` + nilUUID + `),
  coalesce(display_name, ''), active, coalesce(status_reason, ''), last_seq`

func scanOwner(row pgx.Row) (domain.Owner, error) {
	var o domain.Owner
	var t string
	err := row.Scan(&o.ID, &o.Code, &t, &o.CooperativeRef, &o.PersonRef, &o.DisplayName, &o.Active, &o.StatusReason, &o.LastSeq)
	o.Type = domain.OwnerType(t)
	return o, err
}

// OwnerByID đọc chủ sở hữu; forUpdate khóa dòng.
func OwnerByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*domain.Owner, error) {
	q := "SELECT " + ownerCols + " FROM pf_mechanization.owner WHERE id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	o, err := scanOwner(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}

// OwnerByCode đọc chủ sở hữu theo Mã CSH (nhập Excel).
func OwnerByCode(ctx context.Context, tx pgx.Tx, code string) (*domain.Owner, error) {
	o, err := scanOwner(tx.QueryRow(ctx, "SELECT "+ownerCols+" FROM pf_mechanization.owner WHERE code = $1", code))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}

// OwnerByPerson tìm chủ sở hữu gắn mã tham chiếu kho định danh (chống trùng theo SĐT).
func OwnerByPerson(ctx context.Context, tx pgx.Tx, person uuid.UUID) (*domain.Owner, error) {
	o, err := scanOwner(tx.QueryRow(ctx, "SELECT "+ownerCols+" FROM pf_mechanization.owner WHERE person_ref = $1", person))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}

// OwnerOfCoop tìm hồ sơ chủ sở hữu loại "HTX" của một HTX.
func OwnerOfCoop(ctx context.Context, tx pgx.Tx, coop uuid.UUID) (*domain.Owner, error) {
	o, err := scanOwner(tx.QueryRow(ctx, "SELECT "+ownerCols+" FROM pf_mechanization.owner WHERE owner_type = 'htx' AND cooperative_ref = $1", coop))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &o, err
}

const ownerEventCols = `id, tenant_id, owner_id, action, code, owner_type, coalesce(cooperative_ref, ` + nilUUID + `),
  coalesce(person_ref, ` + nilUUID + `), coalesce(display_name, ''), active, coalesce(reason, ''), machines_affected,
  event_time, actor_ref, idem_key, seq, recorded_at`

func scanOwnerEvent(row pgx.Row) (domain.OwnerEvent, error) {
	var e domain.OwnerEvent
	var action, t string
	err := row.Scan(&e.ID, &e.TenantID, &e.Owner.ID, &action, &e.Owner.Code, &t, &e.Owner.CooperativeRef, &e.Owner.PersonRef,
		&e.Owner.DisplayName, &e.Owner.Active, &e.Reason, &e.MachinesAffected, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.Recorded)
	e.Action, e.Owner.Type = domain.OwnerAction(action), domain.OwnerType(t)
	return e, err
}

// InsertOwnerEvent ghi sổ; DB tính seq, prev_hash, row_hash (shared.fn_chain).
func InsertOwnerEvent(ctx context.Context, tx pgx.Tx, e *domain.OwnerEvent) error {
	o := e.Owner
	return tx.QueryRow(ctx, `INSERT INTO pf_mechanization.owner_event
	    (id, tenant_id, stream_key, owner_id, action, code, owner_type, cooperative_ref, person_ref, display_name, active,
	     reason, machines_affected, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.OwnerStreamKey(o.ID), o.ID, string(e.Action), o.Code, string(o.Type), nilIfZero(o.CooperativeRef),
		nilIfZero(o.PersonRef), nilIfEmpty(o.DisplayName), o.Active, nilIfEmpty(e.Reason), e.MachinesAffected, e.EventTime.UTC(),
		e.ActorRef, e.IdemKey, domain.OwnerRuleVersion).Scan(&e.Seq, &e.Recorded)
}

// OwnerEventByIdem tìm dòng sổ đã ghi với idem_key (gọi lặp).
func OwnerEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.OwnerEvent, error) {
	e, err := scanOwnerEvent(tx.QueryRow(ctx, "SELECT "+ownerEventCols+" FROM pf_mechanization.owner_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// OwnerEvents trả lịch sử biến động của chủ sở hữu theo seq (FN-04 B7).
func OwnerEvents(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]domain.OwnerEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+ownerEventCols+" FROM pf_mechanization.owner_event WHERE stream_key = $1 ORDER BY seq",
		domain.OwnerStreamKey(id))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.OwnerEvent, error) { return scanOwnerEvent(r) })
}

// InsertOwner ghi trạng thái hiện tại của chủ sở hữu mới.
func InsertOwner(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, o domain.Owner) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_mechanization.owner
	    (tenant_id, id, code, owner_type, cooperative_ref, person_ref, display_name, active, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tenantID, o.ID, o.Code, string(o.Type), nilIfZero(o.CooperativeRef), nilIfZero(o.PersonRef), nilIfEmpty(o.DisplayName),
		o.Active, o.LastSeq)
	return err
}

// UpdateOwnerStatus ghi trạng thái sau vô hiệu hóa/kích hoạt lại (R-21).
func UpdateOwnerStatus(ctx context.Context, tx pgx.Tx, o domain.Owner) error {
	_, err := tx.Exec(ctx, `UPDATE pf_mechanization.owner SET active = $2, status_reason = $3, last_seq = $4, updated_at = now()
	  WHERE id = $1`, o.ID, o.Active, nilIfEmpty(o.StatusReason), o.LastSeq)
	return err
}

// OwnerFilter là điều kiện liệt kê chủ sở hữu.
type OwnerFilter struct {
	CooperativeRef  uuid.UUID // Nil = mọi HTX
	Type            string
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
}

// OwnerRow là chủ sở hữu kèm số máy đang hoạt động (FN-04 B1: "Số máy sở hữu").
type OwnerRow struct {
	domain.Owner
	Machines int
}

// ListOwners liệt kê chủ sở hữu, phân trang cursor (R-53).
func ListOwners(ctx context.Context, tx pgx.Tx, f OwnerFilter) ([]OwnerRow, error) {
	rows, err := tx.Query(ctx, `SELECT o.id, o.code, o.owner_type, coalesce(o.cooperative_ref, `+nilUUID+`),
	    coalesce(o.person_ref, `+nilUUID+`), coalesce(o.display_name, ''), o.active, coalesce(o.status_reason, ''), o.last_seq,
	    (SELECT count(*) FROM pf_mechanization.machine m WHERE m.owner_id = o.id AND m.active)
	  FROM pf_mechanization.owner o
	  WHERE ($1 = `+nilUUID+` OR o.cooperative_ref = $1) AND ($2 = '' OR o.owner_type = $2)
	    AND ($3::boolean OR o.active) AND o.id > $4
	  ORDER BY o.id LIMIT $5`, f.CooperativeRef, f.Type, f.IncludeInactive, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (OwnerRow, error) {
		var o OwnerRow
		var t string
		err := r.Scan(&o.ID, &o.Code, &t, &o.CooperativeRef, &o.PersonRef, &o.DisplayName, &o.Active, &o.StatusReason, &o.LastSeq, &o.Machines)
		o.Type = domain.OwnerType(t)
		return o, err
	})
}

// OwnersByIDs đọc nhiều chủ sở hữu một lần (panel HTX).
func OwnersByIDs(ctx context.Context, tx pgx.Tx, idList []uuid.UUID) (map[uuid.UUID]domain.Owner, error) {
	rows, err := tx.Query(ctx, "SELECT "+ownerCols+" FROM pf_mechanization.owner WHERE id = ANY($1::uuid[])", uuids(idList))
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Owner, error) { return scanOwner(r) })
	out := map[uuid.UUID]domain.Owner{}
	for _, o := range list {
		out[o.ID] = o
	}
	return out, err
}

// ---------------------------------------------------------------------------
// Máy
// ---------------------------------------------------------------------------

const machineCols = `ref, code, machine_type, farm_stage, cooperative_ref, owner_id, coalesce(brand, ''), coalesce(model, ''),
  coalesce(serial_no, ''), coalesce(chassis_no, ''), year_made, coalesce(fuel, ''), power_hp::text, capacity_ha::text,
  owned_since, status, status_source, status_at, active, deactivated_on, coalesce(status_reason, ''), last_seq, tenant_id`

func scanMachine(row pgx.Row) (domain.Machine, error) {
	var m domain.Machine
	var status string
	var power, capacity *string
	err := row.Scan(&m.Ref, &m.Code, &m.MachineType, &m.FarmStage, &m.CooperativeRef, &m.OwnerID, &m.Brand, &m.Model, &m.SerialNo,
		&m.ChassisNo, &m.YearMade, &m.Fuel, &power, &capacity, &m.OwnedSince, &status, &m.StatusSource, &m.StatusAt, &m.Active,
		&m.DeactivatedOn, &m.StatusReason, &m.LastSeq, &m.TenantID)
	if err != nil {
		return m, err
	}
	m.Status = domain.MachineStatus(status)
	if m.PowerHP, err = parseDec(power); err != nil {
		return m, err
	}
	m.CapacityHa, err = parseDec(capacity)
	return m, err
}

func oneMachine(ctx context.Context, tx pgx.Tx, where string, arg any) (*domain.Machine, error) {
	m, err := scanMachine(tx.QueryRow(ctx, "SELECT "+machineCols+" FROM pf_mechanization.machine WHERE "+where, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

// MachineByRef đọc máy; forUpdate khóa dòng.
func MachineByRef(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*domain.Machine, error) {
	w := "ref = $1"
	if forUpdate {
		w += " FOR UPDATE"
	}
	return oneMachine(ctx, tx, w, ref)
}

// MachineBySerial tìm máy theo Số máy/SN (đã chuẩn hóa).
func MachineBySerial(ctx context.Context, tx pgx.Tx, serial string) (*domain.Machine, error) {
	return oneMachine(ctx, tx, "serial_no = $1 FOR UPDATE", serial)
}

// MachineByChassis tìm máy theo Số khung (đã chuẩn hóa).
func MachineByChassis(ctx context.Context, tx pgx.Tx, chassis string) (*domain.Machine, error) {
	return oneMachine(ctx, tx, "chassis_no = $1 FOR UPDATE", chassis)
}

const machineEventCols = `id, tenant_id, machine_ref, action, source, code, machine_type, farm_stage, cooperative_ref, owner_id,
  coalesce(brand, ''), coalesce(model, ''), coalesce(serial_no, ''), coalesce(chassis_no, ''), year_made, coalesce(fuel, ''),
  power_hp::text, capacity_ha::text, owned_since, status, active, deactivated_on, coalesce(reason, ''), event_time, actor_ref,
  idem_key, seq, recorded_at`

func scanMachineEvent(row pgx.Row) (domain.MachineEvent, error) {
	var e domain.MachineEvent
	var action, status string
	var power, capacity *string
	m := &e.Machine
	err := row.Scan(&e.ID, &e.TenantID, &m.Ref, &action, &e.Source, &m.Code, &m.MachineType, &m.FarmStage, &m.CooperativeRef,
		&m.OwnerID, &m.Brand, &m.Model, &m.SerialNo, &m.ChassisNo, &m.YearMade, &m.Fuel, &power, &capacity, &m.OwnedSince, &status,
		&m.Active, &m.DeactivatedOn, &e.Reason, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.Seq, &e.Recorded)
	if err != nil {
		return e, err
	}
	e.Action, m.Status = domain.MachineAction(action), domain.MachineStatus(status)
	if m.PowerHP, err = parseDec(power); err != nil {
		return e, err
	}
	m.CapacityHa, err = parseDec(capacity)
	return e, err
}

// InsertMachineEvent ghi sổ máy (ảnh chụp đầy đủ sau sự việc).
func InsertMachineEvent(ctx context.Context, tx pgx.Tx, e *domain.MachineEvent) error {
	m := e.Machine
	return tx.QueryRow(ctx, `INSERT INTO pf_mechanization.machine_event
	    (id, tenant_id, stream_key, machine_ref, action, source, code, machine_type, farm_stage, cooperative_ref, owner_id,
	     brand, model, serial_no, chassis_no, year_made, fuel, power_hp, capacity_ha, owned_since, status, active,
	     deactivated_on, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18::numeric, $19::numeric, $20, $21,
	          $22, $23, $24, $25, $26, $27, $28)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, domain.MachineStreamKey(m.Ref), m.Ref, string(e.Action), e.Source, m.Code, m.MachineType, m.FarmStage,
		m.CooperativeRef, m.OwnerID, nilIfEmpty(m.Brand), nilIfEmpty(m.Model), nilIfEmpty(m.SerialNo), nilIfEmpty(m.ChassisNo),
		m.YearMade, nilIfEmpty(m.Fuel), decPtr(m.PowerHP), decPtr(m.CapacityHa), date(m.OwnedSince), string(m.Status), m.Active,
		datePtr(m.DeactivatedOn), nilIfEmpty(e.Reason), e.EventTime.UTC(), e.ActorRef, e.IdemKey, domain.MachineRuleVersion,
	).Scan(&e.Seq, &e.Recorded)
}

// MachineEventByIdem tìm dòng sổ máy đã ghi với idem_key.
func MachineEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*domain.MachineEvent, error) {
	e, err := scanMachineEvent(tx.QueryRow(ctx, "SELECT "+machineEventCols+" FROM pf_mechanization.machine_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &e, err
}

// MachineEvents trả Lịch sử biến động của máy (FN-05 B8).
func MachineEvents(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]domain.MachineEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+machineEventCols+" FROM pf_mechanization.machine_event WHERE stream_key = $1 ORDER BY seq",
		domain.MachineStreamKey(ref))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.MachineEvent, error) { return scanMachineEvent(r) })
}

// InsertMachine ghi trạng thái hiện tại của máy mới.
func InsertMachine(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, m domain.Machine) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_mechanization.machine
	    (tenant_id, ref, code, machine_type, farm_stage, cooperative_ref, owner_id, brand, model, serial_no, chassis_no, year_made,
	     fuel, power_hp, capacity_ha, owned_since, status, status_source, status_at, active, deactivated_on, status_reason, last_seq)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::numeric, $15::numeric, $16, $17, $18, $19, $20, $21, $22, $23)`,
		tenantID, m.Ref, m.Code, m.MachineType, m.FarmStage, m.CooperativeRef, m.OwnerID, nilIfEmpty(m.Brand), nilIfEmpty(m.Model),
		nilIfEmpty(m.SerialNo), nilIfEmpty(m.ChassisNo), m.YearMade, nilIfEmpty(m.Fuel), decPtr(m.PowerHP), decPtr(m.CapacityHa),
		date(m.OwnedSince), string(m.Status), m.StatusSource, m.StatusAt.UTC(), m.Active, datePtr(m.DeactivatedOn),
		nilIfEmpty(m.StatusReason), m.LastSeq)
	return err
}

// UpdateMachine ghi trạng thái sau một biến động (R-21: cùng giao dịch với sổ). Mã không đổi.
func UpdateMachine(ctx context.Context, tx pgx.Tx, m domain.Machine) error {
	_, err := tx.Exec(ctx, `UPDATE pf_mechanization.machine SET machine_type = $2, farm_stage = $3, cooperative_ref = $4,
	    owner_id = $5, brand = $6, model = $7, serial_no = $8, chassis_no = $9, year_made = $10, fuel = $11,
	    power_hp = $12::numeric, capacity_ha = $13::numeric, owned_since = $14, status = $15, status_source = $16, status_at = $17,
	    active = $18, deactivated_on = $19, status_reason = $20, last_seq = $21, updated_at = now()
	  WHERE ref = $1`,
		m.Ref, m.MachineType, m.FarmStage, m.CooperativeRef, m.OwnerID, nilIfEmpty(m.Brand), nilIfEmpty(m.Model),
		nilIfEmpty(m.SerialNo), nilIfEmpty(m.ChassisNo), m.YearMade, nilIfEmpty(m.Fuel), decPtr(m.PowerHP), decPtr(m.CapacityHa),
		date(m.OwnedSince), string(m.Status), m.StatusSource, m.StatusAt.UTC(), m.Active, datePtr(m.DeactivatedOn),
		nilIfEmpty(m.StatusReason), m.LastSeq)
	return err
}

// MachineFilter là điều kiện liệt kê máy.
type MachineFilter struct {
	CooperativeRef  uuid.UUID
	OwnerID         uuid.UUID
	FarmStage       string
	MachineType     string
	Status          string
	IncludeInactive bool
	After           uuid.UUID
	Limit           int
}

// ListMachines liệt kê máy, phân trang cursor theo ref (R-53).
func ListMachines(ctx context.Context, tx pgx.Tx, f MachineFilter) ([]domain.Machine, error) {
	rows, err := tx.Query(ctx, "SELECT "+machineCols+` FROM pf_mechanization.machine
	  WHERE ($1 = `+nilUUID+` OR cooperative_ref = $1) AND ($2 = `+nilUUID+` OR owner_id = $2)
	    AND ($3 = '' OR farm_stage = $3) AND ($4 = '' OR machine_type = $4) AND ($5 = '' OR status = $5)
	    AND ($6::boolean OR active) AND ref > $7
	  ORDER BY ref LIMIT $8`,
		f.CooperativeRef, f.OwnerID, f.FarmStage, f.MachineType, f.Status, f.IncludeInactive, f.After, f.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Machine, error) { return scanMachine(r) })
}

// ActiveMachines trả máy đang hoạt động (chưa vô hiệu hóa) của một HTX hoặc một chủ sở hữu, khóa dòng.
func ActiveMachines(ctx context.Context, tx pgx.Tx, coop, owner uuid.UUID) ([]domain.Machine, error) {
	rows, err := tx.Query(ctx, "SELECT "+machineCols+` FROM pf_mechanization.machine
	  WHERE active AND ($1 = `+nilUUID+` OR cooperative_ref = $1) AND ($2 = `+nilUUID+` OR owner_id = $2)
	  ORDER BY ref FOR UPDATE`, coop, owner)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (domain.Machine, error) { return scanMachine(r) })
}

// CountRow là số máy của một HTX × chủng loại tại thời điểm T.
type CountRow struct {
	CooperativeRef uuid.UUID
	MachineType    string
	FarmStage      string
	Total          int            // máy thuộc HTX tại T (FN-05 BR-09)
	ByStatus       map[string]int // theo tình trạng tại T
}

// CountsAt đếm máy tại ngày day theo FN-05 BR-09/BR-11: Ngày HTX sở hữu ≤ T và
// (chưa vô hiệu hóa hoặc Ngày vô hiệu hóa > T). HTX, chủng loại và tình trạng
// lấy từ ảnh chụp trong sổ có hiệu lực tại T (dòng event_time < until mới
// nhất; chưa có thì dòng đầu tiên) — máy chuyển HTX được tính đúng nơi tại T.
// coops rỗng = mọi HTX.
func CountsAt(ctx context.Context, tx pgx.Tx, day, until time.Time, coops []uuid.UUID) ([]CountRow, error) {
	rows, err := tx.Query(ctx, `WITH snap AS (
	    SELECT s.cooperative_ref, s.machine_type, s.farm_stage, s.status
	    FROM pf_mechanization.machine m
	    CROSS JOIN LATERAL (
	      SELECT e.cooperative_ref, e.machine_type, e.farm_stage, e.status
	      FROM pf_mechanization.machine_event e
	      WHERE e.machine_ref = m.ref
	      ORDER BY (e.event_time < $2) DESC,
	               CASE WHEN e.event_time < $2 THEN e.event_time END DESC NULLS LAST,
	               CASE WHEN e.event_time < $2 THEN e.seq END DESC NULLS LAST,
	               e.seq ASC
	      LIMIT 1) s
	    WHERE m.owned_since <= $1 AND (m.deactivated_on IS NULL OR m.deactivated_on > $1))
	  SELECT cooperative_ref, machine_type, farm_stage, status, count(*)
	  FROM snap
	  WHERE cardinality($3::uuid[]) = 0 OR cooperative_ref = ANY($3::uuid[])
	  GROUP BY cooperative_ref, machine_type, farm_stage, status
	  ORDER BY cooperative_ref, machine_type, status`, date(day), until.UTC(), uuids(coops))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct {
		coop uuid.UUID
		typ  string
	}
	idx := map[key]int{}
	var out []CountRow
	for rows.Next() {
		var coop uuid.UUID
		var typ, stage, status string
		var n int
		if err := rows.Scan(&coop, &typ, &stage, &status, &n); err != nil {
			return nil, err
		}
		k := key{coop, typ}
		i, ok := idx[k]
		if !ok {
			out = append(out, CountRow{CooperativeRef: coop, MachineType: typ, FarmStage: stage, ByStatus: map[string]int{}})
			i = len(out) - 1
			idx[k] = i
		}
		out[i].Total += n
		out[i].ByStatus[status] += n
	}
	return out, rows.Err()
}

// MachineCoops trả mọi HTX từng có máy (đầu vào của cân đối: HTX có máy nhưng chưa có diện tích → "Chưa có dữ liệu").
func MachineCoops(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT DISTINCT cooperative_ref FROM pf_mechanization.machine ORDER BY 1")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// CountActive đếm máy chưa vô hiệu hóa (thẻ "Tổng máy" của dashboard), theo danh sách HTX (rỗng = mọi HTX).
func CountActive(ctx context.Context, tx pgx.Tx, coops []uuid.UUID) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM pf_mechanization.machine
	  WHERE active AND (cardinality($1::uuid[]) = 0 OR cooperative_ref = ANY($1::uuid[]))`, uuids(coops)).Scan(&n)
	return n, err
}
