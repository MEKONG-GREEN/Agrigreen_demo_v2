package app

// KN v7.7 gói P1 — FN-AD-04.04: tài khoản và phân quyền nhiều vai trò. Một tài khoản có nhiều lần gán
// (vai trò × Nút × phòng ban[] × cấp quản lý × xã phụ trách[]); mỗi thay đổi là một dòng sổ
// role_assignment_event mang ảnh chụp trước → sau (khung Log), bảng chiếu cập nhật trong cùng giao dịch
// (R-21). Không xóa cứng (RB-1): "xóa vai trò" = revoked, dữ liệu đã nhập bằng vai trò đó giữ nguyên.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi KN của module (docs/errors.md, mục pf/accounts).
var (
	ErrAssignmentNotFound  = problem.New(http.StatusNotFound, "accounts.assignment_not_found", "Không tìm thấy vai trò của tài khoản")
	ErrAssignmentDuplicate = problem.New(http.StatusConflict, "accounts.assignment_duplicate", "Tài khoản đã có vai trò này trên Nút này")
	ErrAssignmentState     = problem.New(http.StatusConflict, "accounts.assignment_state", "Vai trò đã bị thu hồi")
	ErrDepartmentRequired  = problem.New(http.StatusUnprocessableEntity, "accounts.department_required", "Chọn một phòng ban của vai trò")
	ErrDepartmentNotOpen   = problem.New(http.StatusConflict, "accounts.department_not_open", "Phòng ban chưa ở trạng thái Đang áp dụng")
	ErrDepartmentInvalid   = problem.New(http.StatusUnprocessableEntity, "accounts.department_invalid", "Phòng ban không thuộc đơn vị của Nút hoặc đã bị xóa")
	ErrPrerequisite        = problem.New(http.StatusConflict, "accounts.prerequisite_missing",
		"Chưa có Nút tổ chức hoặc phòng ban — khai báo cơ cấu tổ chức và phòng ban trước (UR-AD-04)")
	ErrCommuneInvalid    = problem.New(http.StatusUnprocessableEntity, "accounts.commune_invalid", "Xã phụ trách không thuộc Nút của vai trò")
	ErrDuplicateIdentity = problem.New(http.StatusConflict, "accounts.duplicate_identity", "Số điện thoại hoặc email đã được dùng cho tài khoản khác")
	ErrKNAdminGrant      = problem.New(http.StatusForbidden, "accounts.kn_admin_grant",
		"Vai trò Admin chỉ quản trị nền tảng gán, cho tài khoản đã có vai platform_admin (bắt buộc MFA)")
	ErrProfileUnsupported = problem.New(http.StatusServiceUnavailable, "accounts.profile_update_unavailable", "Kho tài khoản không hỗ trợ sửa thông tin")
)

// knAdminRoles: chỉ Admin quản lý tài khoản KN (FN-AD-04.04 ma trận: Admin CRUD, vai khác —).
var knAdminRoles = []string{authz.RolePlatformAdmin, port.RoleAccountAdmin}

// knCaller kiểm người gọi quản trị KN và phụ thuộc.
func (s *Service) knCaller(ctx context.Context) (*actor, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return nil, err
	}
	if s.Departments == nil {
		return nil, ErrDependency.WithDetail("chưa nối phòng ban của pf/party")
	}
	a, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	a.p = p
	return a, nil
}

// Warning là cảnh báo kèm kết quả (lưu được nhưng cần chú ý).
type Warning struct {
	Code    string
	Message string
}

// Cảnh báo FN-AD-04.04: Cấp Xã chưa gán xã → cho lưu, chưa xem được dữ liệu nghiệp vụ.
var warnNoCommunes = Warning{Code: "commune_unassigned", Message: "Cấp Xã chưa gán xã phụ trách — tài khoản chưa xem được dữ liệu nghiệp vụ"}

// scopeOf tính phạm vi của một Nút như thể người gọi thuộc Nút đó (ADR-0009, dùng lại ScopeResolver).
func (s *Service) scopeOf(ctx context.Context, tid, org uuid.UUID) (authz.Scope, error) {
	if s.Scope == nil {
		return authz.Scope{}, ErrDependency.WithDetail("chưa nối pf/party")
	}
	return s.Scope.Scope(authz.WithPrincipal(ctx, authz.Principal{TenantID: tid, OrgRef: org}))
}

// checkedAssignment là dữ liệu gán đã kiểm với dữ liệu hiện hành.
type checkedAssignment struct {
	in       domain.AssignmentInput
	unit     uuid.UUID
	warnings []Warning
}

// checkAssignment kiểm tiên quyết UR-AD-04: Nút tồn tại, đang hoạt động, đúng cấp của vai, trong phạm
// vi người gọi; phòng ban thuộc đơn vị của Nút (chưa xóa); xã phụ trách ⊂ xã của Nút. targetRealm là vai
// realm của tài khoản đích (Admin cần platform_admin).
func (s *Service) checkAssignment(ctx context.Context, a *actor, in domain.AssignmentInput, targetRealm []string) (checkedAssignment, error) {
	in, err := domain.NormalizeAssignment(in, s.now())
	if err != nil {
		return checkedAssignment{}, fieldProblem(err)
	}
	out := checkedAssignment{in: in}
	if in.Role == port.KNAdmin {
		if !a.p.Has(authz.RolePlatformAdmin) || !slices.Contains(targetRealm, authz.RolePlatformAdmin) {
			return checkedAssignment{}, ErrKNAdminGrant
		}
		if in.OrgRef != uuid.Nil {
			o, err := s.org(ctx, a, in.OrgRef)
			if err != nil {
				return checkedAssignment{}, err
			}
			if o == nil {
				return checkedAssignment{}, ErrOrgInvalid
			}
		}
		return out, nil
	}
	o, err := s.checkOrg(ctx, a, in.OrgRef)
	if err != nil {
		if errors.Is(err, ErrOrgInvalid) {
			return checkedAssignment{}, ErrPrerequisite.WithDetail("Nút tổ chức không tồn tại")
		}
		return checkedAssignment{}, err
	}
	if err := domain.KNRoleFitsOrg(in.Role, string(o.Kind)); err != nil {
		return checkedAssignment{}, fieldProblem(err)
	}
	unit, err := s.Departments.DepartmentUnit(ctx, o.Ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return checkedAssignment{}, ErrPrerequisite.WithDetail("Nút chưa thuộc đơn vị cấp 1/2 nào")
	}
	if err != nil {
		return checkedAssignment{}, err
	}
	depts, err := s.Departments.UnitDepartments(ctx, unit)
	if err != nil {
		return checkedAssignment{}, err
	}
	if len(depts) == 0 {
		return checkedAssignment{}, ErrPrerequisite.WithDetail("Đơn vị chưa khai báo phòng ban")
	}
	for _, code := range in.Departments {
		if !slices.ContainsFunc(depts, func(d partyport.Department) bool { return d.Code == code && !d.Retired }) {
			return checkedAssignment{}, ErrDepartmentInvalid.With("department_code", code)
		}
	}
	out.unit = unit
	if in.Role == port.KNXa {
		if len(in.Communes) == 0 {
			out.warnings = append(out.warnings, warnNoCommunes)
		} else {
			sc, err := s.scopeOf(ctx, a.p.TenantID, o.Ref)
			if err != nil {
				return checkedAssignment{}, err
			}
			for _, c := range in.Communes {
				if c == o.AdminUnitCode || !sc.Allows(c) {
					return checkedAssignment{}, ErrCommuneInvalid.With("commune_code", c)
				}
			}
		}
	}
	return out, nil
}

// knTarget là tài khoản đích của thao tác gán vai trò.
type knTarget struct {
	user  port.User
	realm []string
	tg    *target
}

// state là trạng thái tài khoản ở pf với vai do module quản lý theo realm hiện hành.
func (t *knTarget) state() domain.Account {
	acc := t.tg.state()
	acc.Roles = domain.ManagedOf(t.realm)
	return acc
}

func (s *Service) knTarget(ctx context.Context, a *actor, id uuid.UUID) (*knTarget, error) {
	if id == a.p.ActorRef {
		return nil, ErrSelfAction
	}
	t, err := s.target(ctx, a, id)
	if err != nil {
		return nil, err
	}
	realm, err := s.Dir.RealmRoles(ctx, id)
	if err != nil {
		return nil, dirErr(err)
	}
	if t.acc != nil && !t.acc.Enabled {
		return nil, ErrState.WithDetail("Tài khoản đang bị khóa.")
	}
	return &knTarget{user: t.user, realm: realm, tg: t}, nil
}

// AssignmentResult là kết quả một thao tác gán vai trò.
type AssignmentResult struct {
	Assignment port.Assignment
	Seq        int64
	Replayed   bool
	Warnings   []Warning
}

func asgEventPayload(e store.AsgEvent) (string, any) {
	a := e.A
	var org *string
	if a.OrgRef != uuid.Nil {
		v := a.OrgRef.String()
		org = &v
	}
	deps, comm := nonNil(a.Departments), nonNil(a.Communes)
	at := e.EventTime.UTC().Format(time.RFC3339Nano)
	switch e.Action {
	case domain.AsgGranted:
		return port.EventAssignmentGranted, port.AssignmentGrantedV1{AssignmentID: a.ID, UserRef: a.UserRef, Role: a.Role, OrgRef: org,
			Departments: deps, Communes: comm, Active: a.Active, ActorRef: e.ActorRef, EventTime: at, Seq: e.Seq}
	case domain.AsgChanged:
		return port.EventAssignmentChanged, port.AssignmentChangedV1{AssignmentID: a.ID, UserRef: a.UserRef, Role: a.Role, OrgRef: org,
			Departments: deps, Communes: comm, Active: a.Active, ActorRef: e.ActorRef, EventTime: at, Seq: e.Seq}
	}
	return port.EventAssignmentRevoked, port.AssignmentRevokedV1{AssignmentID: a.ID, UserRef: a.UserRef, Role: a.Role, OrgRef: org,
		Departments: deps, Communes: comm, Active: a.Active, ActorRef: e.ActorRef, EventTime: at, Seq: e.Seq}
}

func snapshot(a port.Assignment) []byte {
	b, err := json.Marshal(domain.SnapshotOf(a))
	if err != nil {
		panic(err) // chỉ chuỗi, bool
	}
	return b
}

// writeAsg ghi sổ + bảng chiếu + outbox trong một giao dịch. mutate nhận trạng thái hiện hành (nil khi
// gán mới) và trả trạng thái mới. Trùng idem_key → trả bản đã ghi.
func (s *Service) writeAsg(ctx context.Context, tid, actorRef uuid.UUID, key, action string, reason *string, id uuid.UUID,
	mutate func(tx pgx.Tx, cur *port.Assignment) (port.Assignment, error)) (AssignmentResult, error) {
	var res AssignmentResult
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		if prev, err := store.AsgEventByIdem(ctx, tx, key); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if id != uuid.Nil && prev.A.ID != id {
				return problem.IdempotencyConflict
			}
			cur, err := store.AssignmentByID(ctx, tx, prev.A.ID, false)
			if err != nil {
				return err
			}
			if cur == nil {
				return ErrAssignmentNotFound
			}
			res = AssignmentResult{Assignment: *cur, Seq: prev.Seq, Replayed: true}
			return nil
		}
		var cur *port.Assignment
		if id != uuid.Nil {
			var err error
			if cur, err = store.AssignmentByID(ctx, tx, id, true); err != nil {
				return err
			}
			if cur == nil {
				return ErrAssignmentNotFound
			}
		}
		next, err := mutate(tx, cur)
		if err != nil {
			return err
		}
		now := s.now()
		next.UpdatedBy, next.UpdatedAt = actorRef, now
		e := store.AsgEvent{ID: ids.New(), TenantID: tid, Action: action, A: next, After: snapshot(next), Reason: reason,
			EventTime: now, ActorRef: actorRef, IdemKey: key}
		if cur != nil {
			e.Before = snapshot(*cur)
			next.CreatedBy, next.CreatedAt = cur.CreatedBy, cur.CreatedAt
		} else {
			next.CreatedBy, next.CreatedAt = actorRef, now
		}
		e.A = next
		if err := ledger.Classify(store.InsertAsgEvent(ctx, tx, &e)); err != nil {
			return err
		}
		if err := store.PutAssignment(ctx, tx, e); err != nil {
			return err
		}
		typ, payload := asgEventPayload(e)
		if err := outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: Producer, Type: typ, Payload: payload}); err != nil {
			return err
		}
		res = AssignmentResult{Assignment: next, Seq: e.Seq}
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return s.writeAsg(ctx, tid, actorRef, key, action, reason, id, mutate) // bên thua của hai request song song đọc lại
	}
	return res, err
}

// noDuplicate: một tài khoản không có hai lần gán đang hoạt động cùng vai trò trên cùng Nút.
func noDuplicate(ctx context.Context, tx pgx.Tx, user uuid.UUID, in domain.AssignmentInput, except uuid.UUID) error {
	cur, err := store.AssignmentsOfUser(ctx, tx, user, false)
	if err != nil {
		return err
	}
	for _, x := range cur {
		if x.ID != except && x.Role == in.Role && x.OrgRef == in.OrgRef {
			return ErrAssignmentDuplicate
		}
	}
	return nil
}

func assignmentFrom(id, user uuid.UUID, c checkedAssignment) port.Assignment {
	in := c.in
	return port.Assignment{ID: id, UserRef: user, Role: in.Role, OrgRef: in.OrgRef, UnitRef: c.unit, Departments: in.Departments,
		MgmtLevel: in.MgmtLevel, Communes: in.Communes, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, Active: true}
}

// ensureKNUser cấp vai realm kn_user (cổng vào bề mặt KN) và TOTP cho Admin nếu chưa có.
func (s *Service) ensureKNUser(ctx context.Context, t *knTarget, role string) error {
	if !slices.Contains(t.realm, port.RoleKNUser) {
		if err := s.Dir.AddRealmRoles(ctx, t.user.ID, []string{port.RoleKNUser}); err != nil {
			return dirErr(err)
		}
		t.realm = append(t.realm, port.RoleKNUser)
	}
	if role == port.KNAdmin {
		has, err := s.Dir.HasOTP(ctx, t.user.ID)
		if err != nil {
			return dirErr(err)
		}
		if !has && !slices.Contains(t.user.RequiredActions, port.ActionConfigureTOTP) {
			return dirErr(s.Dir.AddRequiredActions(ctx, t.user.ID, []string{port.ActionConfigureTOTP}))
		}
	}
	return nil
}

// GrantAssignment thêm một vai trò cho tài khoản ("Thêm vai trò" ở màn Detail).
func (s *Service) GrantAssignment(ctx context.Context, user uuid.UUID, in domain.AssignmentInput, reason, idem string) (AssignmentResult, error) {
	if err := requireKey(idem); err != nil {
		return AssignmentResult{}, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return AssignmentResult{}, err
	}
	key := "asg:grant:" + idem
	if prev, err := s.asgReplay(ctx, a.p.TenantID, key); err != nil || prev != nil {
		return derefAsg(prev), err
	}
	t, err := s.knTarget(ctx, a, user)
	if err != nil {
		return AssignmentResult{}, err
	}
	c, err := s.checkAssignment(ctx, a, in, t.realm)
	if err != nil {
		return AssignmentResult{}, err
	}
	if err := s.ensureKNUser(ctx, t, c.in.Role); err != nil {
		return AssignmentResult{}, err
	}
	vaultGranted := false
	if c.in.Role == port.KNXa { // A1: Cán bộ xã mặc định có vai kho định danh
		had := slices.Contains(t.realm, KNXaVaultRole)
		if t.realm, err = s.grantKNXaVault(ctx, a.p.TenantID, a.p.ActorRef, t.state(), t.realm, key+":vault"); err != nil {
			return AssignmentResult{}, err
		}
		vaultGranted = !had
	}
	res, err := s.writeAsg(ctx, a.p.TenantID, a.p.ActorRef, key, domain.AsgGranted, optStr(reason), uuid.Nil,
		func(tx pgx.Tx, _ *port.Assignment) (port.Assignment, error) {
			if err := noDuplicate(ctx, tx, user, c.in, uuid.Nil); err != nil {
				return port.Assignment{}, err
			}
			return assignmentFrom(ids.New(), user, c), nil
		})
	if err != nil && vaultGranted {
		err = s.undoKNXaVault(ctx, a, user, key, err)
	}
	res.Warnings = c.warnings
	return res, err
}

// undoKNXaVault bù khi lần gán kn_xa không ghi được sau khi đã tự cấp vai kho định danh (A1): thu lại vai — không để
// tài khoản giữ quyền ghi PII mà không có lần gán kn_xa nào (releaseKNXaVault giữ nguyên nếu còn kn_xa khác hoặc vai do
// Admin cấp tay). Trả lỗi gốc, cộng lỗi bù nếu có.
func (s *Service) undoKNXaVault(ctx context.Context, a *actor, user uuid.UUID, key string, cause error) error {
	if rerr := s.releaseKNXaVault(context.WithoutCancel(ctx), a, user, key+":vault-undo"); rerr != nil {
		return errors.Join(cause, rerr)
	}
	return cause
}

func optStr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func (s *Service) asgReplay(ctx context.Context, tid uuid.UUID, key string) (*AssignmentResult, error) {
	var out *AssignmentResult
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		prev, err := store.AsgEventByIdem(ctx, tx, key)
		if err != nil || prev == nil {
			return err
		}
		cur, err := store.AssignmentByID(ctx, tx, prev.A.ID, false)
		if err != nil || cur == nil {
			return err
		}
		out = &AssignmentResult{Assignment: *cur, Seq: prev.Seq, Replayed: true}
		return nil
	})
	return out, err
}

func derefAsg(r *AssignmentResult) AssignmentResult {
	if r == nil {
		return AssignmentResult{}
	}
	return *r
}

// ChangeAssignment sửa một vai trò (thay toàn bộ phạm vi; vai trò không đổi — đổi vai = thu hồi + gán mới).
func (s *Service) ChangeAssignment(ctx context.Context, user, id uuid.UUID, in domain.AssignmentInput, reason, idem string) (AssignmentResult, error) {
	if err := requireKey(idem); err != nil {
		return AssignmentResult{}, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return AssignmentResult{}, err
	}
	key := "asg:change:" + idem
	if prev, err := s.asgReplay(ctx, a.p.TenantID, key); err != nil || prev != nil {
		return derefAsg(prev), err
	}
	t, err := s.knTarget(ctx, a, user)
	if err != nil {
		return AssignmentResult{}, err
	}
	keepFrom := in.ValidFrom.IsZero()
	c, err := s.checkAssignment(ctx, a, in, t.realm)
	if err != nil {
		return AssignmentResult{}, err
	}
	res, err := s.writeAsg(ctx, a.p.TenantID, a.p.ActorRef, key, domain.AsgChanged, optStr(reason), id,
		func(tx pgx.Tx, cur *port.Assignment) (port.Assignment, error) {
			if cur.UserRef != user {
				return port.Assignment{}, ErrAssignmentNotFound
			}
			if !cur.Active {
				return port.Assignment{}, ErrAssignmentState
			}
			if cur.Role != c.in.Role {
				return port.Assignment{}, problem.Validation.WithField("role", "không đổi vai trò của lần gán — thu hồi và gán vai trò mới")
			}
			if err := noDuplicate(ctx, tx, user, c.in, id); err != nil {
				return port.Assignment{}, err
			}
			next := assignmentFrom(id, user, c)
			if keepFrom {
				next.ValidFrom = cur.ValidFrom // không gửi valid_from → giữ hiệu lực cũ
			}
			if string(snapshot(next)) == string(snapshot(*cur)) {
				return port.Assignment{}, ErrAssignmentState.WithDetail("Không có thay đổi.")
			}
			return next, nil
		})
	res.Warnings = c.warnings
	return res, err
}

// RevokeAssignment thu hồi một vai trò (lý do ≥ 10 ký tự). Ngữ cảnh phiên của vai trò hết hiệu lực ở
// request kế tiếp; dữ liệu đã nhập giữ nguyên.
func (s *Service) RevokeAssignment(ctx context.Context, user, id uuid.UUID, reason, idem string) (AssignmentResult, error) {
	if err := requireKey(idem); err != nil {
		return AssignmentResult{}, err
	}
	if err := domain.CheckKNReason("reason", reason); err != nil {
		return AssignmentResult{}, fieldProblem(err)
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return AssignmentResult{}, err
	}
	key := "asg:revoke:" + idem
	if prev, err := s.asgReplay(ctx, a.p.TenantID, key); err != nil || prev != nil {
		if err == nil && prev.Assignment.Role == port.KNXa {
			err = s.releaseKNXaVault(ctx, a, user, key+":vault") // lần trước lỗi giữa chừng → làm tiếp
		}
		return derefAsg(prev), err
	}
	if _, err := s.knTarget(ctx, a, user); err != nil {
		return AssignmentResult{}, err
	}
	res, err := s.writeAsg(ctx, a.p.TenantID, a.p.ActorRef, key, domain.AsgRevoked, optStr(reason), id,
		func(_ pgx.Tx, cur *port.Assignment) (port.Assignment, error) {
			if cur.UserRef != user {
				return port.Assignment{}, ErrAssignmentNotFound
			}
			if !cur.Active {
				return port.Assignment{}, ErrAssignmentState
			}
			if cur.Role == port.KNAdmin && !a.p.Has(authz.RolePlatformAdmin) {
				return port.Assignment{}, ErrKNAdminGrant
			}
			next := *cur
			next.Active = false
			return next, nil
		})
	if err == nil && res.Assignment.Role == port.KNXa {
		err = s.releaseKNXaVault(ctx, a, user, key+":vault")
	}
	return res, err
}

// ListAssignments liệt kê vai trò của tài khoản (Admin, màn Detail khối "Vai trò và phạm vi").
func (s *Service) ListAssignments(ctx context.Context, user uuid.UUID, all bool) ([]port.Assignment, error) {
	a, err := s.knCaller(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.target(ctx, a, user); err != nil {
		return nil, err
	}
	var out []port.Assignment
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.AssignmentsOfUser(ctx, tx, user, all)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Tạo tài khoản KN (email hoặc SĐT đăng nhập) — FN-AD-04.04 + FN-CH-00.01
// ---------------------------------------------------------------------------

// KNCreateInput là dữ liệu "Tạo tài khoản" của màn Detail.
type KNCreateInput struct {
	Profile     domain.Profile
	Assignments []domain.AssignmentInput
	IdemKey     string
}

// KNCreateResult là kết quả tạo.
type KNCreateResult struct {
	User           port.User
	Assignments    []port.Assignment
	Replayed       bool
	ActivationSent bool
	Warnings       []Warning
}

// loginName: tên đăng nhập Keycloak = SĐT chuẩn hóa (đăng nhập bằng SĐT); không có SĐT → email.
// Email luôn đăng nhập được qua loginWithEmailAllowed của realm.
func loginName(p domain.Profile) string {
	if p.Phone != "" {
		return p.Phone
	}
	return p.Email
}

// checkIdentity: SĐT/email chưa dùng làm tên đăng nhập, email hoặc SĐT của tài khoản khác (except).
func (s *Service) checkIdentity(ctx context.Context, tid uuid.UUID, p domain.Profile, except uuid.UUID) error {
	qs := []port.Query{{Username: p.Email, Max: 2}, {Email: p.Email, Max: 2}}
	if p.Phone != "" {
		qs = append(qs, port.Query{Username: p.Phone, Max: 2},
			port.Query{Attributes: map[string]string{port.AttrTenantID: tid.String(), port.AttrPhone: p.Phone}, Max: 2})
	}
	for _, q := range qs {
		found, err := s.Dir.FindUsers(ctx, q)
		if err != nil {
			return dirErr(err)
		}
		for _, u := range found {
			if u.ID != except {
				field := "email"
				if q.Email == "" && q.Username != p.Email {
					field = "phone"
				}
				return ErrDuplicateIdentity.With("field", field)
			}
		}
	}
	return nil
}

// CreateKNUser tạo tài khoản KN v7.7: Keycloak (tên đăng nhập = SĐT chuẩn hóa hoặc email, vai realm
// kn_user, required actions VERIFY_EMAIL + UPDATE_PASSWORD — lần đầu bắt buộc đặt mật khẩu qua liên kết
// kích hoạt, không mật khẩu tạm), sổ account_event và các lần gán vai trò. Tiên quyết: Nút và phòng ban.
func (s *Service) CreateKNUser(ctx context.Context, in KNCreateInput) (KNCreateResult, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return KNCreateResult{}, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return KNCreateResult{}, err
	}
	prof := in.Profile
	prof.Username = ""
	prof.Phone = domain.NormalizePhone(prof.Phone)
	prof.Username = loginName(domain.Profile{Email: strings.ToLower(strings.TrimSpace(prof.Email)), Phone: prof.Phone})
	if prof, err = prof.Normalize(); err != nil {
		return KNCreateResult{}, fieldProblem(err)
	}
	if len(in.Assignments) == 0 {
		return KNCreateResult{}, problem.Validation.WithField("assignments", "bắt buộc ít nhất một vai trò")
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil {
		return KNCreateResult{}, err
	} else if prev != nil {
		return s.knReplayed(ctx, a, *prev)
	}
	var checked []checkedAssignment
	var warnings []Warning
	for _, x := range in.Assignments {
		c, err := s.checkAssignment(ctx, a, x, []string{port.RoleKNUser})
		if err != nil {
			return KNCreateResult{}, err
		}
		for _, y := range checked {
			if y.in.Role == c.in.Role && y.in.OrgRef == c.in.OrgRef {
				return KNCreateResult{}, ErrAssignmentDuplicate
			}
		}
		checked = append(checked, c)
		warnings = append(warnings, c.warnings...)
	}
	if err := s.checkIdentity(ctx, a.p.TenantID, prof, uuid.Nil); err != nil {
		return KNCreateResult{}, err
	}
	acts := []string{port.ActionVerifyEmail, port.ActionUpdatePassword}
	id, err := s.Dir.CreateUser(ctx, port.NewUser{Username: prof.Username, Email: prof.Email, FirstName: prof.FirstName,
		LastName: prof.LastName, Phone: prof.Phone, TenantID: a.p.TenantID, RequiredActions: acts})
	if err != nil {
		if errors.Is(err, port.ErrUserExists) {
			return KNCreateResult{}, ErrDuplicateIdentity
		}
		return KNCreateResult{}, dirErr(err)
	}
	compensate := func(cause error) (KNCreateResult, error) {
		if derr := s.Dir.DeleteUser(context.WithoutCancel(ctx), id); derr != nil {
			return KNCreateResult{}, errors.Join(cause, derr)
		}
		return KNCreateResult{}, cause
	}
	if err := s.Dir.AddRealmRoles(ctx, id, []string{port.RoleKNUser}); err != nil {
		return compensate(dirErr(err))
	}
	acc := domain.Account{UserRef: id, Roles: []string{port.RoleKNUser}, Enabled: true}
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionCreated, s.now(), in.IdemKey, "")
	ev.Details = map[string]any{"required_actions": acts, "surface": "kn_v77"}
	if replayed, err := s.write(ctx, a.p.TenantID, &ev); err != nil {
		return compensate(err)
	} else if replayed {
		_, _ = compensate(nil)
		return s.knReplayed(ctx, a, ev)
	}
	vaultGranted := false
	if slices.ContainsFunc(checked, func(c checkedAssignment) bool { return c.in.Role == port.KNXa }) {
		// A1: Cán bộ xã mặc định có vai kho định danh. Lỗi ở đây: tài khoản đã ghi sổ, không bù (như lần gán dưới).
		realm, err := s.grantKNXaVault(ctx, a.p.TenantID, a.p.ActorRef, acc, []string{port.RoleKNUser}, in.IdemKey+":vault")
		if err != nil {
			return KNCreateResult{}, err
		}
		acc.Roles = domain.ManagedOf(realm)
		vaultGranted = true
	}
	res := KNCreateResult{Warnings: warnings}
	for i, c := range checked {
		r, err := s.writeAsg(ctx, a.p.TenantID, a.p.ActorRef, in.IdemKey+":asg:"+strconv.Itoa(i), domain.AsgGranted, nil, uuid.Nil,
			func(pgx.Tx, *port.Assignment) (port.Assignment, error) { return assignmentFrom(ids.New(), id, c), nil })
		if err != nil {
			// Tài khoản đã ghi sổ: không xóa, Admin thêm vai trò còn thiếu ở màn Detail. Nhưng vai kho định danh vừa tự
			// cấp thì thu lại nếu chưa có lần gán kn_xa nào ghi được.
			if vaultGranted {
				err = s.undoKNXaVault(ctx, a, id, in.IdemKey, err)
			}
			return KNCreateResult{}, err
		}
		res.Assignments = append(res.Assignments, r.Assignment)
	}
	res.User, _ = s.Dir.GetUser(ctx, id)
	res.ActivationSent = s.sendActions(ctx, a.p.TenantID, a.p.ActorRef, acc, acts, in.IdemKey+":actions") == nil
	return res, nil
}

func (s *Service) knReplayed(ctx context.Context, a *actor, ev domain.Event) (KNCreateResult, error) {
	r, err := s.replayed(ctx, ev)
	if err != nil {
		return KNCreateResult{}, err
	}
	out := KNCreateResult{User: r.User, Replayed: true, ActivationSent: r.ActivationSent}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Assignments, e = store.AssignmentsOfUser(ctx, tx, ev.Account.UserRef, true)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Sửa thông tin tài khoản (họ tên, email, SĐT) — sổ account_profile_change (chỉ tên trường, RB-3)
// ---------------------------------------------------------------------------

// ProfilePatch là thay đổi thông tin tài khoản; nil = giữ nguyên.
type ProfilePatch struct {
	FullName *string `pii:"true"`
	Email    *string `pii:"true"`
	Phone    *string `pii:"true"`
	Reason   string
	IdemKey  string
}

// UpdateProfile sửa thông tin tài khoản ở Keycloak và ghi một dòng sổ chỉnh sửa. Đổi SĐT của tài khoản
// đăng nhập bằng SĐT → đổi luôn tên đăng nhập (FN-CH-00.01).
func (s *Service) UpdateProfile(ctx context.Context, user uuid.UUID, in ProfilePatch) (store.ProfileChange, bool, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return store.ProfileChange{}, false, err
	}
	a, err := s.knCaller(ctx)
	if err != nil {
		return store.ProfileChange{}, false, err
	}
	up, ok := s.Dir.(port.ProfileUpdater)
	if !ok {
		return store.ProfileChange{}, false, ErrProfileUnsupported
	}
	key := "profile:" + in.IdemKey
	var prev *store.ProfileChange
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ProfileChangeByIdem(ctx, tx, key)
		return e
	}); err != nil {
		return store.ProfileChange{}, false, err
	}
	if prev != nil {
		return *prev, true, nil
	}
	if user == a.p.ActorRef {
		return store.ProfileChange{}, false, ErrSelfAction
	}
	t, err := s.target(ctx, a, user)
	if err != nil {
		return store.ProfileChange{}, false, err
	}
	cur := domain.Profile{Username: t.user.Username, Email: t.user.Email, FirstName: t.user.FirstName, LastName: t.user.LastName, Phone: t.user.Phone}
	next := cur
	if in.FullName != nil {
		next.FirstName, next.LastName = domain.SplitFullName(*in.FullName)
	}
	if in.Email != nil {
		next.Email = *in.Email
	}
	if in.Phone != nil {
		next.Phone = *in.Phone
	}
	next.Phone = domain.NormalizePhone(next.Phone)
	if (cur.Phone != "" && cur.Username == cur.Phone) || cur.Username == cur.Email {
		// Tên đăng nhập theo SĐT/email: đi theo giá trị mới.
		next.Username = loginName(domain.Profile{Email: strings.ToLower(strings.TrimSpace(next.Email)), Phone: next.Phone})
	}
	if next, err = next.Normalize(); err != nil {
		return store.ProfileChange{}, false, fieldProblem(err)
	}
	var fields []string
	if next.FirstName != cur.FirstName || next.LastName != cur.LastName {
		fields = append(fields, "full_name")
	}
	if next.Email != strings.ToLower(cur.Email) {
		fields = append(fields, "email")
	}
	if next.Phone != cur.Phone {
		fields = append(fields, "phone")
	}
	if next.Username != strings.ToLower(cur.Username) {
		fields = append(fields, "username")
	}
	if len(fields) == 0 {
		return store.ProfileChange{}, false, ErrState.WithDetail("Không có thay đổi.")
	}
	if slices.Contains(fields, "email") || slices.Contains(fields, "phone") {
		if err := s.checkIdentity(ctx, a.p.TenantID, next, user); err != nil {
			return store.ProfileChange{}, false, err
		}
	}
	if err := up.UpdateProfile(ctx, user, port.ProfileUpdate{Username: next.Username, Email: next.Email, FirstName: next.FirstName,
		LastName: next.LastName, Phone: next.Phone}); err != nil {
		if errors.Is(err, port.ErrUserExists) {
			return store.ProfileChange{}, false, ErrDuplicateIdentity
		}
		return store.ProfileChange{}, false, dirErr(err)
	}
	pc := store.ProfileChange{ID: ids.New(), TenantID: a.p.TenantID, UserRef: user, Fields: fields, Reason: optStr(in.Reason),
		EventTime: s.now(), ActorRef: a.p.ActorRef, IdemKey: key}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		return ledger.Classify(store.InsertProfileChange(ctx, tx, &pc))
	})
	return pc, false, err
}
