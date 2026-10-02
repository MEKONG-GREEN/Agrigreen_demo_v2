package app

// KN v7.7 gói P1 — FN-AD-04.07 "Menu các cấp": Admin (platform_admin, MFA) vào menu của Cấp Quốc gia,
// Tỉnh, Xã để hỗ trợ, nhập liệu thay khi cần. Phiên hỗ trợ là một ngữ cảnh phiên đặc biệt mang
// on_behalf_of; mọi thao tác ghi trong ngữ cảnh này bắt buộc lý do (header X-AG-Reason) và ghi nhật ký
// riêng (support_action_log, không xóa) với Admin thật + ngữ cảnh được hỗ trợ.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi chế độ hỗ trợ.
var (
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "accounts.support_reason_required", "Thao tác trong chế độ hỗ trợ phải nhập lý do (ít nhất 10 ký tự)")
	ErrSupportNotFound = problem.New(http.StatusNotFound, "accounts.support_session_not_found", "Không tìm thấy phiên hỗ trợ")
	ErrSupportEnded    = problem.New(http.StatusConflict, "accounts.support_session_ended", "Phiên hỗ trợ đã kết thúc")
)

// SupportInput là yêu cầu "Vào menu" của màn Menu các cấp.
type SupportInput struct {
	// TargetAssignment: lần gán của tài khoản cần hỗ trợ (vai, Nút, xã lấy từ đó). Nil → chọn trực tiếp
	// Role + OrgRef (+ Communes với Cấp Xã), không gắn tài khoản.
	TargetAssignment uuid.UUID
	Role             string
	OrgRef           uuid.UUID
	Communes         []string
	Department       string
	Reason           string
	IdemKey          string
}

// supportAdmin: chỉ platform_admin (Admin API đã bắt MFA).
func (s *Service) supportAdmin(ctx context.Context) (*actor, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return nil, err
	}
	if !p.MFA {
		return nil, problem.MFARequired
	}
	if s.Departments == nil || s.Scope == nil || s.Orgs == nil {
		return nil, ErrDependency.WithDetail("chưa nối pf/party")
	}
	sc, err := s.Scope.Scope(ctx)
	if err != nil {
		return nil, err
	}
	return &actor{p: p, sc: sc, orgs: map[uuid.UUID]*partyport.Org{}}, nil
}

// StartSupport mở phiên hỗ trợ và trả ngữ cảnh phiên (context_id) để client gửi qua X-AG-Context.
func (s *Service) StartSupport(ctx context.Context, in SupportInput) (ContextView, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return ContextView{}, err
	}
	a, err := s.supportAdmin(ctx)
	if err != nil {
		return ContextView{}, err
	}
	key := "support:start:" + in.IdemKey
	var prevID uuid.UUID
	var found bool
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prevID, found, e = store.SupportEventByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return ContextView{}, err
	}
	if found {
		return s.supportView(ctx, a.p.TenantID, prevID, true)
	}
	if err := domain.CheckKNReason("reason", in.Reason); err != nil {
		return ContextView{}, fieldProblem(err)
	}
	in.Department = strings.TrimSpace(in.Department)
	sup := store.Support{ID: ids.New(), AdminRef: a.p.ActorRef}
	var unit uuid.UUID
	if in.TargetAssignment != uuid.Nil {
		var asg *port.Assignment
		if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
			var e error
			asg, e = store.AssignmentByID(ctx, tx, in.TargetAssignment, false)
			return e
		}); err != nil {
			return ContextView{}, err
		}
		if asg == nil || !asg.EffectiveAt(s.now()) || asg.Role == port.KNAdmin {
			return ContextView{}, ErrAssignmentNotFound
		}
		if asg.UserRef == a.p.ActorRef {
			return ContextView{}, ErrSelfAction.WithDetail("Không hỗ trợ chính mình")
		}
		if !slices.Contains(asg.Departments, in.Department) {
			return ContextView{}, ErrDepartmentRequired
		}
		sup.ActedRole, sup.OrgRef, sup.Communes, sup.OnBehalfOf, sup.TargetAssignment = asg.Role, asg.OrgRef, asg.Communes, asg.UserRef, asg.ID
		unit = asg.UnitRef
	} else {
		if in.Role == port.KNAdmin || !slices.Contains(port.KNRolesAll, in.Role) {
			return ContextView{}, problem.Validation.WithField("role", "kn_tw | kn_tinh | kn_xa")
		}
		c, err := s.checkAssignment(ctx, a, domain.AssignmentInput{Role: in.Role, OrgRef: in.OrgRef, Departments: []string{in.Department},
			Communes: in.Communes}, nil)
		if err != nil {
			return ContextView{}, err
		}
		sup.ActedRole, sup.OrgRef, sup.Communes = c.in.Role, c.in.OrgRef, c.in.Communes
		unit = c.unit
	}
	d, err := s.Departments.DepartmentByCode(ctx, unit, in.Department)
	if errors.Is(err, partyport.ErrDepartmentNotFound) {
		return ContextView{}, ErrDepartmentRequired
	}
	if err != nil {
		return ContextView{}, err
	}
	if !d.Selectable() {
		return ContextView{}, ErrDepartmentNotOpen.WithDetail(d.Name)
	}
	now := s.now()
	sup.Department, sup.ContextID, sup.StartedAt, sup.ExpiresAt = d.Code, ids.New(), now, now.Add(domain.SupportTTL)
	reason := strings.TrimSpace(in.Reason)
	ev := store.SupportEvent{ID: ids.New(), TenantID: a.p.TenantID, Action: "started", S: sup, Reason: &reason, EventTime: now,
		ActorRef: a.p.ActorRef, IdemKey: key}
	row := store.Context{ID: sup.ContextID, TenantID: a.p.TenantID, UserRef: a.p.ActorRef, Role: sup.ActedRole, Department: sup.Department,
		OrgRef: sup.OrgRef, UnitRef: unit, Communes: sup.Communes, SupportID: sup.ID, OnBehalfOf: sup.OnBehalfOf, IdemKey: key,
		CreatedAt: now, ExpiresAt: sup.ExpiresAt}
	if sup.TargetAssignment != uuid.Nil {
		row.AssignmentID = sup.TargetAssignment
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if err := ledger.Classify(store.InsertSupportEvent(ctx, tx, &ev)); err != nil {
			return err
		}
		return store.InsertContext(ctx, tx, row)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return s.StartSupport(ctx, in)
	}
	if err != nil {
		return ContextView{}, err
	}
	return s.contextView(ctx, row, sup.Communes)
}

func (s *Service) supportView(ctx context.Context, tid, id uuid.UUID, replayed bool) (ContextView, error) {
	var sup *store.Support
	var row *store.Context
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		if sup, e = store.SupportByID(ctx, tx, id, false); e != nil || sup == nil {
			return e
		}
		row, e = store.ContextByID(ctx, tx, sup.ContextID)
		return e
	})
	if err != nil {
		return ContextView{}, err
	}
	if sup == nil || row == nil {
		return ContextView{}, ErrSupportNotFound
	}
	v, err := s.contextView(ctx, *row, sup.Communes)
	v.Replayed = replayed
	return v, err
}

// EndSupport kết thúc phiên hỗ trợ ("Thoát chế độ hỗ trợ"); ngữ cảnh hỗ trợ hết hiệu lực ngay.
func (s *Service) EndSupport(ctx context.Context, id uuid.UUID, idem string) (store.Support, error) {
	if err := requireKey(idem); err != nil {
		return store.Support{}, err
	}
	a, err := s.supportAdmin(ctx)
	if err != nil {
		return store.Support{}, err
	}
	key := "support:end:" + idem
	var out store.Support
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if prev, found, err := store.SupportEventByIdem(ctx, tx, key); err != nil || found {
			if err != nil {
				return err
			}
			if prev != id {
				return problem.IdempotencyConflict
			}
			cur, err := store.SupportByID(ctx, tx, id, false)
			if err == nil && cur != nil {
				out = *cur
			}
			return err
		}
		cur, err := store.SupportByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur == nil || cur.AdminRef != a.p.ActorRef {
			return ErrSupportNotFound
		}
		if cur.EndedAt != nil {
			return ErrSupportEnded
		}
		now := s.now()
		next := *cur
		next.EndedAt = &now
		ev := store.SupportEvent{ID: ids.New(), TenantID: a.p.TenantID, Action: "ended", S: next, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertSupportEvent(ctx, tx, &ev)); err != nil {
			return err
		}
		out = next
		return nil
	})
	return out, err
}

// SupportActionInput là một thao tác ghi trong chế độ hỗ trợ (middleware).
type SupportActionInput struct {
	RequestID uuid.UUID
	Phase     string // attempted | completed
	Function  string // "POST /v1/…/:id"
	Content   map[string]any
	Status    int
}

// LogSupportAction ghi một dòng nhật ký hỗ trợ cho request hiện tại (Principal.KN.Support khác nil).
func (s *Service) LogSupportAction(ctx context.Context, in SupportActionInput) error {
	p, ok := authz.FromContext(ctx)
	if !ok || p.KN == nil || p.KN.Support == nil {
		return problem.Forbidden
	}
	k := p.KN
	content, err := json.Marshal(in.Content)
	if err != nil {
		return err
	}
	row := store.SupportAction{ID: ids.New(), TenantID: p.TenantID, SessionID: k.Support.SessionID, ContextID: k.ID, RequestID: in.RequestID,
		Phase: in.Phase, OnBehalfOf: k.Support.OnBehalfOf, ActedRole: k.Role, Department: k.Department, Function: in.Function,
		Content: content, Reason: k.Support.Reason, TraceID: telemetry.TraceID(ctx), EventTime: s.now(), ActorRef: p.ActorRef}
	if in.Phase == "completed" {
		st := in.Status
		row.Status = &st
	}
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		return ledger.Classify(store.InsertSupportAction(ctx, tx, &row))
	})
}

// SupportLogEntry là một dòng nhật ký hỗ trợ đã tra tên.
type SupportLogEntry struct {
	store.SupportAction
	ActorDisplay      string
	OnBehalfOfDisplay string
}

// SupportLog đọc nhật ký hỗ trợ (mới nhất trước, con trỏ theo thời điểm ghi). session Nil = mọi phiên.
func (s *Service) SupportLog(ctx context.Context, session uuid.UUID, beforeAt *time.Time, beforeID uuid.UUID, limit int) ([]SupportLogEntry, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []store.SupportAction
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SupportActions(ctx, tx, session, beforeAt, beforeID, limit)
		return e
	}); err != nil {
		return nil, err
	}
	refs := make([]uuid.UUID, 0, 2*len(rows))
	for _, r := range rows {
		refs = append(refs, r.ActorRef, r.OnBehalfOf)
	}
	names, _ := s.DisplayNames(ctx, refs)
	out := make([]SupportLogEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, SupportLogEntry{SupportAction: r, ActorDisplay: names[r.ActorRef], OnBehalfOfDisplay: names[r.OnBehalfOf]})
	}
	return out, nil
}
