package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	measport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// ConsumerActor là actor_ref của consumer sự kiện nội bộ trong sổ hub (không phải người).
var ConsumerActor = uuid.MustParse("00000000-0000-0000-0000-0000000b0b01")

// Consumer xử lý sự kiện NỘI BỘ MKG (vhost mkg.internal, cmd/worker-internal).
// Mỗi handler chạy trong giao dịch inbox đã mở (vai hub_app, tenant của phong bì):
// inbox (event_id, consumer) + ghi sổ hub cùng một giao dịch (R-61). Lớp idempotent
// thứ hai là idem_key trên sổ theo id nghiệp vụ (ticket_id, reconciliation_id).
// Consumer không dùng pool riêng — chỉ tx được giao.
type Consumer struct{ Clock clock.Clock }

func decode(env outbox.Envelope, v any) error {
	if err := json.Unmarshal(env.Payload, v); err != nil {
		// Payload sai schema đã đóng băng: lỗi → retry → DLQ sau 5 lần (không nuốt).
		return fmt.Errorf("hub: payload %s không hợp lệ: %w", env.EventType, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// mkg.measurement.weigh_ticket.recorded → khối lượng lô (Q-MKG-04)
// ---------------------------------------------------------------------------

// HandleWeighTicket áp phiếu cân vào hub:
//   - HUB_IN, lô chưa có ở hub → ghi "pending"; lô receipt sau đó nhận bằng weigh_ticket_ref.
//   - HUB_IN, lô đã có (nhập tay) → giữ số nhập tay nếu trong dung sai; ngoài dung
//     sai → dịch chuyển adjustment có dấu, ref = phiếu (lý do ghi ở weigh_link).
//   - HUB_IN, lô đã theo phiếu + phiếu bù → điều chỉnh theo đúng chênh.
//   - HUB_IN, lô nguồn của phiếu chuyển đang đi đường → "transfer_arrival" (dùng khi nhận ở đích).
//   - HUB_OUT → gắn với phiếu xuất mới nhất có phân bổ từ lô; lệch ngoài dung sai → cờ.
//   - CUSTOMER_IN → không thuộc hub (measurement/costing), bỏ qua.
func (c Consumer) HandleWeighTicket(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != measport.EventWeighTicketRecorded {
		return nil
	}
	var t measport.WeighTicketRecordedV1
	if err := decode(env, &t); err != nil {
		return err
	}
	if t.Point != measport.PointHubIn && t.Point != measport.PointHubOut {
		return nil
	}
	idem := "weigh:" + t.TicketID.String()
	if prev, err := store.WeighLinkByIdem(ctx, tx, idem); err != nil || prev != nil {
		return err // đã áp (giao lặp qua inbox khác consumer / phát lại)
	}
	if err := store.LockStream(ctx, tx, domain.StreamKey(t.LotRef)); err != nil {
		return err
	}
	wp, pinned, err := activeWeigh(ctx, tx)
	if err != nil {
		return err
	}
	measured := t.MassKg
	src := string(t.Source)
	link := store.WeighLink{
		ID: ids.New(), TenantID: env.TenantID, LotRef: t.LotRef, Point: string(t.Point), TicketID: &t.TicketID, TicketSource: &src,
		SupersedesTicketID: t.SupersedesID, Measured: &measured, MoisturePct: t.MoisturePct, ParamVersion: &pinned,
		EventTime: t.EventTime, ActorRef: ConsumerActor, IdemKey: idem,
	}
	tol := wp.Tolerance(measured)
	link.Tolerance = &tol
	cur, err := store.LotWeighOf(ctx, tx, t.LotRef, string(t.Point))
	if err != nil {
		return err
	}
	if cur != nil && cur.SupersedesTicketID != nil && *cur.SupersedesTicketID == t.TicketID {
		link.Outcome = "stale" // phiếu bù đã tới trước phiếu gốc
		return insert(store.InsertWeighLink(ctx, tx, &link))
	}
	var next store.LotWeigh
	if t.Point == measport.PointHubIn {
		next, err = c.applyHubIn(ctx, tx, env.TenantID, t, wp, cur, &link)
	} else {
		next, err = applyHubOut(ctx, tx, t, wp, cur, &link)
	}
	if err != nil {
		return err
	}
	if err := insert(store.InsertWeighLink(ctx, tx, &link)); err != nil {
		return err
	}
	next.LastLinkID = link.ID
	return store.UpsertLotWeigh(ctx, tx, env.TenantID, next)
}

func (c Consumer) applyHubIn(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t measport.WeighTicketRecordedV1, wp domain.WeighParams,
	cur *store.LotWeigh, link *store.WeighLink) (store.LotWeigh, error) {
	measured := t.MassKg
	next := store.LotWeigh{LotID: t.LotRef, Point: string(t.Point), TicketID: &t.TicketID, SupersedesTicketID: t.SupersedesID,
		Measured: &measured, MoisturePct: t.MoisturePct, Basis: domain.BasisTicket}
	if tr, err := store.InTransitWithLot(ctx, tx, t.LotRef); err != nil {
		return next, err
	} else if tr != nil {
		link.Outcome, next.Status = "transfer_arrival", "transfer_arrival"
		return next, nil
	}
	lot, err := store.LotByID(ctx, tx, t.LotRef)
	if err != nil {
		return next, err
	}
	if lot == nil {
		link.Outcome, next.Status = "pending", "pending"
		return next, nil
	}
	basis, book := domain.BasisManual, lot.Initial
	if cur != nil && cur.Book != nil && (cur.Status == "applied" || cur.Status == "manual") {
		basis, book = cur.Basis, *cur.Book
	}
	diff, adjust := domain.HubInDecision(wp, basis, book, measured)
	link.Book, link.Diff = &book, &diff
	next.Status, next.Basis, next.Book = "applied", basis, &book
	if !adjust {
		link.Outcome = "within_tolerance"
		if basis == domain.BasisTicket {
			next.Book = &measured
		}
		return next, nil
	}
	b, err := store.BalanceForUpdate(ctx, tx, lot.ID)
	if err != nil {
		return next, err
	}
	if b == nil {
		return next, ErrLotNotFound
	}
	after, err := domain.Apply(b.Remaining, diff)
	if err != nil {
		// Cần giảm tồn nhưng hàng của lô đã xuất / biến đổi: không làm tồn âm, chờ kiểm kê.
		link.Outcome = "variance_unresolved"
		return next, nil
	}
	ref := "weigh_ticket"
	m := domain.Movement{
		ID: ids.New(), TenantID: tid, FacilityID: lot.FacilityID, LotID: lot.ID, Kind: domain.MoveAdjustment, Qty: diff,
		BalanceAfter: after, RefType: &ref, RefID: &t.TicketID, EventTime: c.Clock.Now(), ActorRef: ConsumerActor,
		IdemKey: link.IdemKey + ":adj",
	}
	if err := insert(store.InsertMovement(ctx, tx, &m, nil)); err != nil {
		return next, err
	}
	if err := store.SetBalance(ctx, tx, lot.ID, after, m.Seq); err != nil {
		return next, err
	}
	reason := fmt.Sprintf("Phiếu cân HUB_IN %s: cân %s kg, sổ %s kg (chênh %s kg, dung sai %s kg)",
		t.TicketID, measured, book, diff, link.Tolerance.String())
	link.Outcome, link.MovementID, link.Reason = "adjusted", &m.ID, &reason
	next.Basis, next.Book = domain.BasisTicket, &measured
	return next, nil
}

func applyHubOut(ctx context.Context, tx pgx.Tx, t measport.WeighTicketRecordedV1, wp domain.WeighParams, _ *store.LotWeigh,
	link *store.WeighLink) (store.LotWeigh, error) {
	measured := t.MassKg
	next := store.LotWeigh{LotID: t.LotRef, Point: string(t.Point), TicketID: &t.TicketID, SupersedesTicketID: t.SupersedesID,
		Measured: &measured, MoisturePct: t.MoisturePct, Basis: domain.BasisTicket}
	id, allocated, err := store.LatestDispatchWithLot(ctx, tx, t.LotRef)
	if err != nil {
		return next, err
	}
	if id == nil {
		link.Outcome, next.Status = "dispatch_unmatched", "unmatched"
		return next, nil
	}
	diff := measured.Sub(*allocated)
	link.Book, link.Diff, link.DispatchID = allocated, &diff, id
	next.Status, next.DispatchID, next.Book = "linked", id, allocated
	link.Outcome = "dispatch_linked"
	if !wp.Within(measured, diff) {
		link.Outcome = "dispatch_variance" // hàng đã rời hub: chỉ cờ, đối soát thuộc measurement
	}
	return next, nil
}

// ---------------------------------------------------------------------------
// mkg.fieldops.roll_count.recorded → hàng dự kiến về (tùy chọn)
// ---------------------------------------------------------------------------

// HandleRollCount ghi số cuộn đếm được vào bảng chiếu hàng dự kiến về của lô gom.
// Chỉ số cuộn (không yield_kg_per_roll). Bản ghi bù thay bản bị thay.
func (c Consumer) HandleRollCount(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != fieldport.EventRollCountRecorded {
		return nil
	}
	var r fieldport.RollCountRecordedV1
	if err := decode(env, &r); err != nil {
		return err
	}
	return store.UpsertExpected(ctx, tx, env.TenantID, store.Expected{
		RollCountID: r.RollCountID, LotRef: r.LotRef, PlotRef: r.PlotRef, HTXRef: r.HTXRef, Stage: r.Stage, Rolls: r.Rolls,
		SupersedesID: r.SupersedesID, EventTime: r.EventTime,
	})
}

// ---------------------------------------------------------------------------
// mkg.measurement.reconciliation.completed → quy kết hao hụt theo cơ sở
// ---------------------------------------------------------------------------

// HandleReconciliation ghi kết quả đối soát vào sổ lot_loss, quy cho cơ sở đang
// (hoặc từng) giữ lô; bảng chiếu giữ đối soát mới nhất của mỗi lô.
func (c Consumer) HandleReconciliation(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != measport.EventReconciliationCompleted {
		return nil
	}
	var r measport.ReconciliationCompletedV1
	if err := decode(env, &r); err != nil {
		return err
	}
	idem := "recon:" + r.ReconciliationID.String()
	if ok, err := store.LotLossExists(ctx, tx, idem); err != nil || ok {
		return err
	}
	var facility *uuid.UUID
	if lot, err := store.LotByID(ctx, tx, r.LotRef); err != nil {
		return err
	} else if lot != nil {
		facility = &lot.FacilityID
	}
	return insert(store.InsertLotLoss(ctx, tx, &store.LotLoss{
		ID: ids.New(), TenantID: env.TenantID, ReconciliationID: r.ReconciliationID, LotRef: r.LotRef, FacilityID: facility,
		Route: string(r.Route), HubIn: r.HubInKg, HubOut: r.HubOutKg, CustomerIn: r.CustomerInKg, ExpectedLoss: r.ExpectedLossKg,
		AbnormalLoss: r.AbnormalLossKg, CommercialDiff: r.CommercialDiffKg, Outcome: r.Outcome, ToleranceVersion: r.ToleranceVersion,
		LossNormVersion: r.LossNormVersion, DisputeID: r.DisputeID, SourceRule: r.RuleVersion, EventTime: r.EventTime,
		ActorRef: ConsumerActor, IdemKey: idem,
	}))
}
