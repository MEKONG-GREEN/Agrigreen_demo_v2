package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi tài khoản nông dân (docs/errors.md).
var (
	ErrHouseholdInvalid   = problem.New(http.StatusUnprocessableEntity, "accounts.household_invalid", "Nông hộ không tồn tại, đã ngừng hoặc chưa là thành viên HTX đang hoạt động")
	ErrPhoneMismatch      = problem.New(http.StatusUnprocessableEntity, "accounts.phone_household_mismatch", domain.MsgPhoneHouseholdDiff)
	ErrFarmerExists       = problem.New(http.StatusConflict, "accounts.farmer_exists", "Nông hộ này đã có tài khoản nông dân")
	ErrFarmerNotFound     = problem.New(http.StatusNotFound, "accounts.farmer_not_found", "Không tìm thấy tài khoản nông dân")
	ErrActivationDisabled = problem.New(http.StatusServiceUnavailable, "accounts.activation_link_unavailable", "Chưa phát được liên kết kích hoạt (Keycloak chưa cấu hình phần mở rộng)")
	ErrStaffNotFound      = problem.New(http.StatusNotFound, "accounts.staff_not_found", "Không tìm thấy cán bộ HTX")
	ErrNoContact          = problem.New(http.StatusNotFound, "accounts.contact_unavailable", "Cán bộ chưa có số điện thoại hợp lệ")
)

// DefaultGrantLifespan: hạn lượt kích hoạt khi chưa có tham số activation.grant_lifespan_hours.
const DefaultGrantLifespan = 72 * time.Hour

// farmerManagers là vai được tạo tài khoản nông dân và phát lượt kích hoạt:
// quản lý HTX (chỉ HTX của mình), cán bộ KN xã/tỉnh (trong phạm vi), quản trị nền tảng.
var farmerManagers = []string{partyport.RoleCoopManager, partyport.RoleKNOfficer, partyport.RoleKNProvinceAdmin, authz.RolePlatformAdmin}

// farmerActions: nông dân tự đặt mật khẩu qua liên kết một lần (không gửi mật khẩu, không SMS).
var farmerActions = []string{port.ActionUpdatePassword}

func onlyCoopManager(p authz.Principal) bool {
	return p.Has(partyport.RoleCoopManager) && !p.Has(partyport.RoleKNOfficer) && !p.Has(partyport.RoleKNProvinceAdmin) &&
		!p.Has(authz.RolePlatformAdmin)
}

// farmerActor là người gọi thao tác tài khoản nông dân đã kiểm vai.
type farmerActor struct {
	p  authz.Principal
	sc authz.Scope
}

func (s *Service) farmerCaller(ctx context.Context) (*farmerActor, error) {
	p, err := authz.Require(ctx, farmerManagers...)
	if err != nil {
		return nil, err
	}
	if s.Dir == nil {
		return nil, ErrDependency.WithCause(errNoDirectoryYet)
	}
	if s.Households == nil || s.Orgs == nil {
		return nil, ErrDependency.WithDetail("chưa nối pf/party")
	}
	a := &farmerActor{p: p}
	switch {
	case p.Has(authz.RolePlatformAdmin):
		a.sc = authz.Scope{All: true}
	case s.Scope == nil:
		return nil, ErrDependency.WithDetail("chưa nối pf/party")
	default:
		if a.sc, err = s.Scope.Scope(ctx); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// allows: quản lý HTX chỉ HTX của mình (HTX US-ACC-01); cán bộ KN theo phạm vi xã/tỉnh (ADR-0009).
func (a *farmerActor) allows(coop uuid.UUID, commune string) bool {
	if onlyCoopManager(a.p) {
		return a.p.OrgRef != uuid.Nil && coop == a.p.OrgRef
	}
	return a.sc.All || a.sc.Allows(commune)
}

// checkHousehold kiểm nông hộ là thành viên HTX đang hoạt động và trong phạm vi.
func (s *Service) checkHousehold(ctx context.Context, a *farmerActor, ref uuid.UUID) (partyport.HouseholdMembership, error) {
	m, err := s.Households.Membership(ctx, ref)
	if errors.Is(err, partyport.ErrHouseholdNotFound) {
		return m, ErrHouseholdInvalid
	}
	if err != nil {
		return m, err
	}
	if !m.Active || m.CooperativeRef == uuid.Nil {
		return m, ErrHouseholdInvalid
	}
	if !a.allows(m.CooperativeRef, m.CommuneCode) {
		return m, ErrOutOfScope.WithDetail("Nông hộ không thuộc HTX hoặc địa bàn bạn phụ trách.")
	}
	o, err := s.Orgs.Get(ctx, m.CooperativeRef)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return m, ErrHouseholdInvalid
	}
	if err != nil {
		return m, err
	}
	if !o.Active {
		return m, ErrOrgInactive.WithDetail("HTX của nông hộ đang bị vô hiệu hóa.")
	}
	return m, nil
}

// resolveHousehold: SĐT phải là SĐT chủ hộ trong kho định danh (chỉ mục mù). Không
// truyền household_ref → nông hộ tra theo SĐT; có → phải khớp.
func (s *Service) resolveHousehold(ctx context.Context, phone string, want uuid.UUID) (uuid.UUID, error) {
	if s.Vault == nil {
		return uuid.Nil, ErrDependency.WithDetail("chưa nối kho định danh")
	}
	ref, err := s.Vault.Lookup(ctx, client.PurposeRegistryAdmin, phone)
	switch {
	case errors.Is(err, client.ErrNotFound), errors.Is(err, client.ErrErased):
		if want != uuid.Nil {
			return uuid.Nil, ErrPhoneMismatch
		}
		return uuid.Nil, ErrHouseholdInvalid.WithDetail(domain.MsgHouseholdMissing)
	case err != nil:
		var p *problem.Problem
		if errors.As(err, &p) && p.Status >= 400 && p.Status < 500 {
			return uuid.Nil, p
		}
		return uuid.Nil, ErrDependency.WithDetail("kho định danh không phản hồi").WithCause(err)
	}
	if want != uuid.Nil && ref != want {
		return uuid.Nil, ErrPhoneMismatch
	}
	return ref, nil
}

// FarmerInput là dữ liệu tạo tài khoản nông dân.
type FarmerInput struct {
	HouseholdRef uuid.UUID // Nil → tra theo SĐT
	Profile      domain.FarmerProfile
	IdemKey      string
}

// Activation là một lượt kích hoạt vừa phát. Link chứa action token: CHỈ trả một
// lần trong phản hồi — không lưu, không ghi log (R-14).
type Activation struct {
	Grant    domain.Grant
	Link     string `pii:"true"`
	Replayed bool   // gọi lặp: không phát lại liên kết
}

// FarmerResult là kết quả tạo một tài khoản nông dân.
type FarmerResult struct {
	Link     domain.FarmerLink
	Replayed bool
	// Activation nil → chưa phát được liên kết (ActivationErr cho biết vì sao); phát lại sau.
	Activation    *Activation
	ActivationErr error
}

// CreateFarmer tạo tài khoản nông dân cho một nông hộ của HTX (HTX US-ACC-01,
// US-LOGIN-01): tên đăng nhập = SĐT chuẩn hóa, vai farmer, required action
// UPDATE_PASSWORD, KHÔNG org_ref; liên kết tài khoản ↔ nông hộ; rồi phát lượt kích
// hoạt đầu tiên (liên kết + QR). Không gửi mật khẩu, không SMS (g2-conventions §1).
func (s *Service) CreateFarmer(ctx context.Context, in FarmerInput) (FarmerResult, error) {
	a, err := s.farmerCaller(ctx)
	if err != nil {
		return FarmerResult{}, err
	}
	return s.createFarmer(ctx, a, in)
}

func (s *Service) createFarmer(ctx context.Context, a *farmerActor, in FarmerInput) (FarmerResult, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return FarmerResult{}, err
	}
	prof, err := in.Profile.Normalize()
	if err != nil {
		return FarmerResult{}, fieldProblem(err)
	}
	if prev, err := s.linkByKey(ctx, a.p.TenantID, in.IdemKey); err != nil {
		return FarmerResult{}, err
	} else if prev != nil {
		return s.replayedFarmer(ctx, a, *prev, in.IdemKey)
	}
	ref, err := s.resolveHousehold(ctx, prof.Phone, in.HouseholdRef)
	if err != nil {
		return FarmerResult{}, err
	}
	m, err := s.checkHousehold(ctx, a, ref)
	if err != nil {
		return FarmerResult{}, err
	}
	var existing *domain.FarmerLink
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		existing, e = store.LinkByHousehold(ctx, tx, ref)
		return e
	}); err != nil {
		return FarmerResult{}, err
	}
	if existing != nil {
		return FarmerResult{}, ErrFarmerExists
	}
	// Tên đăng nhập duy nhất trong realm; SĐT duy nhất trong tenant (HTX US-ACC-01 AC-7).
	if found, err := s.Dir.FindUsers(ctx, port.Query{Username: prof.Phone, Max: 1}); err != nil {
		return FarmerResult{}, dirErr(err)
	} else if len(found) > 0 {
		return FarmerResult{}, ErrPhoneTaken
	}
	if found, err := s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{
		port.AttrTenantID: a.p.TenantID.String(), port.AttrPhone: prof.Phone}, Max: 1}); err != nil {
		return FarmerResult{}, dirErr(err)
	} else if len(found) > 0 {
		return FarmerResult{}, ErrPhoneTaken
	}
	id, err := s.Dir.CreateUser(ctx, port.NewUser{
		Username: prof.Phone, Phone: prof.Phone, FirstName: prof.FirstName, LastName: prof.LastName,
		TenantID: a.p.TenantID, RequiredActions: farmerActions,
	})
	if err != nil {
		return FarmerResult{}, dirErr(err)
	}
	compensate := func(cause error) (FarmerResult, error) {
		if derr := s.Dir.DeleteUser(context.WithoutCancel(ctx), id); derr != nil {
			return FarmerResult{}, errors.Join(cause, derr)
		}
		return FarmerResult{}, cause
	}
	if err := s.Dir.AddRealmRoles(ctx, id, []string{port.RoleFarmer}); err != nil {
		return compensate(dirErr(err))
	}
	now := s.now()
	acc := domain.Account{UserRef: id, OrgRef: m.CooperativeRef, Roles: []string{port.RoleFarmer}, Enabled: true}
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionCreated, now, in.IdemKey, "")
	ev.Details = map[string]any{"required_actions": farmerActions, "kind": "farmer", "household_ref": ref.String()}
	link := domain.FarmerLink{ID: ids.New(), UserRef: id, HouseholdRef: ref, CooperativeRef: m.CooperativeRef, CommuneCode: m.CommuneCode,
		LinkedBy: a.p.ActorRef, LinkedAt: now, IdemKey: in.IdemKey}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if err := record(ctx, tx, &ev); err != nil {
			return err
		}
		ok, err := store.InsertLink(ctx, tx, a.p.TenantID, link)
		if err != nil {
			return err
		}
		if !ok {
			return ErrFarmerExists
		}
		return nil
	})
	if err != nil {
		if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
			// Request song song cùng khóa đã ghi trước: bỏ người dùng thừa vừa tạo.
			_, _ = compensate(nil)
			if prev, rerr := s.linkByKey(ctx, a.p.TenantID, in.IdemKey); rerr == nil && prev != nil {
				return s.replayedFarmer(ctx, a, *prev, in.IdemKey)
			}
		}
		return compensate(ledger.Classify(err))
	}
	res := FarmerResult{Link: link}
	res.Activation, res.ActivationErr = s.issue(ctx, a.p, id, in.IdemKey+":grant")
	return res, nil
}

func (s *Service) linkByKey(ctx context.Context, tid uuid.UUID, key string) (*domain.FarmerLink, error) {
	var l *domain.FarmerLink
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		l, e = store.LinkByIdemKey(ctx, tx, key)
		return e
	})
	return l, err
}

// replayedFarmer: gọi lặp — trả liên kết đã ghi và siêu dữ liệu lượt kích hoạt, KHÔNG
// phát lại liên kết (token chỉ trao một lần).
func (s *Service) replayedFarmer(ctx context.Context, a *farmerActor, l domain.FarmerLink, key string) (FarmerResult, error) {
	res := FarmerResult{Link: l, Replayed: true}
	var g *domain.Grant
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		g, e = store.GrantByIdemKey(ctx, tx, key+":grant")
		return e
	}); err != nil {
		return FarmerResult{}, err
	}
	if g != nil {
		res.Activation = &Activation{Grant: *g, Replayed: true}
	}
	return res, nil
}

// grantLifespan đọc activation.grant_lifespan_hours tại now (1–720 giờ); chưa có → 72 giờ.
func (s *Service) grantLifespan(ctx context.Context, now time.Time) time.Duration {
	if s.Params == nil {
		return DefaultGrantLifespan
	}
	v, err := s.Params.At(ctx, mdport.ParamActivationGrantLifespan, now)
	if err != nil || v.Value.LessThan(decimal.NewFromInt(1)) || v.Value.GreaterThan(decimal.NewFromInt(720)) {
		return DefaultGrantLifespan
	}
	return time.Duration(v.Value.Mul(decimal.NewFromInt(3600)).IntPart()) * time.Second
}

func (s *Service) actionLinks() (port.ActionLinks, bool) {
	l, ok := s.Dir.(port.ActionLinks)
	return l, ok
}

// issue phát một lượt kích hoạt: gọi Keycloak lấy liên kết một lần (gắn với mã lượt),
// rồi trong MỘT giao dịch thu hồi lượt đang mở (reissued) và ghi lượt mới + dòng sổ
// actions_sent. Chỉ lưu siêu dữ liệu; liên kết chỉ nằm trong giá trị trả về.
func (s *Service) issue(ctx context.Context, p authz.Principal, user uuid.UUID, key string) (*Activation, error) {
	var prev *domain.Grant
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.GrantByIdemKey(ctx, tx, key)
		return e
	}); err != nil {
		return nil, err
	}
	if prev != nil {
		return &Activation{Grant: *prev, Replayed: true}, nil
	}
	links, ok := s.actionLinks()
	if !ok {
		return nil, ErrActivationDisabled.WithCause(port.ErrActionLinkUnsupported)
	}
	now := s.now()
	life := s.grantLifespan(ctx, now)
	g := domain.Grant{ID: ids.New(), UserRef: user, Actions: farmerActions, Channel: domain.GrantChannelLinkQR,
		IssuedBy: p.ActorRef, IssuedAt: now, ExpiresAt: now.Add(life), IdemKey: key}
	link, err := links.ActionLink(ctx, user, g.ID, farmerActions, life)
	if errors.Is(err, port.ErrActionLinkUnsupported) {
		return nil, ErrActivationDisabled.WithCause(err)
	}
	if err != nil {
		return nil, dirErr(err)
	}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		acc, err := store.Account(ctx, tx, user, true)
		if err != nil {
			return err
		}
		if acc == nil {
			return ErrFarmerNotFound
		}
		open, err := store.OpenGrant(ctx, tx, user)
		if err != nil {
			return err
		}
		if open != nil {
			// Phát lại thu hồi lượt trước (kể cả đã hết hạn chưa dùng).
			if err := store.RevokeGrant(ctx, tx, open.ID, p.ActorRef, domain.GrantRevokedReissued, &g.ID, now); err != nil {
				return err
			}
		}
		if err := store.InsertGrant(ctx, tx, p.TenantID, g); err != nil {
			return err
		}
		ev := newEvent(p.TenantID, p.ActorRef, *acc, domain.ActionActionsSent, now, key, "")
		ev.Details = map[string]any{"actions": farmerActions, "channel": domain.GrantChannelLinkQR, "grant_id": g.ID.String(),
			"lifespan_hours": int(life.Hours())}
		return record(ctx, tx, &ev)
	})
	if err != nil {
		return nil, ledger.Classify(err)
	}
	return &Activation{Grant: g, Link: link}, nil
}

// farmerTarget kiểm tài khoản nông dân đích: cùng tenant, có liên kết nông hộ trong phạm vi người gọi.
func (s *Service) farmerTarget(ctx context.Context, a *farmerActor, user uuid.UUID) (port.User, []domain.FarmerLink, error) {
	var links []domain.FarmerLink
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		links, e = store.LinksByUser(ctx, tx, user)
		return e
	}); err != nil {
		return port.User{}, nil, err
	}
	if len(links) == 0 || !slices.ContainsFunc(links, func(l domain.FarmerLink) bool { return a.allows(l.CooperativeRef, l.CommuneCode) }) {
		return port.User{}, nil, ErrFarmerNotFound // không lộ sự tồn tại ngoài phạm vi
	}
	u, err := s.Dir.GetUser(ctx, user)
	if errors.Is(err, port.ErrUserNotFound) {
		return port.User{}, nil, ErrFarmerNotFound
	}
	if err != nil {
		return port.User{}, nil, dirErr(err)
	}
	if u.TenantID != a.p.TenantID {
		return port.User{}, nil, ErrFarmerNotFound
	}
	return u, links, nil
}

// ReissueActivation phát lượt kích hoạt mới (nông dân mất QR, hết hạn, quên mật
// khẩu); lượt đang mở bị thu hồi (reissued). Tài khoản đang khóa → không phát.
func (s *Service) ReissueActivation(ctx context.Context, user uuid.UUID, key string) (*Activation, error) {
	a, err := s.farmerCaller(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireKey(key); err != nil {
		return nil, err
	}
	u, _, err := s.farmerTarget(ctx, a, user)
	if err != nil {
		return nil, err
	}
	if !u.Enabled {
		return nil, ErrState.WithDetail("Tài khoản đang bị khóa; mở khóa trước khi phát liên kết kích hoạt.")
	}
	if !slices.Contains(u.RequiredActions, port.ActionUpdatePassword) {
		// Đã kích hoạt: liên kết mới dùng để đặt lại mật khẩu (HTX US-ACC-01 AC-3, không cấp mật khẩu tạm).
		if err := s.Dir.AddRequiredActions(ctx, user, farmerActions); err != nil {
			return nil, dirErr(err)
		}
	}
	return s.issue(ctx, a.p, user, key)
}

// GrantView là lượt kích hoạt kèm trạng thái tại lúc xem.
type GrantView struct {
	Grant  domain.Grant
	Status string
}

// Activations liệt kê lượt kích hoạt của một tài khoản nông dân (chỉ siêu dữ liệu).
// Lượt đang mở mà Keycloak không còn required action UPDATE_PASSWORD → ghi đã dùng.
func (s *Service) Activations(ctx context.Context, user uuid.UUID) ([]GrantView, error) {
	a, err := s.farmerCaller(ctx)
	if err != nil {
		return nil, err
	}
	u, _, err := s.farmerTarget(ctx, a, user)
	if err != nil {
		return nil, err
	}
	now := s.now()
	var out []GrantView
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		if !slices.Contains(u.RequiredActions, port.ActionUpdatePassword) {
			open, err := store.OpenGrant(ctx, tx, user)
			if err != nil {
				return err
			}
			if open != nil && open.Status(now) == domain.GrantActive {
				if err := store.MarkGrantUsed(ctx, tx, open.ID, now); err != nil {
					return err
				}
			}
		}
		gs, err := store.Grants(ctx, tx, user)
		if err != nil {
			return err
		}
		out = make([]GrantView, 0, len(gs))
		for _, g := range gs {
			out = append(out, GrantView{Grant: g, Status: g.Status(now)})
		}
		return nil
	})
	return out, err
}

// FarmerView là một tài khoản nông dân trong danh sách (không có dữ liệu cá nhân).
type FarmerView struct {
	Link    domain.FarmerLink
	Enabled bool
	Latest  *GrantView
}

// FarmerPage là một trang danh sách.
type FarmerPage struct {
	Items []FarmerView
	Next  *uuid.UUID
}

// ListFarmers liệt kê tài khoản nông dân của một HTX (quản lý HTX: HTX của mình).
func (s *Service) ListFarmers(ctx context.Context, coop, after uuid.UUID, limit int) (FarmerPage, error) {
	a, err := s.farmerCaller(ctx)
	if err != nil {
		return FarmerPage{}, err
	}
	if coop == uuid.Nil {
		coop = a.p.OrgRef
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	o, err := s.Orgs.Get(ctx, coop)
	if errors.Is(err, partyport.ErrOrgNotFound) || (err == nil && o.Kind != partyport.OrgCooperative) {
		return FarmerPage{}, problem.Validation.WithField("cooperative_ref", "không phải HTX")
	}
	if err != nil {
		return FarmerPage{}, err
	}
	if !a.allows(o.Ref, o.AdminUnitCode) {
		return FarmerPage{}, ErrOutOfScope
	}
	now := s.now()
	pg := FarmerPage{Items: []FarmerView{}}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		links, err := store.LinksByCooperative(ctx, tx, coop, after, limit+1)
		if err != nil {
			return err
		}
		if len(links) > limit {
			links = links[:limit]
			last := links[limit-1].ID
			pg.Next = &last
		}
		refs := make([]uuid.UUID, 0, len(links))
		for _, l := range links {
			refs = append(refs, l.UserRef)
		}
		accs, err := store.Accounts(ctx, tx, refs)
		if err != nil {
			return err
		}
		for _, l := range links {
			v := FarmerView{Link: l, Enabled: accs[l.UserRef].Enabled}
			g, err := store.LatestGrant(ctx, tx, l.UserRef)
			if err != nil {
				return err
			}
			if g != nil {
				v.Latest = &GrantView{Grant: *g, Status: g.Status(now)}
			}
			pg.Items = append(pg.Items, v)
		}
		return nil
	})
	return pg, err
}

// HouseholdsForSubject thực hiện port.FarmerHouseholds. Tenant lấy từ context.
func (s *Service) HouseholdsForSubject(ctx context.Context, subject uuid.UUID) ([]port.HouseholdLink, error) {
	var links []domain.FarmerLink
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		links, e = store.LinksByUser(ctx, tx, subject)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]port.HouseholdLink, 0, len(links))
	for _, l := range links {
		out = append(out, port.HouseholdLink{HouseholdRef: l.HouseholdRef, CooperativeRef: l.CooperativeRef, CommuneCode: l.CommuneCode})
	}
	return out, nil
}

var _ port.FarmerHouseholds = (*Service)(nil)

// MyHouseholds: nông dân xem nông hộ liên kết với tài khoản của mình (US-LOGIN-01: phạm vi theo vai).
func (s *Service) MyHouseholds(ctx context.Context) ([]port.HouseholdLink, error) {
	p, err := authz.Require(ctx, port.RoleFarmer)
	if err != nil {
		return nil, err
	}
	return s.HouseholdsForSubject(ctx, p.ActorRef)
}
