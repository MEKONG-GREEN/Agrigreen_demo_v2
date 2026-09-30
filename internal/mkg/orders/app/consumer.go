package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
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
	if tk.SupersedesTicketID != nil {
		return s.compensateApprovedReceipts(ctx, tx, env.TenantID, tk, now)
	}
	return nil
}

// compensateApprovedReceipts — phiếu HUB_IN bị bù (measurement) SAU KHI phiếu nhận đã duyệt (RB-1): không sửa
// phiếu nhận / khoản phải trả đã ghi; ghi bản bù phiếu nhận (kind corrected, supersedes_id = dòng hiệu lực,
// vẫn approved, kiểm ↔ đơn mua lại theo tấn mới — chỉ cảnh báo) và, với đơn mua KHÁC theo kg lấy số từ phiếu
// cân, bản bù khoản phải trả (supersedes_id = khoản hiệu lực, số lượng + số tiền mới, giữ hạn và điều khoản đã
// chốt lúc duyệt). Rơm: phải trả chỉ từ quyết toán costing (costing tự làm mới theo phiếu bù).
func (s *Service) compensateApprovedReceipts(ctx context.Context, tx pgx.Tx, tid uuid.UUID, tk store.Ticket, now time.Time) error {
	rs, err := store.ApprovedReceiptsWithTicket(ctx, tx, *tk.SupersedesTicketID)
	if err != nil {
		return err
	}
	for i := range rs {
		r := rs[i]
		prev, prevQty := r.LastEventID, r.ReceivedQty
		attachHubIn(&r, tk.TicketID, tk.MassKg)
		if err := checkReceipt(ctx, tx, &r, tk.TicketTime); err != nil {
			return err
		}
		r.Status = "approved" // đã duyệt: bản bù không mở lại phê duyệt (người duyệt giữ nguyên, R-56)
		reason := fmt.Sprintf("Phiếu HUB_IN %s (measurement) thay phiếu %s sau khi phiếu nhận đã duyệt — bản bù tự động (RB-1)",
			tk.TicketID, *tk.SupersedesTicketID)
		r.Meta = store.Meta{ID: ids.New(), EventTime: now, ActorRef: ConsumerActor, RuleVersion: domain.RuleReceipt,
			IdemKey: "ticket:" + tk.TicketID.String() + ":receipt:" + r.ReceiptID.String() + ":corrected", SupersedesID: &prev, CorrectionReason: &reason}
		r.Kind = "corrected"
		if err := store.InsertReceiptEvent(ctx, tx, tid, &r); err != nil {
			return err
		}
		if r.Category != domain.CategoryOther || !r.ReceivedQty.Valid || (prevQty.Valid && prevQty.Decimal.Equal(r.ReceivedQty.Decimal)) {
			continue
		}
		if err := compensatePayable(ctx, tx, tid, r, reason, now); err != nil {
			return err
		}
	}
	return nil
}

// compensatePayable ghi bản bù khoản phải trả của phiếu nhận đã duyệt theo số nhận mới (RB-1).
func compensatePayable(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r store.Receipt, reason string, now time.Time) error {
	cur, err := store.PayableOfReceipt(ctx, tx, r.ReceiptID)
	if err != nil || cur == nil {
		return err
	}
	price, err := money.ParsePrice(r.UnitPrice.String())
	if err != nil {
		return err
	}
	amt, err := domain.LineAmount(r.Unit, r.ReceivedQty.Decimal, price)
	if err != nil {
		return err
	}
	prev := cur.ID
	pay := store.Payable{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: ConsumerActor, RuleVersion: domain.RulePayable,
		IdemKey: "payable:" + r.ReceiptID.String() + ":ticket:" + r.TicketRef.String(), SupersedesID: &prev, CorrectionReason: &reason},
		ReceiptID: r.ReceiptID, ReceiptEventID: r.ID, POID: r.POID, Category: r.Category, SupplierRef: r.SupplierRef, Unit: r.Unit,
		Qty: r.ReceivedQty.Decimal, UnitPrice: r.UnitPrice, Amount: amt.Decimal(), DueDate: cur.DueDate, PaymentTermDays: cur.PaymentTermDays,
		ParamVersion: cur.ParamVersion}
	return store.InsertPayable(ctx, tx, tid, &pay)
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
