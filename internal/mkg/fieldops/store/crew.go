package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
)

// CrewStream là khóa chuỗi băm của sổ tổ gom.
func CrewStream(id uuid.UUID) string { return "crewdir:" + id.String() }

// AssignStream là khóa chuỗi băm của sổ phân công.
func AssignStream(orderID uuid.UUID) string { return "assign:" + orderID.String() }

// CrewEvent là một dòng sổ crew_event (một phiên bản đầy đủ của tổ).
type CrewEvent struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	Seq        int64
	CrewID     uuid.UUID
	Kind       string
	Version    int
	Spec       domain.CrewSpec
	Status     string
	Reason     *string
	EventTime  time.Time
	RecordedAt time.Time
	ActorRef   uuid.UUID
	IdemKey    string
}

// InsertCrewEvent ghi sổ tổ gom.
func InsertCrewEvent(ctx context.Context, tx pgx.Tx, e *CrewEvent) error {
	return tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.crew_event
	    (id, tenant_id, stream_key, crew_id, kind, version, code, name, member_refs, capacity_unit, capacity_per_day,
	     service_communes, status, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::uuid[], $10, $11::numeric, $12, $13, $14, $15, $16, $17, $18)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, CrewStream(e.CrewID), e.CrewID, e.Kind, e.Version, e.Spec.Code, e.Spec.Name, uuidStrings(e.Spec.MemberRefs),
		e.Spec.CapacityUnit, e.Spec.CapacityPerDay.String(), e.Spec.ServiceCommunes, e.Status, e.Reason, e.EventTime.UTC(), e.ActorRef,
		e.IdemKey, domain.RuleCrew).Scan(&e.Seq, &e.RecordedAt)
}

const crewEventCols = `id, tenant_id, seq, crew_id, kind, version, code, name, member_refs::text[], capacity_unit, capacity_per_day::text,
  service_communes, status, reason, event_time, recorded_at, actor_ref, idem_key`

func scanCrewEvent(row pgx.Row) (CrewEvent, error) {
	var e CrewEvent
	var members []string
	var capacity string
	err := row.Scan(&e.ID, &e.TenantID, &e.Seq, &e.CrewID, &e.Kind, &e.Version, &e.Spec.Code, &e.Spec.Name, &members,
		&e.Spec.CapacityUnit, &capacity, &e.Spec.ServiceCommunes, &e.Status, &e.Reason, &e.EventTime, &e.RecordedAt, &e.ActorRef, &e.IdemKey)
	if err != nil {
		return e, err
	}
	if e.Spec.MemberRefs, err = parseUUIDs(members); err != nil {
		return e, err
	}
	e.Spec.CapacityPerDay, err = decimal.NewFromString(capacity)
	return e, err
}

// CrewEventByIdem tìm sự kiện tổ gom theo idem_key.
func CrewEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*CrewEvent, error) {
	e, err := scanCrewEvent(tx.QueryRow(ctx, "SELECT "+crewEventCols+" FROM mkg_fieldops.crew_event WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// CrewEvents liệt kê các phiên bản của một tổ.
func CrewEvents(ctx context.Context, tx pgx.Tx, crewID uuid.UUID) ([]CrewEvent, error) {
	rows, err := tx.Query(ctx, "SELECT "+crewEventCols+" FROM mkg_fieldops.crew_event WHERE crew_id = $1 ORDER BY seq", crewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CrewEvent
	for rows.Next() {
		e, err := scanCrewEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Crew là bảng chiếu crew_current.
type Crew struct {
	TenantID  uuid.UUID
	CrewID    uuid.UUID
	Spec      domain.CrewSpec
	Status    string
	Version   int
	LastSeq   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HasMember cho biết actor là thành viên của tổ.
func (c Crew) HasMember(actor uuid.UUID) bool {
	for _, m := range c.Spec.MemberRefs {
		if m == actor {
			return true
		}
	}
	return false
}

const crewDirCols = `tenant_id, crew_id, code, name, member_refs::text[], capacity_unit, capacity_per_day::text, service_communes, status,
  version, last_seq, created_at, updated_at`

func scanCrewDir(row pgx.Row) (Crew, error) {
	var c Crew
	var members []string
	var capacity string
	err := row.Scan(&c.TenantID, &c.CrewID, &c.Spec.Code, &c.Spec.Name, &members, &c.Spec.CapacityUnit, &capacity,
		&c.Spec.ServiceCommunes, &c.Status, &c.Version, &c.LastSeq, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return c, err
	}
	if c.Spec.MemberRefs, err = parseUUIDs(members); err != nil {
		return c, err
	}
	c.Spec.CapacityPerDay, err = decimal.NewFromString(capacity)
	return c, err
}

// GetCrew đọc tổ hiện hành; forUpdate khóa dòng (tuần tự hóa phân công vào cùng tổ — FM-06).
func GetCrew(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (*Crew, error) {
	q := "SELECT " + crewDirCols + " FROM mkg_fieldops.crew_current WHERE crew_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	c, err := scanCrewDir(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CrewCodeTaken cho biết mã tổ đã dùng cho tổ khác trong tenant.
func CrewCodeTaken(ctx context.Context, tx pgx.Tx, code string, except uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM mkg_fieldops.crew_current WHERE code = $1 AND crew_id <> $2)", code, except).Scan(&ok)
	return ok, err
}

// ApplyCrewEvent cập nhật bảng chiếu tổ gom (R-21, cùng giao dịch).
func ApplyCrewEvent(ctx context.Context, tx pgx.Tx, e CrewEvent) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_fieldops.crew_current AS c
	    (tenant_id, crew_id, code, name, member_refs, capacity_unit, capacity_per_day, service_communes, status, version, last_seq, created_at)
	  VALUES ($1, $2, $3, $4, $5::uuid[], $6, $7::numeric, $8, $9, $10, $11, $12)
	  ON CONFLICT (tenant_id, crew_id) DO UPDATE SET
	    code = EXCLUDED.code, name = EXCLUDED.name, member_refs = EXCLUDED.member_refs, capacity_unit = EXCLUDED.capacity_unit,
	    capacity_per_day = EXCLUDED.capacity_per_day, service_communes = EXCLUDED.service_communes, status = EXCLUDED.status,
	    version = EXCLUDED.version, last_seq = EXCLUDED.last_seq, updated_at = now()`,
		e.TenantID, e.CrewID, e.Spec.Code, e.Spec.Name, uuidStrings(e.Spec.MemberRefs), e.Spec.CapacityUnit, e.Spec.CapacityPerDay.String(),
		e.Spec.ServiceCommunes, e.Status, e.Version, e.Seq, e.EventTime.UTC())
	return err
}

// ListCrews liệt kê tổ theo mã (cursor = mã cuối).
func ListCrews(ctx context.Context, tx pgx.Tx, status, after string, limit int) ([]Crew, error) {
	rows, err := tx.Query(ctx, "SELECT "+crewDirCols+` FROM mkg_fieldops.crew_current
	    WHERE ($1 = '' OR status = $1) AND code > $2 ORDER BY code LIMIT $3`, status, after, limit)
	if err != nil {
		return nil, err
	}
	return collectCrews(rows)
}

// ActiveCrews liệt kê mọi tổ đang hoạt động (gợi ý phân công, FM-07/08) theo mã.
func ActiveCrews(ctx context.Context, tx pgx.Tx) ([]Crew, error) {
	rows, err := tx.Query(ctx, "SELECT "+crewDirCols+" FROM mkg_fieldops.crew_current WHERE status = 'active' ORDER BY code")
	if err != nil {
		return nil, err
	}
	return collectCrews(rows)
}

// CrewsOfMember trả mã các tổ mà actor là thành viên (lọc pull của app hiện trường).
func CrewsOfMember(ctx context.Context, tx pgx.Tx, actor uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, "SELECT crew_id FROM mkg_fieldops.crew_current WHERE member_refs @> ARRAY[$1::uuid]", actor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// TouchOrdersOfCrew đẩy sync_seq của các lệnh đang giao cho tổ (thành viên đổi → máy của
// thành viên cũ nhận tombstone, thành viên mới nhận upsert ở lần pull sau).
func TouchOrdersOfCrew(ctx context.Context, tx pgx.Tx, crewID uuid.UUID) error {
	_, err := tx.Exec(ctx, `UPDATE mkg_fieldops.collection_order_current SET sync_seq = nextval('mkg_fieldops.sync_seq'), updated_at = now()
	    WHERE crew_ref = $1 AND status = 'assigned'`, crewID)
	return err
}

func collectCrews(rows pgx.Rows) ([]Crew, error) {
	defer rows.Close()
	var out []Crew
	for rows.Next() {
		c, err := scanCrewDir(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Phân công
// ---------------------------------------------------------------------------

// Assignment là một dòng sổ crew_assignment.
type Assignment struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	Seq            int64
	OrderID        uuid.UUID
	OrderEventID   uuid.UUID
	CrewID         uuid.UUID
	CrewVersion    int
	CapacityUnit   string
	CapacityPerDay decimal.Decimal
	PlannedFrom    time.Time
	PlannedTo      time.Time
	ExpectedQty    decimal.Decimal
	DailyLoad      decimal.Decimal
	PeakLoad       decimal.Decimal
	OverCapacity   bool
	OverrideReason *string
	Mode           string
	SuggestionRank *int
	EventTime      time.Time
	RecordedAt     time.Time
	ActorRef       uuid.UUID
	IdemKey        string
	RuleVersion    string
}

// InsertAssignment ghi sổ phân công và cập nhật bảng chiếu (cùng giao dịch).
func InsertAssignment(ctx context.Context, tx pgx.Tx, a *Assignment) error {
	a.RuleVersion = domain.RuleAssignment
	err := tx.QueryRow(ctx, `INSERT INTO mkg_fieldops.crew_assignment
	    (id, tenant_id, stream_key, order_id, order_event_id, crew_id, crew_version, capacity_unit, capacity_per_day, planned_from,
	     planned_to, expected_qty, daily_load, peak_load, over_capacity, override_reason, mode, suggestion_rank, event_time, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10, $11, $12::numeric, $13::numeric, $14::numeric, $15, $16, $17, $18, $19, $20,
	          $21, $22)
	  RETURNING seq, recorded_at`,
		a.ID, a.TenantID, AssignStream(a.OrderID), a.OrderID, a.OrderEventID, a.CrewID, a.CrewVersion, a.CapacityUnit,
		a.CapacityPerDay.String(), a.PlannedFrom, a.PlannedTo, a.ExpectedQty.String(), a.DailyLoad.String(), a.PeakLoad.String(),
		a.OverCapacity, a.OverrideReason, a.Mode, a.SuggestionRank, a.EventTime.UTC(), a.ActorRef, a.IdemKey, a.RuleVersion,
	).Scan(&a.Seq, &a.RecordedAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_fieldops.crew_assignment_current AS c
	    (tenant_id, order_id, assignment_id, crew_id, crew_version, capacity_unit, planned_from, planned_to, expected_qty, daily_load,
	     over_capacity, override_reason, mode)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::numeric, $10::numeric, $11, $12, $13)
	  ON CONFLICT (tenant_id, order_id) DO UPDATE SET
	    assignment_id = EXCLUDED.assignment_id, crew_id = EXCLUDED.crew_id, crew_version = EXCLUDED.crew_version,
	    capacity_unit = EXCLUDED.capacity_unit, planned_from = EXCLUDED.planned_from, planned_to = EXCLUDED.planned_to,
	    expected_qty = EXCLUDED.expected_qty, daily_load = EXCLUDED.daily_load, over_capacity = EXCLUDED.over_capacity,
	    override_reason = EXCLUDED.override_reason, mode = EXCLUDED.mode, updated_at = now()`,
		a.TenantID, a.OrderID, a.ID, a.CrewID, a.CrewVersion, a.CapacityUnit, a.PlannedFrom, a.PlannedTo, a.ExpectedQty.String(),
		a.DailyLoad.String(), a.OverCapacity, a.OverrideReason, a.Mode)
	return err
}

const assignCols = `id, tenant_id, seq, order_id, order_event_id, crew_id, crew_version, capacity_unit, capacity_per_day::text, planned_from,
  planned_to, expected_qty::text, daily_load::text, peak_load::text, over_capacity, override_reason, mode, suggestion_rank, event_time,
  recorded_at, actor_ref, idem_key, rule_version`

func scanAssignment(row pgx.Row) (Assignment, error) {
	var a Assignment
	var capacity, qty, daily, peak string
	err := row.Scan(&a.ID, &a.TenantID, &a.Seq, &a.OrderID, &a.OrderEventID, &a.CrewID, &a.CrewVersion, &a.CapacityUnit, &capacity,
		&a.PlannedFrom, &a.PlannedTo, &qty, &daily, &peak, &a.OverCapacity, &a.OverrideReason, &a.Mode, &a.SuggestionRank, &a.EventTime,
		&a.RecordedAt, &a.ActorRef, &a.IdemKey, &a.RuleVersion)
	if err != nil {
		return a, err
	}
	for _, x := range []struct {
		s string
		d *decimal.Decimal
	}{{capacity, &a.CapacityPerDay}, {qty, &a.ExpectedQty}, {daily, &a.DailyLoad}, {peak, &a.PeakLoad}} {
		if *x.d, err = decimal.NewFromString(x.s); err != nil {
			return a, err
		}
	}
	return a, nil
}

// AssignmentByIdem tìm phân công theo idem_key.
func AssignmentByIdem(ctx context.Context, tx pgx.Tx, key string) (*Assignment, error) {
	a, err := scanAssignment(tx.QueryRow(ctx, "SELECT "+assignCols+" FROM mkg_fieldops.crew_assignment WHERE idem_key = $1"+ledger.NewestByIdem(""), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// CurrentAssignment đọc phân công hiện hành của lệnh gom (bản cuối của sổ).
func CurrentAssignment(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (*Assignment, error) {
	a, err := scanAssignment(tx.QueryRow(ctx, "SELECT "+assignCols+` FROM mkg_fieldops.crew_assignment
	    WHERE order_id = $1 ORDER BY seq DESC LIMIT 1`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Assignments liệt kê lịch sử phân công của lệnh gom.
func Assignments(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]Assignment, error) {
	rows, err := tx.Query(ctx, "SELECT "+assignCols+" FROM mkg_fieldops.crew_assignment WHERE order_id = $1 ORDER BY seq", orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Assignment
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Bookings trả phân công đang giữ năng lực của các tổ crews trong [from, to]: lệnh gom còn
// assigned với đúng tổ đó, trừ lệnh except. Map theo crew_id.
func Bookings(ctx context.Context, tx pgx.Tx, crews []uuid.UUID, from, to time.Time, except uuid.UUID) (map[uuid.UUID][]domain.Booking, error) {
	rows, err := tx.Query(ctx, `SELECT a.crew_id, a.order_id, a.planned_from, a.planned_to, a.daily_load::text
	    FROM mkg_fieldops.crew_assignment_current a
	    JOIN mkg_fieldops.collection_order_current o ON o.tenant_id = a.tenant_id AND o.order_id = a.order_id
	    WHERE a.crew_id = ANY($1::uuid[]) AND o.status = 'assigned' AND o.crew_ref = a.crew_id
	      AND a.planned_to >= $2 AND a.planned_from <= $3 AND a.order_id <> $4
	    ORDER BY a.crew_id, a.order_id`, uuidStrings(crews), from, to, except)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID][]domain.Booking{}
	for rows.Next() {
		var crew uuid.UUID
		var b domain.Booking
		var daily string
		if err := rows.Scan(&crew, &b.OrderID, &b.From, &b.To, &daily); err != nil {
			return nil, err
		}
		if b.DailyLoad, err = decimal.NewFromString(daily); err != nil {
			return nil, err
		}
		out[crew] = append(out[crew], b)
	}
	return out, rows.Err()
}
