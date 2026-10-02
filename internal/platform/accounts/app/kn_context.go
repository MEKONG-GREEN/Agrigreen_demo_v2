package app

// KN v7.7 gói P1 — FN-CH-00.01: ngữ cảnh phiên (gap §4.5 phương án A). Client chọn vai trò × phòng ban
// (POST /v1/me/context) → context_id, gửi lại qua X-AG-Context ở MỌI request; ResolveContext dựng
// authz.KNContext ở server (vai, phòng ban, phạm vi) — không tin trường nào từ client.

import (
	"context"
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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi ngữ cảnh phiên.
var (
	ErrContextInvalid = problem.New(http.StatusForbidden, "accounts.context_invalid", "Ngữ cảnh phiên không hợp lệ hoặc không thuộc người gọi")
	ErrContextExpired = problem.New(http.StatusForbidden, "accounts.context_expired", "Ngữ cảnh phiên đã hết hạn — chọn lại vai trò và phòng ban")
	ErrContextRevoked = problem.New(http.StatusForbidden, "accounts.context_revoked",
		"Vai trò hoặc phòng ban của ngữ cảnh không còn hiệu lực, hoặc tài khoản đã bị khóa — chọn lại hoặc liên hệ Admin")
	ErrAccountLocked = problem.New(http.StatusForbidden, "accounts.account_locked", "Tài khoản đã bị khóa, vui lòng liên hệ Admin để mở khóa.")
)

// AdminOrgName là tên hiển thị của Admin không gắn Nút (Hình 3c).
const AdminOrgName = "Toàn hệ thống"

// DepartmentView là phòng ban của một lần gán (thẻ phòng ban ở Hình 3b).
type DepartmentView struct {
	Code        string
	Name        string
	Description string
	Status      string // active | not_open | suspended
}

// CommuneView là xã phụ trách.
type CommuneView struct {
	Code string
	Name string
}

// AssignmentView là lần gán đã tra tên (GET /v1/me/contexts, màn Detail tài khoản).
type AssignmentView struct {
	port.Assignment
	OrgName        string
	MgmtLevelLabel string
	CommuneViews   []CommuneView
	DepartmentRefs []DepartmentView
}

// ContextView là ngữ cảnh đã chọn (POST /v1/me/context).
type ContextView struct {
	ContextID      uuid.UUID
	AssignmentID   uuid.UUID
	Role           string
	Department     *DepartmentView
	OrgRef         uuid.UUID
	OrgName        string
	Communes       []CommuneView
	DefaultRoute   string
	ExpiresAt      time.Time
	SupportID      uuid.UUID
	OnBehalfOf     uuid.UUID
	Replayed       bool
	SupportReplays bool
}

// accountEnabled: tài khoản theo dõi ở pf và đang bị khóa → false.
func accountEnabled(ctx context.Context, tx pgx.Tx, user uuid.UUID) (bool, error) {
	acc, err := store.Account(ctx, tx, user, false)
	if err != nil {
		return false, err
	}
	return acc == nil || acc.Enabled, nil
}

func (s *Service) orgName(ctx context.Context, ref uuid.UUID) string {
	if ref == uuid.Nil || s.Orgs == nil {
		return AdminOrgName
	}
	o, err := s.Orgs.Get(ctx, ref)
	if err != nil {
		return ""
	}
	return o.Name
}

func (s *Service) communeViews(ctx context.Context, codes []string) []CommuneView {
	out := make([]CommuneView, 0, len(codes))
	for _, c := range codes {
		v := CommuneView{Code: c, Name: c}
		if s.Units != nil {
			if u, err := s.Units.Get(ctx, c, s.now()); err == nil && u.Name != "" {
				v.Name = u.Name
			}
		}
		out = append(out, v)
	}
	return out
}

// deptViews trả phòng ban (chưa xóa) của lần gán theo thứ tự của đơn vị.
func (s *Service) deptViews(ctx context.Context, a port.Assignment) ([]DepartmentView, error) {
	out := []DepartmentView{}
	if a.Role == port.KNAdmin || len(a.Departments) == 0 {
		return out, nil
	}
	depts, err := s.Departments.UnitDepartments(ctx, a.UnitRef)
	if err != nil {
		return nil, err
	}
	for _, d := range depts {
		if !d.Retired && slices.Contains(a.Departments, d.Code) {
			out = append(out, DepartmentView{Code: d.Code, Name: d.Name, Description: d.Description, Status: d.Status})
		}
	}
	return out, nil
}

func (s *Service) viewOf(ctx context.Context, a port.Assignment) (AssignmentView, error) {
	d, err := s.deptViews(ctx, a)
	if err != nil {
		return AssignmentView{}, err
	}
	return AssignmentView{Assignment: a, OrgName: s.orgName(ctx, a.OrgRef), MgmtLevelLabel: port.MgmtLevelLabel(a.MgmtLevel),
		CommuneViews: s.communeViews(ctx, a.Communes), DepartmentRefs: d}, nil
}

// Views tra tên cho danh sách lần gán.
func (s *Service) Views(ctx context.Context, as []port.Assignment) ([]AssignmentView, error) {
	out := make([]AssignmentView, 0, len(as))
	for _, a := range as {
		v, err := s.viewOf(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Service) knSelf(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	if s.Departments == nil || s.Scope == nil {
		return p, ErrDependency.WithDetail("chưa nối pf/party")
	}
	return p, nil
}

// MyContexts trả các lần gán còn hiệu lực của người gọi (GET /v1/me/contexts). Người dùng không có lần
// gán nào (vai thử cũ, tài khoản G1) → danh sách rỗng: giữ bề mặt hiện có.
func (s *Service) MyContexts(ctx context.Context) ([]AssignmentView, error) {
	p, err := s.knSelf(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var as []port.Assignment
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		ok, err := accountEnabled(ctx, tx, p.ActorRef)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAccountLocked
		}
		all, err := store.AssignmentsOfUser(ctx, tx, p.ActorRef, false)
		for _, a := range all {
			if a.EffectiveAt(now) {
				as = append(as, a)
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Views(ctx, as)
}

// SelectContext tạo ngữ cảnh phiên cho lần gán + phòng ban (POST /v1/me/context). Đổi vai trò/phòng ban
// = chọn lại, không cần đăng nhập lại; ngữ cảnh cũ vẫn hợp lệ tới khi hết hạn (nhiều tab).
func (s *Service) SelectContext(ctx context.Context, assignment uuid.UUID, dept string, idem string) (ContextView, error) {
	p, err := s.knSelf(ctx)
	if err != nil {
		return ContextView{}, err
	}
	if err := requireKey(idem); err != nil {
		return ContextView{}, err
	}
	dept = strings.TrimSpace(dept)
	now := s.now()
	var row store.Context
	var asg port.Assignment
	replayed := false
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ContextByIdem(ctx, tx, p.ActorRef, idem); err != nil || prev != nil {
			if err != nil {
				return err
			}
			row, replayed = *prev, true
			a, err := store.AssignmentByID(ctx, tx, prev.AssignmentID, false)
			if err == nil && a != nil {
				asg = *a
			}
			return err
		}
		ok, err := accountEnabled(ctx, tx, p.ActorRef)
		if err != nil {
			return err
		}
		if !ok {
			return ErrAccountLocked
		}
		a, err := store.AssignmentByID(ctx, tx, assignment, false)
		if err != nil {
			return err
		}
		// Lần gán của người khác coi như không có (không lộ sự tồn tại).
		if a == nil || a.UserRef != p.ActorRef || !a.EffectiveAt(now) {
			return ErrAssignmentNotFound
		}
		asg = *a
		return nil
	})
	if err != nil {
		return ContextView{}, err
	}
	if !replayed {
		if asg.Role == port.KNAdmin {
			// Admin không chọn phòng ban; bắt buộc MFA (NFR bảo mật, Q-22).
			if !p.MFA {
				return ContextView{}, problem.MFARequired
			}
			dept = ""
		} else {
			if dept == "" || !slices.Contains(asg.Departments, dept) {
				return ContextView{}, ErrDepartmentRequired
			}
			d, err := s.Departments.DepartmentByCode(ctx, asg.UnitRef, dept)
			if errors.Is(err, partyport.ErrDepartmentNotFound) {
				return ContextView{}, ErrDepartmentRequired
			}
			if err != nil {
				return ContextView{}, err
			}
			if !d.Selectable() {
				return ContextView{}, ErrDepartmentNotOpen.WithDetail(d.Name)
			}
		}
		exp := now.Add(domain.ContextTTL)
		if asg.ValidTo != nil && asg.ValidTo.Before(exp) {
			exp = *asg.ValidTo
		}
		row = store.Context{ID: ids.New(), TenantID: p.TenantID, UserRef: p.ActorRef, AssignmentID: asg.ID, Role: asg.Role,
			Department: dept, OrgRef: asg.OrgRef, UnitRef: asg.UnitRef, Communes: asg.Communes, IdemKey: idem, CreatedAt: now, ExpiresAt: exp}
		if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { return store.InsertContext(ctx, tx, row) }); err != nil {
			return ContextView{}, err
		}
	}
	v, err := s.contextView(ctx, row, asg.Communes)
	v.Replayed = replayed
	return v, err
}

func (s *Service) contextView(ctx context.Context, row store.Context, communes []string) (ContextView, error) {
	v := ContextView{ContextID: row.ID, AssignmentID: row.AssignmentID, Role: row.Role, OrgRef: row.OrgRef, OrgName: s.orgName(ctx, row.OrgRef),
		Communes: s.communeViews(ctx, communes), DefaultRoute: domain.DefaultRoute(row.Role), ExpiresAt: row.ExpiresAt,
		SupportID: row.SupportID, OnBehalfOf: row.OnBehalfOf}
	if row.Department != "" {
		d, err := s.Departments.DepartmentByCode(ctx, row.UnitRef, row.Department)
		if err != nil && !errors.Is(err, partyport.ErrDepartmentNotFound) {
			return ContextView{}, err
		}
		dv := DepartmentView{Code: row.Department, Name: d.Name, Description: d.Description, Status: d.Status}
		v.Department = &dv
	}
	return v, nil
}

// CurrentContext trả ngữ cảnh của request (GET /v1/me/context).
func (s *Service) CurrentContext(ctx context.Context) (ContextView, error) {
	p, err := s.knSelf(ctx)
	if err != nil {
		return ContextView{}, err
	}
	if p.KN == nil {
		return ContextView{}, authz.ErrNoKNContext
	}
	k := p.KN
	row := store.Context{ID: k.ID, AssignmentID: k.AssignmentID, Role: k.Role, Department: k.Department, OrgRef: k.OrgRef, UnitRef: k.UnitRef,
		ExpiresAt: k.ExpiresAt}
	if k.Support != nil {
		row.SupportID, row.OnBehalfOf = k.Support.SessionID, k.Support.OnBehalfOf
	}
	return s.contextView(ctx, row, k.Communes)
}

// ResolveContext kiểm và dựng ngữ cảnh phiên cho middleware (mọi request có X-AG-Context):
//   - context thuộc tenant của token (RLS) và đúng người trong token (chống giả header);
//   - còn hạn; tài khoản không bị khóa;
//   - ngữ cảnh thường: lần gán còn hiệu lực, cùng vai, phòng ban vẫn trong lần gán và Đang áp dụng;
//     Admin bắt buộc MFA;
//   - ngữ cảnh hỗ trợ: phiên hỗ trợ chưa kết thúc, người gọi vẫn là platform_admin có MFA.
//
// Phạm vi tính lại mỗi request từ Nút (ADR-0009); Cấp Xã = xã phụ trách HIỆN HÀNH của lần gán.
func (s *Service) ResolveContext(ctx context.Context, id uuid.UUID) (*authz.KNContext, error) {
	p, err := s.knSelf(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var row *store.Context
	var asg *port.Assignment
	var sup *store.Support
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		if row, err = store.ContextByID(ctx, tx, id); err != nil {
			return err
		}
		if row == nil || row.UserRef != p.ActorRef {
			return ErrContextInvalid
		}
		if !now.Before(row.ExpiresAt) {
			return ErrContextExpired
		}
		ok, err := accountEnabled(ctx, tx, p.ActorRef)
		if err != nil {
			return err
		}
		if !ok {
			return ErrContextRevoked
		}
		if row.SupportID != uuid.Nil {
			if sup, err = store.SupportByID(ctx, tx, row.SupportID, false); err != nil {
				return err
			}
			return nil
		}
		asg, err = store.AssignmentByID(ctx, tx, row.AssignmentID, false)
		return err
	})
	if err != nil {
		return nil, err
	}
	k := &authz.KNContext{ID: row.ID, AssignmentID: row.AssignmentID, Role: row.Role, Department: row.Department, OrgRef: row.OrgRef,
		UnitRef: row.UnitRef, DefaultRoute: domain.DefaultRoute(row.Role), ExpiresAt: row.ExpiresAt}
	communes := row.Communes
	if row.SupportID != uuid.Nil {
		if sup == nil || sup.EndedAt != nil || !now.Before(sup.ExpiresAt) || sup.AdminRef != p.ActorRef {
			return nil, ErrContextRevoked.WithDetail("Phiên hỗ trợ đã kết thúc")
		}
		if !p.Has(authz.RolePlatformAdmin) || !p.MFA {
			return nil, problem.MFARequired.WithDetail("Chế độ hỗ trợ yêu cầu Admin xác thực nhiều yếu tố")
		}
		k.Support = &authz.KNSupport{SessionID: sup.ID, OnBehalfOf: sup.OnBehalfOf}
	} else {
		if asg == nil || asg.UserRef != p.ActorRef || !asg.EffectiveAt(now) || asg.Role != row.Role {
			return nil, ErrContextRevoked
		}
		if row.Role == port.KNAdmin && !p.MFA {
			return nil, problem.MFARequired
		}
		if row.Role != port.KNAdmin && !slices.Contains(asg.Departments, row.Department) {
			return nil, ErrContextRevoked
		}
		communes, k.OrgRef, k.UnitRef = asg.Communes, asg.OrgRef, asg.UnitRef
	}
	if row.Role != port.KNAdmin {
		d, err := s.Departments.DepartmentByCode(ctx, k.UnitRef, row.Department)
		if errors.Is(err, partyport.ErrDepartmentNotFound) {
			return nil, ErrContextRevoked
		}
		if err != nil {
			return nil, err
		}
		if !d.Selectable() {
			return nil, ErrContextRevoked.WithDetail(d.Name + ": phòng ban không còn Đang áp dụng")
		}
	}
	switch row.Role {
	case port.KNAdmin:
		k.Scope = authz.Scope{All: true}
	case port.KNXa:
		sc, err := s.scopeOf(ctx, p.TenantID, k.OrgRef)
		if err != nil {
			return nil, err
		}
		units := []string{}
		for _, c := range communes {
			if sc.Allows(c) {
				units = append(units, c)
			}
		}
		k.Scope, k.Communes = authz.Scope{Units: units}, units
	default:
		sc, err := s.scopeOf(ctx, p.TenantID, k.OrgRef)
		if err != nil {
			return nil, err
		}
		k.Scope = sc
	}
	return k, nil
}

// ---------------------------------------------------------------------------
// Cổng cho module khác
// ---------------------------------------------------------------------------

// SessionContext thực hiện party/port.SessionContext (menu theo ngữ cảnh — P2).
type SessionContext struct{}

// Current trả đối tượng menu, phòng ban, đơn vị của ngữ cảnh phiên hiện tại.
func (SessionContext) Current(ctx context.Context) (audience, department string, unitRef uuid.UUID, ok bool) {
	p, found := authz.FromContext(ctx)
	if !found || p.KN == nil {
		return "", "", uuid.Nil, false
	}
	return p.KN.Role, p.KN.Department, p.KN.UnitRef, true
}

// DepartmentUsage thực hiện party/port.DepartmentUsage: số tài khoản có vai trò (chưa thu hồi) thuộc
// phòng ban — chặn "xóa" phòng ban còn tài khoản (FN-AD-04.03).
func (s *Service) DepartmentUsage(ctx context.Context, unit uuid.UUID, code string) (partyport.Usage, error) {
	var n int
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		n, e = store.CountUsersByDepartment(ctx, tx, unit, code)
		return e
	})
	return partyport.Usage{Accounts: n}, err
}

// OrgImpact thực hiện party/port.OrgImpactSource ("Ảnh hưởng" khi di chuyển/vô hiệu hóa Nút — FN-AD-04.02).
type OrgImpact struct{ S *Service }

// ImpactKind trả tên dòng.
func (OrgImpact) ImpactKind() string { return partyport.ImpactAccounts }

// OrgImpact đếm tài khoản có vai trò trên Nút hoặc phụ trách xã của nhánh.
func (o OrgImpact) OrgImpact(ctx context.Context, org uuid.UUID, communes []string) (int, error) {
	var n int
	err := tenant.FromCtxTx(ctx, o.S.DB, func(tx pgx.Tx) error {
		var e error
		n, e = store.CountUsersByOrg(ctx, tx, org, communes)
		return e
	})
	return n, err
}

// ActiveAssignments thực hiện port.Assignments.
func (s *Service) ActiveAssignments(ctx context.Context, role, dept string) ([]port.Assignment, error) {
	var out []port.Assignment
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.ActiveAssignmentsAt(ctx, tx, role, dept, s.now())
		return e
	})
	return out, err
}

// DisplayNames thực hiện party/port.ActorNames: họ tên từ Keycloak (nơi giữ định danh cán bộ, ADR-0010),
// dự phòng danh bạ cán bộ (pf/extension). Tên không ghi vào sổ (RB-3) — tra lúc đọc.
func (s *Service) DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	var missing []uuid.UUID
	for _, r := range refs {
		if _, done := out[r]; done || r == uuid.Nil || slices.Contains(missing, r) {
			continue
		}
		if s.Dir != nil {
			if u, err := s.Dir.GetUser(ctx, r); err == nil {
				if n := strings.TrimSpace(u.LastName + " " + u.FirstName); n != "" {
					out[r] = n
					continue
				}
			}
		}
		missing = append(missing, r)
	}
	if len(missing) > 0 && s.Names != nil {
		if names, err := s.Names.DisplayNames(ctx, missing); err == nil {
			for k, v := range names {
				out[k] = v
			}
		}
	}
	return out, nil
}
