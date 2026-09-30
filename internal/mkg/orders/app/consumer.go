package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// HandleWeighTicket xử lý mkg.measurement.weigh_ticket.recorded (hàng đợi port.WeighTicketConsumer, vai
// orders_app, giao dịch inbox — R-61; lớp idempotent thứ hai là idem 'ticket:<ticket_id>' trên sổ):
//   - ghi phiếu vào sổ ticket_event, cập nhật phiếu hiệu lực của (lô, điểm cân) — phiếu bù thay phiếu gốc;
//   - HUB_IN: gắn vào phiếu nhận hàng chưa quyết định của lô, kiểm ↔ đơn mua (> ngưỡng tạm 3 % → cần
//     biên bản chênh lệch, KHÔNG chặn);
//   - CUSTOMER_IN: gắn vào giao nhận planned/dispatched của lô; giao nhận đã chấp nhận mà phiếu hiệu
//     lực đổi (phiếu bù) → chấp nhận bù tự động (supersedes) + sự kiện bản bù;
//   - HUB_OUT: không thuộc orders, bỏ qua.
func (s *Service) HandleWeighTicket(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != measport.EventWeighTicketRecorded {
		return nil
	}
	var t measport.WeighTicketRecordedV1
	if err := json.Unmarshal(env.Payload, &t); err != nil {
		// Payload sai schema đã đóng băng: lỗi → retry → DLQ (không nuốt).
		return fmt.Errorf("orders: payload %s không hợp lệ: %w", env.EventType, err)
	}
	if t.Point != measport.PointHubIn && t.Point != measport.PointCustomerIn {
		return nil
	}
	seen, err := store.TicketSeen(ctx, tx, t.TicketID)
	if err != nil || seen {
		return err // giao lặp
	}
	tid := env.TenantID
	now := s.Clock.Now()
	tk := store.Ticket{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: ConsumerActor, IdemKey: "ticket:" + t.TicketID.String(),
		RuleVersion: domain.RuleTicket}, TicketID: t.TicketID, LotRef: t.LotRef, Point: string(t.Point), Route: string(t.Route),
		Source: string(t.Source), MassKg: t.MassKg.Decimal(), SupersedesTicketID: t.SupersedesID, TicketTime: t.EventTime, SourceRule: t.RuleVersion}
	if err := store.Lock(ctx, tx, tid, "lot:"+t.LotRef.String()); err != nil {
		return err
	}
	if err := store.InsertTicket(ctx, tx, tid, &tk); err != nil {
		return err
	}
	wins, _, err := store.ApplyTicket(ctx, tx, tid, tk)
	if err != nil || !wins {
		return err
	}
	if t.Point == measport.PointHubIn {
		return s.hubInArrived(ctx, tx, env, tk)
	}
	return s.customerInArrived(ctx, tx, env, tk)
}

func (s *Service) hubInArrived(ctx context.Context, tx pgx.Tx, env outbox.Envelope, tk store.Ticket) error {
	rs, err := store.OpenReceiptsOfLot(ctx, tx, tk.LotRef)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	for i := range rs {
		r := rs[i]
		attachHubIn(&r, tk.TicketID, tk.MassKg)
		// Ngưỡng theo thời điểm sự việc (cân HUB_IN).
		if err := checkReceipt(ctx, tx, &r, tk.TicketTime); err != nil {
			return err
		}
		r.Meta = store.Meta{ID: ids.New(), EventTime: now, ActorRef: ConsumerActor, RuleVersion: domain.RuleReceipt,
			IdemKey: "ticket:" + tk.TicketID.String() + ":receipt:" + r.ReceiptID.String()}
		r.Kind = "weighed"
		if err := store.InsertReceiptEvent(ctx, tx, env.TenantID, &r); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) customerInArrived(ctx context.Context, tx pgx.Tx, env outbox.Envelope, tk store.Ticket) error {
	ds, err := store.OpenDeliveriesOfLot(ctx, tx, tk.LotRef)
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	kg := decimal.NullDecimal{Decimal: tk.MassKg, Valid: true}
	for i := range ds {
		d := ds[i]
		idem := "ticket:" + tk.TicketID.String() + ":delivery:" + d.DeliveryID.String()
		if d.Status == string(port.DeliveryAccepted) {
			if d.TicketRef != nil && *d.TicketRef == tk.TicketID {
				continue
			}
			old := "không có"
			if d.TicketRef != nil {
				old = d.TicketRef.String()
			}
			reason := fmt.Sprintf("Phiếu CUSTOMER_IN %s (measurement) thay phiếu %s — chấp nhận bù tự động", tk.TicketID, old)
			prevAcc := *d.AcceptanceID
			d.TicketRef, d.TicketKg = &tk.TicketID, kg
			if err := s.accept(ctx, tx, env.TenantID, &d, store.Meta{ID: ids.New(), EventTime: now, ActorRef: ConsumerActor, IdemKey: idem,
				SupersedesID: &prevAcc, CorrectionReason: &reason}); err != nil {
				return err
			}
			continue
		}
		d.TicketRef, d.TicketKg = &tk.TicketID, kg
		d.Meta = store.Meta{ID: ids.New(), EventTime: now, ActorRef: ConsumerActor, IdemKey: idem, RuleVersion: domain.RuleDelivery}
		d.Kind = "ticket_linked"
		if err := store.InsertDeliveryEvent(ctx, tx, env.TenantID, &d); err != nil {
			return err
		}
	}
	return nil
}
