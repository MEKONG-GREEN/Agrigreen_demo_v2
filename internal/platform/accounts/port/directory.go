package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Thuộc tính người dùng trong Keycloak (khai trong user profile của realm).
const (
	AttrTenantID = "tenant_id"
	AttrOrgRef   = "org_ref"
	AttrPhone    = "phone_number"
)

// User là tài khoản đọc từ Keycloak. Các trường định danh chỉ đi qua bộ nhớ tới
// quản trị viên, không lưu ở pf, không vào log (R-14).
type User struct {
	ID              uuid.UUID
	Username        string `pii:"true"`
	Email           string `pii:"true"`
	FirstName       string `pii:"true"`
	LastName        string `pii:"true"`
	Phone           string `pii:"true"`
	Enabled         bool
	EmailVerified   bool
	TenantID        uuid.UUID
	OrgRef          uuid.UUID
	RequiredActions []string
	CreatedAt       time.Time
}

// NewUser là dữ liệu tạo tài khoản.
type NewUser struct {
	Username        string `pii:"true"`
	Email           string `pii:"true"`
	FirstName       string `pii:"true"`
	LastName        string `pii:"true"`
	Phone           string `pii:"true"` // rỗng = không đặt
	TenantID        uuid.UUID
	OrgRef          uuid.UUID
	RequiredActions []string
}

// Query lọc người dùng. Attributes so khớp chính xác (Keycloak q=key:value).
// Username/Email khác rỗng → tìm chính xác (exact=true).
type Query struct {
	Attributes map[string]string
	Username   string `pii:"true"`
	Email      string `pii:"true"`
	First, Max int
}

// Lỗi của Directory.
var (
	ErrUserNotFound = errors.New("accounts: không có người dùng trong Keycloak")
	ErrUserExists   = errors.New("accounts: tên đăng nhập hoặc email đã có trong Keycloak")
	ErrRoleNotFound = errors.New("accounts: vai không có trong realm")
	ErrEmailFailed  = errors.New("accounts: Keycloak không gửi được email hành động")
	ErrUnavailable  = errors.New("accounts: Keycloak không phản hồi")
)

// ProfileUpdate là thông tin tài khoản sau khi sửa (KN v7.7 FN-AD-04.04). Mọi trường ghi đè; Phone rỗng
// = bỏ SĐT. Username đổi theo SĐT/email khi tài khoản đăng nhập bằng chúng.
type ProfileUpdate struct {
	Username  string `pii:"true"`
	Email     string `pii:"true"`
	FirstName string `pii:"true"`
	LastName  string `pii:"true"`
	Phone     string `pii:"true"`
}

// ProfileUpdater (tùy chọn, kiểm bằng type assertion) sửa thông tin tài khoản trong Keycloak. Tách khỏi
// Directory để không buộc mọi bản cài phải có.
type ProfileUpdater interface {
	UpdateProfile(ctx context.Context, id uuid.UUID, p ProfileUpdate) error
}

// Directory là cổng Keycloak Admin REST (realm agrigreen) bằng tài khoản dịch vụ
// agrigreen-accounts (manage-users, view-users, query-users; view-events cho lịch sử đăng nhập — W11.02). Bản HTTP ở
// accounts.DirectoryFromEnv; bản trong bộ nhớ cho kiểm thử ở accounts.NewMemoryDirectory.
type Directory interface {
	CreateUser(ctx context.Context, u NewUser) (uuid.UUID, error)
	GetUser(ctx context.Context, id uuid.UUID) (User, error)
	FindUsers(ctx context.Context, q Query) ([]User, error)
	// DeleteUser CHỈ dùng để bù khi tạo tài khoản thất bại giữa chừng (chưa có ai dùng).
	DeleteUser(ctx context.Context, id uuid.UUID) error
	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
	SetOrgRef(ctx context.Context, id uuid.UUID, org uuid.UUID) error
	AddRequiredActions(ctx context.Context, id uuid.UUID, actions []string) error
	RealmRoles(ctx context.Context, id uuid.UUID) ([]string, error)
	AddRealmRoles(ctx context.Context, id uuid.UUID, roles []string) error
	RemoveRealmRoles(ctx context.Context, id uuid.UUID, roles []string) error
	HasOTP(ctx context.Context, id uuid.UUID) (bool, error)
	// ExecuteActionsEmail gửi email chứa liên kết một lần để người dùng tự thực hiện actions.
	ExecuteActionsEmail(ctx context.Context, id uuid.UUID, actions []string, lifespan time.Duration) error
	// Logout thu hồi mọi phiên đang hoạt động (KN US-ADM-01 AC-3).
	Logout(ctx context.Context, id uuid.UUID) error
	// BruteForceLocked cho biết tài khoản đang bị khóa do đăng nhập sai nhiều lần (chống dò
	// mật khẩu, realm permanentLockout — GIS US-LOG-01 AC-3). Keycloak ≥ 24 không tắt
	// "enabled" khi khóa vĩnh viễn nên phải hỏi riêng (attack-detection).
	BruteForceLocked(ctx context.Context, id uuid.UUID) (bool, error)
	// ClearBruteForce xóa trạng thái khóa do đăng nhập sai (Admin mở khóa).
	ClearBruteForce(ctx context.Context, id uuid.UUID) error
}

// Session là một phiên đăng nhập đang hoạt động (Keycloak GET /users/{id}/sessions).
type Session struct {
	ID         string
	IPAddress  string
	Start      time.Time
	LastAccess time.Time
	Clients    []string // clientId
}

// LoginEvent là một sự kiện đăng nhập (Keycloak GET /events?user=…; realm phải bật eventsEnabled).
type LoginEvent struct {
	Time      time.Time
	Type      string // LOGIN | LOGIN_ERROR | LOGOUT | CODE_TO_TOKEN_ERROR …
	IPAddress string
	Client    string
	Error     string // mã lỗi Keycloak (vd invalid_user_credentials), không chứa mật khẩu
}

// SessionReader là phần tùy chọn của Directory (FN-AD-04.04 tab "Phiên đang hoạt động", "Lịch sử đăng nhập").
// Tách khỏi Directory để không buộc mọi bản cài phải có; module kiểm bằng type assertion.
type SessionReader interface {
	UserSessions(ctx context.Context, id uuid.UUID) ([]Session, error)
	// LoginEvents trả tối đa max sự kiện mới nhất trước. Realm chưa bật lưu sự kiện → danh sách rỗng.
	LoginEvents(ctx context.Context, id uuid.UUID, max int) ([]LoginEvent, error)
}
