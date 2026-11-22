package app

// Lứa nuôi (NC-02 nhập một lần cho lứa, NC-03 vật tư nhận, NC-15 hạch toán cuối lứa). Một hộ một lứa đang nuôi; kết thúc
// lứa ghi hạch toán (tổng chi theo khoản, tổng thu) — gợi ý chi thức ăn từ NC-09, thu từ NC-13 / thu hoạch BM-19; lợi
// nhuận = thu − chi tính khi đọc, lợi nhuận trung bình/hộ tính ở cấp điểm (danh sách lứa theo điểm).

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Suggest là gợi ý hạch toán từ các lần ghi của lứa (NC-15).
type Suggest struct {
	FeedCost     decimal.Decimal // Σ NC-09
	SoldHeads    int64           // Σ NC-13
	SoldKg       decimal.Decimal
	SalesAmount  decimal.Decimal // Σ NC-13 (khối lượng × đơn giá) hoặc thu hoạch BM-19
	CurrentHeads *int64          // số con sống ở lần ghi gần nhất (BM-08/09)
	LastEntry    *string         // ngày ghi gần nhất
	Counts       map[string]int  // số lần ghi theo trạng thái
}

// CycleView là lứa (bản hiện hành) cho API.
type CycleView struct {
	store.Cycle
	Status      string
	SiteName    string
	ProjectCode string
	OwnerLabel  string
	// Đầu sổ (NC-01): mô hình, thời gian thực hiện (ngày bắt đầu, kết thúc của chương trình/dự án), cán bộ theo dõi điểm.
	ProjectName  string
	ProjectStart time.Time
	ProjectEnd   *time.Time
	OfficerRef   *uuid.UUID
	CanEdit      bool
	TotalCost    decimal.Decimal
	Profit       *decimal.Decimal
	UnitCost     *decimal.Decimal
	Suggest      Suggest
	Versions     []store.Cycle // chỉ chi tiết
	Names        map[uuid.UUID]string
	Replayed     bool
}

func cycleStatus(event string) string {
	if event == domain.EventClosed {
		return domain.CycleClosed
	}
	return domain.CycleOpen
}

func suggest(es []store.Entry) Suggest {
	sg := Suggest{Counts: map[string]int{}}
	var latestAlive *store.Entry
	for i := range es {
		e := es[i]
		sg.Counts[e.Status]++
		if e.Status == domain.StatusDraft {
			continue
		}
		sg.FeedCost = sg.FeedCost.Add(e.Tables.FeedCost())
		h, kg, amt := e.Tables.SalesSum()
		sg.SoldHeads += h
		sg.SoldKg, sg.SalesAmount = sg.SoldKg.Add(kg), sg.SalesAmount.Add(amt)
		if hk := domain.Dec(e.Metrics, "harvest_kg"); hk != nil {
			sg.SoldKg = sg.SoldKg.Add(*hk)
			if pr := domain.Dec(e.Metrics, "harvest_price"); pr != nil {
				sg.SalesAmount = sg.SalesAmount.Add(hk.Mul(*pr).Round(0))
			}
		}
		if _, ok := domain.Int(e.Metrics, "alive"); ok && (latestAlive == nil || e.EntryDate.After(latestAlive.EntryDate)) {
			latestAlive = &es[i]
		}
		d := e.EntryDate.Format("2006-01-02")
		if sg.LastEntry == nil || d > *sg.LastEntry {
			sg.LastEntry = &d
		}
	}
	if latestAlive != nil {
		v, _ := domain.Int(latestAlive.Metrics, "alive")
		sg.CurrentHeads = &v
	}
	return sg
}

func (s *Service) cycleView(c caller, hc *hctx, cy store.Cycle, es []store.Entry) CycleView {
	v := CycleView{Cycle: cy, Status: cycleStatus(cy.Event), SiteName: hc.x.Name, ProjectCode: hc.p.Code, TotalCost: cy.TotalCost(),
		Profit: cy.Profit(), UnitCost: cy.UnitCost(), Suggest: suggest(es), ProjectName: hc.p.Name, ProjectStart: hc.p.Start,
		ProjectEnd: hc.p.End, OfficerRef: hc.x.OfficerRef}
	v.CanEdit = hc.open() && (c.keeper(hc) || (cy.Event == domain.EventClosed && c.reviewer(hc.p)))
	return v
}

// CycleInput là thân tạo/sửa lứa.
type CycleInput struct {
	Ref          *uuid.UUID // tạo: UUIDv7 tùy chọn
	HouseholdRef uuid.UUID  // tạo
	BaseVersion  *int64     // sửa
	domain.Cycle
	IdemKey string
}

func idemCycle(k string) string { return "dcycle:" + k }

// lockCycle đọc lứa, khóa theo hộ, kiểm phạm vi.
func (s *Service) lockCycle(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (*store.Cycle, *hctx, error) {
	cy, err := store.LatestCycle(ctx, tx, ref)
	if err != nil {
		return nil, nil, err
	}
	if cy == nil {
		return nil, nil, ErrCycleNotFound
	}
	if err := store.Lock(ctx, tx, "hh:"+cy.HouseholdRef.String()); err != nil {
		return nil, nil, err
	}
	if cy, err = store.LatestCycle(ctx, tx, ref); err != nil {
		return nil, nil, err
	}
	hc, err := s.loadHousehold(ctx, tx, c, cy.HouseholdRef)
	if err != nil {
		return nil, nil, err
	}
	if hc == nil {
		return nil, nil, ErrCycleNotFound
	}
	return cy, hc, nil
}

func (s *Service) checkCycle(c *domain.Cycle) error {
	if es := c.Normalize(s.today()); len(es) > 0 {
		return fieldsProblem(ErrCycleInvalid, es)
	}
	return nil
}

// CreateCycle mở lứa mới cho hộ.
func (s *Service) CreateCycle(ctx context.Context, in CycleInput) (CycleView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return CycleView{}, err
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	var out store.Cycle
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.CycleByIdem(ctx, tx, idemCycle(in.IdemKey)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		if err := store.Lock(ctx, tx, "hh:"+in.HouseholdRef.String()); err != nil {
			return err
		}
		hc, err := s.loadHousehold(ctx, tx, c, in.HouseholdRef)
		if err != nil {
			return err
		}
		if hc == nil {
			return problem.NotFound.WithField("household_ref", "không có hộ mô hình")
		}
		if !c.keeper(hc) {
			return problem.Forbidden.WithDetail("chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ mở lứa")
		}
		if !hc.open() {
			return ErrCycleClosed.WithDetail("hộ đã rút hoặc dự án/điểm đã kết thúc")
		}
		if in.Form == "" {
			in.Form = hc.x.DiaryForm
		}
		if err := s.checkCycle(&in.Cycle); err != nil {
			return err
		}
		if hc.x.DiaryForm != "" && hc.x.DiaryForm != in.Form {
			return ErrCycleInvalid.WithField("form", "điểm trình diễn dùng mẫu "+hc.x.DiaryForm)
		}
		if old, err := store.LatestCycle(ctx, tx, ref); err != nil {
			return err
		} else if old != nil {
			return ErrCycleInvalid.WithField("id", "mã lứa đã dùng")
		}
		cs, err := store.ListCycles(ctx, tx, store.CycleFilter{Household: &in.HouseholdRef})
		if err != nil {
			return err
		}
		for _, o := range cs {
			if o.Event != domain.EventClosed {
				return ErrCycleOpenExists
			}
		}
		out = store.Cycle{Meta: c.meta(s.now(), idemCycle(in.IdemKey)), Ref: ref, Event: domain.EventCreated, HouseholdRef: hc.h.Ref,
			SiteRef: hc.x.Ref, ProjectRef: hc.p.Ref, CommuneCode: hc.h.CommuneCode, Cycle: in.Cycle}
		return store.InsertCycle(ctx, tx, c.p.TenantID, &out)
	})
	if dup(err) {
		return s.CreateCycle(ctx, in)
	}
	if err != nil {
		return CycleView{}, err
	}
	v, err := s.GetCycle(ctx, out.Ref)
	v.Replayed = replayed
	return v, err
}

// UpdateCycle ghi phiên bản mới của lứa đang nuôi (không đổi hộ, mẫu nhật ký).
func (s *Service) UpdateCycle(ctx context.Context, ref uuid.UUID, in CycleInput) (CycleView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return CycleView{}, err
	}
	if in.BaseVersion == nil {
		return CycleView{}, errBaseVersion
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.CycleByIdem(ctx, tx, idemCycle(in.IdemKey)); err != nil || prev != nil {
			return err
		}
		cy, hc, err := s.lockCycle(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		if !c.keeper(hc) {
			return problem.Forbidden.WithDetail("chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ sửa lứa")
		}
		if cy.Event == domain.EventClosed || !hc.open() {
			return ErrCycleClosed
		}
		if !versionOK(in.BaseVersion, cy.Seq) {
			return ErrCycleConflict
		}
		if in.Form == "" {
			in.Form = cy.Form
		}
		if in.Form != cy.Form {
			return ErrCycleInvalid.WithField("form", "không đổi mẫu nhật ký của lứa")
		}
		if err := s.checkCycle(&in.Cycle); err != nil {
			return err
		}
		n := *cy
		n.Meta, n.Event, n.StatusReason, n.Cycle = c.meta(s.now(), idemCycle(in.IdemKey)), domain.EventUpdated, "", in.Cycle
		return store.InsertCycle(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return CycleView{}, err
	}
	return s.GetCycle(ctx, ref)
}

// CloseCycle kết thúc lứa kèm hạch toán NC-15.
func (s *Service) CloseCycle(ctx context.Context, ref uuid.UUID, base *int64, acc domain.Accounting, idem string) (CycleView, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return CycleView{}, err
	}
	if base == nil {
		return CycleView{}, errBaseVersion
	}
	if es := acc.Normalize(); len(es) > 0 {
		return CycleView{}, fieldsProblem(ErrCycleInvalid, es)
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.CycleByIdem(ctx, tx, idemCycle(idem)); err != nil || prev != nil {
			return err
		}
		cy, hc, err := s.lockCycle(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		if !c.keeper(hc) {
			return problem.Forbidden.WithDetail("chỉ cán bộ theo dõi điểm / người ghi nhật ký của hộ kết thúc lứa")
		}
		if cy.Event == domain.EventClosed {
			return ErrCycleClosed
		}
		if !versionOK(base, cy.Seq) {
			return ErrCycleConflict
		}
		n := *cy
		n.Meta, n.Event, n.StatusReason, n.Accounting = c.meta(s.now(), idemCycle(idem)), domain.EventClosed, "", acc
		return store.InsertCycle(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return CycleView{}, err
	}
	return s.GetCycle(ctx, ref)
}

// ReopenCycle mở lại lứa đã kết thúc — lý do bắt buộc; người ghi hoặc chủ nhiệm.
func (s *Service) ReopenCycle(ctx context.Context, ref uuid.UUID, base *int64, reason, idem string) (CycleView, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return CycleView{}, err
	}
	if base == nil {
		return CycleView{}, errBaseVersion
	}
	if !domain.ValidReason(reason) {
		return CycleView{}, problem.Validation.WithField("reason", "5–1000 ký tự")
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.CycleByIdem(ctx, tx, idemCycle(idem)); err != nil || prev != nil {
			return err
		}
		cy, hc, err := s.lockCycle(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		if !c.keeper(hc) && !c.reviewer(hc.p) {
			return problem.Forbidden.WithDetail("chỉ người ghi nhật ký hoặc chủ nhiệm mở lại lứa")
		}
		if cy.Event != domain.EventClosed {
			return ErrCycleConflict.WithDetail("lứa đang nuôi")
		}
		if !versionOK(base, cy.Seq) {
			return ErrCycleConflict
		}
		n := *cy
		n.Meta, n.Event, n.StatusReason = c.meta(s.now(), idemCycle(idem)), domain.EventReopened, strings.TrimSpace(reason)
		return store.InsertCycle(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return CycleView{}, err
	}
	return s.GetCycle(ctx, ref)
}

// CycleQuery lọc danh sách lứa.
type CycleQuery struct {
	Household *uuid.UUID
	Site      *uuid.UUID
	Project   *uuid.UUID
	Status    string
}

// CycleList là danh sách lứa kèm lợi nhuận trung bình/hộ của các lứa đã hạch toán (cấp mô hình, NC-15).
type CycleList struct {
	Items         []CycleView
	ClosedCount   int
	AvgProfit     *decimal.Decimal
	Names         map[uuid.UUID]string
	CanCreateFor  bool // người gọi mở lứa được cho hộ trong bộ lọc
	HouseholdOpen bool
}

// ListCycles liệt kê lứa trong phạm vi.
func (s *Service) ListCycles(ctx context.Context, q CycleQuery) (CycleList, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return CycleList{}, err
	}
	switch q.Status {
	case "", domain.CycleOpen, domain.CycleClosed:
	default:
		return CycleList{}, problem.Validation.WithField("status", "open hoặc closed")
	}
	out := CycleList{Items: []CycleView{}}
	var refs []uuid.UUID
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		cs, err := store.ListCycles(ctx, tx, store.CycleFilter{Household: q.Household, Site: q.Site, Project: q.Project})
		if err != nil {
			return err
		}
		hcs := map[uuid.UUID]*hctx{}
		if q.Household != nil {
			hc, err := s.loadHousehold(ctx, tx, c, *q.Household)
			if err != nil {
				return err
			}
			if hc != nil {
				out.CanCreateFor, out.HouseholdOpen = c.keeper(hc) && hc.open(), hc.open()
			}
		}
		for _, cy := range cs {
			hc, ok := hcs[cy.HouseholdRef]
			if !ok {
				if hc, err = s.loadHousehold(ctx, tx, c, cy.HouseholdRef); err != nil {
					return err
				}
				hcs[cy.HouseholdRef] = hc
			}
			if hc == nil {
				continue
			}
			es, err := store.ListEntries(ctx, tx, store.EntryFilter{Cycle: &cy.Ref})
			if err != nil {
				return err
			}
			v := s.cycleView(c, hc, cy, es)
			if q.Status != "" && v.Status != q.Status {
				continue
			}
			if cy.Event == domain.EventClosed && v.Profit != nil {
				out.ClosedCount++
				if out.AvgProfit == nil {
					z := decimal.Zero
					out.AvgProfit = &z
				}
				sum := out.AvgProfit.Add(*v.Profit)
				out.AvgProfit = &sum
			}
			out.Items = append(out.Items, v)
			refs = append(refs, cy.ActorRef)
			if v.OfficerRef != nil {
				refs = append(refs, *v.OfficerRef)
			}
		}
		return nil
	})
	if err != nil {
		return CycleList{}, err
	}
	if out.ClosedCount > 0 {
		avg := out.AvgProfit.Div(decimal.NewFromInt(int64(out.ClosedCount))).Round(0)
		out.AvgProfit = &avg
	}
	out.Names = s.names(ctx, refs)
	return out, nil
}

// GetCycle đọc chi tiết lứa kèm lịch sử.
func (s *Service) GetCycle(ctx context.Context, ref uuid.UUID) (CycleView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return CycleView{}, err
	}
	var v CycleView
	var person uuid.UUID
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		vs, err := store.CycleVersions(ctx, tx, ref)
		if err != nil {
			return err
		}
		if len(vs) == 0 {
			return ErrCycleNotFound
		}
		cur := vs[len(vs)-1]
		hc, err := s.loadHousehold(ctx, tx, c, cur.HouseholdRef)
		if err != nil {
			return err
		}
		if hc == nil {
			return ErrCycleNotFound
		}
		es, err := store.ListEntries(ctx, tx, store.EntryFilter{Cycle: &ref})
		if err != nil {
			return err
		}
		v = s.cycleView(c, hc, cur, es)
		v.Versions, person = vs, hc.h.PersonRef
		return nil
	})
	if err != nil {
		return CycleView{}, err
	}
	v.OwnerLabel = s.ownerLabel(ctx, person)
	refs := []uuid.UUID{}
	for _, x := range v.Versions {
		refs = append(refs, x.ActorRef)
	}
	if v.OfficerRef != nil {
		refs = append(refs, *v.OfficerRef)
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}
