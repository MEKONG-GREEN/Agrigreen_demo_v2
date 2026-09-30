package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// WorkloadInput là việc giao cho tổ. Ngày rỗng → khung của lệnh gom; khối lượng theo
// đơn vị năng lực của tổ (bales: expected_bales, ha: expected_ha) — có thể gửi cả hai.
type WorkloadInput struct {
	PlannedFrom   string
	PlannedTo     string
	ExpectedBales *int
	ExpectedHa    *decimal.Decimal
}

// AssignInput là một lần giao / đổi tổ gom.
type AssignInput struct {
	CrewRef  uuid.UUID
	Workload WorkloadInput
	// Mode: manual (mặc định) | suggested (giám sát xác nhận gợi ý FM-08 — không bao giờ vượt năng lực).
	Mode           string
	SuggestionRank *int
	// OverrideReason: giám sát giao VƯỢT năng lực (chỉ manual, FM-06) — 10..500 ký tự.
	OverrideReason string
	IdemKey        string
}

// AssignResult là kết quả giao tổ.
type AssignResult struct {
	OrderResult
	Assignment store.Assignment
	Warnings   []string
}

type workload struct {
	from, to time.Time
	bales    *decimal.Decimal
	ha       *decimal.Decimal
}

// forUnit trả việc theo đơn vị của tổ (nil = thiếu khối lượng theo đơn vị đó).
func (w workload) forUnit(unit string) *domain.Workload {
	q := w.bales
	if unit == domain.UnitHa {
		q = w.ha
	}
	if q == nil {
		return nil
	}
	return &domain.Workload{From: w.from, To: w.to, ExpectedQty: *q}
}

// parseWorkload kiểm việc với khung lệnh gom o.
func parseWorkload(in WorkloadInput, o store.Order) (workload, error) {
	v := problem.Validation
	bad := false
	add := func(f, m string) { v, bad = v.WithField(f, m), true }
	w := workload{from: o.WindowFrom, to: o.WindowTo}
	var err error
	if in.PlannedFrom != "" {
		if w.from, err = time.Parse(dateLayout, in.PlannedFrom); err != nil {
			add("planned_from", "YYYY-MM-DD")
		}
	}
	if in.PlannedTo != "" {
		if w.to, err = time.Parse(dateLayout, in.PlannedTo); err != nil {
			add("planned_to", "YYYY-MM-DD")
		}
	}
	if !bad && (w.from.Before(o.WindowFrom) || w.to.After(o.WindowTo) || w.to.Before(w.from)) {
		add("planned_to", "planned_from ≤ planned_to, trong khung window_from..window_to của lệnh gom")
	}
	if in.ExpectedBales != nil {
		if *in.ExpectedBales <= 0 || *in.ExpectedBales > 10_000_000 {
			add("expected_bales", "số nguyên > 0")
		} else {
			d := decimal.NewFromInt(int64(*in.ExpectedBales))
			w.bales = &d
		}
	}
	if in.ExpectedHa != nil {
		if in.ExpectedHa.Sign() <= 0 || in.ExpectedHa.Exponent() < -3 || in.ExpectedHa.GreaterThan(decimal.NewFromInt(1_000_000)) {
			add("expected_ha", "> 0, tối đa 3 chữ số thập phân")
		} else {
			w.ha = in.ExpectedHa
		}
	}
	if bad {
		return w, v
	}
	if w.bales == nil && w.ha == nil {
		return w, ErrWorkloadRequired
	}
	return w, nil
}

func (s *Service) orderForRead(ctx context.Context, id uuid.UUID) (store.Order, error) {
	var o *store.Order
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		o, e = store.GetOrder(ctx, tx, id, false)
		return e
	})
	if err == nil && o == nil {
		err = ErrOrderNotFound
	}
	if err != nil {
		return store.Order{}, err
	}
	return *o, nil
}

// Assign giao (hoặc đổi) tổ gom cho lệnh đã duyệt (approved | assigned → assigned):
//   - FM-07: tổ phải có trong sổ tổ gom và đang hoạt động;
//   - FM-06: mỗi ngày dự kiến, tải các việc khác của tổ + việc này ≤ năng lực/ngày; vượt →
//     409 fieldops.crew_over_capacity, trừ khi giám sát (manual) ghi override_reason — khi đó
//     giao được, phân công gắn cờ over_capacity;
//   - FM-08: mode = suggested là xác nhận gợi ý (không bao giờ vượt năng lực). Giao lại đúng tổ
//     với đúng việc đang giao → trả phân công hiện hành, không sinh sự kiện mới (không trùng).
func (s *Service) Assign(ctx context.Context, orderID uuid.UUID, in AssignInput) (AssignResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return AssignResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return AssignResult{}, false, err
	}
	if in.CrewRef == uuid.Nil {
		return AssignResult{}, false, problem.Validation.WithField("crew_ref", "bắt buộc, UUID")
	}
	if in.Mode == "" {
		in.Mode = domain.ModeManual
	}
	if in.Mode != domain.ModeManual && in.Mode != domain.ModeSuggested {
		return AssignResult{}, false, problem.Validation.WithField("mode", "manual | suggested")
	}
	override, err := domain.CheckReason("override_reason", in.OverrideReason, false)
	if err != nil {
		return AssignResult{}, false, mapErr(err)
	}
	if override != "" && in.Mode != domain.ModeManual {
		return AssignResult{}, false, problem.Validation.WithField("override_reason", "chỉ giao tay (manual) mới được vượt năng lực")
	}
	if in.SuggestionRank != nil && (in.Mode != domain.ModeSuggested || *in.SuggestionRank < 1) {
		return AssignResult{}, false, problem.Validation.WithField("suggestion_rank", "≥ 1, chỉ với mode = suggested")
	}
	var res AssignResult
	var replayedOrder bool
	if err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		replayedOrder, e = s.replayAssign(ctx, in.IdemKey, &res)(tx)
		return e
	}); err != nil {
		return res, false, err
	}
	if replayedOrder {
		return res, true, nil
	}
	o0, err := s.orderForRead(ctx, orderID)
	if err != nil {
		return AssignResult{}, false, err
	}
	w, err := parseWorkload(in.Workload, o0)
	if err != nil {
		return AssignResult{}, false, err
	}
	harvests := s.fetchPlots(ctx, o0) // ngoài giao dịch: làm mới bảng đệm ngày gặt (FM-01/02)
	now := s.Clock.Now()
	noChange := false
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		res = AssignResult{}
		o, err := store.GetOrder(ctx, tx, orderID, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if err := domain.Transition(o.Status, domain.OrderAssigned); err != nil {
			return err
		}
		crew, err := store.GetCrew(ctx, tx, in.CrewRef, true) // khóa: tuần tự hóa phân công vào cùng tổ
		if err != nil {
			return err
		}
		if crew == nil {
			return ErrCrewUnknown
		}
		if crew.Status != domain.CrewActive {
			return ErrCrewInactive // FM-07
		}
		job := w.forUnit(crew.Spec.CapacityUnit)
		if job == nil {
			return ErrWorkloadRequired.WithDetail("tổ " + crew.Spec.Code + " tính năng lực theo " + crew.Spec.CapacityUnit + "/ngày")
		}
		// FM-08: xác nhận lặp / giao lại đúng việc đang giao → không sinh phân công trùng.
		if o.Status == domain.OrderAssigned && o.CrewRef != nil && *o.CrewRef == crew.CrewID {
			cur, err := store.CurrentAssignment(ctx, tx, o.OrderID)
			if err != nil {
				return err
			}
			if cur != nil && cur.CrewID == crew.CrewID && cur.PlannedFrom.Equal(job.From) && cur.PlannedTo.Equal(job.To) &&
				cur.ExpectedQty.Equal(job.ExpectedQty) {
				res.Order, res.Assignment, noChange = *o, *cur, true
				return nil
			}
		}
		books, err := store.Bookings(ctx, tx, []uuid.UUID{crew.CrewID}, job.From, job.To, o.OrderID)
		if err != nil {
			return err
		}
		capRes := domain.CheckCapacity(books[crew.CrewID], *job, crew.Spec.CapacityPerDay)
		if !capRes.Fits && override == "" {
			return ErrOverCapacity.WithDetail("tổ " + crew.Spec.Code + " ngày " + capRes.OverDay.Format(dateLayout) + ": tải " +
				capRes.OverLoad.String() + " > năng lực " + crew.Spec.CapacityPerDay.String() + " " + crew.Spec.CapacityUnit + "/ngày")
		}
		if err := putHarvests(ctx, tx, p.TenantID, o.OrderID, harvests); err != nil {
			return err
		}
		crewID := crew.CrewID
		e := store.OrderEvent{
			ID: ids.New(), TenantID: p.TenantID, OrderID: o.OrderID, Kind: domain.OrderAssigned, Revision: o.Revision, HTXRef: o.HTXRef,
			CommuneCode: o.CommuneCode, WindowFrom: o.WindowFrom, WindowTo: o.WindowTo, PlotRefs: o.PlotRefs, CrewRef: &crewID,
			CreatorRef: o.CreatorRef, EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		if err := s.applyEvent(ctx, tx, &e, &res.OrderResult); err != nil {
			return err
		}
		a := store.Assignment{
			ID: ids.New(), TenantID: p.TenantID, OrderID: o.OrderID, OrderEventID: e.ID, CrewID: crew.CrewID, CrewVersion: crew.Version,
			CapacityUnit: crew.Spec.CapacityUnit, CapacityPerDay: crew.Spec.CapacityPerDay, PlannedFrom: job.From, PlannedTo: job.To,
			ExpectedQty: job.ExpectedQty, DailyLoad: job.DailyLoad(), PeakLoad: capRes.PeakLoad, OverCapacity: !capRes.Fits,
			OverrideReason: strPtr(override), Mode: in.Mode, SuggestionRank: in.SuggestionRank, EventTime: now, ActorRef: p.ActorRef,
			IdemKey: in.IdemKey,
		}
		if !capRes.Fits {
			res.Warnings = append(res.Warnings, WarnOverCapacity)
		}
		if err := insert(store.InsertAssignment(ctx, tx, &a)); err != nil {
			return err
		}
		res.Assignment = a
		warns, err := s.planWarnings(ctx, tx, *o, *job)
		res.Warnings = append(res.Warnings, warns...)
		return err
	}, s.replayAssign(ctx, in.IdemKey, &res))
	return res, replayed || noChange, err
}

// planWarnings: cảnh báo FM-01/FM-02 của khoảng ngày dự kiến so với ngày gặt đệm của các thửa.
func (s *Service) planWarnings(ctx context.Context, tx pgx.Tx, o store.Order, w domain.Workload) ([]string, error) {
	hs, err := store.PlotHarvests(ctx, tx, o.OrderID)
	if err != nil {
		return nil, err
	}
	days, _, _, err := balingDays(ctx, tx, domain.Day(s.Clock.Now()))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, p := range o.PlotRefs {
		h, ok := hs[p]
		if !ok || h.Check != domain.HarvestOK || h.Date == nil {
			check := domain.HarvestUnavailable
			if ok {
				check = h.Check
			}
			add(harvestWarning(check))
			continue
		}
		if w.From.Before(*h.Date) {
			add(WarnPlannedBeforeHarvest)
		}
		if w.To.After(domain.AddDays(*h.Date, days)) {
			add(WarnDeadlineAtRisk)
		}
	}
	return out, nil
}

func (s *Service) replayAssign(ctx context.Context, idem string, res *AssignResult) func(pgx.Tx) (bool, error) {
	order := s.replayOrder(ctx, idem, &res.OrderResult)
	return func(tx pgx.Tx) (bool, error) {
		a, err := store.AssignmentByIdem(ctx, tx, idem)
		if err != nil || a == nil {
			return false, err
		}
		ok, err := order(tx)
		if err != nil || !ok {
			return false, err
		}
		res.Assignment = *a
		return true, nil
	}
}

// ---------------------------------------------------------------------------
// FM-08: gợi ý phân công
// ---------------------------------------------------------------------------

// SuggestView là một tổ trong kết quả gợi ý.
type SuggestView struct {
	domain.Suggestion
	Crew     store.Crew
	Workload *domain.Workload
}

// SuggestResult là kết quả gợi ý (chỉ đọc — không ghi gì).
type SuggestResult struct {
	Order       store.Order
	From, To    time.Time
	Suggestions []SuggestView // tổ vừa năng lực, xếp hạng (Rank 1 = gợi ý hàng đầu)
	Unfit       []SuggestView // tổ đang hoạt động nhưng không vừa (lý do Unfit)
	Inactive    int           // số tổ ngừng hoạt động (không bao giờ được gợi ý, FM-07)
}

// Suggest gợi ý tổ cho lệnh gom (FM-08): tổ đang hoạt động, vừa năng lực mọi ngày của
// khoảng dự kiến, gần nhất (xã của lệnh thuộc xã phục vụ của tổ) rồi ít tải nhất, hòa theo
// mã tổ. Tất định; không ghi gì — giám sát xác nhận bằng POST …/assign mode = suggested.
func (s *Service) Suggest(ctx context.Context, orderID uuid.UUID, in WorkloadInput) (SuggestResult, error) {
	if _, err := authz.Require(ctx, RoleSupervisor); err != nil {
		return SuggestResult{}, err
	}
	var res SuggestResult
	err := s.read(ctx, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, orderID, false)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if o.Status != domain.OrderApproved && o.Status != domain.OrderAssigned {
			return ErrOrderTransition
		}
		w, err := parseWorkload(in, *o)
		if err != nil {
			return err
		}
		res.Order, res.From, res.To = *o, w.from, w.to
		all, err := store.ListCrews(ctx, tx, "", "", 100_000)
		if err != nil {
			return err
		}
		var active []store.Crew
		crewIDs := make([]uuid.UUID, 0, len(all))
		for _, c := range all {
			if c.Status != domain.CrewActive {
				res.Inactive++
				continue
			}
			active = append(active, c)
			crewIDs = append(crewIDs, c.CrewID)
		}
		books, err := store.Bookings(ctx, tx, crewIDs, w.from, w.to, o.OrderID)
		if err != nil {
			return err
		}
		byID := map[uuid.UUID]store.Crew{}
		cands := make([]domain.Candidate, 0, len(active))
		for _, c := range active {
			byID[c.CrewID] = c
			cands = append(cands, domain.Candidate{CrewID: c.CrewID, Code: c.Spec.Code, CapacityPerDay: c.Spec.CapacityPerDay,
				ServiceCommunes: c.Spec.ServiceCommunes, Workload: w.forUnit(c.Spec.CapacityUnit), Bookings: books[c.CrewID]})
		}
		fit, unfit := domain.Suggest(cands, o.CommuneCode)
		view := func(xs []domain.Suggestion) []SuggestView {
			out := make([]SuggestView, 0, len(xs))
			for _, x := range xs {
				c := byID[x.CrewID]
				out = append(out, SuggestView{Suggestion: x, Crew: c, Workload: w.forUnit(c.Spec.CapacityUnit)})
			}
			return out
		}
		res.Suggestions, res.Unfit = view(fit), view(unfit)
		return nil
	})
	return res, err
}

// Assignments đọc lịch sử phân công của lệnh gom.
func (s *Service) Assignments(ctx context.Context, orderID uuid.UUID) ([]store.Assignment, error) {
	var out []store.Assignment
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		out, e = store.Assignments(ctx, tx, orderID)
		return e
	})
	return out, err
}
