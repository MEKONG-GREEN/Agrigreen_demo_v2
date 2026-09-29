package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var communeRe = regexp.MustCompile(`^[0-9]{5}$`)

// maxWindowDays là độ dài tối đa của khung thời gian một lệnh gom.
const maxWindowDays = 90

// PlanInput là kế hoạch (lập mới hoặc sửa khi còn planned).
type PlanInput struct {
	HTXRef      uuid.UUID
	CommuneCode string // rỗng → lấy mã xã của HTX từ nền tảng
	WindowFrom  string // YYYY-MM-DD
	WindowTo    string
	PlotRefs    []uuid.UUID
	IdemKey     string
}

// OrderResult là kết quả một thao tác trên lệnh gom.
type OrderResult struct {
	Order   store.Order
	Event   store.OrderEvent
	Exports []store.ValveExport
}

type plan struct {
	htx      uuid.UUID
	commune  string
	from, to time.Time
	plots    []uuid.UUID
}

// validatePlan kiểm dữ liệu và đối chiếu thửa/HTX với nền tảng (ngoài giao dịch DB).
func (s *Service) validatePlan(ctx context.Context, in PlanInput) (plan, error) {
	v := problem.Validation
	bad := false
	add := func(f, m string) { v, bad = v.WithField(f, m), true }
	pl := plan{htx: in.HTXRef, commune: strings.TrimSpace(in.CommuneCode)}
	if in.HTXRef == uuid.Nil {
		add("htx_ref", "bắt buộc, UUID")
	}
	var err error
	if pl.from, err = time.Parse(dateLayout, in.WindowFrom); err != nil {
		add("window_from", "YYYY-MM-DD")
	}
	if pl.to, err = time.Parse(dateLayout, in.WindowTo); err != nil {
		add("window_to", "YYYY-MM-DD")
	}
	if !pl.from.IsZero() && !pl.to.IsZero() && (pl.to.Before(pl.from) || pl.to.Sub(pl.from) > maxWindowDays*24*time.Hour) {
		add("window_to", "≥ window_from, khung tối đa 90 ngày")
	}
	seen := map[uuid.UUID]bool{}
	for _, p := range in.PlotRefs {
		if p == uuid.Nil || seen[p] {
			add("plot_refs", "UUID, không trùng")
			break
		}
		seen[p] = true
		pl.plots = append(pl.plots, p)
	}
	if len(pl.plots) == 0 || len(pl.plots) > 200 {
		add("plot_refs", "1..200 thửa")
	}
	if pl.commune != "" && !communeRe.MatchString(pl.commune) {
		add("commune_code", "mã xã 5 chữ số")
	}
	if bad {
		return pl, v
	}
	h, err := s.Directory.Cooperative(ctx, in.HTXRef)
	switch {
	case errors.Is(err, port.ErrDirectoryNotFound):
		return pl, ErrHTXNotFound
	case err != nil:
		return pl, mapErr(err)
	case !h.Active:
		return pl, ErrHTXNotFound.WithDetail("HTX đã vô hiệu hóa")
	}
	if pl.commune == "" {
		pl.commune = h.CommuneCode
	}
	if !communeRe.MatchString(pl.commune) {
		return pl, problem.Validation.WithField("commune_code", "nền tảng không có mã xã của HTX — ghi commune_code")
	}
	for _, ref := range pl.plots {
		p, err := s.Directory.Plot(ctx, ref)
		switch {
		case errors.Is(err, port.ErrDirectoryNotFound):
			return pl, ErrPlotNotFound.WithDetail(ref.String())
		case err != nil:
			return pl, mapErr(err)
		case p.CooperativeRef == nil || *p.CooperativeRef != in.HTXRef || p.Status == "retired":
			return pl, ErrPlotOtherHTX.WithDetail(ref.String())
		}
	}
	return pl, nil
}

// CreateOrder lập lệnh gom (planned, revision 1). Vai field_supervisor.
func (s *Service) CreateOrder(ctx context.Context, in PlanInput) (OrderResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return OrderResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return OrderResult{}, false, err
	}
	var res OrderResult
	// Gọi lặp: trả kết quả cũ, không gọi lại nền tảng.
	var prev *store.OrderEvent
	if err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		prev, e = store.OrderEventByIdem(ctx, tx, in.IdemKey)
		return e
	}); err != nil {
		return res, false, err
	}
	var pl plan
	if prev == nil {
		if pl, err = s.validatePlan(ctx, in); err != nil {
			return res, false, err
		}
	}
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		e := store.OrderEvent{
			ID: ids.New(), TenantID: p.TenantID, OrderID: ids.New(), Kind: domain.OrderPlanned, Revision: 1,
			HTXRef: pl.htx, CommuneCode: pl.commune, WindowFrom: pl.from, WindowTo: pl.to, PlotRefs: pl.plots,
			CreatorRef: p.ActorRef, EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey,
		}
		return s.applyEvent(ctx, tx, &e, &res)
	}, s.replayOrder(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// ReviseOrder sửa kế hoạch khi còn planned (revision + 1; người sửa thành người lập).
func (s *Service) ReviseOrder(ctx context.Context, orderID uuid.UUID, in PlanInput) (OrderResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return OrderResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return OrderResult{}, false, err
	}
	pl, err := s.validatePlan(ctx, in)
	if err != nil {
		return OrderResult{}, false, err
	}
	return s.transition(ctx, p, orderID, domain.OrderPlanned, in.IdemKey, func(e *store.OrderEvent, o store.Order) error {
		e.Revision = o.Revision + 1
		e.HTXRef, e.CommuneCode, e.WindowFrom, e.WindowTo, e.PlotRefs = pl.htx, pl.commune, pl.from, pl.to, pl.plots
		e.CreatorRef = p.ActorRef
		return nil
	})
}

// Decide duyệt (approved) hoặc từ chối (cancelled kèm lý do) lệnh gom planned.
// Người duyệt có vai field_supervisor và KHÁC người lập (R-56; CHECK DB lớp hai).
func (s *Service) Decide(ctx context.Context, orderID uuid.UUID, approve bool, reason, idem string) (OrderResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return OrderResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return OrderResult{}, false, err
	}
	r, err := domain.CheckReason("reason", reason, !approve)
	if err != nil {
		return OrderResult{}, false, mapErr(err)
	}
	kind := domain.OrderApproved
	if !approve {
		kind = domain.OrderCancelled
	}
	return s.transition(ctx, p, orderID, kind, idem, func(e *store.OrderEvent, o store.Order) error {
		if o.Status != domain.OrderPlanned {
			return ErrOrderTransition
		}
		if approve && o.CreatorRef == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval)
		}
		e.Reason = strPtr(r)
		return nil
	})
}

// Assign giao (hoặc đổi) tổ gom cho lệnh đã duyệt.
func (s *Service) Assign(ctx context.Context, orderID, crewRef uuid.UUID, idem string) (OrderResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return OrderResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return OrderResult{}, false, err
	}
	if crewRef == uuid.Nil {
		return OrderResult{}, false, problem.Validation.WithField("crew_ref", "bắt buộc, UUID")
	}
	return s.transition(ctx, p, orderID, domain.OrderAssigned, idem, func(e *store.OrderEvent, _ store.Order) error {
		e.CrewRef = &crewRef
		return nil
	})
}

// Close đóng lệnh gom đang thực hiện.
func (s *Service) Close(ctx context.Context, orderID uuid.UUID, idem string) (OrderResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return OrderResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return OrderResult{}, false, err
	}
	return s.transition(ctx, p, orderID, domain.OrderClosed, idem, nil)
}

// Cancel hủy lệnh gom (không xóa — sự kiện cancelled kèm lý do, RB-1).
func (s *Service) Cancel(ctx context.Context, orderID uuid.UUID, reason, idem string) (OrderResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return OrderResult{}, false, err
	}
	if err := requireIdem(idem); err != nil {
		return OrderResult{}, false, err
	}
	r, err := domain.CheckReason("reason", reason, true)
	if err != nil {
		return OrderResult{}, false, mapErr(err)
	}
	return s.transition(ctx, p, orderID, domain.OrderCancelled, idem, func(e *store.OrderEvent, _ store.Order) error {
		e.Reason = &r
		return nil
	})
}

// transition ghi một sự kiện lệnh gom sau khi khóa bảng chiếu và kiểm chuyển trạng thái.
func (s *Service) transition(ctx context.Context, p authz.Principal, orderID uuid.UUID, kind, idem string,
	mutate func(*store.OrderEvent, store.Order) error) (OrderResult, bool, error) {
	var res OrderResult
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, orderID, true)
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if err := domain.Transition(o.Status, kind); err != nil {
			return err
		}
		e := store.OrderEvent{
			ID: ids.New(), TenantID: p.TenantID, OrderID: o.OrderID, Kind: kind, Revision: o.Revision, HTXRef: o.HTXRef,
			CommuneCode: o.CommuneCode, WindowFrom: o.WindowFrom, WindowTo: o.WindowTo, PlotRefs: o.PlotRefs, CrewRef: o.CrewRef,
			CreatorRef: o.CreatorRef, EventTime: now, ActorRef: p.ActorRef, IdemKey: idem,
		}
		if mutate != nil {
			if err := mutate(&e, *o); err != nil {
				return err
			}
		}
		return s.applyEvent(ctx, tx, &e, &res)
	}, s.replayOrder(ctx, idem, &res))
	return res, replayed, err
}

// applyEvent ghi sổ, cập nhật bảng chiếu và (khi lịch đổi trạng thái công bố) xuất van.
func (s *Service) applyEvent(ctx context.Context, tx pgx.Tx, e *store.OrderEvent, res *OrderResult) error {
	if err := insert(store.InsertOrderEvent(ctx, tx, e)); err != nil {
		return err
	}
	prev, err := store.GetOrder(ctx, tx, e.OrderID, false)
	if err != nil {
		return err
	}
	wasPublished := prev != nil && prev.Published
	status, publish := domain.ScheduleStatus(e.Kind, wasPublished)
	if err := store.ApplyOrderEvent(ctx, tx, *e, publish); err != nil {
		return err
	}
	o, err := store.GetOrder(ctx, tx, e.OrderID, false)
	if err != nil {
		return err
	}
	res.Order, res.Event = *o, *e
	if publish {
		x, err := s.export(ctx, tx, e.TenantID, e.ActorRef, schedulePayload(*o, status), o.OrderID, o.OrderID, e.EventTime,
			e.IdemKey+":valve:schedule")
		if err != nil {
			return err
		}
		res.Exports = append(res.Exports, x)
	}
	return nil
}

func (s *Service) replayOrder(ctx context.Context, idem string, res *OrderResult) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		e, err := store.OrderEventByIdem(ctx, tx, idem)
		if err != nil || e == nil {
			return false, err
		}
		o, err := store.GetOrder(ctx, tx, e.OrderID, false)
		if err != nil || o == nil {
			return false, err
		}
		res.Order, res.Event = *o, *e
		xs, err := store.ValveExports(ctx, tx, e.OrderID, 200)
		if err != nil {
			return false, err
		}
		for _, x := range xs {
			if x.IdemKey == idem+":valve:schedule" {
				res.Exports = append(res.Exports, x)
			}
		}
		return true, nil
	}
}

// Order đọc lệnh gom hiện hành.
func (s *Service) Order(ctx context.Context, id uuid.UUID) (store.Order, []store.OrderEvent, error) {
	var o *store.Order
	var hist []store.OrderEvent
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if o, e = store.GetOrder(ctx, tx, id, false); e != nil || o == nil {
			return e
		}
		hist, e = store.OrderEvents(ctx, tx, id)
		return e
	})
	if err != nil {
		return store.Order{}, nil, err
	}
	if o == nil {
		return store.Order{}, nil, ErrOrderNotFound
	}
	return *o, hist, nil
}

// OrderPage là một trang lệnh gom.
type OrderPage struct {
	Items []store.Order
	Next  *uuid.UUID
}

// Orders liệt kê lệnh gom, cursor theo order_id (R-53).
func (s *Service) Orders(ctx context.Context, status string, htx *uuid.UUID, after uuid.UUID, limit int) (OrderPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var items []store.Order
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListOrders(ctx, tx, status, htx, after, limit+1)
		return e
	})
	var pg OrderPage
	if err != nil {
		return pg, err
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].OrderID
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}
