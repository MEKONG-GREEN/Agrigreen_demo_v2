package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// refigure tính lại số liệu quyết toán gốc từ bảng chiếu lệnh gom (PM-01/02/03). Giá, cách tính,
// khấu trừ giữ nguyên; số cuộn, khối lượng cân, số tiền, trạng thái làm mới.
func refigure(st store.Settlement, c *store.Collection, pending bool) store.Settlement {
	if st.Kind != "original" {
		return st
	}
	next := st
	next.Rolls, next.MassKg = nil, nil
	if c != nil {
		next.Rolls, next.MassKg = c.Rolls, c.WeighedKg
	}
	basis := ""
	if st.Basis != nil {
		basis = *st.Basis
	}
	f := domain.ComputeSettlement(domain.SettlementInput{PriceBasis: basis, UnitPrice: st.UnitPrice, Rolls: next.Rolls,
		WeighedKg: next.MassKg, PendingLoads: pending, Deductions: st.Deductions})
	next.Status, next.BlockedReason, next.GrossAmount, next.Amount = f.Status, strPtr(f.Blocked), f.Gross, f.Amount
	return next
}

func eqStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sameFigures(a, b store.Settlement) bool {
	return a.Status == b.Status && eqStr(a.BlockedReason, b.BlockedReason) && eqStr(kgArgs(a.MassKg), kgArgs(b.MassKg)) &&
		eqStr(amtArgs(a.GrossAmount), amtArgs(b.GrossAmount)) && eqStr(amtArgs(a.Amount), amtArgs(b.Amount)) &&
		((a.Rolls == nil && b.Rolls == nil) || (a.Rolls != nil && b.Rolls != nil && *a.Rolls == *b.Rolls))
}

func kgArgs(k *mass.Kg) *string {
	if k == nil {
		return nil
	}
	s := k.String()
	return &s
}

func amtArgs(a *money.Amount) *string {
	if a == nil {
		return nil
	}
	s := a.String()
	return &s
}

// CreateSettlementInput là yêu cầu lập quyết toán HTX cho một lệnh gom.
type CreateSettlementInput struct {
	CollectionOrderRef uuid.UUID
	HTXRef             uuid.UUID
	HarvestDate        string // YYYY-MM-DD — ngày tra hợp đồng hiệu lực (proto contractFor)
	Deductions         money.Amount
	Reason             string // bắt buộc khi có khấu trừ
	IdemKey            string
}

func parseDate(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", s, clock.Zone())
	if err != nil {
		return time.Time{}, problem.Validation.WithField("harvest_date", "YYYY-MM-DD")
	}
	return t, nil
}

func (s *Service) loadSettlement(ctx context.Context, tx pgx.Tx, id uuid.UUID, forUpdate bool) (store.Settlement, error) {
	st, err := store.SettlementByID(ctx, tx, id, forUpdate)
	if err != nil {
		return store.Settlement{}, err
	}
	if st == nil {
		return store.Settlement{}, ErrSettlementNotFound
	}
	return *st, nil
}

// settlementReplay đọc quyết toán của bản ghi sổ có idem_key.
func settlementReplay(ctx context.Context, tx pgx.Tx, idem string, out *store.Settlement) (bool, error) {
	id, err := store.SettlementIDByIdem(ctx, tx, idem)
	if err != nil || id == nil {
		return false, err
	}
	st, err := store.SettlementByID(ctx, tx, *id, false)
	if err == nil && st != nil {
		*out = *st
	}
	return true, err
}

// CreateSettlement lập quyết toán HTX (cost_accountant). Giá lấy từ hợp đồng hiệu lực ngày gặt
// (orders/port.Contracts); ErrNoContract → quyết toán bị chặn "no_contract", không có số tiền —
// hệ thống KHÔNG đoán giá (proto PM-03).
func (s *Service) CreateSettlement(ctx context.Context, in CreateSettlementInput) (st store.Settlement, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return st, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return st, false, err
	}
	if in.CollectionOrderRef == uuid.Nil || in.HTXRef == uuid.Nil {
		return st, false, problem.Validation.WithField("collection_order_ref", "bắt buộc").WithField("htx_ref", "bắt buộc")
	}
	day, err := parseDate(in.HarvestDate)
	if err != nil {
		return st, false, err
	}
	if in.Deductions.Decimal().IsNegative() {
		return st, false, problem.Validation.WithField("deductions", "≥ 0")
	}
	reason := strings.TrimSpace(in.Reason)
	if !in.Deductions.IsZero() && (len([]rune(reason)) < 10 || len([]rune(reason)) > 500) {
		return st, false, problem.Validation.WithField("reason", "khấu trừ cần lý do 10..500 ký tự")
	}
	if s.Deps.Contracts == nil {
		return st, false, ErrContractsUnavailable
	}
	terms, cerr := s.Deps.Contracts.ContractFor(ctx, p.TenantID, in.HTXRef, day)
	noContract := errors.Is(cerr, ordport.ErrNoContract)
	if cerr != nil && !noContract {
		return st, false, ErrContractsUnavailable.WithCause(cerr)
	}
	pending := s.pendingLoads(ctx, p.TenantID, in.CollectionOrderRef)
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockKey(ctx, tx, "order:"+in.CollectionOrderRef.String()); err != nil {
			return err
		}
		c, err := store.CollectionByOrder(ctx, tx, in.CollectionOrderRef)
		if err != nil {
			return err
		}
		draft := store.Settlement{ID: ids.New(), Kind: "original", CollectionOrderRef: in.CollectionOrderRef, HTXRef: in.HTXRef,
			HarvestDate: in.HarvestDate, Deductions: in.Deductions, PreparedBy: p.ActorRef}
		if !noContract {
			basis, src, ver, price := string(terms.Basis), "contract", terms.Version, terms.UnitPrice
			cid := terms.ContractID
			draft.ContractRef, draft.ContractVersion, draft.Basis, draft.PriceSource, draft.UnitPrice = &cid, &ver, &basis, &src, &price
		}
		st = refigure(draft, c, pending)
		e := store.SettlementEvent{ID: ids.New(), Action: "drafted", State: st, Reason: strPtr(reason), Pins: domain.Pins{},
			EventTime: s.now(), ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		if err := store.InsertSettlementEvent(ctx, tx, p.TenantID, &e); err != nil {
			return err
		}
		st = e.State
		return nil
	}, func(tx pgx.Tx) (bool, error) { return settlementReplay(ctx, tx, in.IdemKey, &st) })
	return st, replayed, err
}

// PriceSettlementInput là giá thỏa thuận người lập nhập khi không có hợp đồng (PM-03).
type PriceSettlementInput struct {
	ID        uuid.UUID
	Basis     string
	UnitPrice money.Price
	Reason    string
	IdemKey   string
}

// PriceSettlement ghi giá thỏa thuận cho quyết toán không có hợp đồng (cost_accountant — người nhập
// giá trở thành người lập, người duyệt phải khác).
func (s *Service) PriceSettlement(ctx context.Context, in PriceSettlementInput) (st store.Settlement, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return st, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return st, false, err
	}
	if in.Basis != string(ordport.PricingPerTonne) && in.Basis != string(ordport.PricingPerBale) {
		return st, false, problem.Validation.WithField("basis", "per_tonne hoặc per_bale")
	}
	if in.UnitPrice.Decimal().Sign() <= 0 {
		return st, false, problem.Validation.WithField("unit_price", "> 0 — PM-03: phải nhập đơn giá thỏa thuận")
	}
	reason := strings.TrimSpace(in.Reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return st, false, problem.Validation.WithField("reason", "bắt buộc 10..500 ký tự (căn cứ giá thỏa thuận)")
	}
	var order uuid.UUID
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		cur, err := s.loadSettlement(ctx, tx, in.ID, false)
		order = cur.CollectionOrderRef
		return err
	}); err != nil {
		return st, false, mapErr(err)
	}
	pending := s.pendingLoads(ctx, p.TenantID, order)
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := s.loadSettlement(ctx, tx, in.ID, true)
		if err != nil {
			return err
		}
		if cur.Kind != "original" || (cur.Status != port.SettlementPendingConfirmation && cur.Status != port.SettlementAwaitingWeighing) ||
			(cur.PriceSource != nil && *cur.PriceSource == "contract") {
			return ErrSettlementTransition.WithDetail("chỉ nhập giá cho quyết toán chưa duyệt, không có hợp đồng")
		}
		c, err := store.CollectionByOrder(ctx, tx, cur.CollectionOrderRef)
		if err != nil {
			return err
		}
		next := cur
		basis, src, price := in.Basis, "manual", in.UnitPrice
		next.Basis, next.PriceSource, next.UnitPrice, next.PreparedBy = &basis, &src, &price, p.ActorRef
		next = refigure(next, c, pending)
		e := store.SettlementEvent{ID: ids.New(), Action: "priced", State: next, Reason: &reason, Pins: domain.Pins{}, EventTime: s.now(),
			ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		if err := store.InsertSettlementEvent(ctx, tx, p.TenantID, &e); err != nil {
			return err
		}
		st = e.State
		return nil
	}, func(tx pgx.Tx) (bool, error) { return settlementReplay(ctx, tx, in.IdemKey, &st) })
	return st, replayed, err
}

// ApproveSettlement duyệt quyết toán (chief_accountant ≠ người lập — R-56 app + CHECK DB): ghi ĐÚNG
// MỘT khoản phải trả, hạn thanh toán = ngày duyệt + tham số (15 ngày, tạm), đóng băng số liệu;
// phát mkg.costing.settlement.approved (nội bộ, không qua van).
func (s *Service) ApproveSettlement(ctx context.Context, id uuid.UUID, idem string) (st store.Settlement, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleChiefAccountant)
	if err != nil {
		return st, false, err
	}
	if err := requireIdem(idem); err != nil {
		return st, false, err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := s.loadSettlement(ctx, tx, id, true)
		if err != nil {
			return err
		}
		switch cur.Status {
		case port.SettlementApproved, port.SettlementPaid:
			return ErrSettlementFrozen
		case port.SettlementCancelled:
			return ErrSettlementTransition
		case port.SettlementAwaitingWeighing:
			return ErrSettlementNotReady.WithDetail("PM-02: còn ghe chưa cân — trả theo tấn cân phải cân đủ trước khi duyệt")
		}
		if cur.Amount == nil || cur.Basis == nil || cur.UnitPrice == nil {
			return ErrSettlementNotReady.WithDetail("PM-03: chưa có đơn giá — người lập nhập giá thỏa thuận trước")
		}
		if (cur.Kind == "original" && cur.Amount.Decimal().Sign() <= 0) || cur.Amount.IsZero() {
			return ErrSettlementNotReady.WithDetail("số tiền phải khác 0 (quyết toán gốc: > 0)")
		}
		if cur.PreparedBy == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval)
		}
		now := s.now()
		term, err := paramAt(ctx, tx, port.ParamSettlementTermDays, now, nil)
		if err != nil {
			return err
		}
		period, _, err := s.targetPeriod(ctx, tx, p.TenantID, now)
		if err != nil {
			return err
		}
		due := domain.BusinessDate(now.AddDate(0, 0, term.Int()))
		next := cur
		approver, pid := p.ActorRef, period.ID
		next.Status, next.ApprovedBy, next.DueOn, next.PeriodRef = port.SettlementApproved, &approver, &due, &pid
		next.BlockedReason = nil
		e := store.SettlementEvent{ID: ids.New(), Action: "approved", State: next, Pins: domain.Pins{}.Add(term), EventTime: now,
			ActorRef: p.ActorRef, IdemKey: idem}
		if err := store.InsertSettlementEvent(ctx, tx, p.TenantID, &e); err != nil {
			return err
		}
		st = e.State
		ev := port.SettlementApprovedV1{
			SettlementID: st.ID, PeriodID: &pid, HTXRef: st.HTXRef, CollectionOrderRef: &st.CollectionOrderRef, ContractRef: st.ContractRef,
			Basis: *st.Basis, Rolls: st.Rolls, UnitPrice: st.UnitPrice.String(), Amount: *st.Amount, DueOn: due, PreparedBy: st.PreparedBy,
			ApprovedBy: approver, RuleVersion: domain.RuleSettlement, SupersedesID: st.SupersedesID, EventTime: now,
		}
		if *st.Basis == string(ordport.PricingPerTonne) {
			ev.MassKg = st.MassKg
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: e.ID, TenantID: p.TenantID, Type: port.EventSettlementApproved, SchemaVersion: 1,
			OccurredAt: now, TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: ev, Kind: outbox.KindInternal})
	}, func(tx pgx.Tx) (bool, error) { return settlementReplay(ctx, tx, idem, &st) })
	return st, replayed, err
}

// PaySettlement ghi đã trả (khoản phải trả đã duyệt).
func (s *Service) PaySettlement(ctx context.Context, id uuid.UUID, idem string) (st store.Settlement, replayed bool, err error) {
	return s.transition(ctx, id, idem, "paid", "", []string{port.RoleCostAccountant, port.RoleChiefAccountant},
		func(cur store.Settlement) (store.Settlement, error) {
			if cur.Status != port.SettlementApproved {
				return cur, ErrSettlementTransition.WithDetail("chỉ trả quyết toán đã duyệt (đang là khoản phải trả)")
			}
			cur.Status = port.SettlementPaid
			return cur, nil
		})
}

// CancelSettlement hủy quyết toán chưa duyệt (có lý do). Đã duyệt → chỉ hoàn bằng quyết toán điều
// chỉnh; đã trả → không hủy (proto cancelTicket).
func (s *Service) CancelSettlement(ctx context.Context, id uuid.UUID, reason, idem string) (st store.Settlement, replayed bool, err error) {
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return st, false, problem.Validation.WithField("reason", "bắt buộc 10..500 ký tự")
	}
	return s.transition(ctx, id, idem, "cancelled", reason, []string{port.RoleCostAccountant},
		func(cur store.Settlement) (store.Settlement, error) {
			switch cur.Status {
			case port.SettlementPaid:
				return cur, ErrSettlementPaid
			case port.SettlementApproved:
				return cur, ErrSettlementFrozen
			case port.SettlementCancelled:
				return cur, ErrSettlementTransition
			}
			cur.Status = port.SettlementCancelled
			return cur, nil
		})
}

func (s *Service) transition(ctx context.Context, id uuid.UUID, idem, action, reason string, roles []string,
	fn func(store.Settlement) (store.Settlement, error)) (st store.Settlement, replayed bool, err error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return st, false, err
	}
	if err := requireIdem(idem); err != nil {
		return st, false, err
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := s.loadSettlement(ctx, tx, id, true)
		if err != nil {
			return err
		}
		next, err := fn(cur)
		if err != nil {
			return err
		}
		e := store.SettlementEvent{ID: ids.New(), Action: action, State: next, Reason: strPtr(reason), Pins: domain.Pins{},
			EventTime: s.now(), ActorRef: p.ActorRef, IdemKey: idem}
		if err := store.InsertSettlementEvent(ctx, tx, p.TenantID, &e); err != nil {
			return err
		}
		st = e.State
		return nil
	}, func(tx pgx.Tx) (bool, error) { return settlementReplay(ctx, tx, idem, &st) })
	return st, replayed, err
}

// AdjustSettlement lập quyết toán ĐIỀU CHỈNH cho quyết toán đã duyệt / đã trả (bút toán đảo khi
// âm): số tiền có dấu ≠ 0, lý do bắt buộc; đi qua duyệt như quyết toán gốc (R-56).
func (s *Service) AdjustSettlement(ctx context.Context, id uuid.UUID, amount money.Amount, reason, idem string) (st store.Settlement, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return st, false, err
	}
	if err := requireIdem(idem); err != nil {
		return st, false, err
	}
	if amount.IsZero() {
		return st, false, problem.Validation.WithField("amount", "≠ 0 (âm = đảo)")
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return st, false, problem.Validation.WithField("reason", "bắt buộc 10..500 ký tự")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := s.loadSettlement(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur.Status != port.SettlementApproved && cur.Status != port.SettlementPaid {
			return ErrSettlementTransition.WithDetail("chỉ điều chỉnh quyết toán đã duyệt / đã trả; chưa duyệt thì sửa trực tiếp")
		}
		old := cur.ID
		a := amount
		adj := store.Settlement{ID: ids.New(), Kind: "adjustment", Status: port.SettlementPendingConfirmation,
			CollectionOrderRef: cur.CollectionOrderRef, HTXRef: cur.HTXRef, HarvestDate: cur.HarvestDate, ContractRef: cur.ContractRef,
			ContractVersion: cur.ContractVersion, Basis: cur.Basis, PriceSource: cur.PriceSource, UnitPrice: cur.UnitPrice,
			Deductions: money.MustAmount("0"), Amount: &a, PreparedBy: p.ActorRef, SupersedesID: &old}
		e := store.SettlementEvent{ID: ids.New(), Action: "drafted", State: adj, Reason: &reason, Pins: domain.Pins{}, EventTime: s.now(),
			ActorRef: p.ActorRef, IdemKey: idem}
		if err := store.InsertSettlementEvent(ctx, tx, p.TenantID, &e); err != nil {
			return err
		}
		st = e.State
		return nil
	}, func(tx pgx.Tx) (bool, error) { return settlementReplay(ctx, tx, idem, &st) })
	return st, replayed, err
}

// Settlement đọc một quyết toán.
func (s *Service) Settlement(ctx context.Context, id uuid.UUID) (st store.Settlement, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		st, err = s.loadSettlement(ctx, tx, id, false)
		return err
	})
	return st, err
}

// Settlements liệt kê quyết toán.
func (s *Service) Settlements(ctx context.Context, status string, order *uuid.UUID, before *uuid.UUID, limit int) (out []store.Settlement, err error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		out, err = store.ListSettlements(ctx, tx, status, order, before, limit)
		return err
	})
	return out, err
}
