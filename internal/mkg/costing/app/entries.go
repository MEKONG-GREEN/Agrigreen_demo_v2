package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ManualEntryInput là bút toán chi phí ghi tay của kế toán giá thành (gom, vật tư, lưu kho, bốc xếp…).
type ManualEntryInput struct {
	LotRef        uuid.UUID
	Category      port.CostCategory
	Amount        money.Amount
	MassKg        *mass.Kg
	DimensionKind string
	DimensionRef  *uuid.UUID
	EventTime     time.Time
	Note          string
	IdemKey       string
}

var dimKinds = map[string]bool{"facility": true, "htx": true, "customer": true, "mode": true, "none": true}

// ManualEntry ghi bút toán chi phí vào kỳ của event_time (kỳ phải mở — kỳ đã đóng dùng điều chỉnh).
func (s *Service) ManualEntry(ctx context.Context, in ManualEntryInput) (e store.Entry, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return e, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return e, false, err
	}
	v := problem.Validation
	bad := false
	if in.LotRef == uuid.Nil {
		v, bad = v.WithField("lot_ref", "bắt buộc"), true
	}
	if !domain.ManualCategories[in.Category] {
		v, bad = v.WithField("category", "nhóm chi phí ghi tay: collection_labour, consumables, transport_road, transport_waterway, hub_processing, storage, handling, loss"), true
	}
	if in.DimensionKind == "" {
		in.DimensionKind = "none"
	}
	if !dimKinds[in.DimensionKind] || (in.DimensionKind == "none") != (in.DimensionRef == nil) {
		v, bad = v.WithField("dimension_kind", "facility | htx | customer | mode kèm dimension_ref, hoặc none"), true
	}
	if in.EventTime.IsZero() {
		in.EventTime = s.now()
	}
	if in.EventTime.After(s.now().Add(5 * time.Minute)) {
		v, bad = v.WithField("event_time", "không ở tương lai"), true
	}
	if bad {
		return e, false, v
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		per, err := s.ensurePeriod(ctx, tx, p.TenantID, domain.PeriodCode(in.EventTime), domain.SystemActorRef)
		if err != nil {
			return err
		}
		if per.Status != port.PeriodOpen {
			return ErrPeriodNotOpen.WithDetail("kỳ " + per.Code + " không mở — sai sót sau đóng kỳ ghi bằng bút toán điều chỉnh ở kỳ đang mở")
		}
		e = store.Entry{ID: ids.New(), LotRef: in.LotRef, PeriodCode: per.Code, Category: string(in.Category), Amount: in.Amount,
			DeltaAmount: in.Amount, MassKg: in.MassKg, DeltaMassKg: in.MassKg, DimensionKind: in.DimensionKind, DimensionRef: in.DimensionRef,
			SourceKind: "manual", EntryKind: "normal", Note: strPtr(strings.TrimSpace(in.Note)), EventTime: in.EventTime.UTC(),
			ActorRef: p.ActorRef, IdemKey: in.IdemKey, RuleVersion: domain.RuleManual}
		return store.InsertEntry(ctx, tx, p.TenantID, &e)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.EntryByIdem(ctx, tx, in.IdemKey)
		if prev != nil {
			e = *prev
		}
		return prev != nil, err
	})
	return e, replayed, err
}

// AdjustEntry lập bút toán ĐIỀU CHỈNH thay một bút toán hiệu lực (kể cả của kỳ đã đóng): dòng mới
// ở kỳ ĐANG MỞ (tháng hiện tại), supersedes_id trỏ dòng cũ, delta = số mới − số cũ. Người lập điều
// chỉnh không được đóng kỳ đó (R-56).
func (s *Service) AdjustEntry(ctx context.Context, id uuid.UUID, amount money.Amount, kg *mass.Kg, reason, idem string) (e store.Entry, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return e, false, err
	}
	if err := requireIdem(idem); err != nil {
		return e, false, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return e, false, problem.Validation.WithField("reason", "bắt buộc 10..500 ký tự")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		old, err := store.EntryByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if old == nil {
			return ErrEntryNotFound
		}
		if err := store.LockKey(ctx, tx, "entry:"+id.String()); err != nil {
			return err
		}
		if sup, err := store.EntrySuperseded(ctx, tx, id); err != nil {
			return err
		} else if sup {
			return ErrEntrySuperseded
		}
		now := s.now()
		per, late, err := s.targetPeriod(ctx, tx, p.TenantID, now)
		if err != nil {
			return err
		}
		lateFrom := late
		if old.PeriodCode != per.Code {
			c := old.PeriodCode
			lateFrom = &c
		}
		e = store.Entry{ID: ids.New(), LotRef: old.LotRef, PeriodCode: per.Code, LateFrom: lateFrom, Category: old.Category, Amount: amount,
			DeltaAmount: amount.Sub(old.Amount), MassKg: kg, DeltaMassKg: deltaKg(kg, old.MassKg), DimensionKind: old.DimensionKind,
			DimensionRef: old.DimensionRef, SourceKind: "adjusting", SourceRef: old.SourceRef, AllocationRef: old.AllocationRef,
			EntryKind: "adjusting", EventTime: now, SupersedesID: &old.ID, CorrectionReason: &reason, ActorRef: p.ActorRef, IdemKey: idem,
			RuleVersion: domain.RuleAdjusting}
		return store.InsertEntry(ctx, tx, p.TenantID, &e)
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.EntryByIdem(ctx, tx, idem)
		if prev != nil {
			e = *prev
		}
		return prev != nil, err
	})
	return e, replayed, err
}

// Recompute tính lại một kết quả phân bổ hiệu lực bằng đầu vào đã lưu và tham số tại thời điểm SỰ
// VIỆC theo quyết định đã biết lúc asOf (R-13; nil = mọi quyết định đã ghi). Khác kết quả cũ → ghi
// kết quả MỚI supersedes_id (kết quả cũ không đổi) + bút toán delta; giống → unchanged.
func (s *Service) Recompute(ctx context.Context, id uuid.UUID, asOf *time.Time, reason, idem string) (a store.Allocation, changed, replayed bool, err error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant)
	if err != nil {
		return a, false, false, err
	}
	if err := requireIdem(idem); err != nil {
		return a, false, false, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return a, false, false, problem.Validation.WithField("reason", "bắt buộc 10..500 ký tự")
	}
	replayed, err = s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		old, err := store.AllocationByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if old == nil {
			return ErrAllocationNotFound
		}
		if err := store.LockKey(ctx, tx, old.Kind+":"+old.SourceRef.String()); err != nil {
			return err
		}
		if old.SupersededBy != nil {
			return ErrAllocationSuperseded
		}
		var in any
		switch old.Kind {
		case AllocHubProcessing:
			in = &HubInputs{}
		case AllocTripCost:
			in = &TripInputs{}
		case AllocDelivery:
			in = &DeliveryInputs{}
		default:
			in = &LossInputs{}
		}
		if err := jsonUnmarshal(old.Inputs, in); err != nil {
			return err
		}
		a, changed, err = s.persistAllocation(ctx, tx, p.TenantID, allocReq{Kind: old.Kind, SourceRef: old.SourceRef,
			EventTime: old.SourceEventTime, Inputs: in, Old: old, AsOf: asOf, Actor: p.ActorRef, IdemKey: idem, Reason: reason})
		return err
	}, func(tx pgx.Tx) (bool, error) {
		prev, err := store.AllocationByIdem(ctx, tx, idem)
		if prev != nil {
			a, changed = *prev, true
		}
		return prev != nil, err
	})
	return a, changed, replayed, err
}

// Allocation đọc một kết quả phân bổ.
func (s *Service) Allocation(ctx context.Context, id uuid.UUID) (a store.Allocation, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		cur, err := store.AllocationByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrAllocationNotFound
		}
		a = *cur
		return nil
	})
	return a, err
}

// AllocationsForSource liệt kê mọi kết quả (kể cả đã bị thay) của một nguồn.
func (s *Service) AllocationsForSource(ctx context.Context, source uuid.UUID) (out []store.Allocation, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		out, err = store.AllocationsForSource(ctx, tx, source)
		return err
	})
	return out, err
}

// LotCostView là giá thành lô (proto buildCostChain).
type LotCostView struct {
	LotRef       uuid.UUID
	Categories   []store.LotCost
	TotalCost    money.Amount  // Σ nhóm chi phí (không gồm doanh thu, carbon, mua rơm, khấu hao)
	Revenue      money.Amount  // doanh thu giao nhận
	DeliveredKg  mass.Kg       // khối lượng giao (CUSTOMER_IN chấp nhận)
	CostPerTonne *money.Amount // nil = "không xác định" (tấn giao = 0)
}

// LotCost đọc giá thành lô từ bảng chiếu.
func (s *Service) LotCost(ctx context.Context, lot uuid.UUID) (v LotCostView, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		cs, err := store.LotCosts(ctx, tx, lot)
		if err != nil {
			return err
		}
		if len(cs) == 0 {
			return ErrLotNotFound
		}
		v = LotCostView{LotRef: lot, Categories: cs}
		for _, c := range cs {
			switch {
			case domain.CostCategories[port.CostCategory(c.Category)]:
				v.TotalCost = v.TotalCost.Add(c.Amount)
			case c.Category == string(port.CatSalesRevenue):
				v.Revenue = v.Revenue.Add(c.Amount)
				if c.MassKg != nil {
					v.DeliveredKg = v.DeliveredKg.Add(*c.MassKg)
				}
			}
		}
		v.CostPerTonne = domain.CostPerTonne(v.TotalCost, v.DeliveredKg)
		return nil
	})
	return v, err
}

// LotEntries liệt kê bút toán của lô theo chuỗi sổ.
func (s *Service) LotEntries(ctx context.Context, lot uuid.UUID, afterSeq int64, limit int) (out []store.Entry, err error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		out, err = store.EntriesForLot(ctx, tx, lot, afterSeq, limit)
		return err
	})
	return out, err
}

// Conversion đọc quy đổi cuộn → tấn hiệu lực của lệnh gom.
func (s *Service) Conversion(ctx context.Context, order uuid.UUID) (c store.Conversion, err error) {
	err = s.read(ctx, func(tx pgx.Tx, _ authz.Principal) error {
		cur, err := store.CurrentConversion(ctx, tx, order)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrConversionNotFound
		}
		c = *cur
		return nil
	})
	return c, err
}

// RebuildProjections dựng lại mọi bảng chiếu của tenant từ sổ (R-22) — chief_accountant / cost_accountant.
func (s *Service) RebuildProjections(ctx context.Context) (map[string]int64, error) {
	p, err := authz.Require(ctx, port.RoleCostAccountant, port.RoleChiefAccountant)
	if err != nil {
		return nil, err
	}
	return s.RebuildTenant(ctx, p.TenantID)
}

// RebuildTenant dựng lại bảng chiếu của tenant tid từ sổ — tenant TƯỜNG MINH, không kiểm vai: chỉ cho công cụ
// vận hành chạy trên BẢN SAO database (diễn tập R-22, cmd/rebuilddrill). API dùng RebuildProjections (có vai).
func (s *Service) RebuildTenant(ctx context.Context, tid uuid.UUID) (map[string]int64, error) {
	var out map[string]int64
	err := s.writeNoReplay(ctx, tid, func(tx pgx.Tx) error {
		var err error
		out, err = store.RebuildAll(ctx, tx, tid)
		return err
	})
	return out, err
}

func (s *Service) writeNoReplay(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	_, err := s.write(ctx, tid, fn, func(pgx.Tx) (bool, error) { return false, nil })
	return err
}
