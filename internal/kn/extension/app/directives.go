package app

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// IssueDirectiveInput là dữ liệu vào của IssueDirective.
type IssueDirectiveInput struct {
	Title         string
	Body          string
	Severity      string
	AudienceUnits []string // mã tỉnh và/hoặc xã nhận
	ExpiresAt     *time.Time
	IdemKey       string
}

// DirectiveResult là kết quả ban hành.
type DirectiveResult struct {
	Directive domain.Directive
	Replayed  bool
}

// IssueDirective ban hành chỉ đạo khẩn tới ĐÚNG vùng đã chọn (US-TASK-03 AC): chụp
// danh sách cán bộ nhận từ danh bạ tại thời điểm ban hành, phát
// pf.extension.directive.issued để gửi thông báo.
func (s *Service) IssueDirective(ctx context.Context, in IssueDirectiveInput) (DirectiveResult, error) {
	p, err := authz.Require(ctx, supervisors...)
	if err != nil {
		return DirectiveResult{}, err
	}
	if in.IdemKey == "" {
		return DirectiveResult{}, problem.IdempotencyRequired
	}
	now := s.now()
	d := domain.Directive{
		ID: ids.New(), Title: in.Title, Body: in.Body, Severity: in.Severity, AudienceUnits: in.AudienceUnits,
		IssuerRef: p.ActorRef, IssuedAt: now, ExpiresAt: in.ExpiresAt,
	}
	if err := domain.ValidateDirective(&d, now); err != nil {
		return DirectiveResult{}, fieldProblem(err)
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return DirectiveResult{}, err
	}
	provinces := map[string]bool{}
	for _, u := range d.AudienceUnits {
		if !sc.Allows(u) {
			return DirectiveResult{}, ErrOutOfScope.WithField("audience_units", u+" ngoài phạm vi phụ trách")
		}
		prov, _, err := s.provinceOf(ctx, u, "audience_units")
		if err != nil {
			return DirectiveResult{}, err
		}
		provinces[prov] = true
	}
	for pc := range provinces {
		d.ProvinceCodes = append(d.ProvinceCodes, pc)
	}
	sort.Strings(d.ProvinceCodes)

	var res DirectiveResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.DirectiveByIdemKey(ctx, tx, p.ActorRef, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			res = DirectiveResult{Directive: *prev, Replayed: true}
			return nil
		}
		staff, err := store.StaffInUnits(ctx, tx, false, d.AudienceUnits)
		if err != nil {
			return err
		}
		var targets []store.Target
		var refs []uuid.UUID
		for _, st := range staff {
			if st.ActorRef == p.ActorRef {
				continue // người ban hành không tự nhận
			}
			targets = append(targets, store.Target{ActorRef: st.ActorRef, UnitCode: st.UnitCode, ProvinceCode: st.ProvinceCode})
			refs = append(refs, st.ActorRef)
		}
		if len(targets) == 0 {
			return ErrNoTargets
		}
		if err := store.InsertDirective(ctx, tx, p.TenantID, d, in.IdemKey, targets); err != nil {
			return err
		}
		payload := port.DirectiveIssuedV1{
			DirectiveID: d.ID, Severity: d.Severity, AudienceUnits: d.AudienceUnits, ProvinceCodes: d.ProvinceCodes,
			TargetRefs: refs, IssuerRef: d.IssuerRef, IssuedAt: d.IssuedAt.Format(time.RFC3339),
		}
		if d.ExpiresAt != nil {
			e := d.ExpiresAt.UTC().Format(time.RFC3339)
			payload.ExpiresAt = &e
		}
		if err := outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventDirectiveIssued, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: port.EventProducerName, Payload: payload,
		}); err != nil {
			return err
		}
		out, err := store.DirectiveByID(ctx, tx, p.ActorRef, d.ID)
		if err != nil {
			return err
		}
		res = DirectiveResult{Directive: *out}
		return nil
	})
	return res, err
}

// AckDirective ghi xác nhận đã đọc của chính người gọi; gọi lặp giữ lần đầu.
func (s *Service) AckDirective(ctx context.Context, id uuid.UUID, device *string) (domain.Directive, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return domain.Directive{}, err
	}
	var out *domain.Directive
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DirectiveByID(ctx, tx, p.ActorRef, id)
		if err != nil {
			return err
		}
		if d == nil {
			return ErrDirectiveNotFound
		}
		if !d.IsTarget {
			return ErrNotTarget
		}
		if _, err := store.InsertAck(ctx, tx, p.TenantID, id, p.ActorRef, s.now(), device); err != nil {
			return err
		}
		out, err = store.DirectiveByID(ctx, tx, p.ActorRef, id)
		return err
	})
	if err != nil {
		return domain.Directive{}, err
	}
	return *out, nil
}

// DirectiveDetail là chỉ đạo kèm danh sách chưa xác nhận (người giám sát).
type DirectiveDetail struct {
	Directive domain.Directive
	Pending   []store.Target // nil với người nhận thường
}

// GetDirective đọc chỉ đạo: người nhận, hoặc người giám sát có vùng nhận trong phạm vi.
func (s *Service) GetDirective(ctx context.Context, id uuid.UUID) (DirectiveDetail, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return DirectiveDetail{}, err
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return DirectiveDetail{}, err
	}
	var out DirectiveDetail
	var found bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DirectiveByID(ctx, tx, p.ActorRef, id)
		if err != nil || d == nil {
			return err
		}
		supervise := isSupervisor(p) && directiveInScope(sc, *d)
		if !d.IsTarget && !supervise {
			return nil
		}
		found, out.Directive = true, *d
		if supervise {
			out.Pending, err = store.PendingTargets(ctx, tx, id)
			if out.Pending == nil {
				out.Pending = []store.Target{}
			}
		}
		return err
	})
	if err != nil {
		return DirectiveDetail{}, err
	}
	if !found {
		return DirectiveDetail{}, ErrDirectiveNotFound
	}
	return out, nil
}

func directiveInScope(sc authz.Scope, d domain.Directive) bool {
	if sc.All {
		return true
	}
	for _, u := range append(append([]string{}, d.AudienceUnits...), d.ProvinceCodes...) {
		if sc.Allows(u) {
			return true
		}
	}
	return false
}

// DirectiveQuery là tham số liệt kê.
type DirectiveQuery struct {
	Mine  bool // chỉ chỉ đạo mình là người nhận
	After int64
	Limit int
}

// DirectivePage là một trang chỉ đạo (mới nhất trước).
type DirectivePage struct {
	Items []domain.Directive
	Next  *int64
}

// ListDirectives liệt kê chỉ đạo người gọi nhận, cộng chỉ đạo trong phạm vi giám sát.
func (s *Service) ListDirectives(ctx context.Context, q DirectiveQuery) (DirectivePage, error) {
	p, err := authz.Require(ctx, knReaders...)
	if err != nil {
		return DirectivePage{}, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return DirectivePage{}, err
	}
	f := store.DirectiveFilter{
		Actor: p.ActorRef, All: sc.All, Units: sc.Units, OnlyMine: q.Mine || !isSupervisor(p),
		After: q.After, Limit: q.Limit + 1, Descending: true,
	}
	var items []domain.Directive
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListDirectives(ctx, tx, f)
		return e
	})
	if err != nil {
		return DirectivePage{}, err
	}
	pg := DirectivePage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].SyncSeq
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []domain.Directive{}
	}
	return pg, nil
}
