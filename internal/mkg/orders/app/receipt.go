package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Phiếu nhận hàng ↔ đơn mua (proto createGoodsReceipt; g3-acceptance "ngoài G3 → G4"):
//   - đơn theo kg: số nhận = tịnh của phiếu cân HUB_IN của lô (measurement), KHÔNG nhập tay với rơm;
//     phiếu chưa về → awaiting_weigh, consumer mkg.orders.weigh_ticket gắn khi phiếu về;
//   - đơn theo cuộn: số nhận = số cuộn đếm lúc nhận (phiếu cân HUB_IN vẫn được gắn để tham chiếu);
//   - |nhận − đặt| / đặt × 100 > ngưỡng (orders.receipt.po_variance_warn_pct, mặc định 3, tạm chờ khách)
//     → needs_variance_report + ghi chú kiểm toán; CHỈ cảnh báo, không chặn duyệt;
//   - duyệt: sales_approver ≠ người lập (R-56). Đơn mua KHÁC → đúng một khoản phải trả (sổ payable_event);
//     rơm → không ghi phải trả ở đây (chỉ từ quyết toán costing — g4-conventions §1).

// ReceiptInput là phiếu nhận hàng mới.
type ReceiptInput struct {
	POID        uuid.UUID
	LotRef      *uuid.UUID
	ReceivedQty *decimal.Decimal // cuộn (đơn theo cuộn) hoặc kg (chỉ đơn mua khác theo kg, không cân)
	IdemKey     string
}

// ReceiptView là phiếu nhận kèm khoản phải trả (nếu có).
type ReceiptView struct {
	Receipt store.Receipt
	Payable *store.Payable
}

// CreateReceipt lập phiếu nhận hàng cho đơn mua đã duyệt.
func (s *Service) CreateReceipt(ctx context.Context, in ReceiptInput) (ReceiptView, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return ReceiptView{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return ReceiptView{}, false, err
	}
	now := s.Clock.Now()
	var out ReceiptView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		po, err := store.GetPO(ctx, tx, in.POID, true)
		if err != nil {
			return err
		}
		if po == nil {
			return ErrPONotFound
		}
		if po.Status != "approved" {
			return ErrPONotApproved
		}
		if po.Category == domain.CategoryStraw && in.LotRef == nil {
			return mapErr(domain.Field("lot_ref", "bắt buộc với rơm: lô cân HUB_IN (measurement)"))
		}
		r := store.Receipt{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleReceipt},
			Kind: "created", ReceiptID: ids.New(), POID: po.POID, Category: po.Category, SupplierRef: po.SupplierRef, LotRef: in.LotRef,
			Unit: po.Unit, OrderedQty: po.OrderedQty, UnitPrice: po.UnitPrice, CreatorRef: p.ActorRef}
		if err := setReceivedInput(&r, in.ReceivedQty); err != nil {
			return mapErr(err)
		}
		if in.LotRef != nil {
			lt, err := store.GetLotTicket(ctx, tx, *in.LotRef, "HUB_IN")
			if err != nil {
				return err
			}
			if lt != nil {
				attachHubIn(&r, lt.TicketID, lt.MassKg)
			}
		}
		if err := checkReceipt(ctx, tx, &r, now); err != nil {
			return err
		}
		if err := store.Lock(ctx, tx, p.TenantID, "code:receipt"); err != nil {
			return err
		}
		n, err := store.NextCode(ctx, tx, "receipt_current", "receipt_id")
		if err != nil {
			return err
		}
		r.Code = domain.Code("GRN", n, 6)
		if err := store.InsertReceiptEvent(ctx, tx, p.TenantID, &r); err != nil {
			return err
		}
		return loadReceipt(ctx, tx, r.ReceiptID, &out)
	}, s.replayReceipt(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// setReceivedInput áp số nhận nhập tay: cuộn (đơn theo cuộn) hoặc kg của đơn mua KHÁC. Rơm theo kg
// chỉ lấy từ phiếu cân HUB_IN.
func setReceivedInput(r *store.Receipt, q *decimal.Decimal) error {
	switch {
	case r.Unit == domain.UnitBale:
		if q == nil {
			return domain.Field("received_qty", "bắt buộc: số cuộn đếm lúc nhận (đơn theo cuộn)")
		}
		if err := domain.CheckQty("received_qty", r.Unit, *q, true); err != nil {
			return err
		}
		src := "count"
		r.ReceivedQty, r.QtySource = decimal.NullDecimal{Decimal: *q, Valid: true}, &src
	case q != nil && r.Category == domain.CategoryStraw:
		return domain.Field("received_qty", "rơm theo kg: số nhận là tịnh của phiếu cân HUB_IN (measurement), không nhập tay")
	case q != nil:
		if err := domain.CheckQty("received_qty", r.Unit, *q, true); err != nil {
			return err
		}
		src := "entered"
		r.ReceivedQty, r.QtySource = decimal.NullDecimal{Decimal: *q, Valid: true}, &src
	}
	return nil
}

// attachHubIn gắn phiếu cân HUB_IN; đơn theo kg (không nhập tay) lấy tịnh của phiếu làm số nhận.
func attachHubIn(r *store.Receipt, ticket uuid.UUID, kg decimal.Decimal) {
	r.TicketRef, r.TicketKg = &ticket, decimal.NullDecimal{Decimal: kg, Valid: true}
	if r.Unit == domain.UnitKg && (r.QtySource == nil || *r.QtySource == "ticket") {
		src := "ticket"
		r.ReceivedQty, r.QtySource = decimal.NullDecimal{Decimal: kg, Valid: true}, &src
	}
}

// checkReceipt tính kiểm ↔ đơn mua với ngưỡng hiệu lực tại thời điểm sự việc; chưa có số nhận → awaiting_weigh.
func checkReceipt(ctx context.Context, tx pgx.Tx, r *store.Receipt, at time.Time) error {
	if !r.ReceivedQty.Valid {
		r.Status = "awaiting_weigh"
		r.VariancePct, r.ThresholdPct, r.ParamVersion, r.NeedsVarianceReport, r.AuditNote = decimal.NullDecimal{}, decimal.NullDecimal{}, nil, false, nil
		return nil
	}
	pv, err := effectiveParam(ctx, tx, port.ParamReceiptVariancePct, at)
	if err != nil {
		return err
	}
	v := domain.CheckReceipt(r.Unit, r.OrderedQty, r.ReceivedQty.Decimal, pv.Value, pv.Label)
	r.Status = "pending_approval"
	r.VariancePct = decimal.NullDecimal{Decimal: v.Pct, Valid: true}
	r.ThresholdPct = decimal.NullDecimal{Decimal: v.Threshold, Valid: true}
	r.ParamVersion, r.NeedsVarianceReport, r.AuditNote = &v.ParamVersion, v.NeedsReport, strPtr(v.AuditNote)
	return nil
}

// FileVarianceReport ghi biên bản chênh lệch (ghi chú + tệp ở kho đối tượng) cho phiếu nhận.
func (s *Service) FileVarianceReport(ctx context.Context, id uuid.UUID, note string, fileRef *uuid.UUID, idem string) (ReceiptView, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return ReceiptView{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return ReceiptView{}, false, err
	}
	if n := len([]rune(note)); n < 10 || n > 2000 {
		return ReceiptView{}, false, mapErr(domain.Field("note", "10–2000 ký tự"))
	}
	var out ReceiptView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		r, err := store.GetReceipt(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrReceiptNotFound
		}
		if r.Status == "rejected" {
			return ErrReceiptTransition
		}
		r.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleReceipt}
		r.Kind, r.ReportNote, r.ReportFileRef, r.ReportBy = "report_filed", &note, fileRef, &p.ActorRef
		if err := store.InsertReceiptEvent(ctx, tx, p.TenantID, r); err != nil {
			return err
		}
		return loadReceipt(ctx, tx, id, &out)
	}, s.replayReceipt(ctx, idem, &out))
	return out, replayed, err
}

// CorrectReceipt sửa số nhận nhập tay (cuộn / kg đơn mua khác) trước khi duyệt — bản ghi bù có lý do
// (supersedes_id = dòng sổ hiệu lực trước, RB-1). Rơm theo kg: sửa ở measurement (phiếu cân bù).
func (s *Service) CorrectReceipt(ctx context.Context, id uuid.UUID, qty decimal.Decimal, reason, idem string) (ReceiptView, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return ReceiptView{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return ReceiptView{}, false, err
	}
	r0, err := domain.CheckReason("reason", reason, true)
	if err != nil {
		return ReceiptView{}, false, mapErr(err)
	}
	now := s.Clock.Now()
	var out ReceiptView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		r, err := store.GetReceipt(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrReceiptNotFound
		}
		if r.Status == "approved" || r.Status == "rejected" {
			return ErrReceiptTransition
		}
		prev := r.LastEventID
		if r.Unit == domain.UnitKg && r.Category == domain.CategoryStraw {
			return mapErr(domain.Field("received_qty", "rơm theo kg: sửa bằng phiếu cân bù ở measurement"))
		}
		r.QtySource = nil
		if err := setReceivedInput(r, &qty); err != nil {
			return mapErr(err)
		}
		if err := checkReceipt(ctx, tx, r, now); err != nil {
			return err
		}
		r.Meta = store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleReceipt,
			SupersedesID: &prev, CorrectionReason: &r0}
		r.Kind = "corrected"
		if err := store.InsertReceiptEvent(ctx, tx, p.TenantID, r); err != nil {
			return err
		}
		return loadReceipt(ctx, tx, id, &out)
	}, s.replayReceipt(ctx, idem, &out))
	return out, replayed, err
}

// DecideReceipt duyệt / từ chối phiếu nhận. Không bị chặn bởi chênh lệch (chỉ cảnh báo).
func (s *Service) DecideReceipt(ctx context.Context, id uuid.UUID, approve bool, reason, idem string) (ReceiptView, bool, error) {
	p, err := authz.Require(ctx, RoleApprover)
	if err != nil {
		return ReceiptView{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return ReceiptView{}, false, err
	}
	r0, err := domain.CheckReason("reason", reason, !approve)
	if err != nil {
		return ReceiptView{}, false, mapErr(err)
	}
	now := s.Clock.Now()
	var out ReceiptView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		r, err := store.GetReceipt(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrReceiptNotFound
		}
		switch r.Status {
		case "awaiting_weigh":
			if approve {
				return ErrReceiptNotWeighed
			}
		case "pending_approval":
		default:
			return ErrReceiptTransition
		}
		if err := authz.EnsureDistinctApproverFor(r.CreatorRef, p, RoleApprover); err != nil {
			return err
		}
		r.Meta = store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleReceipt}
		if !approve {
			r.Kind, r.Status, r.Reason = "rejected", "rejected", &r0
			if err := store.InsertReceiptEvent(ctx, tx, p.TenantID, r); err != nil {
				return err
			}
			return loadReceipt(ctx, tx, id, &out)
		}
		r.Kind, r.Status, r.ApproverRef = "approved", "approved", &p.ActorRef
		if r0 != "" {
			r.Reason = &r0
		}
		if err := store.InsertReceiptEvent(ctx, tx, p.TenantID, r); err != nil {
			return err
		}
		if r.Category == domain.CategoryOther {
			if err := s.writePayable(ctx, tx, p.TenantID, *r, now); err != nil {
				return err
			}
		}
		return loadReceipt(ctx, tx, id, &out)
	}, s.replayReceipt(ctx, idem, &out))
	return out, replayed, err
}

// writePayable ghi ĐÚNG MỘT khoản phải trả cho phiếu nhận đã duyệt của đơn mua khác (idem theo phiếu).
func (s *Service) writePayable(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r store.Receipt, now time.Time) error {
	price, err := money.ParsePrice(r.UnitPrice.String())
	if err != nil {
		return err
	}
	amt, err := domain.LineAmount(r.Unit, r.ReceivedQty.Decimal, price)
	if err != nil {
		return err
	}
	pv, err := effectiveParam(ctx, tx, port.ParamPaymentTermDays, now)
	if err != nil {
		return err
	}
	days := int(pv.Value.IntPart())
	pay := store.Payable{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: r.ActorRef, IdemKey: "payable:" + r.ReceiptID.String(),
		RuleVersion: domain.RulePayable}, ReceiptID: r.ReceiptID, ReceiptEventID: r.ID, POID: r.POID, Category: r.Category,
		SupplierRef: r.SupplierRef, Unit: r.Unit, Qty: r.ReceivedQty.Decimal, UnitPrice: r.UnitPrice, Amount: amt.Decimal(),
		DueDate: domain.Day(now).AddDate(0, 0, days), PaymentTermDays: days, ParamVersion: pv.Label}
	return store.InsertPayable(ctx, tx, tid, &pay)
}

// Receipt đọc phiếu nhận hàng.
func (s *Service) Receipt(ctx context.Context, id uuid.UUID) (ReceiptView, error) {
	var out ReceiptView
	err := s.read(ctx, func(tx pgx.Tx) error { return loadReceipt(ctx, tx, id, &out) })
	return out, err
}

// Payables liệt kê khoản phải trả đơn mua khác.
func (s *Service) Payables(ctx context.Context, po *uuid.UUID, after *uuid.UUID, limit int) ([]store.Payable, error) {
	var out []store.Payable
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.Payables(ctx, tx, po, after, clampLimit(limit))
		return e
	})
	return out, err
}

// ReceiptCheck cài đặt port.Receipts: kết quả kiểm phiếu nhận ↔ đơn mua (tenant tường minh).
func (s *Service) ReceiptCheck(ctx context.Context, tid, id uuid.UUID) (*port.ReceiptCheck, error) {
	var out *port.ReceiptCheck
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		r, err := store.GetReceipt(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if r == nil {
			return port.ErrReceiptNotFound
		}
		out, err = ReceiptCheckOf(*r)
		return err
	})
	return out, err
}

// ReceiptCheckOf trả kết quả kiểm phiếu nhận ↔ đơn mua của phiếu đã có số nhận (nil = chưa có).
func ReceiptCheckOf(r store.Receipt) (*port.ReceiptCheck, error) {
	if !r.VariancePct.Valid {
		return nil, nil
	}
	c := &port.ReceiptCheck{PurchaseOrderRef: r.POID, ReceiptRef: r.ReceiptID, VariancePct: r.VariancePct.Decimal.StringFixed(2),
		NeedsVarianceReport: r.NeedsVarianceReport, ThresholdPct: r.ThresholdPct.Decimal.String(), Unit: r.Unit}
	if r.ParamVersion != nil {
		c.ParamVersion = paramSeqOf(*r.ParamVersion)
	}
	if r.Unit == domain.UnitKg {
		var err error
		if c.OrderedKg, err = mass.ParseKg(r.OrderedQty.String()); err != nil {
			return nil, err
		}
		if c.ReceivedKg, err = mass.ParseKg(r.ReceivedQty.Decimal.String()); err != nil {
			return nil, err
		}
	} else {
		ob, rb := r.OrderedQty.IntPart(), r.ReceivedQty.Decimal.IntPart()
		c.OrderedBales, c.ReceivedBales = &ob, &rb
		if r.TicketKg.Valid {
			k, err := mass.ParseKg(r.TicketKg.Decimal.String())
			if err != nil {
				return nil, err
			}
			c.ReceivedKg = k
		}
	}
	return c, nil
}

// paramSeqOf đọc số phiên bản từ nhãn "<key>:v<n>" (0 = mặc định tạm).
func paramSeqOf(label string) int {
	i := strings.LastIndex(label, ":v")
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(label[i+2:])
	return n
}

func loadReceipt(ctx context.Context, tx pgx.Tx, id uuid.UUID, out *ReceiptView) error {
	r, err := store.GetReceipt(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if r == nil {
		return ErrReceiptNotFound
	}
	out.Receipt = *r
	out.Payable, err = store.PayableOfReceipt(ctx, tx, id)
	return err
}

func (s *Service) replayReceipt(ctx context.Context, idem string, out *ReceiptView) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		id, err := store.EntityByIdem(ctx, tx, "receipt_event", "receipt_id", idem)
		if err != nil || id == nil {
			return false, err
		}
		return true, loadReceipt(ctx, tx, *id, out)
	}
}
