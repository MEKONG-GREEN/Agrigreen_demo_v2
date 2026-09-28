// Package app: use case quản trị tài khoản cán bộ (KN US-ADM-01..03, HTX
// US-ACC-01/02, US-HTXSTATUS-01..03, CGH FN-01).
//
// Thứ tự một thao tác ghi: kiểm vai, phạm vi và ma trận cấp vai → gọi Keycloak →
// ghi sổ account_event + bảng account + outbox trong MỘT giao dịch tenant. Tạo
// tài khoản mà giao dịch thất bại thì xóa người dùng vừa tạo ở Keycloak (bù). Các
// thao tác khác lặp lại được: sổ chống trùng theo idem_key.
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrDependency     = problem.New(http.StatusServiceUnavailable, "accounts.dependency_unavailable", "Keycloak hoặc pf/party chưa sẵn sàng")
	ErrUserNotFound   = problem.New(http.StatusNotFound, "accounts.user_not_found", "Không tìm thấy tài khoản")
	ErrOutOfScope     = problem.New(http.StatusForbidden, "accounts.out_of_scope", "Đơn vị ngoài phạm vi phụ trách")
	ErrNotGrantable   = problem.New(http.StatusForbidden, "accounts.role_not_grantable", "Không được cấp hoặc thao tác trên vai này")
	ErrSelfAction     = problem.New(http.StatusForbidden, "accounts.self_action", "Không được tự khóa, mở khóa hoặc đổi vai của chính mình")
	ErrUserExists     = problem.New(http.StatusConflict, "accounts.user_exists", "Tên đăng nhập hoặc email đã được dùng")
	ErrPhoneTaken     = problem.New(http.StatusConflict, "accounts.phone_taken", "Số điện thoại này đã được đăng ký cho một tài khoản khác.")
	ErrState          = problem.New(http.StatusConflict, "accounts.state_conflict", "Trạng thái tài khoản không cho phép thao tác này")
	ErrOrgInactive    = problem.New(http.StatusConflict, "accounts.org_inactive", "Tổ chức đang bị vô hiệu hóa")
	ErrOrgInvalid     = problem.New(http.StatusUnprocessableEntity, "accounts.org_invalid", "Không có tổ chức với org_ref này")
	ErrEmailFailed    = problem.New(http.StatusBadGateway, "accounts.email_failed", "Keycloak không gửi được email kích hoạt")
	ErrImportInvalid  = problem.New(http.StatusUnprocessableEntity, "accounts.import_invalid", "Tệp nhập không đúng mẫu quy định")
	errNoDirectoryYet = errors.New("accounts: chưa cấu hình Keycloak Admin (AG_KC_*)")
)

// Producer ghi vào phong bì sự kiện.
const Producer = "pf/accounts"

// DefaultActionsLifespan: hạn liên kết kích hoạt (72 giờ).
const DefaultActionsLifespan = 72 * time.Hour

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai accounts_app
	Clock clock.Clock
	Dir   port.Directory          // nil → ErrDependency
	Scope partyport.ScopeResolver // nil → ErrDependency
	Orgs  partyport.Orgs          // nil → ErrDependency
	// ActionsLifespan: hạn liên kết kích hoạt; 0 → DefaultActionsLifespan.
	ActionsLifespan time.Duration
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) lifespan() time.Duration {
	if s.ActionsLifespan > 0 {
		return s.ActionsLifespan
	}
	return DefaultActionsLifespan
}

// dirErr quy lỗi Keycloak về problem.
func dirErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, port.ErrUserNotFound):
		return ErrUserNotFound
	case errors.Is(err, port.ErrUserExists):
		return ErrUserExists
	case errors.Is(err, port.ErrRoleNotFound):
		return ErrNotGrantable.WithDetail("vai không có trong realm Keycloak").WithCause(err)
	case errors.Is(err, port.ErrEmailFailed):
		return ErrEmailFailed.WithCause(err)
	case errors.Is(err, port.ErrUnavailable):
		return ErrDependency.WithCause(err)
	}
	return err
}

func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Msg)
	}
	return err
}

// actor là người gọi Admin API đã kiểm vai.
type actor struct {
	p         authz.Principal
	sc        authz.Scope
	grantable []string
	orgs      map[uuid.UUID]*partyport.Org // bộ nhớ đệm tra tổ chức trong một request
}

func (s *Service) caller(ctx context.Context) (*actor, error) {
	p, err := authz.Require(ctx, port.AdminRoles...)
	if err != nil {
		return nil, err
	}
	if s.Dir == nil {
		return nil, ErrDependency.WithCause(errNoDirectoryYet)
	}
	if s.Scope == nil || s.Orgs == nil {
		return nil, ErrDependency.WithDetail("chưa nối pf/party")
	}
	sc, err := s.Scope.Scope(ctx)
	if err != nil {
		return nil, err
	}
	return &actor{p: p, sc: sc, grantable: port.Grantable(p.Roles), orgs: map[uuid.UUID]*partyport.Org{}}, nil
}

// org tra tổ chức (có đệm); nil nếu không có.
func (s *Service) org(ctx context.Context, a *actor, ref uuid.UUID) (*partyport.Org, error) {
	if o, ok := a.orgs[ref]; ok {
		return o, nil
	}
	o, err := s.Orgs.Get(ctx, ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		a.orgs[ref] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.orgs[ref] = &o
	return &o, nil
}

func (a *actor) allowsOrg(o *partyport.Org) bool {
	if o == nil {
		return a.sc.All
	}
	if o.AdminUnitCode == "" {
		return a.sc.All
	}
	return a.sc.Allows(o.AdminUnitCode)
}

// checkOrg kiểm tổ chức đích khi gán: tồn tại, đang hoạt động, trong phạm vi.
func (s *Service) checkOrg(ctx context.Context, a *actor, ref uuid.UUID) (*partyport.Org, error) {
	if ref == uuid.Nil {
		return nil, problem.Validation.WithField("org_ref", "bắt buộc, phải là UUID")
	}
	o, err := s.org(ctx, a, ref)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, ErrOrgInvalid
	}
	if !a.allowsOrg(o) {
		return nil, ErrOutOfScope
	}
	if !o.Active {
		return nil, ErrOrgInactive.WithDetail("Không gán tài khoản vào tổ chức đã bị vô hiệu hóa.")
	}
	return o, nil
}

// checkGrant: mọi vai trong roles phải nằm trong tập người gọi được cấp.
func (a *actor) checkGrant(roles []string) error {
	for _, r := range roles {
		if !slices.Contains(a.grantable, r) {
			return ErrNotGrantable.WithDetail("Bạn không được cấp hoặc thao tác trên vai " + r + ".")
		}
	}
	return nil
}

// target là tài khoản đích đã kiểm tenant, phạm vi và quyền thao tác.
type target struct {
	user  port.User
	roles []string // vai do module quản lý, đang có ở Keycloak
	acc   *domain.Account
}

func (s *Service) target(ctx context.Context, a *actor, id uuid.UUID) (*target, error) {
	u, err := s.Dir.GetUser(ctx, id)
	if err != nil {
		return nil, dirErr(err)
	}
	// Tenant chỉ lấy từ token (R-16): người dùng của tenant khác coi như không có.
	if u.TenantID != a.p.TenantID {
		return nil, ErrUserNotFound
	}
	o, err := s.org(ctx, a, u.OrgRef)
	if err != nil {
		return nil, err
	}
	if !a.allowsOrg(o) {
		return nil, ErrOutOfScope.WithDetail("Tài khoản thuộc đơn vị ngoài phạm vi phụ trách.")
	}
	all, err := s.Dir.RealmRoles(ctx, id)
	if err != nil {
		return nil, dirErr(err)
	}
	if slices.Contains(all, authz.RolePlatformAdmin) && !a.p.Has(authz.RolePlatformAdmin) {
		return nil, ErrNotGrantable.WithDetail("Tài khoản quản trị nền tảng chỉ quản trị nền tảng thao tác.")
	}
	managed := domain.ManagedOf(all)
	if err := a.checkGrant(managed); err != nil {
		return nil, err
	}
	t := &target{user: u, roles: managed}
	err = tenant.WithTenantTx(ctx, s.DB, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		t.acc, e = store.Account(ctx, tx, id, false)
		return e
	})
	return t, err
}

// state là trạng thái hiện tại ở pf (hoặc dựng từ Keycloak nếu tài khoản tạo ngoài nền tảng).
func (t *target) state() domain.Account {
	if t.acc != nil {
		a := *t.acc
		a.Roles = t.roles
		a.OrgRef = t.user.OrgRef
		return a
	}
	a := domain.Account{UserRef: t.user.ID, OrgRef: t.user.OrgRef, Roles: t.roles, Enabled: t.user.Enabled}
	if !t.user.Enabled {
		a.LockCause = port.LockManual
	}
	return a
}

// Result là kết quả một thao tác ghi.
type Result struct {
	Account  domain.Account
	Event    domain.Event
	User     port.User
	Replayed bool
	// ActivationSent: đã gửi email liên kết kích hoạt (chỉ khi tạo tài khoản).
	ActivationSent bool
}

func newEvent(tid, actorRef uuid.UUID, acc domain.Account, action domain.Action, now time.Time, key, reason string) domain.Event {
	return domain.Event{
		ID: ids.New(), TenantID: tid, Account: acc, Action: action, Reason: reason,
		EventTime: now, ActorRef: actorRef, IdemKey: key,
	}
}

// record ghi sổ + bảng hiện tại + sự kiện (nếu có) trong tx.
func record(ctx context.Context, tx pgx.Tx, e *domain.Event) error {
	if err := store.InsertEvent(ctx, tx, e); err != nil {
		return err
	}
	if err := store.SaveAccount(ctx, tx, *e); err != nil {
		return err
	}
	return enqueue(ctx, tx, *e)
}

func enqueue(ctx context.Context, tx pgx.Tx, e domain.Event) error {
	ev := outbox.Event{
		ID: ids.New(), TenantID: e.TenantID, SchemaVersion: 1, OccurredAt: e.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: Producer,
	}
	at := e.EventTime.UTC().Format(time.RFC3339Nano)
	switch e.Action {
	case domain.ActionCreated:
		ev.Type = port.EventUserCreated
		ev.Payload = port.UserCreatedV1{UserRef: e.Account.UserRef, OrgRef: e.Account.OrgRef, Roles: e.Account.Roles, ActorRef: e.ActorRef, EventTime: at, Seq: e.Seq}
	case domain.ActionLocked:
		ev.Type = port.EventUserLocked
		ev.Payload = port.UserLockedV1{UserRef: e.Account.UserRef, OrgRef: e.Account.OrgRef, Cause: string(e.Account.LockCause), ActorRef: e.ActorRef, EventTime: at, Seq: e.Seq}
	case domain.ActionUnlocked:
		prev, _ := e.Details["prev_cause"].(string)
		ev.Type = port.EventUserUnlocked
		ev.Payload = port.UserUnlockedV1{UserRef: e.Account.UserRef, OrgRef: e.Account.OrgRef, PrevCause: prev, ActorRef: e.ActorRef, EventTime: at, Seq: e.Seq}
	default:
		return nil
	}
	return outbox.Enqueue(ctx, tx, ev)
}

// replay tìm bản ghi sổ của lần gọi trước cùng idem_key.
func (s *Service) replay(ctx context.Context, tid uuid.UUID, key string) (*domain.Event, error) {
	if key == "" {
		return nil, nil
	}
	var ev *domain.Event
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		ev, e = store.EventByIdemKey(ctx, tx, key)
		return e
	})
	return ev, err
}

// write ghi một dòng sổ; trùng idem_key (gọi song song) → đọc lại bản đã ghi.
func (s *Service) write(ctx context.Context, tid uuid.UUID, e *domain.Event) (replayed bool, err error) {
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return record(ctx, tx, e) })
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		prev, rerr := s.replay(ctx, tid, e.IdemKey)
		if rerr == nil && prev != nil {
			*e = *prev
			return true, nil
		}
	}
	return false, ledger.Classify(err)
}

func requireKey(key string) error {
	if key == "" {
		return problem.IdempotencyRequired
	}
	return nil
}
