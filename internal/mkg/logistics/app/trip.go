package app

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// TripResult là kết quả một thao tác chuyến.
type TripResult struct {
	Trip  store.Trip
	Legs  []store.Leg
	Event store.TripEvent
}

// vehicleFlags là các cờ phụ thuộc phương tiện (tính lại mỗi lần gán / sửa tấn ước).
var vehicleFlags = []string{domain.FlagNoVehicle, domain.FlagVesselUnregistered, domain.FlagVesselInactive,
	domain.FlagRegistrationExpire, domain.FlagRegistrationSoon, domain.FlagOverload}

// mergeFlags bỏ remove khỏi cur rồi thêm add; kết quả sắp, không trùng.
func mergeFlags(cur, remove, add []string) []string {
	set := map[string]bool{}
	for _, f := range cur {
		set[f] = true
	}
	for _, f := range remove {
		delete(set, f)
	}
	for _, f := range add {
		if f != "" {
			set[f] = true
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// checkVehicle trả cờ cảnh báo phương tiện (proto FM-05, GH-02): ngoài danh mục, ngừng, hết hạn / sắp
// hết hạn đăng kiểm, tấn ước > hệ số × tải rơm thực chở. Chỉ cảnh báo, không chặn.
func checkVehicle(ctx context.Context, tx pgx.Tx, vehicle *uuid.UUID, planned *mass.Kg, at time.Time) ([]string, *store.Vessel, error) {
	if vehicle == nil {
		return []string{domain.FlagNoVehicle}, nil, nil
	}
	v, err := store.GetVessel(ctx, tx, *vehicle, false)
	if err != nil {
		return nil, nil, err
	}
	if v == nil {
		return []string{domain.FlagVesselUnregistered}, nil, nil
	}
	var out []string
	if v.Status != "active" {
		out = append(out, domain.FlagVesselInactive)
	}
	warn, err := param(ctx, tx, port.ParamExpiryWarnDays, at)
	if err != nil {
		return nil, nil, err
	}
	state, _ := domain.ExpiryStatus(v.RegistrationExpiry, day(at), int(warn.Value.IntPart()))
	out = append(out, domain.ExpiryFlag(state))
	if planned != nil && v.VehicleKind != domain.KindTruck {
		payload := v.StrawPayloadTons
		if payload == nil && v.VehicleKind == domain.KindBoat {
			p, err := param(ctx, tx, port.ParamStrawPayloadTons, at)
			if err != nil {
				return nil, nil, err
			}
			payload = &p.Value
		}
		if payload != nil {
			f, err := param(ctx, tx, port.ParamPayloadWarnFactor, at)
			if err != nil {
				return nil, nil, err
			}
			if domain.Overloaded(*planned, *payload, f.Value) {
				out = append(out, domain.FlagOverload)
			}
		}
	}
	return out, v, nil
}

// appendEvent ghi một sự kiện chuyến, áp bảng chiếu cùng giao dịch (R-21) và — với completed — phát
// mkg.logistics.trip.completed (sự kiện nội bộ, outbox.KindInternal).
func appendEvent(ctx context.Context, tx pgx.Tx, cur *store.Trip, e *store.TripEvent) (store.Trip, error) {
	if err := insert(store.InsertTripEvent(ctx, tx, e)); err != nil {
		return store.Trip{}, err
	}
	t := store.Fold(cur, *e)
	if err := store.UpsertTrip(ctx, tx, t); err != nil {
		return store.Trip{}, mapErr(err)
	}
	switch e.Kind {
	case store.KindDeparted, store.KindArrived, store.KindCompleted, store.KindCancelled:
	default:
		return t, nil
	}
	legs, err := store.LatestPlan(ctx, tx, t.TripID, nil)
	if err != nil {
		return t, err
	}
	v, err := tripVessel(ctx, tx, t.VehicleRef)
	if err != nil {
		return t, err
	}
	if e.Kind == store.KindCompleted {
		if err := enqueueCompleted(ctx, tx, e.TenantID, completedPayload(t, legs, *e, v)); err != nil {
			return t, err
		}
	}
	// proto FM-05: thông báo hàng đến mang mã chuyến cho cơ sở đích (mkg.hub.trip_milestone).
	return t, enqueueMilestone(ctx, tx, t, legs, v, e.Kind, e.ID, e.EventTime)
}

// destinationFacility trả cơ sở đích của phiên bản kế hoạch (chặng cuối to_kind = facility); nil nếu đích không
// phải cơ sở (khách, điểm) hoặc chuyến chưa có chặng (FM-05 lập từ xuống ghe khi chưa xác định đích).
func destinationFacility(legs []store.Leg) *uuid.UUID {
	if len(legs) == 0 {
		return nil
	}
	l := legs[len(legs)-1]
	if l.ToKind != "facility" || l.ToRef == nil {
		return nil
	}
	r := *l.ToRef
	return &r
}

// enqueueMilestone phát mkg.logistics.trip.milestone_recorded (nội bộ) khi chuyến có đích là cơ sở: hub ghi thông
// báo hàng đến (cho_den → da_den) mang mã chuyến. Không cước, không tài xế (R-04, mkg.md §3.1).
func enqueueMilestone(ctx context.Context, tx pgx.Tx, t store.Trip, legs []store.Leg, v *port.TripVesselRefV1, milestone string,
	source uuid.UUID, at time.Time) error {
	fac := destinationFacility(legs)
	if fac == nil {
		return nil
	}
	lots := make([]port.TripMilestoneLotV1, 0, len(t.Cargo))
	for _, c := range t.Cargo {
		lots = append(lots, port.TripMilestoneLotV1{LotRef: c.LotRef, MassKg: c.MassKg})
	}
	payload := port.TripMilestoneRecordedV1{
		TripID: t.TripID, TripCode: t.Code, Milestone: milestone, Status: t.Status, FacilityRef: *fac, Mode: mainMode(t, legs),
		CollectionOrderRef: t.CollectionOrderRef, Lots: lots, PlannedKg: t.PlannedKg, SourceEventRef: source, EventTime: at.UTC(),
	}
	if v != nil {
		code := v.Code
		payload.VesselCode = &code
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: t.TenantID, Type: port.EventTripMilestoneRecorded, SchemaVersion: 1, OccurredAt: payload.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: payload, Kind: outbox.KindInternal,
	})
}

func tripVessel(ctx context.Context, tx pgx.Tx, ref *uuid.UUID) (*port.TripVesselRefV1, error) {
	if ref == nil {
		return nil, nil
	}
	v, err := store.GetVessel(ctx, tx, *ref, false)
	if err != nil || v == nil {
		return nil, err
	}
	return &port.TripVesselRefV1{VesselRef: v.VesselID, Code: v.Code, Kind: v.VehicleKind}, nil
}

func enqueueCompleted(ctx context.Context, tx pgx.Tx, tid uuid.UUID, payload port.TripCompletedV1) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventTripCompleted, SchemaVersion: 1, OccurredAt: payload.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: payload, Kind: outbox.KindInternal,
	})
}

// mainMode là phương thức của chặng dài nhất (không có chặng → phương thức của chuyến).
func mainMode(t store.Trip, legs []store.Leg) port.Mode {
	m, best := t.Mode, decimal.NewFromInt(-1)
	for _, l := range legs {
		if l.DistanceKm.GreaterThan(best) {
			m, best = l.Mode, l.DistanceKm
		}
	}
	return m
}

func completedPayload(t store.Trip, legs []store.Leg, e store.TripEvent, v *port.TripVesselRefV1) port.TripCompletedV1 {
	p := port.TripCompletedV1{
		TripID: t.TripID, Mode: mainMode(t, legs), VehicleRef: t.VehicleRef, CollectionOrderRef: t.CollectionOrderRef,
		DeliveryRef: t.DeliveryRef, PlannedKg: t.PlannedKg, ActualKg: *t.ActualKg, ActualCost: *t.ActualCost, CostSource: *t.CostSource,
		DepartedAt: t.PlannedAt.UTC(), ArrivedAt: e.EventTime.UTC(), RuleVersion: domain.RuleTripCost, SupersedesID: e.SupersedesID,
		EventTime: e.EventTime.UTC(), Vessel: v, Legs: []port.TripLegV1{}, Cargo: []port.TripCargoV1{},
	}
	if t.DepartedAt != nil {
		p.DepartedAt = t.DepartedAt.UTC()
	}
	if t.ArrivedAt != nil {
		p.ArrivedAt = t.ArrivedAt.UTC()
	}
	for _, l := range legs {
		p.Legs = append(p.Legs, port.TripLegV1{LegID: l.LegID, Seq: l.LegSeq, Mode: l.Mode, FromKind: l.FromKind, FromRef: l.FromRef,
			ToKind: l.ToKind, ToRef: l.ToRef, DistanceKm: l.DistanceKm.StringFixed(3), RouteSource: l.RouteSource,
			RouteSnapshotRef: l.SnapshotID, NetworkVersion: l.NetworkVersion, MassKg: nil})
	}
	for _, c := range t.Cargo {
		p.Cargo = append(p.Cargo, port.TripCargoV1{LotRef: c.LotRef, MassKg: c.MassKg, TicketRef: c.TicketRef})
	}
	return p
}

// ---------------------------------------------------------------------------
// Lập chuyến (điều phối)
// ---------------------------------------------------------------------------

// CargoInput là một lô trên chuyến.
type CargoInput struct {
	LotRef uuid.UUID
	MassKg mass.Kg
}

// CreateTripInput là yêu cầu lập chuyến.
type CreateTripInput struct {
	Mode               port.Mode
	VehicleRef         *uuid.UUID
	DriverRef          *uuid.UUID
	CollectionOrderRef *uuid.UUID
	DeliveryRef        *uuid.UUID
	Cargo              []CargoInput
	PlannedKg          *mass.Kg
	EventTime          time.Time
	IdemKey            string
}

func sumCargo(c []store.Cargo) mass.Kg {
	s := mass.MustKg("0")
	for _, x := range c {
		s = s.Add(x.MassKg)
	}
	return s
}

// newTrip lập chuyến planned trong tx (dùng chung cho điều phối và consumer FM-05).
func newTrip(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, e store.TripEvent) (TripResult, error) {
	n, err := store.NextTripNo(ctx, tx, tid)
	if err != nil {
		return TripResult{}, err
	}
	e.ID, e.TenantID, e.TripID, e.Kind, e.Status, e.Code = ids.New(), tid, ids.New(), store.KindPlanned, port.TripPlanned, domain.TripCode(n)
	e.ActorRef, e.RuleVersion = actor, domain.RuleTrip
	vf, _, err := checkVehicle(ctx, tx, e.VehicleRef, e.PlannedKg, e.EventTime)
	if err != nil {
		return TripResult{}, err
	}
	e.Flags = mergeFlags(e.Flags, nil, append(vf, domain.FlagNoRoute))
	t, err := appendEvent(ctx, tx, nil, &e)
	if err != nil {
		return TripResult{}, err
	}
	return TripResult{Trip: t, Event: e}, nil
}

// CreateTrip lập chuyến planned (điều phối). Tấn kế hoạch mặc định = Σ khối lượng lô.
func (s *Service) CreateTrip(ctx context.Context, in CreateTripInput) (TripResult, bool, error) {
	p, err := authz.Require(ctx, RoleDispatcher)
	if err != nil {
		return TripResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return TripResult{}, false, err
	}
	if in.Mode != port.ModeRoad && in.Mode != port.ModeWaterway {
		return TripResult{}, false, problem.Validation.WithField("mode", "road | waterway")
	}
	cargo := make([]store.Cargo, 0, len(in.Cargo))
	for _, c := range in.Cargo {
		cargo = append(cargo, store.Cargo{LotRef: c.LotRef, MassKg: c.MassKg})
	}
	planned := in.PlannedKg
	if planned == nil {
		k := sumCargo(cargo)
		planned = &k
	}
	now := s.Clock.Now()
	at := in.EventTime
	if at.IsZero() {
		at = now
	}
	var res TripResult
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		r, err := newTrip(ctx, tx, p.TenantID, p.ActorRef, store.TripEvent{
			Mode: in.Mode, VehicleRef: in.VehicleRef, DriverRef: in.DriverRef, CollectionOrderRef: in.CollectionOrderRef,
			DeliveryRef: in.DeliveryRef, PlannedKg: planned, Cargo: cargo, EventTime: at, IdemKey: in.IdemKey,
		})
		res = r
		return err
	}, s.replayTrip(ctx, in.IdemKey, &res))
	return res, replayed, err
}

func (s *Service) replayTrip(ctx context.Context, idem string, res *TripResult) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		e, err := store.TripEventByIdem(ctx, tx, idem)
		if err != nil || e == nil {
			return false, err
		}
		t, err := store.GetTrip(ctx, tx, e.TripID, false)
		if err != nil || t == nil {
			return false, err
		}
		legs, err := store.LatestPlan(ctx, tx, t.TripID, nil)
		if err != nil {
			return false, err
		}
		*res = TripResult{Trip: *t, Legs: legs, Event: *e}
		return true, nil
	}
}

// loadOpen khóa và đọc chuyến.
func loadTrip(ctx context.Context, tx pgx.Tx, tid, trip uuid.UUID) (*store.Trip, error) {
	if err := store.LockTrip(ctx, tx, tid, trip); err != nil {
		return nil, err
	}
	t, err := store.GetTrip(ctx, tx, trip, true)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTripNotFound
	}
	return t, nil
}

// base dựng sự kiện kế tiếp mang trạng thái hiện hành của chuyến.
func base(t *store.Trip, kind string, status port.TripStatus, actor uuid.UUID, at time.Time, idem string) store.TripEvent {
	return store.TripEvent{
		ID: ids.New(), TenantID: t.TenantID, TripID: t.TripID, Kind: kind, Status: status, Code: t.Code, Mode: t.Mode,
		VehicleRef: t.VehicleRef, DriverRef: t.DriverRef, CollectionOrderRef: t.CollectionOrderRef, DeliveryRef: t.DeliveryRef,
		PlannedKg: t.PlannedKg, Cargo: t.Cargo, Flags: t.Flags, EventTime: at, ActorRef: actor, IdemKey: idem, RuleVersion: domain.RuleTrip,
	}
}

func (s *Service) tripOp(ctx context.Context, roles []string, trip uuid.UUID, idem string,
	fn func(tx pgx.Tx, p authz.Principal, cur *store.Trip) (*store.TripEvent, error)) (TripResult, bool, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return TripResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return TripResult{}, false, err
	}
	var res TripResult
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := loadTrip(ctx, tx, p.TenantID, trip)
		if err != nil {
			return err
		}
		e, err := fn(tx, p, cur)
		if err != nil {
			return err
		}
		t, err := appendEvent(ctx, tx, cur, e)
		if err != nil {
			return err
		}
		legs, err := store.LatestPlan(ctx, tx, trip, nil)
		if err != nil {
			return err
		}
		res = TripResult{Trip: t, Legs: legs, Event: *e}
		return nil
	}, s.replayTrip(ctx, idem, &res))
	if err == nil && replayed && res.Trip.TripID != trip {
		return TripResult{}, false, problem.IdempotencyConflict // khóa đã dùng cho chuyến khác
	}
	return res, replayed, err
}

// AssignVehicle gán phương tiện / tài xế (điều phối) cho chuyến planned|departed; cảnh báo ghe ngoài
// danh mục, ngừng, hết hạn đăng kiểm, quá tải (chỉ cảnh báo — FM-05, GH-02).
func (s *Service) AssignVehicle(ctx context.Context, trip, vehicle uuid.UUID, driver *uuid.UUID, idem string) (TripResult, bool, error) {
	return s.tripOp(ctx, []string{RoleDispatcher}, trip, idem, func(tx pgx.Tx, p authz.Principal, cur *store.Trip) (*store.TripEvent, error) {
		if cur.Status != port.TripPlanned && cur.Status != port.TripDeparted {
			return nil, ErrTripClosed.WithDetail("chỉ gán phương tiện cho chuyến planned / departed")
		}
		at := s.Clock.Now()
		e := base(cur, store.KindVehicleAssigned, cur.Status, p.ActorRef, at, idem)
		e.VehicleRef, e.DriverRef = &vehicle, driver
		vf, _, err := checkVehicle(ctx, tx, &vehicle, cur.PlannedKg, at)
		if err != nil {
			return nil, err
		}
		e.Flags = mergeFlags(cur.Flags, vehicleFlags, vf)
		return &e, nil
	})
}

// MilestoneInput là mốc chuyến của tài xế. Tọa độ tùy chọn (máy không có GPS → nil, lưu NULL, không bao giờ
// 0,0); AccuracyM tùy chọn (sổ trip_milestone_fix).
type MilestoneInput struct {
	Kind      string // departed | arrived
	EventTime time.Time
	Lat, Lng  *decimal.Decimal
	AccuracyM *decimal.Decimal
	DeviceID  *string
	IdemKey   string
}

// RecordMilestone ghi mốc xuất phát / đến của tài xế (GPS + event_time), từng bậc (L-02).
func (s *Service) RecordMilestone(ctx context.Context, trip uuid.UUID, in MilestoneInput) (TripResult, bool, error) {
	var target port.TripStatus
	switch in.Kind {
	case store.KindDeparted:
		target = port.TripDeparted
	case store.KindArrived:
		target = port.TripArrived
	default:
		return TripResult{}, false, problem.Validation.WithField("kind", "departed | arrived")
	}
	if in.EventTime.IsZero() {
		return TripResult{}, false, problem.Validation.WithField("event_time", "bắt buộc, RFC 3339")
	}
	if in.EventTime.After(s.Clock.Now().Add(5 * time.Minute)) {
		return TripResult{}, false, problem.Validation.WithField("event_time", "không được ở tương lai")
	}
	if (in.Lat == nil) != (in.Lng == nil) {
		return TripResult{}, false, problem.Validation.WithField("lng", "gửi cả lat và lng, hoặc bỏ cả hai (không có GPS)")
	}
	if in.Lat != nil && !domain.ValidPoint(port.GeoPoint{Lat: *in.Lat, Lng: *in.Lng}) {
		return TripResult{}, false, problem.Validation.WithField("lat", "tọa độ WGS84")
	}
	if in.AccuracyM != nil && (in.AccuracyM.Sign() < 0 || in.Lat == nil || in.AccuracyM.GreaterThan(decimal.NewFromInt(9_999_999))) {
		return TripResult{}, false, problem.Validation.WithField("accuracy_m", "mét ≥ 0, chỉ kèm tọa độ")
	}
	return s.tripOp(ctx, []string{RoleDriver, RoleDispatcher}, trip, in.IdemKey, func(tx pgx.Tx, p authz.Principal, cur *store.Trip) (*store.TripEvent, error) {
		if !p.Has(RoleDispatcher) && !assignedTo(cur, p) {
			return nil, ErrNotAssignedDriver // G4 tích hợp mục 9: chỉ tài xế được giao (hoặc điều phối)
		}
		if err := domain.CanTransition(cur.Status, target); err != nil {
			return nil, err
		}
		e := base(cur, in.Kind, target, p.ActorRef, in.EventTime, in.IdemKey)
		e.DeviceID = in.DeviceID
		if in.Lat != nil {
			lat, lng := in.Lat.Round(7), in.Lng.Round(7)
			e.Lat, e.Lng = &lat, &lng
		}
		if in.AccuracyM != nil {
			a := in.AccuracyM.Round(2)
			e.AccuracyM = &a
			if err := store.InsertMilestoneFix(ctx, tx, e, a); err != nil {
				return nil, err
			}
		}
		return &e, nil
	})
}

// CancelTrip hủy chuyến planned|departed có lý do (điều phối).
func (s *Service) CancelTrip(ctx context.Context, trip uuid.UUID, reason, idem string) (TripResult, bool, error) {
	r, err := domain.CheckReason("reason", reason, true)
	if err != nil {
		return TripResult{}, false, mapErr(err)
	}
	return s.tripOp(ctx, []string{RoleDispatcher}, trip, idem, func(_ pgx.Tx, p authz.Principal, cur *store.Trip) (*store.TripEvent, error) {
		if err := domain.CanTransition(cur.Status, port.TripCancelled); err != nil {
			return nil, err
		}
		e := base(cur, store.KindCancelled, port.TripCancelled, p.ActorRef, s.Clock.Now(), idem)
		e.Reason = &r
		return &e, nil
	})
}

// ---------------------------------------------------------------------------
// Hoàn tất chuyến: tấn thật + cước thực (proto completeTrip, GH-03)
// ---------------------------------------------------------------------------

// costFor tính cước thực: đơn giá thuê ghe (GH-03) nếu phương tiện có, không thì tấn thật × km × đơn giá
// tham số theo phương thức của từng chặng (#22/#23) tại thời điểm sự việc. Không có chặng → 0 + cờ no_route.
func costFor(ctx context.Context, tx pgx.Tx, t *store.Trip, actual mass.Kg, at time.Time) (money.Amount, port.CostSource, string, []string, error) {
	legs, err := store.LatestPlan(ctx, tx, t.TripID, nil)
	if err != nil {
		return money.Amount{}, "", "", nil, err
	}
	total := decimal.Zero
	for _, l := range legs {
		total = total.Add(l.DistanceKm)
	}
	if t.VehicleRef != nil {
		v, err := store.GetVessel(ctx, tx, *t.VehicleRef, false)
		if err != nil {
			return money.Amount{}, "", "", nil, err
		}
		if v != nil && v.RateType != nil {
			if a, ok := domain.VesselCost(*v.RateType, v.RateVND, actual, total); ok {
				return a, port.CostByVesselRate, "vessel:" + v.VesselID.String() + ":v" + itoa(v.Version), nil, nil
			}
		}
	}
	if len(legs) == 0 {
		return domain.Amount(decimal.Zero), port.CostByParamRate, "", []string{domain.FlagNoRoute}, nil
	}
	sum, versions := decimal.Zero, map[string]bool{}
	for _, l := range legs {
		r, err := param(ctx, tx, rateKey(l.Mode), at)
		if err != nil {
			return money.Amount{}, "", "", nil, err
		}
		versions[r.Version] = true
		sum = sum.Add(domain.LegCost(actual, l.DistanceKm, r.Value).Decimal())
	}
	vs := make([]string, 0, len(versions))
	for v := range versions {
		vs = append(vs, v)
	}
	sort.Strings(vs)
	return domain.Amount(sum), port.CostByParamRate, joinVersions(vs), nil, nil
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }

func joinVersions(vs []string) string {
	out := ""
	for i, v := range vs {
		if i > 0 {
			out += ";"
		}
		out += v
	}
	return out
}

// completion dựng sự kiện completed (lần đầu hoặc bản bù supersedes_id → completion trước).
func completion(ctx context.Context, tx pgx.Tx, cur *store.Trip, actor uuid.UUID, actual mass.Kg, ticket *uuid.UUID,
	manual *money.Amount, reason, correction string, at, completedAt time.Time, idem string) (*store.TripEvent, error) {
	e := base(cur, store.KindCompleted, port.TripCompleted, actor, at, idem)
	e.RuleVersion = domain.RuleTripCost
	e.ActualKg = &actual
	e.SourceRef = ticket
	if cur.Status == port.TripCompleted {
		if correction == "" {
			return nil, ErrCorrectionReason
		}
		e.SupersedesID, e.CorrectionReason = cur.CompletionEventID, &correction
		if cur.CompletedAt != nil {
			completedAt = *cur.CompletedAt
		}
	} else if err := domain.CanTransition(cur.Status, port.TripCompleted); err != nil {
		return nil, err
	}
	ca := completedAt
	e.CompletedAt = &ca
	// Lô trên chuyến: khối lượng thật chia theo tấn ước của từng lô; gắn phiếu cân.
	weights := make([]mass.Kg, len(cur.Cargo))
	for i, c := range cur.Cargo {
		weights[i] = c.MassKg
	}
	alloc := domain.AllocateKg(actual, weights)
	cargo := make([]store.Cargo, len(cur.Cargo))
	for i, c := range cur.Cargo {
		cargo[i] = store.Cargo{LotRef: c.LotRef, MassKg: alloc[i], TicketRef: ticket}
	}
	e.Cargo = cargo
	flags := mergeFlags(cur.Flags, []string{domain.FlagWeighVariance, domain.FlagNoRoute}, nil)
	if cur.PlannedKg != nil {
		if v := domain.VariancePct(*cur.PlannedKg, actual); v != nil && v.Abs().GreaterThan(domain.WeighVariancePct) {
			flags = mergeFlags(flags, nil, []string{domain.FlagWeighVariance})
		}
	}
	if manual != nil {
		r, err := domain.CheckReason("reason", reason, true)
		if err != nil {
			return nil, err
		}
		src := port.CostManual
		e.ActualCost, e.CostSource, e.Reason = manual, &src, &r
	} else {
		a, src, pv, cf, err := costFor(ctx, tx, cur, actual, at)
		if err != nil {
			return nil, err
		}
		e.ActualCost, e.CostSource, e.ParamVersion = &a, &src, strPtr(pv)
		flags = mergeFlags(flags, nil, cf)
		if r := reasonOrEmpty(reason); r != "" {
			e.Reason = &r
		}
	}
	e.Flags = flags
	return &e, nil
}

func reasonOrEmpty(s string) string {
	r, err := domain.CheckReason("reason", s, false)
	if err != nil {
		return ""
	}
	return r
}

// CompleteInput là xác nhận hoàn tất / sửa chuyến của điều phối.
type CompleteInput struct {
	ActualKg         mass.Kg
	TicketRef        *uuid.UUID
	ActualCost       *money.Amount // nil = tính theo đơn giá thuê ghe / tham số
	Reason           string        // bắt buộc khi cước nhập tay (chứng từ)
	CorrectionReason string        // bắt buộc khi chuyến đã completed (bản bù)
	EventTime        time.Time
	IdemKey          string
}

// CompleteTrip hoàn tất chuyến với tấn thật và cước thực (điều phối) — hoặc, với chuyến đã hoàn tất,
// ghi BẢN BÙ (supersedes_id → bản completed trước) và phát lại trip.completed có supersedes_id (RB-1).
func (s *Service) CompleteTrip(ctx context.Context, trip uuid.UUID, in CompleteInput) (TripResult, bool, error) {
	if in.ActualKg.Decimal().Sign() <= 0 {
		return TripResult{}, false, problem.Validation.WithField("actual_kg", "phải lớn hơn 0")
	}
	if in.ActualCost != nil && in.ActualCost.Decimal().Sign() < 0 {
		return TripResult{}, false, problem.Validation.WithField("actual_cost", "không âm")
	}
	corr, err := domain.CheckReason("correction_reason", in.CorrectionReason, false)
	if err != nil {
		return TripResult{}, false, mapErr(err)
	}
	return s.tripOp(ctx, []string{RoleDispatcher}, trip, in.IdemKey, func(tx pgx.Tx, p authz.Principal, cur *store.Trip) (*store.TripEvent, error) {
		now := s.Clock.Now()
		at := in.EventTime
		if at.IsZero() {
			at = now
		}
		e, err := completion(ctx, tx, cur, p.ActorRef, in.ActualKg, in.TicketRef, in.ActualCost, in.Reason, corr, at, at, in.IdemKey)
		if err != nil {
			return nil, mapErr(err)
		}
		return e, nil
	})
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// GetTrip đọc chuyến + chặng hiệu lực (điều phối, tài xế). Người chỉ có vai tài xế chỉ đọc chuyến giao cho
// mình (chuyến khác → 404, không lộ sự tồn tại).
func (s *Service) GetTrip(ctx context.Context, trip uuid.UUID) (TripResult, error) {
	var res TripResult
	p, _ := authz.FromContext(ctx)
	err := s.read(ctx, func(tx pgx.Tx) error {
		t, err := store.GetTrip(ctx, tx, trip, false)
		if err != nil {
			return err
		}
		if t == nil || (DriverOnly(p) && !assignedTo(t, p)) {
			return ErrTripNotFound
		}
		res.Trip = *t
		res.Legs, err = store.LatestPlan(ctx, tx, trip, nil)
		return err
	})
	return res, err
}

// ListTrips liệt kê chuyến theo con trỏ (planned_at, trip_id) giảm dần (giữ cho tương thích; xem QueryTrips).
func (s *Service) ListTrips(ctx context.Context, status string, afterAt *time.Time, afterID uuid.UUID, limit int) ([]store.Trip, error) {
	out, _, err := s.QueryTrips(ctx, TripQuery{Status: status, AfterAt: afterAt, AfterID: afterID, Limit: limit})
	return out, err
}

// TripEvents đọc sổ của một chuyến (điều phối — kiểm toán), kèm accuracy_m của mốc (sổ trip_milestone_fix).
func (s *Service) TripEvents(ctx context.Context, trip uuid.UUID) ([]store.TripEvent, error) {
	var out []store.TripEvent
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.TripEvents(ctx, tx, trip)
		if e == nil && len(out) == 0 {
			return ErrTripNotFound
		}
		if e != nil {
			return e
		}
		fx, e := store.MilestoneFixes(ctx, tx, trip)
		for i := range out {
			if a, ok := fx[out[i].ID]; ok {
				out[i].AccuracyM = &a
			}
		}
		return e
	}, RoleDispatcher)
	return out, err
}

// RouteSnapshot đọc ảnh chụp định tuyến nguyên văn của một chặng — KHÔNG gọi lại nền tảng (L-03).
func (s *Service) RouteSnapshot(ctx context.Context, trip, leg uuid.UUID) (*store.Snapshot, error) {
	var out *store.Snapshot
	err := s.read(ctx, func(tx pgx.Tx) error {
		legs, err := store.LatestPlan(ctx, tx, trip, nil)
		if err != nil {
			return err
		}
		for _, l := range legs {
			if l.LegID == leg && l.SnapshotID != nil {
				out, err = store.GetSnapshot(ctx, tx, *l.SnapshotID)
				if err != nil {
					return err
				}
			}
		}
		if out == nil {
			return ErrSnapshotNotFound
		}
		return nil
	})
	return out, err
}

// RebuildProjections dựng lại trip_current của tenant thẳng từ sổ (R-22).
func (s *Service) RebuildProjections(ctx context.Context) (int64, error) {
	p, err := authz.Require(ctx, RoleDispatcher)
	if err != nil {
		return 0, err
	}
	return s.RebuildTenant(ctx, p.TenantID)
}

// RebuildTenant dựng lại bảng chiếu của tenant tid từ sổ — tenant TƯỜNG MINH, không kiểm vai: chỉ cho công cụ
// vận hành chạy trên BẢN SAO database (diễn tập R-22, cmd/rebuilddrill). API dùng RebuildProjections (có vai).
func (s *Service) RebuildTenant(ctx context.Context, tid uuid.UUID) (int64, error) {
	var n int64
	_, err := s.write(ctx, tid, func(tx pgx.Tx) error {
		var e error
		n, e = store.RebuildTrips(ctx, tx)
		return e
	}, func(pgx.Tx) (bool, error) { return false, nil })
	return n, err
}
