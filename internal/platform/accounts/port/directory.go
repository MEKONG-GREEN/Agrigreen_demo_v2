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

// Directory là cổng Keycloak Admin REST (realm agrigreen) bằng tài khoản dịch vụ
// agrigreen-accounts (manage-users, view-users, query-users). Bản HTTP ở
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
