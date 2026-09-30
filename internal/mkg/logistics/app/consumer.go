package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// ConsumerActor là actor_ref của consumer sự kiện nội bộ trong sổ logistics (không phải người).
var ConsumerActor = uuid.MustParse("00000000-0000-0000-0000-00000010c001")

// DefaultKgPerRoll là kg/cuộn cuối chuỗi dự phòng (proto FM-09: HTX học → hệ thống → cấu hình → 20).
var DefaultKgPerRoll = mass.MustKg("20")

// Consumer xử lý sự kiện NỘI BỘ MKG (vhost mkg.internal, cmd/worker-internal), trong giao dịch inbox
// đã mở (vai logistics_app, tenant của phong bì): inbox + ghi sổ chuyến cùng giao dịch (R-61). Lớp
// idempotent thứ hai là idem_key theo id nghiệp vụ (roll_count_id, ticket_id).
type Consumer struct {
	Clock clock.Clock
	// KgPerRoll: ước lượng kg/cuộn của HTX (fieldops/port, pool vai fieldops_app); nil → 20 kg.
	KgPerRoll fieldport.KgPerRollEstimates
}

func decode(env outbox.Envelope, v any) error {
	if err := json.Unmarshal(env.Payload, v); err != nil {
		return fmt.Errorf("logistics: payload %s không hợp lệ: %w", env.EventType, err)
	}
	return nil
}

func (c Consumer) kgPerRoll(ctx context.Context, tid uuid.UUID, ev fieldport.RollCountRecordedV1) mass.Kg {
	if c.KgPerRoll == nil {
		return DefaultKgPerRoll
	}
	e, err := c.KgPerRoll.KgPerRollAsOf(ctx, tid, ev.HTXRef, ev.EventTime, ev.LotRef)
	if err != nil || e.Kg.Decimal().Sign() <= 0 {
		return DefaultKgPerRoll // không gọi được fieldops: không chặn lập chuyến (FM-05)
	}
	return e.Kg
}

// HandleRollCount — proto FM-05: mỗi lượt xuống ghe (đếm cuộn công đoạn boat_load) sinh MỘT chuyến đường
// thủy planned, chưa gán phương tiện (sự kiện chưa mang số hiệu ghe — g4-conventions §4), tấn ước = cuộn ×
// kg/cuộn. Bản bù đếm cuộn → sửa tấn ước của chuyến còn mở (revised) hoặc hủy nếu về 0 cuộn. Lỗi lập chuyến
// không bao giờ chặn ghi đếm cuộn (consumer chạy sau, bất đồng bộ).
func (c Consumer) HandleRollCount(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != fieldport.EventRollCountRecorded {
		return nil
	}
	var ev fieldport.RollCountRecordedV1
	if err := decode(env, &ev); err != nil {
		return err
	}
	if ev.Stage != fieldport.StageBoatLoad {
		return nil
	}
	idem := "rollcount:" + ev.RollCountID.String()
	if prev, err := store.TripEventByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	est := domain.EstimateKg(ev.Rolls, c.kgPerRoll(ctx, env.TenantID, ev))
	cargo := []store.Cargo{{LotRef: ev.LotRef, MassKg: est}}
	src := ev.RollCountID
	if ev.SupersedesID != nil {
		cur, err := store.TripBySource(ctx, tx, *ev.SupersedesID)
		if err != nil {
			return err
		}
		if cur != nil {
			return c.reviseFromRolls(ctx, tx, cur, ev, est, cargo, idem)
		}
	}
	if ev.Rolls <= 0 {
		return nil // không có cuộn nào xuống ghe → không lập chuyến
	}
	order := ev.LotRef
	_, err := newTrip(ctx, tx, env.TenantID, ConsumerActor, store.TripEvent{
		Mode: port.ModeWaterway, CollectionOrderRef: &order, PlannedKg: &est, Cargo: cargo, SourceRef: &src,
		EventTime: ev.EventTime, IdemKey: idem, DeviceID: ev.DeviceID,
	})
	return err
}

func (c Consumer) reviseFromRolls(ctx context.Context, tx pgx.Tx, cur *store.Trip, ev fieldport.RollCountRecordedV1, est mass.Kg,
	cargo []store.Cargo, idem string) error {
	if err := store.LockTrip(ctx, tx, cur.TenantID, cur.TripID); err != nil {
		return err
	}
	if cur.Status == port.TripCompleted || cur.Status == port.TripCancelled {
		return nil // đã cân / đã hủy: tấn thật là của phiếu cân, không sửa theo cuộn
	}
	src := ev.RollCountID
	if ev.Rolls <= 0 {
		e := base(cur, store.KindCancelled, port.TripCancelled, ConsumerActor, ev.EventTime, idem)
		r := "Đếm cuộn xuống ghe được bù về 0 cuộn (proto FM-05)"
		e.Reason, e.SourceRef = &r, &src
		if err := domain.CanTransition(cur.Status, port.TripCancelled); err != nil {
			return nil // đã đến bến: giữ chuyến, chờ phiếu cân
		}
		_, err := appendEvent(ctx, tx, cur, &e)
		return err
	}
	e := base(cur, store.KindRevised, cur.Status, ConsumerActor, ev.EventTime, idem)
	e.PlannedKg, e.Cargo, e.SourceRef = &est, cargo, &src
	vf, _, err := checkVehicle(ctx, tx, cur.VehicleRef, &est, ev.EventTime)
	if err != nil {
		return err
	}
	e.Flags = mergeFlags(cur.Flags, vehicleFlags, append(vf, domain.FlagRollsCorrected))
	_, err = appendEvent(ctx, tx, cur, &e)
	return err
}

// HandleWeighTicket — proto plant weighing: phiếu cân HUB_IN / CUSTOMER_IN của lô trên chuyến đóng chuyến
// mở CŨ NHẤT chở lô đó (một phiếu một lượt ghe) với tấn thật = khối lượng phiếu; cước = đơn giá thuê ghe
// (GH-03) nếu có, không thì tham số (#22/#23); lệch > 5 % so với tấn ước → cờ. Phiếu bù → bản bù của lần
// hoàn tất trước (supersedes_id) và phát lại trip.completed.
func (c Consumer) HandleWeighTicket(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != measport.EventWeighTicketRecorded {
		return nil
	}
	var t measport.WeighTicketRecordedV1
	if err := decode(env, &t); err != nil {
		return err
	}
	if t.Point != measport.PointHubIn && t.Point != measport.PointCustomerIn {
		return nil
	}
	if t.MassKg.Decimal().Sign() <= 0 {
		return nil
	}
	ticket := t.TicketID
	if t.SupersedesID != nil {
		done, err := store.TripsWithTicket(ctx, tx, *t.SupersedesID)
		if err != nil {
			return err
		}
		for i := range done {
			if err := c.completeWith(ctx, tx, &done[i], t, ticket, "Phiếu cân được bù — khối lượng thật theo phiếu mới (RB-1)"); err != nil {
				return err
			}
		}
		if len(done) > 0 {
			return nil
		}
	}
	lot := t.LotRef
	cands, err := store.TripsForLot(ctx, tx, lot)
	if err != nil {
		return err
	}
	if t.CollectionOrderRef != nil && *t.CollectionOrderRef != lot {
		more, err := store.TripsForLot(ctx, tx, *t.CollectionOrderRef)
		if err != nil {
			return err
		}
		cands = append(cands, more...)
	}
	for i := range cands {
		tr := &cands[i]
		if tr.Status == port.TripCompleted || tr.Status == port.TripCancelled {
			continue
		}
		return c.completeWith(ctx, tx, tr, t, ticket, "")
	}
	return nil // không có chuyến mở chở lô này (vd chuyến tay đã đóng) — không lập chuyến từ phiếu cân
}

func (c Consumer) completeWith(ctx context.Context, tx pgx.Tx, tr *store.Trip, t measport.WeighTicketRecordedV1, ticket uuid.UUID, correction string) error {
	idem := "ticket:" + ticket.String() + ":" + tr.TripID.String()
	if prev, err := store.TripEventByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err
	}
	cur, err := loadTrip(ctx, tx, tr.TenantID, tr.TripID)
	if err != nil {
		return err
	}
	if cur.Status == port.TripCancelled {
		return nil
	}
	if cur.Status == port.TripCompleted && correction == "" {
		return nil
	}
	// Tham số cước áp theo thời điểm sự việc (lúc cân), không theo lúc consumer chạy.
	e, err := completion(ctx, tx, cur, ConsumerActor, t.MassKg, &ticket, nil, "", correction, t.EventTime, t.EventTime, idem)
	if err != nil {
		return err
	}
	_, err = appendEvent(ctx, tx, cur, e)
	return err
}
