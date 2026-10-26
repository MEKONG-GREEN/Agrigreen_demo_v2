package app

// W11.07 — phân quyền trong vai (D4 X3, D3 H0/C3–C5; Q-059, Q-067, Q-068):
//   - thuộc tính của lần gán (sổ phụ assignment_attr_event cạnh sổ gán vai trò): nhóm Cấp Tỉnh, cờ QTNV báo giá,
//     cờ người phụ trách biểu, đơn vị nội bộ (Cấp Xã: đơn vị quản lý HS-37), phạm vi Cấp Xã theo cơ sở/hộ;
//   - ủy quyền có hạn (sổ delegation_event): lãnh đạo phòng giao quyền (mở lại kỳ, duyệt báo cáo) cho cán bộ Cấp
//     Tỉnh cùng phòng ban, tối đa 30 ngày, hết hạn tự mất hiệu lực;
//   - phạm vi nhân sự (BR-04 HCTH): HCTH/Admin toàn bộ; lãnh đạo xem người thuộc đơn vị nội bộ của mình và cán bộ
//     xã được phân công quản lý (HS-37).
// ResolveContext (kn_context.go) đưa các giá trị này vào authz.KNContext ở MỖI request.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi W11.07 (docs/errors.md, mục pf/accounts).
var (
	ErrInternalUnitInvalid = problem.New(http.StatusUnprocessableEntity, "accounts.internal_unit_invalid",
		"Đơn vị nội bộ không thuộc đơn vị của lần gán hoặc đã ngừng")
	// ErrFacilityInvalid (W11.41): facility_refs phải là cơ sở giết mổ đang dùng của danh mục CNTS (pf/masterdata).
	ErrFacilityInvalid = problem.New(http.StatusUnprocessableEntity, "accounts.facility_invalid",
		"Cơ sở không có trong danh mục cơ sở giết mổ đang dùng")
	// ErrFacilityCommune (W11.42): cơ sở của lần gán Cấp Xã phải thuộc một xã phụ trách của lần gán.
	ErrFacilityCommune = problem.New(http.StatusUnprocessableEntity, "accounts.facility_commune_mismatch",
		"Cơ sở giết mổ không thuộc xã phụ trách của lần gán")
	ErrContextReadOnly = problem.New(http.StatusForbidden, "accounts.context_read_only",
		"Ngữ cảnh chỉ xem (Lãnh đạo đơn vị xem phòng ban khác, Ban Giám đốc) — không thao tác ghi được")
	ErrDelegationNotFound  = problem.New(http.StatusNotFound, "accounts.delegation_not_found", "Không tìm thấy ủy quyền")
	ErrDelegationForbidden = problem.New(http.StatusForbidden, "accounts.delegation_forbidden",
		"Chỉ lãnh đạo phòng (Cấp Tỉnh) ủy quyền trong phòng ban của ngữ cảnh phiên; chỉ người giao hoặc Admin thu hồi")
	ErrDelegationGrantee = problem.New(http.StatusUnprocessableEntity, "accounts.delegation_grantee_invalid",
		"Người nhận phải có vai trò Cấp Tỉnh còn hiệu lực cùng đơn vị và cùng phòng ban")
	ErrDelegationState = problem.New(http.StatusConflict, "accounts.delegation_state", "Ủy quyền đã thu hồi hoặc đã hết hạn")
)

// AttrsResult là kết quả đặt thuộc tính vai.
type AttrsResult struct {
	Attrs    port.AssignmentAttrs
	Replayed bool
}

// SetAssignmentAttrs đặt (thay toàn bộ) thuộc tính phân quyền trong vai của một lần gán đang hoạt động (Admin).
func (s *Service) SetAssignmentAttrs(ctx context.Context, user, id uuid.UUID, in domain.AttrsInput, reason, idem string) (AttrsResult, error) {
	if err := requireKey(idem); err != nil {
		return AttrsResult{}, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return AttrsResult{}, err
	}
	key := "asgattr:put:" + idem
	if res, ok, err := s.attrsReplay(ctx, a.p.TenantID, key, id); err != nil || ok {
		return res, err
	}
	if _, err := s.knTarget(ctx, a, user); err != nil {
		return AttrsResult{}, err
	}
	var asg *port.Assignment
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		asg, e = store.AssignmentByID(ctx, tx, id, false)
		return e
	}); err != nil {
		return AttrsResult{}, err
	}
	if asg == nil || asg.UserRef != user {
		return AttrsResult{}, ErrAssignmentNotFound
	}
	if !asg.Active {
		return AttrsResult{}, ErrAssignmentState
	}
	if in, err = domain.NormalizeAttrsWithCatalog(asg.Role, in, s.AttributeCatalog()); err != nil {
		return AttrsResult{}, fieldProblem(err)
	}
	if s.Facilities != nil && len(in.Facilities) > 0 {
		unknown, err := s.Facilities.UnknownFacilities(ctx, in.Facilities)
		if err != nil {
			return AttrsResult{}, err
		}
		if len(unknown) > 0 {
			return AttrsResult{}, ErrFacilityInvalid.With("facility_refs", unknown[0].String())
		}
		// W11.42 (mục mở của W11.41): cơ sở phải thuộc một xã phụ trách của lần gán Cấp Xã.
		if asg.Role == port.KNXa {
			communes, err := s.Facilities.FacilityCommunes(ctx, in.Facilities, s.now())
			if err != nil {
				return AttrsResult{}, err
			}
			for _, f := range in.Facilities {
				if c := communes[f]; !slices.Contains(asg.Communes, c) {
					return AttrsResult{}, ErrFacilityCommune.With("facility_refs", f.String()+" (xã "+c+")")
				}
			}
		}
	}
	if in.InternalUnit != "" {
		u, err := s.InternalUnitByCode(ctx, asg.UnitRef, in.InternalUnit)
		if errors.Is(err, port.ErrInternalUnitNotFound) || (err == nil && !u.Active) {
			return AttrsResult{}, ErrInternalUnitInvalid.With("internal_unit_code", in.InternalUnit)
		}
		if err != nil {
			return AttrsResult{}, err
		}
	}
	var out AttrsResult
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.AttrsOf(ctx, tx, id, true)
		if err != nil {
			return err
		}
		next := port.AssignmentAttrs{AssignmentID: id, ProvinceGroup: in.ProvinceGroup, Duties: in.Duties, InternalUnit: in.InternalUnit,
			Facilities: in.Facilities, Households: in.Households}
		if domain.AttrsSnapshotOf(next) == domain.AttrsSnapshotOf(cur) {
			out = AttrsResult{Attrs: cur}
			return nil // không đổi → không ghi sổ
		}
		now := s.now()
		next.UpdatedBy, next.UpdatedAt = a.p.ActorRef, now
		e := store.AttrEvent{ID: ids.New(), TenantID: a.p.TenantID, UserRef: user, Role: asg.Role, A: next, After: attrsJSON(next),
			Reason: optStr(reason), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: key}
		if cur.Seq > 0 {
			e.Before = attrsJSON(cur)
		}
		if err := ledger.Classify(store.InsertAttrEvent(ctx, tx, &e)); err != nil {
			return err
		}
		next.Seq = e.Seq
		out = AttrsResult{Attrs: next}
		return store.PutAttrs(ctx, tx, e)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return s.SetAssignmentAttrs(ctx, user, id, in, reason, idem)
	}
	return out, err
}

func attrsJSON(a port.AssignmentAttrs) []byte {
	b, err := json.Marshal(domain.AttrsSnapshotOf(a))
	if err != nil {
		panic(err) // chỉ chuỗi
	}
	return b
}

func (s *Service) attrsReplay(ctx context.Context, tid uuid.UUID, key string, id uuid.UUID) (AttrsResult, bool, error) {
	var out AttrsResult
	found := false
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		prev, ok, err := store.AttrEventByIdem(ctx, tx, key)
		if err != nil || !ok {
			return err
		}
		if prev != id {
			return problem.IdempotencyConflict
		}
		cur, err := store.AttrsOf(ctx, tx, id, false)
		out, found = AttrsResult{Attrs: cur, Replayed: true}, true
		return err
	})
	return out, found, err
}

// attrsByUser đọc thuộc tính các lần gán của một người (tenant từ context).
func (s *Service) attrsByUser(ctx context.Context, user uuid.UUID) (map[uuid.UUID]port.AssignmentAttrs, error) {
	var out map[uuid.UUID]port.AssignmentAttrs
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.AttrsOfUser(ctx, tx, user)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Ủy quyền có hạn
// ---------------------------------------------------------------------------

// DelegationResult là kết quả một thao tác ủy quyền.
type DelegationResult struct {
	Delegation port.Delegation
	Replayed   bool
}

// CreateDelegation: lãnh đạo phòng (ngữ cảnh Cấp Tỉnh, Manager, không chỉ xem) ủy quyền có hạn cho cán bộ Cấp
// Tỉnh cùng đơn vị, cùng phòng ban của phiên (POST /v1/me/delegations).
func (s *Service) CreateDelegation(ctx context.Context, in domain.DelegationInput, idem string) (DelegationResult, error) {
	if err := requireKey(idem); err != nil {
		return DelegationResult{}, err
	}
	p, k, err := authz.RequireKN(ctx, port.KNTinh)
	if err != nil {
		return DelegationResult{}, err
	}
	if in.Department == "" {
		in.Department = k.Department
	}
	if !k.Manager || k.ReadOnly() || k.Support != nil || in.Department != k.Department {
		return DelegationResult{}, ErrDelegationForbidden
	}
	now := s.now()
	if in, err = domain.NormalizeDelegation(p.ActorRef, in, now); err != nil {
		return DelegationResult{}, fieldProblem(err)
	}
	key := "deleg:grant:" + idem
	var out DelegationResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, ok, err := store.DelegEventByIdem(ctx, tx, key); err != nil || ok {
			if err != nil {
				return err
			}
			d, err := store.DelegationByID(ctx, tx, prev, false)
			if err != nil || d == nil {
				return notFound(err, ErrDelegationNotFound)
			}
			if d.GrantorRef != p.ActorRef {
				return problem.IdempotencyConflict
			}
			out = DelegationResult{Delegation: *d, Replayed: true}
			return nil
		}
		as, err := store.AssignmentsOfUser(ctx, tx, in.GranteeRef, false)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(as, func(a port.Assignment) bool {
			return a.Role == port.KNTinh && a.UnitRef == k.UnitRef && slices.Contains(a.Departments, in.Department) &&
				a.EffectiveAt(now) && (a.ValidTo == nil || !a.ValidTo.Before(in.ValidTo))
		}) {
			return ErrDelegationGrantee
		}
		d := port.Delegation{ID: ids.New(), GrantorRef: p.ActorRef, GranteeRef: in.GranteeRef, UnitRef: k.UnitRef, Department: in.Department,
			Power: in.Power, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, Reason: in.Reason, CreatedBy: p.ActorRef, CreatedAt: now, UpdatedAt: now}
		e := store.DelegEvent{ID: ids.New(), TenantID: p.TenantID, Action: "granted", D: d, Reason: in.Reason, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertDelegEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = DelegationResult{Delegation: d}
		return store.PutDelegation(ctx, tx, e)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return s.CreateDelegation(ctx, in, idem)
	}
	return out, err
}

func notFound(err, nf error) error {
	if err != nil {
		return err
	}
	return nf
}

// RevokeDelegation thu hồi một ủy quyền còn hiệu lực: người giao (qua Application API) hoặc Admin (Admin API).
func (s *Service) RevokeDelegation(ctx context.Context, id uuid.UUID, reason, idem string, admin bool) (DelegationResult, error) {
	if err := requireKey(idem); err != nil {
		return DelegationResult{}, err
	}
	if err := domain.CheckKNReason("reason", reason); err != nil {
		return DelegationResult{}, fieldProblem(err)
	}
	var p authz.Principal
	var err error
	if admin {
		a, err := s.knCaller(ctx)
		if err != nil {
			return DelegationResult{}, err
		}
		p = a.p
	} else if p, _, err = authz.RequireKN(ctx, port.KNTinh); err != nil {
		return DelegationResult{}, err
	}
	key := "deleg:revoke:" + idem
	now := s.now()
	var out DelegationResult
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, ok, err := store.DelegEventByIdem(ctx, tx, key); err != nil || ok {
			if err != nil {
				return err
			}
			if prev != id {
				return problem.IdempotencyConflict
			}
			d, err := store.DelegationByID(ctx, tx, id, false)
			if err != nil || d == nil {
				return notFound(err, ErrDelegationNotFound)
			}
			out = DelegationResult{Delegation: *d, Replayed: true}
			return nil
		}
		d, err := store.DelegationByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if d == nil || (!admin && d.GrantorRef != p.ActorRef) {
			return ErrDelegationNotFound // không lộ ủy quyền của người khác
		}
		if d.Revoked || !now.Before(d.ValidTo) {
			return ErrDelegationState
		}
		d.Revoked, d.UpdatedAt = true, now
		e := store.DelegEvent{ID: ids.New(), TenantID: p.TenantID, Action: "revoked", D: *d, Reason: reason, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertDelegEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = DelegationResult{Delegation: *d}
		return store.PutDelegation(ctx, tx, e)
	})
	return out, err
}

// MyDelegations liệt kê ủy quyền người gọi đã giao hoặc đã nhận (GET /v1/me/delegations).
func (s *Service) MyDelegations(ctx context.Context) ([]port.Delegation, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	return s.delegationsOf(ctx, p.TenantID, p.ActorRef)
}

// UserDelegations liệt kê ủy quyền của một tài khoản (Admin, màn Detail tài khoản).
func (s *Service) UserDelegations(ctx context.Context, user uuid.UUID) ([]port.Delegation, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return nil, err
	}
	return s.delegationsOf(ctx, a.p.TenantID, user)
}

func (s *Service) delegationsOf(ctx context.Context, tid, user uuid.UUID) ([]port.Delegation, error) {
	var out []port.Delegation
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		out, e = store.DelegationsOfUser(ctx, tx, user)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Phạm vi nhân sự (BR-04 HCTH, HS-37)
// ---------------------------------------------------------------------------

var _ port.KNPeople = (*Service)(nil)

// PeopleInScope thực hiện port.KNPeople cho ngữ cảnh phiên hiện tại:
//   - Admin, Cấp Tỉnh của Phòng HCTH: toàn bộ (All);
//   - Cấp Tỉnh là lãnh đạo (Manager) có đơn vị nội bộ: người Cấp Tỉnh thuộc đơn vị đó + cán bộ Cấp Xã có đơn vị
//     quản lý (HS-37) là đơn vị đó;
//   - Lãnh đạo đơn vị, Ban Giám đốc: toàn bộ (chỉ đọc — BR-04, Q-059);
//   - còn lại (kể cả Cấp TW, Cấp Xã, ngữ cảnh chỉ xem không phải lãnh đạo): chỉ chính mình.
func (s *Service) PeopleInScope(ctx context.Context) (port.KNPeopleScope, error) {
	p, k, err := authz.RequireKN(ctx)
	if err != nil {
		return port.KNPeopleScope{}, err
	}
	caps, err := s.attributeCapabilities(port.Assignment{Role: k.Role, MgmtLevel: k.MgmtLevel}, port.AssignmentAttrs{ProvinceGroup: k.ProvinceGroup, Duties: k.Duties})
	if err != nil {
		return port.KNPeopleScope{}, err
	}
	self := port.KNPeopleScope{InternalUnit: k.InternalUnit, Members: []uuid.UUID{p.ActorRef}, CommuneOfficers: []uuid.UUID{}}
	switch {
	case k.Role == port.KNAdmin, k.Role == port.KNTinh && k.Department == partyport.DeptHanhChinhTongHop && !k.ReadOnly() && !caps.ForceReadOnly,
		caps.ReadsAllDepartments:
		return port.KNPeopleScope{All: true, InternalUnit: k.InternalUnit, Members: []uuid.UUID{}, CommuneOfficers: []uuid.UUID{}}, nil
	case k.Role != port.KNTinh || !caps.Manager || k.InternalUnit == "":
		return self, nil
	}
	now := s.now()
	out := port.KNPeopleScope{InternalUnit: k.InternalUnit}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if out.Members, e = store.UsersOfInternalUnit(ctx, tx, k.UnitRef, k.InternalUnit, port.KNTinh, now); e != nil {
			return e
		}
		out.CommuneOfficers, e = store.UsersOfInternalUnit(ctx, tx, k.UnitRef, k.InternalUnit, port.KNXa, now)
		return e
	})
	if err != nil {
		return port.KNPeopleScope{}, err
	}
	if !slices.Contains(out.Members, p.ActorRef) {
		out.Members = append(out.Members, p.ActorRef)
	}
	return out, nil
}
