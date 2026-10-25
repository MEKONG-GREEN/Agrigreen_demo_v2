package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// CreateInput là dữ liệu tạo tài khoản.
type CreateInput struct {
	Profile domain.Profile
	OrgRef  uuid.UUID
	Roles   []string
	IdemKey string
}

// Create tạo tài khoản cán bộ (KN US-ADM-01, HTX US-ACC-01, CGH FN-01):
// thuộc tính tenant_id (từ token) và org_ref (trong phạm vi), vai theo ma trận
// cấp vai, required actions VERIFY_EMAIL + UPDATE_PASSWORD (+ CONFIGURE_TOTP với
// vai [MFA]) và gửi liên kết kích hoạt một lần — không có mật khẩu tạm (ADR-0010).
func (s *Service) Create(ctx context.Context, in CreateInput) (Result, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Result{}, err
	}
	return s.create(ctx, a, in)
}

// prepared là dữ liệu tạo đã kiểm.
type prepared struct {
	prof  domain.Profile
	roles []string
	org   uuid.UUID
}

func (s *Service) prepare(ctx context.Context, a *actor, in CreateInput) (prepared, error) {
	prof, err := in.Profile.Normalize()
	if err != nil {
		return prepared{}, fieldProblem(err)
	}
	roles, err := domain.NormalizeRolesWithCatalog(in.Roles, s.RoleCatalog())
	if err != nil {
		return prepared{}, fieldProblem(err)
	}
	if err := a.checkGrant(roles); err != nil {
		return prepared{}, err
	}
	if err := s.checkModules(ctx, a, roles); err != nil {
		return prepared{}, err
	}
	o, err := s.checkOrg(ctx, a, in.OrgRef)
	if err != nil {
		return prepared{}, err
	}
	if err := domain.RolesFitOrgWithCatalog(roles, string(o.Kind), s.RoleCatalog()); err != nil {
		return prepared{}, fieldProblem(err)
	}
	return prepared{prof: prof, roles: roles, org: o.Ref}, nil
}

// checkUnique: tên đăng nhập/email duy nhất trong realm (Keycloak), SĐT duy nhất trong tenant.
func (s *Service) checkUnique(ctx context.Context, a *actor, prof domain.Profile) error {
	for _, q := range []port.Query{{Username: prof.Username, Max: 1}, {Email: prof.Email, Max: 1}} {
		found, err := s.Dir.FindUsers(ctx, q)
		if err != nil {
			return dirErr(err)
		}
		if len(found) > 0 {
			return ErrUserExists
		}
	}
	if prof.Phone != "" {
		found, err := s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{
			port.AttrTenantID: a.p.TenantID.String(), port.AttrPhone: prof.Phone,
		}, Max: 1})
		if err != nil {
			return dirErr(err)
		}
		if len(found) > 0 {
			return ErrPhoneTaken
		}
	}
	return nil
}

func (s *Service) create(ctx context.Context, a *actor, in CreateInput) (Result, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return Result{}, err
	}
	pr, err := s.prepare(ctx, a, in)
	if err != nil {
		return Result{}, err
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil {
		return Result{}, err
	} else if prev != nil {
		return s.replayed(ctx, *prev)
	}
	if err := s.checkUnique(ctx, a, pr.prof); err != nil {
		return Result{}, err
	}
	acts := domain.RequiredActionsWithCatalog(pr.roles, s.RoleCatalog())
	id, err := s.Dir.CreateUser(ctx, port.NewUser{
		Username: pr.prof.Username, Email: pr.prof.Email, FirstName: pr.prof.FirstName, LastName: pr.prof.LastName,
		Phone: pr.prof.Phone, TenantID: a.p.TenantID, OrgRef: pr.org, RequiredActions: acts,
	})
	if err != nil {
		return Result{}, dirErr(err)
	}
	// Bù: người dùng vừa tạo chưa ai dùng, chưa có liên kết kích hoạt.
	compensate := func(cause error) (Result, error) {
		if derr := s.Dir.DeleteUser(context.WithoutCancel(ctx), id); derr != nil {
			return Result{}, errors.Join(cause, derr)
		}
		return Result{}, cause
	}
	if err := s.Dir.AddRealmRoles(ctx, id, pr.roles); err != nil {
		return compensate(dirErr(err))
	}
	acc := domain.Account{UserRef: id, OrgRef: pr.org, Roles: pr.roles, Enabled: true}
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionCreated, s.now(), in.IdemKey, "")
	ev.Details = map[string]any{"required_actions": acts}
	replayed, err := s.write(ctx, a.p.TenantID, &ev)
	if err != nil {
		return compensate(err)
	}
	if replayed {
		// Request song song cùng khóa đã ghi trước: bỏ người dùng thừa vừa tạo.
		_, _ = compensate(nil)
		return s.replayed(ctx, ev)
	}
	res := Result{Account: acc, Event: ev}
	res.User, _ = s.Dir.GetUser(ctx, id)
	res.ActivationSent = s.sendActions(ctx, a.p.TenantID, a.p.ActorRef, acc, acts, in.IdemKey+":actions") == nil
	return res, nil
}

// replayed dựng lại kết quả của lần tạo trước.
func (s *Service) replayed(ctx context.Context, ev domain.Event) (Result, error) {
	res := Result{Account: ev.Account, Event: ev, Replayed: true}
	u, err := s.Dir.GetUser(ctx, ev.Account.UserRef)
	if err != nil {
		return Result{}, dirErr(err)
	}
	res.User = u
	sent, err := s.replay(ctx, ev.TenantID, ev.IdemKey+":actions")
	res.ActivationSent = err == nil && sent != nil
	return res, nil
}

// sendActions gửi liên kết một lần và ghi sổ actions_sent.
func (s *Service) sendActions(ctx context.Context, tid, actorRef uuid.UUID, acc domain.Account, acts []string, key string) error {
	if err := s.Dir.ExecuteActionsEmail(ctx, acc.UserRef, acts, s.lifespan()); err != nil {
		return dirErr(err)
	}
	ev := newEvent(tid, actorRef, acc, domain.ActionActionsSent, s.now(), key, "")
	ev.Details = map[string]any{"actions": acts, "lifespan_hours": int(s.lifespan().Hours())}
	_, err := s.write(ctx, tid, &ev)
	return err
}

// LockInput là dữ liệu khóa/mở khóa.
type LockInput struct {
	UserID  uuid.UUID
	Reason  string
	IdemKey string
}

// Lock khóa tài khoản (enabled=false) và thu hồi mọi phiên (KN US-ADM-01 AC-3, HTX US-ACC-01 AC-4/5).
func (s *Service) Lock(ctx context.Context, in LockInput) (Result, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Result{}, err
	}
	return s.lock(ctx, a, in)
}

func (s *Service) lock(ctx context.Context, a *actor, in LockInput) (Result, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return Result{}, err
	}
	if err := domain.CheckReason(in.Reason); err != nil {
		return Result{}, fieldProblem(err)
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		return Result{Account: derefAcc(prev), Event: derefEv(prev), Replayed: prev != nil}, err
	}
	if in.UserID == a.p.ActorRef {
		return Result{}, ErrSelfAction
	}
	t, err := s.target(ctx, a, in.UserID)
	if err != nil {
		return Result{}, err
	}
	// Đã khóa theo pf → xung đột. Keycloak đã tắt mà pf chưa ghi (lần chạy trước
	// dừng giữa chừng, hoặc tắt trong console) → chỉ ghi sổ.
	if t.acc != nil && !t.acc.Enabled {
		return Result{}, ErrState.WithDetail("Tài khoản đã bị khóa.")
	}
	if t.user.Enabled {
		if err := s.Dir.SetEnabled(ctx, t.user.ID, false); err != nil {
			return Result{}, dirErr(err)
		}
	}
	revoked := s.Dir.Logout(ctx, t.user.ID) == nil
	acc := t.state()
	acc.Enabled, acc.LockCause = false, port.LockManual
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionLocked, s.now(), in.IdemKey, in.Reason)
	ev.Details = map[string]any{"sessions_revoked": revoked}
	replayed, err := s.write(ctx, a.p.TenantID, &ev)
	return Result{Account: ev.Account, Event: ev, User: t.user, Replayed: replayed}, err
}

func derefAcc(e *domain.Event) domain.Account {
	if e == nil {
		return domain.Account{}
	}
	return e.Account
}

func derefEv(e *domain.Event) domain.Event {
	if e == nil {
		return domain.Event{}
	}
	return *e
}

// LockCauseBruteForce là nguyên nhân khóa ghi vào chi tiết sự kiện mở khóa khi tài khoản bị
// Keycloak khóa do đăng nhập sai nhiều lần (không phải trạng thái của bảng account).
const LockCauseBruteForce = "brute_force"

// Unlock mở khóa tài khoản (HTX US-ACC-01 AC-6: đăng nhập lại ngay, không cần reset).
// Tổ chức đang bị vô hiệu hóa → không mở được (kích hoạt lại tổ chức sẽ mở).
func (s *Service) Unlock(ctx context.Context, in LockInput) (Result, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Result{}, err
	}
	return s.unlock(ctx, a, in)
}

func (s *Service) unlock(ctx context.Context, a *actor, in LockInput) (Result, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return Result{}, err
	}
	if err := domain.CheckReason(in.Reason); err != nil {
		return Result{}, fieldProblem(err)
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		return Result{Account: derefAcc(prev), Event: derefEv(prev), Replayed: prev != nil}, err
	}
	if in.UserID == a.p.ActorRef {
		return Result{}, ErrSelfAction
	}
	t, err := s.target(ctx, a, in.UserID)
	if err != nil {
		return Result{}, err
	}
	cur := t.state()
	// GIS US-LOG-01 AC-3: khóa do đăng nhập sai 5 lần là khóa tạm 15 phút rồi tự mở (P0-VPS-OPS #131);
	// Admin vẫn mở sớm được — Keycloak giữ enabled = true nên phải hỏi trạng thái chống dò riêng.
	bruteForce, err := s.Dir.BruteForceLocked(ctx, t.user.ID)
	if err != nil {
		return Result{}, dirErr(err)
	}
	if cur.Enabled && t.user.Enabled && !bruteForce {
		return Result{}, ErrState.WithDetail("Tài khoản đang hoạt động.")
	}
	o, err := s.org(ctx, a, t.user.OrgRef)
	if err != nil {
		return Result{}, err
	}
	if o != nil && !o.Active {
		return Result{}, ErrOrgInactive.WithDetail("Tổ chức của tài khoản đang bị vô hiệu hóa; kích hoạt lại tổ chức để mở khóa.")
	}
	if bruteForce {
		if err := s.Dir.ClearBruteForce(ctx, t.user.ID); err != nil {
			return Result{}, dirErr(err)
		}
	}
	if !t.user.Enabled {
		if err := s.Dir.SetEnabled(ctx, t.user.ID, true); err != nil {
			return Result{}, dirErr(err)
		}
	}
	prevCause := string(cur.LockCause)
	if prevCause == "" && bruteForce {
		prevCause = LockCauseBruteForce
	}
	acc := cur
	acc.Enabled, acc.LockCause = true, ""
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionUnlocked, s.now(), in.IdemKey, in.Reason)
	ev.Details = map[string]any{"prev_cause": prevCause}
	replayed, err := s.write(ctx, a.p.TenantID, &ev)
	return Result{Account: ev.Account, Event: ev, User: t.user, Replayed: replayed}, err
}

// ResendInput là dữ liệu gửi lại liên kết.
type ResendInput struct {
	UserID  uuid.UUID
	Actions []string // rỗng → required actions đang chờ; không còn → UPDATE_PASSWORD (đặt lại mật khẩu)
	IdemKey string
}

// Resend gửi lại liên kết một lần (KN US-ADM-03, HTX US-ACC-01 AC-3 "reset mật khẩu"
// bằng UPDATE_PASSWORD — không cấp mật khẩu tạm, ADR-0010).
func (s *Service) Resend(ctx context.Context, in ResendInput) (Result, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Result{}, err
	}
	return s.resend(ctx, a, in)
}

func (s *Service) resend(ctx context.Context, a *actor, in ResendInput) (Result, error) {
	if err := requireKey(in.IdemKey); err != nil {
		return Result{}, err
	}
	acts, err := domain.CheckActions(in.Actions)
	if err != nil {
		return Result{}, fieldProblem(err)
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		return Result{Account: derefAcc(prev), Event: derefEv(prev), Replayed: prev != nil, ActivationSent: prev != nil}, err
	}
	t, err := s.target(ctx, a, in.UserID)
	if err != nil {
		return Result{}, err
	}
	if !t.user.Enabled {
		return Result{}, ErrState.WithDetail("Tài khoản đang bị khóa; mở khóa trước khi gửi liên kết.")
	}
	if len(acts) == 0 {
		for _, ra := range t.user.RequiredActions {
			if slices.Contains(port.AllowedActions, ra) {
				acts = append(acts, ra)
			}
		}
	}
	if len(acts) == 0 {
		acts = []string{port.ActionUpdatePassword}
	}
	acc := t.state()
	if err := s.sendActions(ctx, a.p.TenantID, a.p.ActorRef, acc, acts, in.IdemKey); err != nil {
		return Result{}, err
	}
	return Result{Account: acc, User: t.user, ActivationSent: true}, nil
}

// AssignInput đổi đơn vị công tác và/hoặc vai.
type AssignInput struct {
	UserID  uuid.UUID
	OrgRef  uuid.UUID // Nil = giữ nguyên
	Roles   []string  // nil = giữ nguyên; là TẬP ĐẦY ĐỦ vai do module quản lý sau thay đổi
	IdemKey string
}

// Assign đổi đơn vị và vai (HTX US-ACC-01 AC-2: áp dụng ngay, không bắt đăng nhập lại).
func (s *Service) Assign(ctx context.Context, in AssignInput) (Result, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := requireKey(in.IdemKey); err != nil {
		return Result{}, err
	}
	if in.OrgRef == uuid.Nil && in.Roles == nil {
		return Result{}, problem.Validation.WithField("org_ref", "cần org_ref hoặc roles")
	}
	if prev, err := s.replay(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		return Result{Account: derefAcc(prev), Event: derefEv(prev), Replayed: prev != nil}, err
	}
	if in.UserID == a.p.ActorRef {
		return Result{}, ErrSelfAction
	}
	t, err := s.target(ctx, a, in.UserID)
	if err != nil {
		return Result{}, err
	}
	newRoles := t.roles
	if in.Roles != nil {
		if newRoles, err = domain.NormalizeRolesWithCatalog(in.Roles, s.RoleCatalog()); err != nil {
			return Result{}, fieldProblem(err)
		}
		if err := a.checkGrant(newRoles); err != nil {
			return Result{}, err
		}
		// W1.4: chỉ vai THÊM MỚI bị chặn khi module đã tắt — gỡ hoặc giữ vai cũ vẫn được.
		var added []string
		for _, r := range newRoles {
			if !slices.Contains(t.roles, r) {
				added = append(added, r)
			}
		}
		if err := s.checkModules(ctx, a, added); err != nil {
			return Result{}, err
		}
	}
	orgRef := t.user.OrgRef
	if in.OrgRef != uuid.Nil {
		orgRef = in.OrgRef
	}
	o, err := s.checkOrg(ctx, a, orgRef)
	if err != nil {
		return Result{}, err
	}
	if err := domain.RolesFitOrgWithCatalog(newRoles, string(o.Kind), s.RoleCatalog()); err != nil {
		return Result{}, fieldProblem(err)
	}
	if orgRef != t.user.OrgRef {
		if err := s.Dir.SetOrgRef(ctx, t.user.ID, orgRef); err != nil {
			return Result{}, dirErr(err)
		}
	}
	var add, remove []string
	for _, r := range newRoles {
		if !slices.Contains(t.roles, r) {
			add = append(add, r)
		}
	}
	for _, r := range t.roles {
		if !slices.Contains(newRoles, r) {
			remove = append(remove, r)
		}
	}
	if err := s.Dir.AddRealmRoles(ctx, t.user.ID, add); err != nil {
		return Result{}, dirErr(err)
	}
	if err := s.Dir.RemoveRealmRoles(ctx, t.user.ID, remove); err != nil {
		return Result{}, dirErr(err)
	}
	totp := false
	// D24: cấp thêm vai bắt TOTP lúc đăng nhập (vai [MFA] hoặc cán bộ KN) mà chưa có TOTP → đăng ký ở lần sau.
	if s.RoleCatalog().NeedsLoginTOTP(newRoles) && !s.RoleCatalog().NeedsLoginTOTP(t.roles) {
		has, err := s.Dir.HasOTP(ctx, t.user.ID)
		if err != nil {
			return Result{}, dirErr(err)
		}
		if !has && !slices.Contains(t.user.RequiredActions, port.ActionConfigureTOTP) {
			if err := s.Dir.AddRequiredActions(ctx, t.user.ID, []string{port.ActionConfigureTOTP}); err != nil {
				return Result{}, dirErr(err)
			}
			totp = true
		}
	}
	prev := t.state()
	acc := prev
	acc.OrgRef, acc.Roles = orgRef, newRoles
	ev := newEvent(a.p.TenantID, a.p.ActorRef, acc, domain.ActionAssignmentChanged, s.now(), in.IdemKey, "")
	ev.Details = map[string]any{"prev_org_ref": prev.OrgRef.String(), "prev_roles": prev.Roles, "added": nonNil(add), "removed": nonNil(remove), "totp_required": totp}
	replayed, err := s.write(ctx, a.p.TenantID, &ev)
	return Result{Account: ev.Account, Event: ev, User: t.user, Replayed: replayed}, err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// UserView là tài khoản hiển thị cho quản trị viên.
type UserView struct {
	User port.User
	// Roles: vai do module quản lý (đọc thẳng Keycloak ở Get; ảnh chụp pf ở List, nil nếu tạo ngoài nền tảng).
	Roles     []string
	LockCause port.LockCause
	Tracked   bool // có trong pf_accounts (tạo hoặc thao tác qua nền tảng)
	// MFA: trạng thái TOTP (chỉ ở Get — đọc thêm Keycloak; D24).
	MFA *MFAStatus
	// LoginLocked is runtime Keycloak lockout, queried only for the authorized detail view.
	// Nil means not queried (list/create); it is distinct from administratively disabled.
	LoginLocked *bool
	// LoginLockedUntil: khóa tạm 15 phút (P0-VPS-OPS #131) dự kiến tự hết lúc này; nil khi không khóa
	// hoặc không suy được. Admin vẫn mở khóa sớm được.
	LoginLockedUntil *time.Time
}

// Get đọc một tài khoản trong phạm vi.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (UserView, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return UserView{}, err
	}
	t, err := s.target(ctx, a, id)
	if err != nil {
		return UserView{}, err
	}
	v := UserView{User: t.user, Roles: t.roles, Tracked: t.acc != nil}
	if t.acc != nil {
		v.LockCause = t.acc.LockCause
	} else if !t.user.Enabled {
		v.LockCause = port.LockManual
	}
	realm, err := s.Dir.RealmRoles(ctx, id)
	if err != nil {
		return UserView{}, dirErr(err)
	}
	m, err := s.mfaStatus(ctx, t.user, realm)
	if err != nil {
		return UserView{}, err
	}
	v.MFA = &m
	lock, err := s.Dir.LoginLockStatus(ctx, id)
	if err != nil {
		return UserView{}, dirErr(err)
	}
	v.LoginLocked, v.LoginLockedUntil = &lock.Locked, lock.Until
	return v, nil
}

// Events đọc nhật ký thao tác của một tài khoản (chỉ đọc, US-HTXSTATUS-03).
func (s *Service) Events(ctx context.Context, id uuid.UUID) ([]domain.Event, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.target(ctx, a, id); err != nil {
		return nil, err
	}
	var out []domain.Event
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.Events(ctx, tx, id)
		return e
	})
	return out, err
}

// ListInput lọc danh sách.
type ListInput struct {
	OrgRef uuid.UUID // Nil = mọi tổ chức trong phạm vi
	Offset int       // con trỏ: vị trí trong kết quả Keycloak (đã giải mã ở handler)
	Limit  int
}

// Page là một trang danh sách.
type Page struct {
	Items []UserView
	Next  *int
}

// Kích thước lô đọc Keycloak và số lô tối đa mỗi trang (người ngoài phạm vi bị lọc bỏ).
const (
	listBatch      = 100
	listMaxBatches = 20
)

// List liệt kê tài khoản trong phạm vi: đọc Keycloak theo tenant_id (và org_ref
// nếu lọc), giữ người có org_ref thuộc phạm vi của người gọi. Con trỏ là vị trí
// trong kết quả Keycloak (R-53).
func (s *Service) List(ctx context.Context, in ListInput) (Page, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return Page{}, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	attrs := map[string]string{port.AttrTenantID: a.p.TenantID.String()}
	if in.OrgRef != uuid.Nil {
		o, err := s.org(ctx, a, in.OrgRef)
		if err != nil {
			return Page{}, err
		}
		if o == nil || !a.allowsOrg(o) {
			return Page{}, ErrOutOfScope
		}
		attrs[port.AttrOrgRef] = in.OrgRef.String()
	}
	off := max(in.Offset, 0)
	var items []UserView
	done := false
scan:
	for batch := 0; batch < listMaxBatches && !done && len(items) < limit; batch++ {
		users, err := s.Dir.FindUsers(ctx, port.Query{Attributes: attrs, First: off, Max: listBatch})
		if err != nil {
			return Page{}, dirErr(err)
		}
		if len(users) < listBatch {
			done = true
		}
		for i, u := range users {
			if len(items) == limit {
				done = false
				off += i
				break scan
			}
			o, err := s.org(ctx, a, u.OrgRef)
			if err != nil {
				return Page{}, err
			}
			if u.TenantID == a.p.TenantID && a.allowsOrg(o) {
				items = append(items, UserView{User: u})
			}
		}
		off += len(users)
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		refs = append(refs, it.User.ID)
	}
	var accs map[uuid.UUID]domain.Account
	if err := tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		accs, e = store.Accounts(ctx, tx, refs)
		return e
	}); err != nil {
		return Page{}, err
	}
	for i := range items {
		if acc, ok := accs[items[i].User.ID]; ok {
			items[i].Roles, items[i].LockCause, items[i].Tracked = acc.Roles, acc.LockCause, true
		} else if !items[i].User.Enabled {
			items[i].LockCause = port.LockManual
		}
	}
	p := Page{Items: items}
	if !done {
		n := off
		p.Next = &n
	}
	return p, nil
}
