package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// RuleArrival là phiên bản quy tắc thông báo hàng đến (proto FM-05: cho_den → da_den, mang mã chuyến).
const RuleArrival = "hub.arrival.v1"

// arrivalKind quyết định loại bản ghi sổ cho một mốc chuyến, hoặc "" (bỏ qua): trạng thái chỉ tiến expected →
// arrived; cancelled chỉ từ expected; arrived / cancelled là cuối. Đổi đích (rerouted) khi còn chờ → expected
// ở cơ sở mới. Giao lệch thứ tự (đến trước xuất phát) → arrived, mốc xuất phát tới sau bị bỏ qua.
func arrivalKind(cur *store.Arrival, milestone string, facility uuid.UUID) string {
	switch milestone {
	case logport.MilestoneDeparted, logport.MilestoneRerouted:
		if cur == nil {
			return port.ArrivalExpected
		}
		if cur.Status == port.ArrivalExpected && cur.FacilityRef != facility {
			return port.ArrivalExpected
		}
	case logport.MilestoneArrived, logport.MilestoneCompleted:
		if cur == nil || cur.Status == port.ArrivalExpected {
			return port.ArrivalArrived
		}
	case logport.MilestoneCancelled:
		if cur != nil && cur.Status == port.ArrivalExpected {
			return port.ArrivalCancelled
		}
	}
	return ""
}

// HandleTripMilestone: mkg.logistics.trip.milestone_recorded → thông báo hàng đến của cơ sở đích mang mã chuyến
// (proto FM-05). Sổ arrival_event + bảng chiếu arrival_current cùng giao dịch inbox (R-21, R-61); idem_key theo
// dòng nguồn của logistics.
func (c Consumer) HandleTripMilestone(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != logport.EventTripMilestoneRecorded {
		return nil
	}
	var m logport.TripMilestoneRecordedV1
	if err := decode(env, &m); err != nil {
		return err
	}
	idem := "milestone:" + m.SourceEventRef.String() + ":" + m.Milestone
	if ok, err := store.ArrivalEventExists(ctx, tx, idem); err != nil || ok {
		return err
	}
	if err := store.LockStream(ctx, tx, store.ArrivalStream(m.TripID)); err != nil {
		return err
	}
	cur, err := store.ArrivalByTrip(ctx, tx, m.TripID)
	if err != nil {
		return err
	}
	kind := arrivalKind(cur, m.Milestone, m.FacilityRef)
	if kind == "" {
		return nil
	}
	lots := make([]uuid.UUID, 0, len(m.Lots))
	for _, l := range m.Lots {
		lots = append(lots, l.LotRef)
	}
	e := store.ArrivalEvent{
		ID: ids.New(), TenantID: env.TenantID, TripID: m.TripID, TripCode: m.TripCode, FacilityRef: m.FacilityRef, Kind: kind,
		Milestone: m.Milestone, Mode: string(m.Mode), VesselCode: m.VesselCode, CollectionOrderRef: m.CollectionOrderRef, LotRefs: lots,
		PlannedKg: m.PlannedKg, SourceEventRef: m.SourceEventRef, EventTime: m.EventTime, ActorRef: ConsumerActor, IdemKey: idem,
		RuleVersion: RuleArrival,
	}
	if err := insert(store.InsertArrivalEvent(ctx, tx, &e)); err != nil {
		return err
	}
	return store.ProjectArrival(ctx, tx, m.TripID)
}

// Arrivals liệt kê thông báo hàng đến (hàng sắp đến / đã đến theo chuyến), mới nhất trước. facility nil = mọi
// cơ sở; status rỗng = mọi trạng thái. Con trỏ = trip_id cuối trang.
func (s *Service) Arrivals(ctx context.Context, facility *uuid.UUID, status string, before *uuid.UUID, limit int) ([]store.Arrival, *uuid.UUID, error) {
	switch status {
	case "", port.ArrivalExpected, port.ArrivalArrived, port.ArrivalCancelled:
	default:
		return nil, nil, problem.Validation.WithField("status", "expected | arrived | cancelled")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.Arrival
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListArrivals(ctx, tx, facility, status, before, limit+1)
		return e
	})
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].TripID
		next = &id
	}
	return out, next, err
}
