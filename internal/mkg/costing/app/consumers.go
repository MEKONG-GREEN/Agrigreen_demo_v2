package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Consumer chỉ ghi sổ đầu vào / tính lại kết quả của nguồn — không bao giờ tự đóng kỳ (g4-conventions §4).
// Mỗi hàm idempotent theo khóa nguồn (R-61; inbox là lớp thứ nhất).

// OnWeighTicket: mkg.measurement.weigh_ticket.recorded → đầu vào khối lượng; HUB_IN → chi phí sơ
// chế hub (#55) theo tham số tại thời điểm cân; làm mới quy đổi cuộn → tấn và quyết toán theo tấn.
func (s *Service) OnWeighTicket(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev measport.WeighTicketRecordedV1) error {
	idem := "weigh:" + ev.TicketID.String()
	if prev, err := store.InputByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	order := ev.LotRef
	if ev.CollectionOrderRef != nil {
		order = *ev.CollectionOrderRef
	}
	if err := store.LockKey(ctx, tx, "order:"+order.String()); err != nil {
		return err
	}
	point, route := string(ev.Point), string(ev.Route)
	kg := ev.MassKg
	in := store.Input{ID: ids.New(), LotRef: ev.LotRef, Kind: "weigh", SourceRef: ev.TicketID, CollectionOrderRef: order, Point: &point,
		Route: &route, MassKg: &kg, MoisturePct: ev.MoisturePct, EventTime: ev.EventTime.UTC(), ActorRef: domain.SystemActorRef, IdemKey: idem}
	var oldTicket *uuid.UUID
	if ev.SupersedesID != nil {
		prev, err := store.InputBySource(ctx, tx, *ev.SupersedesID)
		if err != nil {
			return err
		}
		if prev != nil {
			reason := "phiếu cân bù " + ev.TicketID.String()
			in.SupersedesID, in.CorrectionReason = &prev.ID, &reason
		}
		oldTicket = ev.SupersedesID
	}
	if err := store.InsertInput(ctx, tx, tid, &in); err != nil {
		return err
	}
	if err := s.refreshCollection(ctx, tx, tid, order); err != nil {
		return err
	}
	if ev.Point == measport.PointHubIn {
		var old *store.Allocation
		if oldTicket != nil {
			o, err := store.LiveAllocation(ctx, tx, AllocHubProcessing, *oldTicket)
			if err != nil {
				return err
			}
			old = o
		}
		if _, _, err := s.persistAllocation(ctx, tx, tid, allocReq{Kind: AllocHubProcessing, SourceRef: ev.TicketID, EventTime: ev.EventTime,
			Inputs: HubInputs{TicketID: ev.TicketID, LotRef: ev.LotRef, MassKg: ev.MassKg, EventTime: ev.EventTime.UTC()}, Old: old,
			Actor: domain.SystemActorRef, IdemKey: "hub:" + ev.TicketID.String(), Reason: reasonIf(old, "phiếu cân bù")}); err != nil {
			return err
		}
	}
	if err := s.refreshConversion(ctx, tx, tid, order); err != nil {
		return err
	}
	return s.refreshSettlements(ctx, tx, tid, order)
}

func reasonIf(old *store.Allocation, why string) string {
	if old == nil {
		return ""
	}
	return why + " — thay kết quả " + old.ID.String()
}

// OnRollCount: mkg.fieldops.roll_count.recorded → số cuộn của lệnh gom (quyết toán theo cuộn PM-02,
// quy đổi cuộn → tấn).
func (s *Service) OnRollCount(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev fieldport.RollCountRecordedV1) error {
	idem := "rolls:" + ev.RollCountID.String()
	if prev, err := store.InputByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	order := ev.LotRef
	if err := store.LockKey(ctx, tx, "order:"+order.String()); err != nil {
		return err
	}
	rolls, htx, plot := ev.Rolls, ev.HTXRef, ev.PlotRef
	stage := ev.Stage
	in := store.Input{ID: ids.New(), LotRef: order, Kind: "rolls", SourceRef: ev.RollCountID, CollectionOrderRef: order, HTXRef: &htx,
		Rolls: &rolls, PlotRef: &plot, Stage: &stage, EventTime: ev.EventTime.UTC(), ActorRef: domain.SystemActorRef, IdemKey: idem}
	if stage != fieldport.StageBale && stage != fieldport.StageGather && stage != fieldport.StageBoatLoad {
		in.Stage = nil
	}
	if ev.SupersedesID != nil {
		prev, err := store.InputBySource(ctx, tx, *ev.SupersedesID)
		if err != nil {
			return err
		}
		if prev != nil {
			reason := "đếm cuộn bù " + ev.RollCountID.String()
			in.SupersedesID, in.CorrectionReason = &prev.ID, &reason
		}
	}
	if err := store.InsertInput(ctx, tx, tid, &in); err != nil {
		return err
	}
	if err := s.refreshCollection(ctx, tx, tid, order); err != nil {
		return err
	}
	if err := s.refreshConversion(ctx, tx, tid, order); err != nil {
		return err
	}
	return s.refreshSettlements(ctx, tx, tid, order)
}

// OnReconciliation: mkg.measurement.reconciliation.completed → quy kết hao hụt về lô nguồn theo tỷ
// lệnh nguồn của lô (hub/port.LotOrigins, Σ = 1). Một kết quả hiệu lực mỗi lô: đối soát mới thay cũ.
func (s *Service) OnReconciliation(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev measport.ReconciliationCompletedV1) error {
	if ev.AbnormalLossKg == nil && ev.ExpectedLossKg == nil {
		return nil // tuyến DIRECT / chưa đủ phiếu — không có hao hụt để quy kết
	}
	idem := "loss:" + ev.ReconciliationID.String()
	if prev, err := store.AllocationByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	in := LossInputs{ReconciliationID: ev.ReconciliationID, LotRef: ev.LotRef, HubInKg: ev.HubInKg, EventTime: ev.EventTime.UTC()}
	if ev.AbnormalLossKg != nil {
		in.AbnormalLossKg = *ev.AbnormalLossKg
	}
	if ev.ExpectedLossKg != nil {
		in.ExpectedLossKg = *ev.ExpectedLossKg
	}
	if s.Deps.Origins != nil {
		info, err := s.Deps.Origins.LotOrigin(ctx, tid, ev.LotRef)
		switch {
		case err == nil && len(info.Sources) > 0:
			in.OriginFound = true
			for _, src := range info.Sources {
				in.Sources = append(in.Sources, LossSource{Kind: src.Kind, Ref: src.Ref, Ratio: src.Ratio})
			}
		case err != nil && !errors.Is(err, hubport.ErrLotNotFound):
			return err // hub tạm lỗi → giao lại (R-62)
		}
	}
	if !in.OriginFound {
		in.Sources = []LossSource{{Kind: "lot", Ref: ev.LotRef, Ratio: "1.000000"}}
	}
	old, err := store.LiveAllocation(ctx, tx, AllocLoss, ev.LotRef)
	if err != nil {
		return err
	}
	_, _, err = s.persistAllocation(ctx, tx, tid, allocReq{Kind: AllocLoss, SourceRef: ev.LotRef, EventTime: ev.EventTime, Inputs: in,
		Old: old, Actor: domain.SystemActorRef, IdemKey: idem, Reason: reasonIf(old, "đối soát mới "+ev.ReconciliationID.String())})
	return err
}

// OnTrip: mkg.logistics.trip.completed → cước thực tế phân bổ về lô trên chuyến theo khóa phân bổ
// có phiên bản (mặc định khối lượng). Bản bù (SupersedesID) → kết quả mới thay kết quả cũ.
func (s *Service) OnTrip(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev logport.TripCompletedV1) error {
	if len(ev.Cargo) == 0 {
		return nil // chuyến rỗng: không có lô để phân bổ
	}
	in := TripInputs{TripID: ev.TripID, Mode: string(ev.Mode), ActualCost: ev.ActualCost, CostSource: string(ev.CostSource),
		EventTime: ev.EventTime.UTC()}
	for _, c := range ev.Cargo {
		in.Cargo = append(in.Cargo, CargoLot{LotRef: c.LotRef, MassKg: c.MassKg})
	}
	if err := s.ingestSource(ctx, tx, tid, AllocTripCost, ev.TripID, ev.SupersedesID, ev.EventTime, in, "trip:"); err != nil {
		return err
	}
	// PM-02: chuyến ghe của lệnh gom vừa hoàn tất → làm mới quyết toán chưa duyệt (hết "chờ cân"). Không phụ thuộc
	// thứ tự giao giữa hàng đợi mkg.costing.weigh_ticket và mkg.logistics.weigh_ticket (E2E G4): phiếu cân tới costing
	// trước khi logistics đóng chuyến thì quyết toán còn awaiting_weighing tới sự kiện này.
	if ev.CollectionOrderRef == nil {
		return nil
	}
	if err := store.LockKey(ctx, tx, "order:"+ev.CollectionOrderRef.String()); err != nil {
		return err
	}
	return s.refreshSettlements(ctx, tx, tid, *ev.CollectionOrderRef)
}

// OnDelivery: mkg.orders.delivery.accepted → doanh thu giao nhận theo lô (khối lượng CUSTOMER_IN).
func (s *Service) OnDelivery(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev ordport.DeliveryAcceptedV1) error {
	if len(ev.Lots) == 0 {
		return nil
	}
	in := DeliveryInputs{DeliveryID: ev.DeliveryID, CustomerRef: ev.CustomerRef, Amount: ev.Amount, EventTime: ev.EventTime.UTC()}
	for _, l := range ev.Lots {
		in.Lots = append(in.Lots, CargoLot{LotRef: l.LotRef, MassKg: l.MassKg})
	}
	return s.ingestSource(ctx, tx, tid, AllocDelivery, ev.DeliveryID, ev.SupersedesID, ev.EventTime, in, "delivery:")
}

// ingestSource ghi kết quả phân bổ của một nguồn (chuyến / giao nhận), thay kết quả hiệu lực cũ
// của cùng nguồn hoặc của nguồn bị bù. Cùng đầu vào → không ghi (idempotent theo băm đầu vào).
func (s *Service) ingestSource(ctx context.Context, tx pgx.Tx, tid uuid.UUID, kind string, source uuid.UUID, supersedes *uuid.UUID,
	at time.Time, in any, prefix string) error {
	if err := store.LockKey(ctx, tx, kind+":"+source.String()); err != nil {
		return err
	}
	old, err := store.LiveAllocation(ctx, tx, kind, source)
	if err != nil {
		return err
	}
	if old == nil && supersedes != nil && *supersedes != source {
		if old, err = store.LiveAllocation(ctx, tx, kind, *supersedes); err != nil {
			return err
		}
	}
	isha, err := domain.InputsSHA256(in)
	if err != nil {
		return err
	}
	if old != nil && old.InputsSHA256 == isha {
		return nil // cùng đầu vào (giao lặp / đọc lại khi đóng kỳ) — kết quả hiệu lực giữ nguyên
	}
	idem := prefix + source.String() + ":" + isha[:24]
	if prev, err := store.AllocationByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	_, _, err = s.persistAllocation(ctx, tx, tid, allocReq{Kind: kind, SourceRef: source, EventTime: at, Inputs: in, Old: old,
		Actor: domain.SystemActorRef, IdemKey: idem, Reason: reasonIf(old, "bản bù của nguồn "+source.String())})
	return err
}

// OnJournalPushed: mkg.acctbridge.journal.pushed → kỳ đã đóng chuyển posted (một lần).
func (s *Service) OnJournalPushed(ctx context.Context, tx pgx.Tx, tid uuid.UUID, ev abport.JournalPushedV1) error {
	p, err := store.PeriodByID(ctx, tx, ev.PeriodID, true)
	if err != nil || p == nil {
		return err // kỳ không thuộc tenant này — bỏ qua
	}
	if p.Status != port.PeriodClosed || p.CloseRunID == nil || *p.CloseRunID != ev.CloseRunID {
		return nil // đã posted, hoặc bút toán của lần đóng khác — không đổi trạng thái
	}
	jr, ext := ev.JournalID, ev.ExternalRef
	e := store.PeriodEvent{ID: ids.New(), PeriodID: p.ID, Code: p.Code, Action: "posted", From: p.From, To: p.To, CloseRunID: p.CloseRunID,
		JournalRef: &jr, ExternalRef: strPtr(truncate(ext, 200)), EventTime: ev.EventTime.UTC(), ActorRef: domain.SystemActorRef,
		IdemKey: "period-posted:" + p.ID.String()}
	return store.InsertPeriodEvent(ctx, tx, tid, &e)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// refreshCollection tính lại bảng chiếu của lệnh gom từ đầu vào hiệu lực (R-21).
func (s *Service) refreshCollection(ctx context.Context, tx pgx.Tx, tid, order uuid.UUID) error {
	ins, err := store.EffectiveInputsForOrder(ctx, tx, order)
	if err != nil {
		return err
	}
	c := store.AggregateCollection(order, ins)
	if c == nil {
		return nil
	}
	return store.UpsertCollection(ctx, tx, tid, *c)
}

// ConversionInputs là đầu vào quy đổi cuộn → tấn (băm vào inputs_sha256).
type ConversionInputs struct {
	OrderRef   uuid.UUID `json:"order_ref"`
	Rolls      int       `json:"rolls"`
	WeighedKg  mass.Kg   `json:"weighed_kg"`
	WeighPoint string    `json:"weigh_point"`
}

// refreshConversion ghi quy đổi mới (supersedes quy đổi cũ) khi số cuộn hoặc khối lượng cân đổi.
func (s *Service) refreshConversion(ctx context.Context, tx pgx.Tx, tid, order uuid.UUID) error {
	c, err := store.CollectionByOrder(ctx, tx, order)
	if err != nil || c == nil || c.Rolls == nil || *c.Rolls <= 0 || c.WeighedKg == nil {
		return err
	}
	in := ConversionInputs{OrderRef: order, Rolls: *c.Rolls, WeighedKg: *c.WeighedKg, WeighPoint: *c.WeighPoint}
	isha, err := domain.InputsSHA256(in)
	if err != nil {
		return err
	}
	cur, err := store.CurrentConversion(ctx, tx, order)
	if err != nil {
		return err
	}
	if cur != nil && cur.InputsSHA256 == isha {
		return nil
	}
	moist, err := paramAt(ctx, tx, port.ParamMoistureBasePct, c.LastEventTime, nil)
	if err != nil {
		return err
	}
	conv, err := domain.Convert(in.WeighedKg, in.Rolls, moist)
	if err != nil {
		return err
	}
	r := store.Conversion{ID: ids.New(), OrderRef: order, Rolls: in.Rolls, WeighedKg: in.WeighedKg, WeighPoint: in.WeighPoint,
		YieldKgPerRoll: conv.YieldKgPerRoll, MoistureRule: conv.MoistureRule, ConvertedKg: conv.ConvertedKg, InputsSHA256: isha,
		Pins: domain.Pins{}.Add(moist), EventTime: c.LastEventTime, IdemKey: "conv:" + order.String() + ":" + isha[:24],
		RuleVersion: domain.RuleConversion}
	if cur != nil {
		r.SupersedesID = &cur.ID
	}
	return store.InsertConversion(ctx, tx, tid, &r)
}

// refreshSettlements làm mới quyết toán CHƯA duyệt của lệnh gom sau mỗi lần cân / đếm cuộn (PM-01).
func (s *Service) refreshSettlements(ctx context.Context, tx pgx.Tx, tid, order uuid.UUID) error {
	open, err := store.OpenSettlementsForOrder(ctx, tx, order)
	if err != nil || len(open) == 0 {
		return err
	}
	pending := s.pendingLoads(ctx, tid, order)
	c, err := store.CollectionByOrder(ctx, tx, order)
	if err != nil {
		return err
	}
	for _, st := range open {
		next := refigure(st, c, pending)
		if sameFigures(st, next) {
			continue
		}
		e := store.SettlementEvent{ID: ids.New(), Action: "refreshed", State: next, Pins: domain.Pins{}, EventTime: s.now(),
			ActorRef: domain.SystemActorRef, IdemKey: "settle-refresh:" + st.ID.String() + ":" + ids.New().String()}
		if err := store.InsertSettlementEvent(ctx, tx, tid, &e); err != nil {
			return err
		}
	}
	return nil
}

// pendingLoads: còn chuyến (ghe) của lệnh gom chưa hoàn tất / chưa cân (logistics) — PM-02.
// Không nối logistics → false (chỉ dựa vào phiếu cân đã có).
func (s *Service) pendingLoads(ctx context.Context, tid, order uuid.UUID) bool {
	if s.Deps.Trips == nil {
		return false
	}
	trips, err := s.Deps.Trips.TripsForLot(ctx, tid, order)
	if err != nil {
		return false
	}
	for _, t := range trips {
		if t.Status != logport.TripCompleted && t.Status != logport.TripCancelled {
			return true
		}
	}
	return false
}
