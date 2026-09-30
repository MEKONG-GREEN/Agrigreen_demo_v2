package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// POInput là một đơn mua mới (proto createPurchaseOrder).
type POInput struct {
	Category     string // straw | other
	SupplierRef  uuid.UUID
	FacilityRef  *uuid.UUID
	Unit         string // kg | bale; rơm có hợp đồng: rỗng = theo cách tính của hợp đồng
	OrderedQty   decimal.Decimal
	UnitPrice    *money.Price // rơm có hợp đồng: rỗng = giá hợp đồng; không hợp đồng → bắt buộc (không đoán giá)
	ExpectedDate string
	Description  string
	IdemKey      string
}

// CreatePO lập đơn mua (draft). Rơm của HTX: giá lấy từ hợp đồng hiệu lực (Contracts.ContractFor) tại ngày
// dự kiến nhận; không có hợp đồng thì phải nhập giá — hệ thống không bao giờ đoán giá.
func (s *Service) CreatePO(ctx context.Context, in POInput) (store.PO, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer)
	if err != nil {
		return store.PO{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return store.PO{}, false, err
	}
	if in.Category != domain.CategoryStraw && in.Category != domain.CategoryOther {
		return store.PO{}, false, mapErr(domain.Field("category", "straw | other"))
	}
	if in.SupplierRef == uuid.Nil {
		return store.PO{}, false, mapErr(domain.Field("supplier_ref", "bắt buộc"))
	}
	if in.Unit != "" && in.Unit != domain.UnitKg && in.Unit != domain.UnitBale {
		return store.PO{}, false, mapErr(domain.Field("unit", "kg | bale"))
	}
	if len([]rune(in.Description)) > 1000 {
		return store.PO{}, false, mapErr(domain.Field("description", "tối đa 1000 ký tự"))
	}
	now := s.Clock.Now()
	on := domain.Day(now)
	var expected *time.Time
	if in.ExpectedDate != "" {
		d, err := domain.ParseDate("expected_date", in.ExpectedDate)
		if err != nil {
			return store.PO{}, false, mapErr(err)
		}
		expected, on = &d, d
	}
	var out store.PO
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		po := store.PO{Meta: store.Meta{ID: ids.New(), EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RulePO},
			Kind: "created", POID: ids.New(), Status: "draft", Category: in.Category, SupplierRef: in.SupplierRef, FacilityRef: in.FacilityRef,
			Unit: in.Unit, OrderedQty: in.OrderedQty, ExpectedDate: expected, Description: strPtr(in.Description), CreatorRef: p.ActorRef,
			PriceSource: "entered"}
		if in.Category == domain.CategoryStraw {
			c, err := store.ContractOn(ctx, tx, in.SupplierRef, on)
			if err != nil {
				return err
			}
			if c != nil {
				unit := domain.UnitForBasis(port.PricingBasis(c.Basis))
				if po.Unit == "" {
					po.Unit = unit
				}
				if po.Unit != unit {
					return ErrBasisMismatch.WithDetail("hợp đồng " + c.Code + " tính " + c.Basis)
				}
				if in.UnitPrice != nil && !in.UnitPrice.Decimal().Equal(c.UnitPrice) {
					return mapErr(domain.Field("unit_price", "khác đơn giá hợp đồng "+c.Code+" ("+c.UnitPrice.StringFixed(4)+")"))
				}
				po.UnitPrice, po.PriceSource, po.ContractRef, po.ContractVersion = c.UnitPrice, "contract", &c.ContractID, &c.Version
			}
		}
		if po.PriceSource == "entered" {
			if in.UnitPrice == nil {
				return ErrPriceRequired
			}
			po.UnitPrice = in.UnitPrice.Decimal()
		}
		if po.Unit == "" {
			return mapErr(domain.Field("unit", "bắt buộc: kg | bale"))
		}
		if po.UnitPrice.Sign() <= 0 {
			return mapErr(domain.Field("unit_price", "đơn giá phải lớn hơn 0"))
		}
		if err := domain.CheckQty("ordered_qty", po.Unit, po.OrderedQty, false); err != nil {
			return mapErr(err)
		}
		if err := store.Lock(ctx, tx, p.TenantID, "code:po"); err != nil {
			return err
		}
		n, err := store.NextCode(ctx, tx, "po_current", "po_id")
		if err != nil {
			return err
		}
		po.Code = domain.Code("PO", n, 6)
		if err := store.InsertPOEvent(ctx, tx, p.TenantID, &po); err != nil {
			return err
		}
		return loadPO(ctx, tx, po.POID, &out)
	}, s.replayPO(ctx, in.IdemKey, &out))
	return out, replayed, err
}

// DecidePO duyệt / từ chối đơn mua nháp — người duyệt (sales_approver) KHÁC người lập (R-56).
func (s *Service) DecidePO(ctx context.Context, id uuid.UUID, approve bool, reason, idem string) (store.PO, bool, error) {
	p, err := authz.Require(ctx, RoleApprover)
	if err != nil {
		return store.PO{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return store.PO{}, false, err
	}
	r, err := domain.CheckReason("reason", reason, !approve)
	if err != nil {
		return store.PO{}, false, mapErr(err)
	}
	var out store.PO
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		po, err := store.GetPO(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if po == nil {
			return ErrPONotFound
		}
		if po.Status != "draft" {
			return ErrPOTransition.WithDetail("chỉ duyệt đơn mua nháp (proto approvePurchaseOrder)")
		}
		if err := authz.EnsureDistinctApproverFor(po.CreatorRef, p, RoleApprover); err != nil {
			return err
		}
		po.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RulePO}
		if approve {
			po.Kind, po.Status, po.ApproverRef = "approved", "approved", &p.ActorRef
		} else {
			po.Kind, po.Status, po.Reason = "rejected", "rejected", &r
		}
		if err := store.InsertPOEvent(ctx, tx, p.TenantID, po); err != nil {
			return err
		}
		return loadPO(ctx, tx, id, &out)
	}, s.replayPO(ctx, idem, &out))
	return out, replayed, err
}

// CancelPO hủy đơn mua nháp / đã duyệt chưa có phiếu nhận hiệu lực — có lý do.
func (s *Service) CancelPO(ctx context.Context, id uuid.UUID, reason, idem string) (store.PO, bool, error) {
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
	if err != nil {
		return store.PO{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return store.PO{}, false, err
	}
	r, err := domain.CheckReason("reason", reason, true)
	if err != nil {
		return store.PO{}, false, mapErr(err)
	}
	var out store.PO
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		po, err := store.GetPO(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if po == nil {
			return ErrPONotFound
		}
		if po.Status != "draft" && po.Status != "approved" {
			return ErrPOTransition.WithDetail("đơn mua đã hủy / bị từ chối")
		}
		rs, err := store.ReceiptsOfPO(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, x := range rs {
			if x.Status != "rejected" {
				return ErrPOTransition.WithDetail("đơn mua đã có phiếu nhận hàng " + x.Code)
			}
		}
		po.Meta = store.Meta{ID: ids.New(), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem, RuleVersion: domain.RulePO}
		po.Kind, po.Status, po.Reason = "cancelled", "cancelled", &r
		if err := store.InsertPOEvent(ctx, tx, p.TenantID, po); err != nil {
			return err
		}
		return loadPO(ctx, tx, id, &out)
	}, s.replayPO(ctx, idem, &out))
	return out, replayed, err
}

// POView là đơn mua kèm phiếu nhận.
type POView struct {
	PO       store.PO
	Receipts []store.Receipt
}

// PO đọc đơn mua và các phiếu nhận.
func (s *Service) PO(ctx context.Context, id uuid.UUID) (POView, error) {
	var v POView
	err := s.read(ctx, func(tx pgx.Tx) error {
		po, err := store.GetPO(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if po == nil {
			return ErrPONotFound
		}
		v.PO = *po
		v.Receipts, err = store.ReceiptsOfPO(ctx, tx, id)
		return err
	})
	return v, err
}

// ListPOs liệt kê đơn mua.
func (s *Service) ListPOs(ctx context.Context, supplier *uuid.UUID, status string, limit int) ([]store.PO, error) {
	var out []store.PO
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListPOs(ctx, tx, supplier, status, clampLimit(limit))
		return e
	})
	return out, err
}

func loadPO(ctx context.Context, tx pgx.Tx, id uuid.UUID, out *store.PO) error {
	po, err := store.GetPO(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if po == nil {
		return ErrPONotFound
	}
	*out = *po
	return nil
}

func (s *Service) replayPO(ctx context.Context, idem string, out *store.PO) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		id, err := store.EntityByIdem(ctx, tx, "po_event", "po_id", idem)
		if err != nil || id == nil {
			return false, err
		}
		return true, loadPO(ctx, tx, *id, out)
	}
}
