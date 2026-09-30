// Package store: SQL của mkg/logistics (schema mkg_logistics, vai logistics_app). Mọi hàm chạy
// trong giao dịch tenant đã mở ở app/ (RLS lọc theo tenant — RB-2).
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// Cargo là một lô trên chuyến (jsonb).
type Cargo struct {
	LotRef    uuid.UUID  `json:"lot_ref"`
	MassKg    mass.Kg    `json:"mass_kg"`
	TicketRef *uuid.UUID `json:"ticket_ref,omitempty"`
}

// TripEvent là một dòng sổ trip_event.
type TripEvent struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Seq                int64
	TripID             uuid.UUID
	Kind               string
	Status             port.TripStatus
	Code               string
	Mode               port.Mode
	VehicleRef         *uuid.UUID
	DriverRef          *uuid.UUID
	CollectionOrderRef *uuid.UUID
	DeliveryRef        *uuid.UUID
	PlannedKg          *mass.Kg
	ActualKg           *mass.Kg
	ActualCost         *money.Amount
	CostSource         *port.CostSource
	Cargo              []Cargo
	Lat, Lng           *decimal.Decimal
	AccuracyM          *decimal.Decimal // không ở trip_event: sổ trip_milestone_fix (mkg 000020)
	CompletedAt        *time.Time
	Reason             *string
	Flags              []string
	SourceRef          *uuid.UUID
	ParamVersion       *string
	EventTime          time.Time
	RecordedAt         time.Time
	SupersedesID       *uuid.UUID
	CorrectionReason   *string
	ActorRef           uuid.UUID
	DeviceID           *string
	IdemKey            string
	RuleVersion        string
}

// Kinds của sổ chuyến.
const (
	KindPlanned         = "planned"
	KindRevised         = "revised"
	KindVehicleAssigned = "vehicle_assigned"
	KindDeparted        = "departed"
	KindArrived         = "arrived"
	KindCompleted       = "completed"
	KindCancelled       = "cancelled"
)

func decStr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func kgStr(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func amtStr(a *money.Amount) *string {
	if a == nil {
		return nil
	}
	s := a.String()
	return &s
}

func cargoJSON(c []Cargo) ([]byte, error) {
	if c == nil {
		c = []Cargo{}
	}
	return json.Marshal(c)
}

func flags(f []string) []string {
	if f == nil {
		return []string{}
	}
	return f
}

// InsertTripEvent ghi sổ chuyến (seq, băm do DB tính).
func InsertTripEvent(ctx context.Context, tx pgx.Tx, e *TripEvent) error {
	cargo, err := cargoJSON(e.Cargo)
	if err != nil {
		return err
	}
	var cs *string
	if e.CostSource != nil {
		s := string(*e.CostSource)
		cs = &s
	}
	return tx.QueryRow(ctx, `INSERT INTO mkg_logistics.trip_event
	    (id, tenant_id, stream_key, trip_id, kind, status, code, mode, vehicle_ref, driver_ref, collection_order_ref, delivery_ref,
	     planned_kg, actual_kg, actual_cost, cost_source, cargo, lat, lng, completed_at, reason, flags, source_ref, param_version,
	     event_time, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14::numeric, $15::numeric, $16, $17, $18::numeric,
	          $19::numeric, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31)
	  RETURNING seq, recorded_at`,
		e.ID, e.TenantID, "trip:"+e.TripID.String(), e.TripID, e.Kind, string(e.Status), e.Code, string(e.Mode), e.VehicleRef, e.DriverRef,
		e.CollectionOrderRef, e.DeliveryRef, kgStr(e.PlannedKg), kgStr(e.ActualKg), amtStr(e.ActualCost), cs, string(cargo), decStr(e.Lat),
		decStr(e.Lng), utcPtr(e.CompletedAt), e.Reason, flags(e.Flags), e.SourceRef, e.ParamVersion, e.EventTime.UTC(), e.SupersedesID,
		e.CorrectionReason, e.ActorRef, e.DeviceID, e.IdemKey, e.RuleVersion).Scan(&e.Seq, &e.RecordedAt)
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

const tripEventCols = `id, tenant_id, seq, trip_id, kind, status, code, mode, vehicle_ref, driver_ref, collection_order_ref, delivery_ref,
  planned_kg::text, actual_kg::text, actual_cost::text, cost_source, cargo, lat::text, lng::text, completed_at, reason, flags, source_ref,
  param_version, event_time, recorded_at, supersedes_id, correction_reason, actor_ref, device_id, idem_key, rule_version`

func parseKg(s *string) (*mass.Kg, error) {
	if s == nil {
		return nil, nil
	}
	k, err := mass.ParseKg(*s)
	return &k, err
}

func parseAmt(s *string) (*money.Amount, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	a, err := money.ParseAmount(d.StringFixed(2))
	return &a, err
}

func parseDec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	return &d, err
}

func scanTripEvent(row pgx.Row) (TripEvent, error) {
	var e TripEvent
	var status, mode string
	var cs, pk, ak, ac, lat, lng *string
	var cargo []byte
	err := row.Scan(&e.ID, &e.TenantID, &e.Seq, &e.TripID, &e.Kind, &status, &e.Code, &mode, &e.VehicleRef, &e.DriverRef,
		&e.CollectionOrderRef, &e.DeliveryRef, &pk, &ak, &ac, &cs, &cargo, &lat, &lng, &e.CompletedAt, &e.Reason, &e.Flags, &e.SourceRef,
		&e.ParamVersion, &e.EventTime, &e.RecordedAt, &e.SupersedesID, &e.CorrectionReason, &e.ActorRef, &e.DeviceID, &e.IdemKey,
		&e.RuleVersion)
	if err != nil {
		return e, err
	}
	e.Status, e.Mode = port.TripStatus(status), port.Mode(mode)
	if cs != nil {
		c := port.CostSource(*cs)
		e.CostSource = &c
	}
	if e.PlannedKg, err = parseKg(pk); err != nil {
		return e, err
	}
	if e.ActualKg, err = parseKg(ak); err != nil {
		return e, err
	}
	if e.ActualCost, err = parseAmt(ac); err != nil {
		return e, err
	}
	if e.Lat, err = parseDec(lat); err != nil {
		return e, err
	}
	if e.Lng, err = parseDec(lng); err != nil {
		return e, err
	}
	err = json.Unmarshal(cargo, &e.Cargo)
	return e, err
}

func queryTripEvents(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]TripEvent, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TripEvent
	for rows.Next() {
		e, err := scanTripEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TripEventByIdem tìm sự kiện chuyến theo idem_key.
func TripEventByIdem(ctx context.Context, tx pgx.Tx, key string) (*TripEvent, error) {
	e, err := scanTripEvent(tx.QueryRow(ctx, "SELECT "+tripEventCols+" FROM mkg_logistics.trip_event WHERE idem_key = $1", key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// TripEventByID đọc một sự kiện chuyến.
func TripEventByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*TripEvent, error) {
	e, err := scanTripEvent(tx.QueryRow(ctx, "SELECT "+tripEventCols+" FROM mkg_logistics.trip_event WHERE id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// TripEvents đọc toàn bộ sổ của một chuyến theo seq.
func TripEvents(ctx context.Context, tx pgx.Tx, trip uuid.UUID) ([]TripEvent, error) {
	return queryTripEvents(ctx, tx, "SELECT "+tripEventCols+" FROM mkg_logistics.trip_event WHERE trip_id = $1 ORDER BY seq", trip)
}

// LatestCompletions đọc bản ghi completed mới nhất (đã tính bản bù) của mỗi chuyến có recorded_at < asOf và
// completed_at ∈ [from, to), sắp theo (completed_at, trip_id), sau con trỏ (afterAt, afterID).
func LatestCompletions(ctx context.Context, tx pgx.Tx, from, to, asOf time.Time, afterAt *time.Time, afterID uuid.UUID, limit int) ([]TripEvent, error) {
	return queryTripEvents(ctx, tx, `SELECT `+tripEventCols+` FROM (
	    SELECT DISTINCT ON (trip_id) * FROM mkg_logistics.trip_event
	     WHERE kind = 'completed' AND recorded_at < $3
	     ORDER BY trip_id, recorded_at DESC, seq DESC) t
	  WHERE completed_at >= $1 AND completed_at < $2
	    AND ($4::timestamptz IS NULL OR (completed_at, trip_id) > ($4, $5))
	  ORDER BY completed_at, trip_id LIMIT $6`, from.UTC(), to.UTC(), asOf.UTC(), utcPtr(afterAt), afterID, limit)
}

// LockTrip tuần tự hóa ghi một chuyến trong tenant.
func LockTrip(ctx context.Context, tx pgx.Tx, tid, trip uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "mkg_logistics.trip:"+tid.String()+":"+trip.String())
	return err
}

// NextTripNo cấp số mã chuyến kế tiếp của tenant (khóa tư vấn theo tenant).
func NextTripNo(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (int64, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", "mkg_logistics.tripcode:"+tid.String()); err != nil {
		return 0, err
	}
	var n int64
	err := tx.QueryRow(ctx, "SELECT count(*) + 1 FROM mkg_logistics.trip_current").Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Bảng chiếu trip_current
// ---------------------------------------------------------------------------

// Trip là một dòng trip_current.
type Trip struct {
	TenantID           uuid.UUID
	TripID             uuid.UUID
	Code               string
	Status             port.TripStatus
	Mode               port.Mode
	VehicleRef         *uuid.UUID
	DriverRef          *uuid.UUID
	CollectionOrderRef *uuid.UUID
	DeliveryRef        *uuid.UUID
	Cargo              []Cargo
	PlannedKg          *mass.Kg
	ActualKg           *mass.Kg
	PlannedCost        *money.Amount
	ActualCost         *money.Amount
	CostSource         *port.CostSource
	PlanRevision       int
	PlannedAt          time.Time
	DepartedAt         *time.Time
	ArrivedAt          *time.Time
	CompletedAt        *time.Time
	CancelledAt        *time.Time
	CancelReason       *string
	CompletionEventID  *uuid.UUID
	TicketRef          *uuid.UUID
	SourceRef          *uuid.UUID
	Flags              []string
	RuleVersion        string
	LastSeq            int64
	RecordedAt         time.Time
}

// Fold áp một sự kiện sổ lên trạng thái chuyến (cur nil = chưa có). Dùng cho cả ghi trực tiếp và
// dựng lại bảng chiếu (R-22) — cùng một hàm nên dựng lại = trạng thái sống.
func Fold(cur *Trip, e TripEvent) Trip {
	var t Trip
	if cur != nil {
		t = *cur
	}
	t.TenantID, t.TripID, t.Code, t.Mode, t.Status = e.TenantID, e.TripID, e.Code, e.Mode, e.Status
	t.LastSeq, t.RuleVersion = e.Seq, e.RuleVersion
	if e.RecordedAt.After(t.RecordedAt) {
		t.RecordedAt = e.RecordedAt
	}
	switch e.Kind {
	case KindPlanned:
		t.PlannedAt, t.SourceRef = e.EventTime, e.SourceRef
		t.CollectionOrderRef, t.DeliveryRef = e.CollectionOrderRef, e.DeliveryRef
		t.VehicleRef, t.DriverRef, t.Cargo, t.PlannedKg = e.VehicleRef, e.DriverRef, e.Cargo, e.PlannedKg
		t.Flags = e.Flags
	case KindRevised:
		t.Cargo, t.PlannedKg, t.Flags = e.Cargo, e.PlannedKg, e.Flags
		if e.SourceRef != nil {
			t.SourceRef = e.SourceRef
		}
	case KindVehicleAssigned:
		t.VehicleRef, t.DriverRef, t.Flags = e.VehicleRef, e.DriverRef, e.Flags
	case KindDeparted:
		at := e.EventTime
		t.DepartedAt = &at
	case KindArrived:
		at := e.EventTime
		t.ArrivedAt = &at
	case KindCompleted:
		t.ActualKg, t.ActualCost, t.CostSource, t.CompletedAt = e.ActualKg, e.ActualCost, e.CostSource, e.CompletedAt
		t.Cargo, t.Flags = e.Cargo, e.Flags
		id := e.ID
		t.CompletionEventID = &id
		t.TicketRef = nil
		for _, c := range e.Cargo {
			if c.TicketRef != nil {
				r := *c.TicketRef
				t.TicketRef = &r
			}
		}
	case KindCancelled:
		at := e.EventTime
		t.CancelledAt, t.CancelReason = &at, e.Reason
	}
	return t
}

const tripCols = `tenant_id, trip_id, code, status, mode, vehicle_ref, driver_ref, collection_order_ref, delivery_ref, cargo,
  planned_kg::text, actual_kg::text, planned_cost::text, actual_cost::text, cost_source, plan_revision, planned_at, departed_at,
  arrived_at, completed_at, cancelled_at, cancel_reason, completion_event_id, ticket_ref, source_ref, flags, rule_version, last_seq,
  recorded_at`

func scanTrip(row pgx.Row) (Trip, error) {
	var t Trip
	var status, mode string
	var cargo []byte
	var pk, ak, pc, ac, cs *string
	err := row.Scan(&t.TenantID, &t.TripID, &t.Code, &status, &mode, &t.VehicleRef, &t.DriverRef, &t.CollectionOrderRef, &t.DeliveryRef,
		&cargo, &pk, &ak, &pc, &ac, &cs, &t.PlanRevision, &t.PlannedAt, &t.DepartedAt, &t.ArrivedAt, &t.CompletedAt, &t.CancelledAt,
		&t.CancelReason, &t.CompletionEventID, &t.TicketRef, &t.SourceRef, &t.Flags, &t.RuleVersion, &t.LastSeq, &t.RecordedAt)
	if err != nil {
		return t, err
	}
	t.Status, t.Mode = port.TripStatus(status), port.Mode(mode)
	if cs != nil {
		c := port.CostSource(*cs)
		t.CostSource = &c
	}
	if t.PlannedKg, err = parseKg(pk); err != nil {
		return t, err
	}
	if t.ActualKg, err = parseKg(ak); err != nil {
		return t, err
	}
	if t.PlannedCost, err = parseAmt(pc); err != nil {
		return t, err
	}
	if t.ActualCost, err = parseAmt(ac); err != nil {
		return t, err
	}
	err = json.Unmarshal(cargo, &t.Cargo)
	return t, err
}

func queryTrips(ctx context.Context, tx pgx.Tx, q string, args ...any) ([]Trip, error) {
	rows, err := tx.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trip
	for rows.Next() {
		t, err := scanTrip(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTrip đọc chuyến; forUpdate khóa dòng. nil = không có.
func GetTrip(ctx context.Context, tx pgx.Tx, trip uuid.UUID, forUpdate bool) (*Trip, error) {
	q := "SELECT " + tripCols + " FROM mkg_logistics.trip_current WHERE trip_id = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	t, err := scanTrip(tx.QueryRow(ctx, q, trip))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TripBySource đọc chuyến sinh từ bản ghi nguồn (roll_count_id — FM-05).
func TripBySource(ctx context.Context, tx pgx.Tx, source uuid.UUID) (*Trip, error) {
	t, err := scanTrip(tx.QueryRow(ctx, "SELECT "+tripCols+" FROM mkg_logistics.trip_current WHERE source_ref = $1 ORDER BY planned_at LIMIT 1", source))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TripsForLot đọc chuyến chở lô lot (trong cargo) hoặc sinh từ lệnh gom lot, cũ nhất trước.
func TripsForLot(ctx context.Context, tx pgx.Tx, lot uuid.UUID) ([]Trip, error) {
	probe, _ := json.Marshal([]map[string]string{{"lot_ref": lot.String()}})
	return queryTrips(ctx, tx, "SELECT "+tripCols+` FROM mkg_logistics.trip_current
	    WHERE cargo @> $1::jsonb OR collection_order_ref = $2 ORDER BY planned_at, trip_id`, string(probe), lot)
}

// TripsWithTicket đọc chuyến đã hoàn tất bằng phiếu cân ticket.
func TripsWithTicket(ctx context.Context, tx pgx.Tx, ticket uuid.UUID) ([]Trip, error) {
	return queryTrips(ctx, tx, "SELECT "+tripCols+" FROM mkg_logistics.trip_current WHERE ticket_ref = $1 ORDER BY planned_at, trip_id", ticket)
}

// ListTrips liệt kê chuyến (lọc trạng thái nếu có), sắp (planned_at DESC, trip_id DESC) sau con trỏ.
func ListTrips(ctx context.Context, tx pgx.Tx, status string, afterAt *time.Time, afterID uuid.UUID, limit int) ([]Trip, error) {
	return queryTrips(ctx, tx, "SELECT "+tripCols+` FROM mkg_logistics.trip_current
	    WHERE ($1 = '' OR status = $1) AND ($2::timestamptz IS NULL OR (planned_at, trip_id) < ($2, $3))
	    ORDER BY planned_at DESC, trip_id DESC LIMIT $4`, status, utcPtr(afterAt), afterID, limit)
}

// UnweighedTrips liệt kê chuyến sinh từ xuống ghe (source_ref) chưa hoàn tất / hủy, lập trước before.
func UnweighedTrips(ctx context.Context, tx pgx.Tx, before time.Time) ([]Trip, error) {
	return queryTrips(ctx, tx, "SELECT "+tripCols+` FROM mkg_logistics.trip_current
	    WHERE status IN ('planned', 'departed', 'arrived') AND source_ref IS NOT NULL AND planned_at < $1
	    ORDER BY planned_at, trip_id LIMIT 500`, before.UTC())
}

// AllTrips đọc mọi dòng trip_current (so sánh dựng lại).
func AllTrips(ctx context.Context, tx pgx.Tx) ([]Trip, error) {
	return queryTrips(ctx, tx, "SELECT "+tripCols+" FROM mkg_logistics.trip_current ORDER BY trip_id")
}

// UpsertTrip ghi bảng chiếu chuyến (trigger fn_trip_forward chặn lùi trạng thái).
func UpsertTrip(ctx context.Context, tx pgx.Tx, t Trip) error {
	cargo, err := cargoJSON(t.Cargo)
	if err != nil {
		return err
	}
	var cs *string
	if t.CostSource != nil {
		s := string(*t.CostSource)
		cs = &s
	}
	_, err = tx.Exec(ctx, `INSERT INTO mkg_logistics.trip_current
	    (tenant_id, trip_id, code, status, mode, vehicle_ref, driver_ref, collection_order_ref, delivery_ref, cargo, planned_kg, actual_kg,
	     planned_cost, actual_cost, cost_source, plan_revision, planned_at, departed_at, arrived_at, completed_at, cancelled_at, cancel_reason,
	     completion_event_id, ticket_ref, source_ref, flags, rule_version, last_seq, recorded_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::numeric, $12::numeric, $13::numeric, $14::numeric, $15, $16, $17, $18, $19,
	          $20, $21, $22, $23, $24, $25, $26, $27, $28, $29)
	  ON CONFLICT (tenant_id, trip_id) DO UPDATE SET
	    code = EXCLUDED.code, status = EXCLUDED.status, mode = EXCLUDED.mode, vehicle_ref = EXCLUDED.vehicle_ref,
	    driver_ref = EXCLUDED.driver_ref, collection_order_ref = EXCLUDED.collection_order_ref, delivery_ref = EXCLUDED.delivery_ref,
	    cargo = EXCLUDED.cargo, planned_kg = EXCLUDED.planned_kg, actual_kg = EXCLUDED.actual_kg, planned_cost = EXCLUDED.planned_cost,
	    actual_cost = EXCLUDED.actual_cost, cost_source = EXCLUDED.cost_source, plan_revision = EXCLUDED.plan_revision,
	    planned_at = EXCLUDED.planned_at, departed_at = EXCLUDED.departed_at, arrived_at = EXCLUDED.arrived_at,
	    completed_at = EXCLUDED.completed_at, cancelled_at = EXCLUDED.cancelled_at, cancel_reason = EXCLUDED.cancel_reason,
	    completion_event_id = EXCLUDED.completion_event_id, ticket_ref = EXCLUDED.ticket_ref, source_ref = EXCLUDED.source_ref,
	    flags = EXCLUDED.flags, rule_version = EXCLUDED.rule_version, last_seq = EXCLUDED.last_seq, recorded_at = EXCLUDED.recorded_at,
	    updated_at = now()`,
		t.TenantID, t.TripID, t.Code, string(t.Status), string(t.Mode), t.VehicleRef, t.DriverRef, t.CollectionOrderRef, t.DeliveryRef, string(cargo),
		kgStr(t.PlannedKg), kgStr(t.ActualKg), amtStr(t.PlannedCost), amtStr(t.ActualCost), cs, t.PlanRevision, t.PlannedAt.UTC(),
		utcPtr(t.DepartedAt), utcPtr(t.ArrivedAt), utcPtr(t.CompletedAt), utcPtr(t.CancelledAt), t.CancelReason, t.CompletionEventID,
		t.TicketRef, t.SourceRef, flags(t.Flags), t.RuleVersion, t.LastSeq, t.RecordedAt.UTC())
	return err
}

// RebuildTrips dựng lại trip_current của tenant hiện tại thẳng từ sổ trip_event + leg_event (R-22).
func RebuildTrips(ctx context.Context, tx pgx.Tx) (int64, error) {
	if _, err := tx.Exec(ctx, "DELETE FROM mkg_logistics.trip_current"); err != nil {
		return 0, err
	}
	events, err := queryTripEvents(ctx, tx, "SELECT "+tripEventCols+" FROM mkg_logistics.trip_event ORDER BY trip_id, seq")
	if err != nil {
		return 0, err
	}
	var n int64
	var cur *Trip
	flush := func() error {
		if cur == nil {
			return nil
		}
		plan, err := LatestPlan(ctx, tx, cur.TripID, nil)
		if err != nil {
			return err
		}
		ApplyPlan(cur, plan)
		n++
		return UpsertTrip(ctx, tx, *cur)
	}
	for _, e := range events {
		if cur != nil && cur.TripID != e.TripID {
			if err := flush(); err != nil {
				return n, err
			}
			cur = nil
		}
		t := Fold(cur, e)
		cur = &t
	}
	return n, flush()
}

// ApplyPlan ghi phiên bản kế hoạch chặng hiệu lực (Σ cước kế hoạch) vào trạng thái chuyến.
func ApplyPlan(t *Trip, plan []Leg) {
	if len(plan) == 0 {
		return
	}
	sum := decimal.Zero
	for _, l := range plan {
		sum = sum.Add(l.PlannedCost.Decimal())
		if l.RecordedAt.After(t.RecordedAt) {
			t.RecordedAt = l.RecordedAt
		}
	}
	a, err := money.ParseAmount(sum.StringFixed(2))
	if err == nil {
		t.PlannedCost = &a
	}
	t.PlanRevision = plan[0].PlanRevision
	// Có chặng → bỏ cờ no_route (cờ khác giữ nguyên thứ tự đã sắp).
	kept := make([]string, 0, len(t.Flags))
	for _, f := range t.Flags {
		if f != "no_route" {
			kept = append(kept, f)
		}
	}
	t.Flags = kept
}
