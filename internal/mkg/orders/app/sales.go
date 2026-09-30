package app

import (
	"context"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SOInput là một đơn bán mới. Khách là customer_ref (định danh ở vault) — không có tên, mã số thuế…
type SOInput struct {
	CustomerRef  uuid.UUID
	Product      string
	OrderedKg    mass.Kg
	ListPrice    *money.Price // giá bảng giá VND/tấn (tùy chọn) — căn cứ ngưỡng lệch giá
	UnitPrice    money.Price  // VND/tấn trước chiết khấu
	DiscountPct  decimal.Decimal
	DeliveryFrom string
	DeliveryTo   string
	IdemKey      string
}

// SOView là đơn bán kèm lịch giao.
type SOView struct {
	Order      store.SalesOrder
	Deliveries []store.Delivery
}

// CreateSO lập đơn bán nháp (proto createSalesOrder): khối lượng > 0, đơn giá > 0 (Go thêm). Ngưỡng lệch
// giá bảng / chiết khấu (tham số có phiên bản, chờ khách) quyết định đơn có cần sales_approver xác nhận.
func (s *Service) CreateSO(ctx context.Context, in SOInput) (SOView, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return SOView{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return SOView{}, false, err
	}
	x := problem.Validation
	bad := false
	add := func(f, m string) { x, bad = x.WithField(f, m), true }
	if in.CustomerRef == uuid.Nil {
		add("customer_ref", "bắt buộc")
	}
	if !domain.ValidProduct(in.Product) {
		add("product", strings.Join(domain.Products, " | "))
	}
	if in.OrderedKg.Decimal().Sign() <= 0 {
		add("ordered_kg", "khối lượng bán phải lớn hơn 0 (proto createSalesOrder)")
	}
	if in.UnitPrice.Decimal().Sign() <= 0 {
		add("unit_price", "đơn giá phải lớn hơn 0")
	}
	if in.ListPrice != nil && in.ListPrice.Decimal().Sign() <= 0 {
		add("list_price", "giá bảng phải lớn hơn 0")
	}
	if domain.CheckDiscount(in.DiscountPct) != nil {
		add("discount_pct", "0 ≤ chiết khấu < 100, tối đa 2 chữ số thập phân")
	}
	var from, to *time.Time
	for _, d := range []struct {
		f   string
		s   string
		dst **time.Time
	}{{"delivery_from", in.DeliveryFrom, &from}, {"delivery_to", in.DeliveryTo, &to}} {
		if d.s == "" {
			continue
		}
		t, err := domain.ParseDate(d.f, d.s)
		if err != nil {
			add(d.f, "ngày YYYY-MM-DD")
			continue
		}
		*d.dst = &t
	}
	if from != nil && to != nil && to.Before(*from) {
		add("delivery_to", "không được trước delivery_from")
	}
	if bad {
		return SOView{}, false, x
	}
	net, err := domain.NetPrice(in.UnitPrice, in.DiscountPct)
	if err != nil || net.Decimal().Sign() <= 0 {
		return SOView{}, false, problem.Validation.WithField("discount_pct", "đơn giá sau chiết khấu phải lớn hơn 0")
	}
	amt, err := domain.KgAmount(in.OrderedKg, net)
	if err != nil {
		return SOView{}, false, problem.Validation.WithField("ordered_kg", "thành tiền vượt numeric(18,2)")
	}
	now := s.Clock.Now()
	var out SOView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		delta, err := effectiveParam(ctx, tx, port.ParamPriceApprovalDelta, now)
		if err != nil {
			return err
		}
		disc, err := effectiveParam(ctx, tx, port.ParamDiscountApprovalPct, now)
		if err != nil {
			return err
		}
		term, err := effectiveParam(ctx, tx, port.ParamPaymentTermDays, now)
		if err != nil {
			return err
		}
		pa := domain.CheckPriceApproval(in.ListPrice, in.UnitPrice, in.DiscountPct, delta.Value, disc.Value)
		o := store.SalesOrder{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleSales},
			Kind: "created", OrderID: ids.New(), Status: domain.SODraft, CustomerRef: in.CustomerRef, Product: in.Product,
			OrderedKg: in.OrderedKg.Decimal(), UnitPrice: in.UnitPrice.Decimal(), DiscountPct: in.DiscountPct, NetPrice: net.Decimal(),
			Amount: amt.Decimal(), NeedsPriceApproval: pa.Needed, PriceRule: delta.Label + "," + disc.Label,
			PaymentTermDays: int(term.Value.IntPart()), DeliveryFrom: from, DeliveryTo: to, CreatorRef: p.ActorRef}
		if in.ListPrice != nil {
			o.ListPrice = decimal.NullDecimal{Decimal: in.ListPrice.Decimal(), Valid: true}
		}
		if err := store.Lock(ctx, tx, p.TenantID, "code:order"); err != nil {
			return err
		}
		n, err := store.NextCode(ctx, tx, "order_current", "order_id")
		if err != nil {
			return err
		}
		o.Code = domain.Code("SO", n, 6)
		if err := store.InsertOrderEvent(ctx, tx, p.TenantID, &o); err != nil {
			return err
		}
		return loadSO(ctx, tx, o.OrderID, &out)
	}, s.replaySO(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// ConfirmSO xác nhận đơn nháp. Giá / chiết khấu vượt ngưỡng → chỉ sales_approver KHÁC người lập (R-56,
// app/ + CHECK DB); trong ngưỡng → sales_officer xác nhận được.
func (s *Service) ConfirmSO(ctx context.Context, id uuid.UUID, idem string) (SOView, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return SOView{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return SOView{}, false, err
	}
	var out SOView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if o.Status != domain.SODraft {
			return ErrOrderTransition
		}
		if o.NeedsPriceApproval {
			if !p.Has(RoleApprover) {
				return ErrPriceApproval
			}
			if err := authz.EnsureDistinctApproverFor(o.CreatorRef, p, RoleApprover); err != nil {
				return err
			}
			o.ApproverRef = &p.ActorRef
		}
		o.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RuleSales}
		o.Kind, o.Status = "confirmed", domain.SOConfirmed
		if err := store.InsertOrderEvent(ctx, tx, p.TenantID, o); err != nil {
			return err
		}
		return loadSO(ctx, tx, id, &out)
	}, s.replaySO(ctx, idem, &out))
	return out, replayed, err
}

// AdvanceSO chuyển đơn bán tiến (delivering → delivered → completed) hoặc hủy (từ draft / confirmed, có lý do).
func (s *Service) AdvanceSO(ctx context.Context, id uuid.UUID, next, reason, idem string) (SOView, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return SOView{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return SOView{}, false, err
	}
	switch next {
	case domain.SODelivering, domain.SODelivered, domain.SOCompleted, domain.SOCancelled:
	case domain.SOConfirmed:
		return SOView{}, false, problem.Validation.WithField("status", "xác nhận qua POST /confirm")
	default:
		return SOView{}, false, problem.Validation.WithField("status", "delivering | delivered | completed | cancelled")
	}
	r, err := domain.CheckReason("reason", reason, next == domain.SOCancelled)
	if err != nil {
		return SOView{}, false, mapErr(err)
	}
	var out SOView
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if err := s.advance(ctx, tx, p.TenantID, o, next, strPtr(r), p.ActorRef, idem); err != nil {
			return err
		}
		return loadSO(ctx, tx, id, &out)
	}, s.replaySO(ctx, idem, &out))
	return out, replayed, err
}

func (s *Service) advance(ctx context.Context, tx pgx.Tx, tid uuid.UUID, o *store.SalesOrder, next string, reason *string, actor uuid.UUID, idem string) error {
	if !domain.CanAdvance(o.Status, next) {
		return ErrOrderTransition.WithDetail("không chuyển được từ " + o.Status + " sang " + next)
	}
	o.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: actor, IdemKey: idem, RuleVersion: domain.RuleSales}
	o.Kind, o.Status = next, next
	if reason != nil {
		o.Reason = reason
	}
	return store.InsertOrderEvent(ctx, tx, tid, o)
}

// SO đọc đơn bán và lịch giao.
func (s *Service) SO(ctx context.Context, id uuid.UUID) (SOView, error) {
	var out SOView
	err := s.read(ctx, func(tx pgx.Tx) error { return loadSO(ctx, tx, id, &out) })
	return out, err
}

// ListSOs liệt kê đơn bán.
func (s *Service) ListSOs(ctx context.Context, customer *uuid.UUID, status string, limit int) ([]store.SalesOrder, error) {
	var out []store.SalesOrder
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListOrders(ctx, tx, customer, status, clampLimit(limit))
		return e
	})
	return out, err
}

func loadSO(ctx context.Context, tx pgx.Tx, id uuid.UUID, out *SOView) error {
	o, err := store.GetOrder(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if o == nil {
		return ErrOrderNotFound
	}
	out.Order = *o
	out.Deliveries, err = store.DeliveriesOfOrder(ctx, tx, id)
	return err
}

func (s *Service) replaySO(ctx context.Context, idem string, out *SOView) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		id, err := store.EntityByIdem(ctx, tx, "order_event", "order_id", idem)
		if err != nil || id == nil {
			return false, err
		}
		return true, loadSO(ctx, tx, *id, out)
	}
}
