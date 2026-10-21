package keycloak

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// Memory là Directory trong bộ nhớ, mô phỏng hành vi Keycloak mà module dựa vào:
// tên đăng nhập và email duy nhất trong realm (không phân biệt hoa thường), lọc
// thuộc tính so khớp chính xác, phân trang first/max theo thứ tự tạo, vai realm
// phải có trong realm. Chỉ dùng cho kiểm thử và chạy thử tại chỗ.
type Memory struct {
	mu      sync.Mutex
	realm   []string
	order   []uuid.UUID
	users   map[uuid.UUID]*memUser
	sent    []SentEmail
	links   []IssuedLink
	logout  map[uuid.UUID]int
	failFor map[string]error // tên thao tác → lỗi giả
	// sessions, events: phiên và sự kiện đăng nhập giả lập (RecordLogin, sessions.go).
	sessions map[uuid.UUID][]port.Session
	events   map[uuid.UUID][]port.LoginEvent
	now      func() time.Time
}

type memUser struct {
	u     port.User
	roles []string
	otp   bool
	// bruteForce: khóa do đăng nhập sai nhiều lần (realm permanentLockout) — enabled vẫn true.
	bruteForce bool
}

// SentEmail là một lần gửi liên kết hành động (không chứa địa chỉ email).
type SentEmail struct {
	UserID   uuid.UUID
	Actions  []string
	Lifespan time.Duration
}

// IssuedLink là một liên kết hành động đã phát (ActionLink). Chỉ kiểm thử đọc Link.
type IssuedLink struct {
	UserID   uuid.UUID
	GrantID  uuid.UUID
	Actions  []string
	Lifespan time.Duration
	Link     string
}

var (
	_ port.Directory   = (*Memory)(nil)
	_ port.ActionLinks = (*Memory)(nil)
)

// NewMemory tạo Directory trong bộ nhớ. realmRoles rỗng → mọi vai trong
// port.ManagedRoles cùng platform_admin, offline_access, cgh_sync.
func NewMemory(realmRoles ...string) *Memory {
	if len(realmRoles) == 0 {
		realmRoles = append(slices.Clone(port.ManagedRoles), "platform_admin", "offline_access", "cgh_sync", port.RoleFarmer)
	}
	return &Memory{
		realm: realmRoles, users: map[uuid.UUID]*memUser{}, logout: map[uuid.UUID]int{},
		failFor: map[string]error{}, now: time.Now,
	}
}

// Fail làm thao tác op (tên phương thức, vd "ExecuteActionsEmail") trả err; err nil → bỏ.
func (m *Memory) Fail(op string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err == nil {
		delete(m.failFor, op)
		return
	}
	m.failFor[op] = err
}

func (m *Memory) fail(op string) error { return m.failFor[op] }

// Sent trả các lần gửi liên kết hành động.
func (m *Memory) Sent() []SentEmail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.sent)
}

// Logouts trả số lần thu hồi phiên của người dùng.
func (m *Memory) Logouts(id uuid.UUID) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.logout[id]
}

// SetOTP đánh dấu người dùng đã cấu hình TOTP.
func (m *Memory) SetOTP(id uuid.UUID, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		u.otp = on
	}
}

// Seed thêm người dùng có sẵn (vd tạo ngoài nền tảng, trong console Keycloak).
func (m *Memory) Seed(nu port.NewUser, enabled bool, roles ...string) uuid.UUID {
	id, err := m.CreateUser(context.Background(), nu)
	if err != nil {
		panic(err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[id].u.Enabled = enabled
	m.users[id].roles = slices.Clone(roles)
	return id
}

// SetEmailVerified đặt cờ email đã xác minh (Keycloak đặt khi người dùng hoàn tất VERIFY_EMAIL).
func (m *Memory) SetEmailVerified(id uuid.UUID, v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		u.u.EmailVerified = v
	}
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// CreateUser thực hiện port.Directory.
func (m *Memory) CreateUser(_ context.Context, nu port.NewUser) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("CreateUser"); err != nil {
		return uuid.Nil, err
	}
	if nu.Username == "" {
		return uuid.Nil, errors.New("keycloak(memory): thiếu username")
	}
	for _, u := range m.users {
		if fold(u.u.Username) == fold(nu.Username) || (nu.Email != "" && fold(u.u.Email) == fold(nu.Email)) {
			return uuid.Nil, port.ErrUserExists
		}
	}
	id := uuid.New() // Keycloak sinh UUIDv4
	m.users[id] = &memUser{u: port.User{
		ID: id, Username: fold(nu.Username), Email: fold(nu.Email), FirstName: nu.FirstName, LastName: nu.LastName,
		Phone: nu.Phone, Enabled: true, TenantID: nu.TenantID, OrgRef: nu.OrgRef,
		RequiredActions: slices.Clone(nu.RequiredActions), CreatedAt: m.now().UTC(),
	}}
	m.order = append(m.order, id)
	return id, nil
}

func (m *Memory) get(id uuid.UUID) (*memUser, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, port.ErrUserNotFound
	}
	return u, nil
}

// GetUser thực hiện port.Directory.
func (m *Memory) GetUser(_ context.Context, id uuid.UUID) (port.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("GetUser"); err != nil {
		return port.User{}, err
	}
	u, err := m.get(id)
	if err != nil {
		return port.User{}, err
	}
	out := u.u
	out.RequiredActions = slices.Clone(u.u.RequiredActions)
	return out, nil
}

func attr(u port.User, k string) string {
	switch k {
	case port.AttrTenantID:
		if u.TenantID == uuid.Nil {
			return ""
		}
		return u.TenantID.String()
	case port.AttrOrgRef:
		if u.OrgRef == uuid.Nil {
			return ""
		}
		return u.OrgRef.String()
	case port.AttrPhone:
		return u.Phone
	}
	return ""
}

// FindUsers thực hiện port.Directory.
func (m *Memory) FindUsers(_ context.Context, q port.Query) ([]port.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("FindUsers"); err != nil {
		return nil, err
	}
	var match []port.User
	for _, id := range m.order {
		mu, ok := m.users[id]
		if !ok {
			continue
		}
		u := mu.u
		ok = true
		for k, v := range q.Attributes {
			if attr(u, k) != v {
				ok = false
			}
		}
		if q.Username != "" && fold(u.Username) != fold(q.Username) {
			ok = false
		}
		if q.Email != "" && fold(u.Email) != fold(q.Email) {
			ok = false
		}
		if ok {
			u.RequiredActions = slices.Clone(u.RequiredActions)
			match = append(match, u)
		}
	}
	maxN := q.Max
	if maxN <= 0 {
		maxN = 100
	}
	if q.First >= len(match) {
		return []port.User{}, nil
	}
	match = match[q.First:]
	if len(match) > maxN {
		match = match[:maxN]
	}
	return match, nil
}

// DeleteUser thực hiện port.Directory.
func (m *Memory) DeleteUser(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.get(id); err != nil {
		return err
	}
	delete(m.users, id)
	return nil
}

// SetEnabled thực hiện port.Directory.
func (m *Memory) SetEnabled(_ context.Context, id uuid.UUID, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("SetEnabled"); err != nil {
		return err
	}
	u, err := m.get(id)
	if err != nil {
		return err
	}
	u.u.Enabled = enabled
	return nil
}

// UpdateProfile thực hiện port.ProfileUpdater: tên đăng nhập/email vẫn duy nhất trong realm.
func (m *Memory) UpdateProfile(_ context.Context, id uuid.UUID, p port.ProfileUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("UpdateProfile"); err != nil {
		return err
	}
	u, err := m.get(id)
	if err != nil {
		return err
	}
	for oid, o := range m.users {
		if oid != id && (fold(o.u.Username) == fold(p.Username) || (p.Email != "" && fold(o.u.Email) == fold(p.Email))) {
			return port.ErrUserExists
		}
	}
	if p.Username != "" {
		u.u.Username = fold(p.Username)
	}
	if fold(p.Email) != fold(u.u.Email) {
		u.u.EmailVerified = false
	}
	u.u.Email, u.u.FirstName, u.u.LastName, u.u.Phone = fold(p.Email), p.FirstName, p.LastName, p.Phone
	return nil
}

var _ port.ProfileUpdater = (*Memory)(nil)

// SetOrgRef thực hiện port.Directory.
func (m *Memory) SetOrgRef(_ context.Context, id uuid.UUID, org uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.get(id)
	if err != nil {
		return err
	}
	u.u.OrgRef = org
	return nil
}

// AddRequiredActions thực hiện port.Directory.
func (m *Memory) AddRequiredActions(_ context.Context, id uuid.UUID, actions []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.get(id)
	if err != nil {
		return err
	}
	for _, a := range actions {
		if !slices.Contains(u.u.RequiredActions, a) {
			u.u.RequiredActions = append(u.u.RequiredActions, a)
		}
	}
	return nil
}

// RealmRoles thực hiện port.Directory.
func (m *Memory) RealmRoles(_ context.Context, id uuid.UUID) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.get(id)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(u.roles)
	slices.Sort(out)
	return out, nil
}

// AddRealmRoles thực hiện port.Directory.
func (m *Memory) AddRealmRoles(_ context.Context, id uuid.UUID, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("AddRealmRoles"); err != nil {
		return err
	}
	u, err := m.get(id)
	if err != nil {
		return err
	}
	for _, r := range roles {
		if !slices.Contains(m.realm, r) {
			return port.ErrRoleNotFound
		}
	}
	for _, r := range roles {
		if !slices.Contains(u.roles, r) {
			u.roles = append(u.roles, r)
		}
	}
	return nil
}

// RemoveRealmRoles thực hiện port.Directory.
func (m *Memory) RemoveRealmRoles(_ context.Context, id uuid.UUID, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.get(id)
	if err != nil {
		return err
	}
	u.roles = slices.DeleteFunc(u.roles, func(r string) bool { return slices.Contains(roles, r) })
	return nil
}

// HasOTP thực hiện port.Directory.
func (m *Memory) HasOTP(_ context.Context, id uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.get(id)
	if err != nil {
		return false, err
	}
	return u.otp, nil
}

// ExecuteActionsEmail thực hiện port.Directory.
func (m *Memory) ExecuteActionsEmail(_ context.Context, id uuid.UUID, actions []string, lifespan time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.get(id)
	if err != nil {
		return err
	}
	if err := m.fail("ExecuteActionsEmail"); err != nil {
		return err
	}
	if u.u.Email == "" || !u.u.Enabled {
		return port.ErrEmailFailed
	}
	m.sent = append(m.sent, SentEmail{UserID: id, Actions: slices.Clone(actions), Lifespan: lifespan})
	return nil
}

// ActionLink thực hiện port.ActionLinks: liên kết giả có token ngẫu nhiên (mỗi lần một token mới).
func (m *Memory) ActionLink(_ context.Context, id, grantID uuid.UUID, actions []string, lifespan time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("ActionLink"); err != nil {
		return "", err
	}
	u, err := m.get(id)
	if err != nil {
		return "", err
	}
	if !u.u.Enabled {
		return "", port.ErrUserNotFound
	}
	link := "https://kc.test/realms/agrigreen/login-actions/action-token?key=" + uuid.NewString() + uuid.NewString()
	m.links = append(m.links, IssuedLink{UserID: id, GrantID: grantID, Actions: slices.Clone(actions), Lifespan: lifespan, Link: link})
	return link, nil
}

// Links trả các liên kết hành động đã phát.
func (m *Memory) Links() []IssuedLink {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.links)
}

// CompleteActions mô phỏng người dùng hoàn tất liên kết: bỏ các required action đã làm.
func (m *Memory) CompleteActions(id uuid.UUID, actions ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		u.u.RequiredActions = slices.DeleteFunc(u.u.RequiredActions, func(a string) bool { return slices.Contains(actions, a) })
	}
}

// Logout thực hiện port.Directory.
func (m *Memory) Logout(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.get(id); err != nil {
		return err
	}
	m.logout[id]++
	delete(m.sessions, id)
	return nil
}

// LockByBruteForce giả lập Keycloak khóa vĩnh viễn tài khoản sau 5 lần sai (kiểm thử GIS US-LOG-01).
func (m *Memory) LockByBruteForce(id uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, err := m.get(id); err == nil {
		u.bruteForce = true
	}
}

// BruteForceLocked thực hiện port.Directory.
func (m *Memory) BruteForceLocked(_ context.Context, id uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("BruteForceLocked"); err != nil {
		return false, err
	}
	u, err := m.get(id)
	if err != nil {
		return false, err
	}
	return u.bruteForce, nil
}

// ClearBruteForce thực hiện port.Directory.
func (m *Memory) ClearBruteForce(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("ClearBruteForce"); err != nil {
		return err
	}
	u, err := m.get(id)
	if err != nil {
		return err
	}
	u.bruteForce = false
	return nil
}
